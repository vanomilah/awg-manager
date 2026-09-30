package serverwizard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/serveringress"
	"github.com/hoaxisr/awg-manager/internal/serverwizard/cdn"
	"github.com/hoaxisr/awg-manager/internal/serverwizard/egress"
	"github.com/hoaxisr/awg-manager/internal/tgwebproxy"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

type CoordinatorBridge interface {
	WithIngressLock(txID string, fn func() error) error
	ExecuteIngressTransaction(ctx context.Context, params serveringress.IngressTransactionParams) error
	IsRecoveryRequired() (bool, string)
}

type XrayServerComponent interface {
	GetConfig() xrayserver.Config
	GetStatus() xrayserver.Status
	GenerateLinks(clientID string) (*xrayserver.ShareLinks, error)
}

type TgWebProxyComponent interface {
	GetConfig() tgwebproxy.PublicConfig
	GetStatus() tgwebproxy.Status
	RevealSecret() (tgwebproxy.RevealData, error)
}

type DispatcherComponent interface {
	GetConfig() cdndispatcher.Config
	IsRunning() bool
}

type WizardCapabilitiesResponse struct {
	Kind         string               `json:"kind"`
	Scenarios    []string             `json:"scenarios,omitempty"`
	Modes        []string             `json:"modes,omitempty"`
	Profiles     []cdn.Profile        `json:"profiles"`
	EgressOpts   []egress.EgressOption `json:"egress_options"`
	Configured   bool                 `json:"configured"`
	Running      bool                 `json:"running"`
	RecoveryNeed bool                 `json:"recovery_required"`
}

type WizardService struct {
	coord       CoordinatorBridge
	xray        XrayServerComponent
	tg          TgWebProxyComponent
	disp        DispatcherComponent
	preflight   *PreflightEngine
	fingerprint *FingerprintEngine
	planStore   *PlanStore
	jobRunner   *JobRunner
	egress      *egress.Adapter
	dialTimeout func(network, address string, timeout time.Duration) (net.Conn, error)
	httpDoer    HTTPDoer
}

// HTTPDoer abstracts HTTP requests for readiness probing and testing.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func NewWizardService(
	coord CoordinatorBridge,
	xray XrayServerComponent,
	tg TgWebProxyComponent,
	disp DispatcherComponent,
	preflight *PreflightEngine,
	fingerprint *FingerprintEngine,
	planStore *PlanStore,
	jobRunner *JobRunner,
	egress *egress.Adapter,
) *WizardService {
	return &WizardService{
		coord:       coord,
		xray:        xray,
		tg:          tg,
		disp:        disp,
		preflight:   preflight,
		fingerprint: fingerprint,
		planStore:   planStore,
		jobRunner:   jobRunner,
		egress:      egress,
		dialTimeout: net.DialTimeout,
	}
}

// SetDialTimeout configures a custom dialer function for testing readiness probes.
func (s *WizardService) SetDialTimeout(fn func(network, address string, timeout time.Duration) (net.Conn, error)) {
	s.dialTimeout = fn
}

// SetHTTPDoer configures a custom HTTP client for testing readiness probes.
func (s *WizardService) SetHTTPDoer(doer HTTPDoer) {
	s.httpDoer = doer
}

// GetCapabilities returns dynamic capabilities, profiles, and egress routes for the given wizard.
func (s *WizardService) GetCapabilities(ctx context.Context, kind string) WizardCapabilitiesResponse {
	var egressOpts []egress.EgressOption
	if s.egress != nil {
		egressOpts = s.egress.ListOptions(ctx)
	}

	profiles := cdn.ListProfilesForServer(kind)

	var configured, running, recReq bool
	if s.coord != nil {
		recReq, _ = s.coord.IsRecoveryRequired()
	}

	var scenarios []string
	var modes []string

	switch kind {
	case "tgwebproxy":
		scenarios = []string{"dual", "direct_fake_tls", "cdn_http"}
		if s.tg != nil {
			st := s.tg.GetStatus()
			configured = st.Installed
			running = st.Running
		}
	case "xray":
		modes = []string{"xhttp_get", "ws"}
		if s.xray != nil {
			st := s.xray.GetStatus()
			configured = st.Configured
			running = st.Running
		}
	}

	return WizardCapabilitiesResponse{
		Kind:         kind,
		Scenarios:    scenarios,
		Modes:        modes,
		Profiles:     profiles,
		EgressOpts:   egressOpts,
		Configured:   configured,
		Running:      running,
		RecoveryNeed: recReq,
	}
}

// Preflight runs readiness checks and captures current state fingerprint.
func (s *WizardService) Preflight(ctx context.Context, req WizardPlanRequest) PreflightResponse {
	resp := s.preflight.Run(ctx, req)
	resp.Fingerprint = s.fingerprint.Compute(ctx)
	return resp
}

// Plan builds a dry-run change plan and saves it server-side with fingerprint and TTL.
func (s *WizardService) Plan(ctx context.Context, sessionID string, req WizardPlanRequest) (*ServerPlanRecord, error) {
	// 1. Build desired configuration first (normalizes, validates strict matrix, resolves egress)
	desired, err := BuildDesiredConfig(ctx, s.egress, req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRequest, err.Error())
	}

	// 2. Run preflight checks
	preflightResp := s.preflight.Run(ctx, req)
	if !preflightResp.CanProceed {
		return nil, fmt.Errorf("%w: preflight checks blocked progress", ErrInvalidRequest)
	}

	// 3. Compute current fingerprint with ComputeStrict
	fp, err := s.fingerprint.ComputeStrict(ctx)
	if err != nil {
		return nil, fmt.Errorf("state probe failed: %w", err)
	}

	// 4. Generate plan
	var plan ChangePlan
	switch req.Kind {
	case "tgwebproxy":
		plan = s.buildTelegramPlan(desired, fp)
	case "xray":
		plan = s.buildXrayPlan(desired, fp)
	default:
		return nil, fmt.Errorf("%w: unsupported wizard kind: %s", ErrInvalidRequest, req.Kind)
	}

	// 5. Save server-side (stores Desired with zero secrets!)
	rec := s.planStore.SavePlan(req, desired, sessionID, plan, fp, DefaultPlanTTL)
	return rec, nil
}

func (s *WizardService) buildTelegramPlan(desired DesiredWizardConfig, fp string) ChangePlan {
	directPort := desired.DirectPort
	if directPort <= 0 {
		directPort = 8443
	}
	listenPort := desired.ListenPort
	if listenPort <= 0 {
		listenPort = 8085
	}
	dispPort := desired.DispatcherPort
	if dispPort <= 0 {
		dispPort = 9009
	}
	upstream := desired.UpstreamDevice
	if upstream == "" {
		upstream = "direct"
	}

	var items []PlanItem

	if desired.Scenario == "direct_fake_tls" || desired.Scenario == "dual" {
		items = append(items, PlanItem{
			Action:      "enable",
			Target:      "telemt",
			Description: fmt.Sprintf("MTProto Fake-TLS direct прокси на порту %d", directPort),
			NewValue:    fmt.Sprintf(":%d", directPort),
		})
		if desired.DirectHost != "" {
			items = append(items, PlanItem{
				Action:      "modify",
				Target:      "direct_host",
				Description: fmt.Sprintf("Прямой хост подключения: %s", desired.DirectHost),
				NewValue:    desired.DirectHost,
			})
		}
	}

	if desired.Scenario == "cdn_http" || desired.Scenario == "dual" {
		items = append(items,
			PlanItem{
				Action:      "enable",
				Target:      "tproxy-server",
				Description: fmt.Sprintf("Web Proxy демон на локальном порту %d для CDN туннеля", listenPort),
				NewValue:    fmt.Sprintf("127.0.0.1:%d", listenPort),
			},
			PlanItem{
				Action:      "modify",
				Target:      "cdn-dispatcher",
				Description: "CDN Диспетчер: проксирование пути /?bridge= на порт Web Proxy",
				NewValue:    fmt.Sprintf("0.0.0.0:%d -> http://127.0.0.1:%d", dispPort, listenPort),
			},
		)
		if desired.PublicHostname != "" {
			items = append(items, PlanItem{
				Action:      "modify",
				Target:      "public_domain",
				Description: fmt.Sprintf("CDN Ingress домен: %s", desired.PublicHostname),
				NewValue:    desired.PublicHostname,
			})
		}
	}

	items = append(items, PlanItem{
		Action:      "modify",
		Target:      "upstream_egress",
		Description: fmt.Sprintf("Маршрутизация исходящего трафика Telegram через %s", upstream),
		NewValue:    upstream,
	})

	return ChangePlan{
		Summary:          "Настройка сервера Telegram Web Proxy (MTProto Fake-TLS + Web CDN)",
		Items:            items,
		RestartRequired:  true,
		StateFingerprint: fp,
	}
}

func (s *WizardService) buildXrayPlan(desired DesiredWizardConfig, fp string) ChangePlan {
	listenPort := desired.ListenPort
	if listenPort <= 0 {
		listenPort = 9008
	}
	dispPort := desired.DispatcherPort
	if dispPort <= 0 {
		dispPort = 9009
	}
	path := desired.Path
	if path == "" {
		path = "/cdn-bridge/"
	}
	transportDesc := "xhttp/packet-up"
	if desired.Transport == "ws" {
		transportDesc = "WebSocket"
	}
	remark := desired.ClientRemark
	if remark == "" {
		remark = "Client"
	}

	items := []PlanItem{
		{
			Action:      "enable",
			Target:      "xray-core",
			Description: fmt.Sprintf("Запуск ядра Xray VLESS (локальный порт %d, транспорт %s)", listenPort, transportDesc),
			NewValue:    fmt.Sprintf("127.0.0.1:%d", listenPort),
		},
		{
			Action:      "enable",
			Target:      "cdn-dispatcher",
			Description: fmt.Sprintf("CDN Диспетчер: маршрутизация пути %s на локальный Xray", path),
			NewValue:    fmt.Sprintf("0.0.0.0:%d -> 127.0.0.1:%d", dispPort, listenPort),
		},
		{
			Action:      "create",
			Target:      "client",
			Description: fmt.Sprintf("Создание клиента подключения '%s' с криптографическим UUID", remark),
			NewValue:    remark,
		},
	}

	outboundDesc := "Прямой выход в интернет (direct)"
	outboundVal := "direct"
	if desired.OutboundMode == "socks" {
		outboundDesc = "Маршрутизация исходящего трафика VLESS через политики Mihomo (:1099)"
		outboundVal = "127.0.0.1:1099"
	} else if desired.OutboundMode == "interface" && desired.OutboundInterface != "" {
		outboundDesc = fmt.Sprintf("Маршрутизация исходящего трафика VLESS через интерфейс %s", desired.OutboundInterface)
		outboundVal = desired.OutboundInterface
	}

	items = append(items, PlanItem{
		Action:      "modify",
		Target:      "outbound_routing",
		Description: outboundDesc,
		NewValue:    outboundVal,
	})

	if desired.PublicHostname != "" {
		items = append(items, PlanItem{
			Action:      "modify",
			Target:      "public_domain",
			Description: fmt.Sprintf("CDN Ingress домен: %s", desired.PublicHostname),
			NewValue:    desired.PublicHostname,
		})
	}

	return ChangePlan{
		Summary:          fmt.Sprintf("Настройка сервера Xray VLESS для клиента '%s'", remark),
		Items:            items,
		RestartRequired:  true,
		StateFingerprint: fp,
	}
}

// Apply atomically reserves plan and starts async execution.
func (s *WizardService) Apply(ctx context.Context, sessionID, planID string) (string, error) {
	jobID := s.jobRunner.GenerateJobID()

	// 1. Reserve plan atomically
	rec, err := s.planStore.Reserve(planID, sessionID, jobID)
	if err != nil {
		return "", err
	}

	// 2. Create async job with reserved jobID
	jobCtx, cancelFn := context.WithCancel(context.Background())
	job := s.jobRunner.CreateJobWithID(jobID, rec.Kind, sessionID, cancelFn)

	// 3. Start execution goroutine
	go s.executeApplyJob(jobCtx, job.ID, rec)

	return job.ID, nil
}

func (s *WizardService) executeApplyJob(ctx context.Context, jobID string, rec *ServerPlanRecord) {
	txID := fmt.Sprintf("wiz-apply-%d-%d", time.Now().UnixNano(), os.Getpid())

	// Phase 1: Preparing
	s.jobRunner.UpdateStep(jobID, JobPhasePreparing, 20, "Проверка состояния и подготовка транзакции...")
	if s.jobRunner.IsCancelRequested(jobID) {
		_ = s.planStore.Release(rec.PlanID, jobID)
		s.jobRunner.MarkCancelled(jobID)
		return
	}

	// Phase 2: Applying
	s.jobRunner.UpdateStep(jobID, JobPhaseApplying, 45, "Генерация секретов и запись конфигурационных файлов...")
	if s.jobRunner.IsCancelRequested(jobID) {
		_ = s.planStore.Release(rec.PlanID, jobID)
		s.jobRunner.MarkCancelled(jobID)
		return
	}

	txParams := serveringress.IngressTransactionParams{
		TxID:                txID,
		ExpectedFingerprint: rec.StateFingerprint,
		FingerprintFunc: func(checkCtx context.Context) (string, error) {
			return s.fingerprint.ComputeStrict(checkCtx)
		},
		ServerKind: rec.Kind,
		ReadinessProbe: func(probeCtx context.Context, topo serveringress.IngressTopology) error {
			s.jobRunner.UpdateStep(jobID, JobPhaseVerifying, 75, "Проверка готовности сетевых сокетов...")
			return s.probeTopologyReadiness(probeCtx, topo)
		},
		OnPointOfNoReturn: func(ptxID string) error {
			if _, err := s.planStore.MarkConsumed(rec.PlanID, jobID); err != nil {
				s.jobRunner.SetPhaseNonCancellable(jobID, JobPhaseRecoveryRequired, 100, "Координатор требует восстановления: plan_consume_failed_after_commit_boundary")
				return fmt.Errorf("plan_consume_failed_after_commit_boundary: %w", err)
			}
			s.jobRunner.SetPhaseNonCancellable(jobID, JobPhaseCommitting, 80, "Фиксация конфигурации (точка невозврата)...")
			return nil
		},
	}

	var creds *RevealCredentials
	var tgSecret string
	var clientUUID string

	switch rec.Kind {
	case "tgwebproxy":
		secBytes := make([]byte, 16)
		if _, err := rand.Read(secBytes); err != nil {
			_ = s.planStore.Release(rec.PlanID, jobID)
			s.jobRunner.FailJob(jobID, ErrCodeInvalidRequest, "генерация криптографического ключа не удалась")
			return
		}
		tgSecret = hex.EncodeToString(secBytes)

		// Pre-commit link generation via pure function BuildTgLinks
		tgCfg := tgwebproxy.Config{
			Enabled:        true,
			Scenario:       rec.Desired.Scenario,
			DirectHost:     rec.Desired.DirectHost,
			DirectPort:     rec.Desired.DirectPort,
			TlsDomain:      rec.Desired.TlsDomain,
			ListenPort:     rec.Desired.ListenPort,
			PublicHostname: rec.Desired.PublicHostname,
			CarrierMode:    rec.Desired.CarrierMode,
			UpstreamDevice: rec.Desired.UpstreamDevice,
			Secret:         tgSecret,
		}
		reveal, err := tgwebproxy.BuildTgLinks(tgCfg, tgSecret)
		if err != nil {
			_ = s.planStore.Release(rec.PlanID, jobID)
			s.jobRunner.FailJob(jobID, ErrCodeInvalidRequest, fmt.Sprintf("построение Telegram ссылок не удалось: %v", err))
			return
		}
		creds = &RevealCredentials{
			Kind:       "tgwebproxy",
			DirectLink: reveal.MtproxyURL,
			WebLink:    reveal.TgURL,
			TgSecret:   tgSecret,
			DirectHost: rec.Desired.DirectHost,
			DirectPort: rec.Desired.DirectPort,
			PublicHost: rec.Desired.PublicHostname,
		}

		txParams.Telegram = &serveringress.IngressTelegramCandidate{
			Enabled:        true,
			Scenario:       rec.Desired.Scenario,
			DirectHost:     rec.Desired.DirectHost,
			DirectPort:     rec.Desired.DirectPort,
			TlsDomain:      rec.Desired.TlsDomain,
			ListenPort:     rec.Desired.ListenPort,
			BackendPort:    2398,
			AdminPort:      8086,
			PublicHostname: rec.Desired.PublicHostname,
			CarrierMode:    rec.Desired.CarrierMode,
			UpstreamDevice: rec.Desired.UpstreamDevice,
			Secret:         tgSecret,
		}

	case "xray":
		uuidBytes := make([]byte, 16)
		if _, err := rand.Read(uuidBytes); err != nil {
			_ = s.planStore.Release(rec.PlanID, jobID)
			s.jobRunner.FailJob(jobID, ErrCodeInvalidRequest, "генерация UUID клиента не удалась")
			return
		}
		uuidBytes[6] = (uuidBytes[6] & 0x0f) | 0x40
		uuidBytes[8] = (uuidBytes[8] & 0x3f) | 0x80
		clientUUID = fmt.Sprintf("%x-%x-%x-%x-%x",
			uuidBytes[0:4], uuidBytes[4:6], uuidBytes[6:8], uuidBytes[8:10], uuidBytes[10:16])

		// Pre-commit link generation via pure function BuildShareLinks
		xrayPublicPort := rec.Desired.PublicPort
		if xrayPublicPort <= 0 {
			xrayPublicPort = 443
		}
		xrayPath := rec.Desired.Path
		if xrayPath == "" {
			xrayPath = "/cdn-bridge/"
		}

		xrayCfg := xrayserver.Config{
			Enabled:           true,
			ListenAddress:     "127.0.0.1",
			ListenPort:        rec.Desired.ListenPort,
			PublicDomain:      rec.Desired.PublicHostname,
			PublicPort:        xrayPublicPort,
			Path:              xrayPath,
			Transport:         rec.Desired.Transport,
			Mode:              rec.Desired.Mode,
			UplinkMethod:      rec.Desired.UplinkMethod,
			OutboundMode:      rec.Desired.OutboundMode,
			OutboundInterface: rec.Desired.OutboundInterface,
			OutboundSocksPort: rec.Desired.OutboundSocksPort,
		}
		client := xrayserver.Client{
			ID:      clientUUID,
			Remark:  rec.Desired.ClientRemark,
			Enabled: true,
		}
		shareLinks, err := xrayserver.BuildShareLinks(xrayCfg, client)
		if err != nil {
			_ = s.planStore.Release(rec.PlanID, jobID)
			s.jobRunner.FailJob(jobID, ErrCodeInvalidRequest, fmt.Sprintf("построение Xray ссылок не удалось: %v", err))
			return
		}
		creds = &RevealCredentials{
			Kind:        "xray",
			VlessURL:    shareLinks.VlessURL,
			HappJSON:    shareLinks.HappJSON,
			SingboxJSON: shareLinks.SingboxJSON,
			MihomoYAML:  shareLinks.MihomoYAML,
			UUID:        clientUUID,
			Remark:      rec.Desired.ClientRemark,
			PublicHost:  rec.Desired.PublicHostname,
		}

		txParams.Xray = &serveringress.IngressXrayCandidate{
			Enabled:           true,
			ListenAddress:     "127.0.0.1",
			ListenPort:        rec.Desired.ListenPort,
			PublicDomain:      rec.Desired.PublicHostname,
			PublicPort:        xrayPublicPort,
			Path:              xrayPath,
			Transport:         rec.Desired.Transport,
			Mode:              rec.Desired.Mode,
			UplinkMethod:      rec.Desired.UplinkMethod,
			OutboundMode:      rec.Desired.OutboundMode,
			OutboundInterface: rec.Desired.OutboundInterface,
			OutboundSocksPort: rec.Desired.OutboundSocksPort,
			ClientRemark:      rec.Desired.ClientRemark,
			ClientUUID:        clientUUID,
		}

	default:
		_ = s.planStore.Release(rec.PlanID, jobID)
		s.jobRunner.FailJob(jobID, ErrCodeInvalidRequest, fmt.Sprintf("неизвестный тип сервера: %s", rec.Kind))
		return
	}

	// Execute transaction through coordinator
	err := s.coord.ExecuteIngressTransaction(ctx, txParams)
	if err != nil {
		if errors.Is(err, serveringress.ErrRecoveryRequired) {
			diagMsg := "Координатор находится в состоянии требуемого восстановления"
			if strings.Contains(err.Error(), "plan_consume_failed_after_commit_boundary") {
				diagMsg = "Координатор требует восстановления: plan_consume_failed_after_commit_boundary"
			}
			s.jobRunner.MarkRecoveryRequired(jobID, diagMsg)
			return
		}
		if s.jobRunner.IsCancelRequested(jobID) {
			_ = s.planStore.Release(rec.PlanID, jobID)
			s.jobRunner.MarkCancelled(jobID)
			return
		}
		if errors.Is(err, serveringress.ErrPlanStale) {
			_ = s.planStore.Release(rec.PlanID, jobID)
			s.jobRunner.FailJob(jobID, ErrCodePlanStale, "План устарел: системное состояние изменилось во время подготовки")
			return
		}
		_ = s.planStore.Release(rec.PlanID, jobID)
		s.jobRunner.FailJob(jobID, ErrCodeInvalidRequest, err.Error())
		return
	}

	// Phase 4: Succeeded (plan was already marked consumed in OnPointOfNoReturn)
	s.jobRunner.UpdateStep(jobID, JobPhaseCommitting, 90, "Фиксация изменений конфигурации...")
	s.jobRunner.SucceedJob(jobID, creds)
}

func (s *WizardService) probeTopologyReadiness(ctx context.Context, topo serveringress.IngressTopology) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dialFn := s.dialTimeout
	if dialFn == nil {
		dialFn = net.DialTimeout
	}

	deadline := time.Now().Add(25 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	probeInterval := 50 * time.Millisecond

	// 1. Probe Xray listener if configured and enabled
	if topo.XrayEnabled && topo.XrayPort > 0 {
		target := fmt.Sprintf("127.0.0.1:%d", topo.XrayPort)
		ready := false
		var lastErr error
		for time.Now().Before(deadline) {
			if ctx.Err() != nil {
				break
			}
			conn, err := dialFn("tcp", target, 200*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				ready = true
				break
			}
			lastErr = err
			time.Sleep(probeInterval)
		}
		if !ready {
			return fmt.Errorf("xray listener %s not ready: %w", target, lastErr)
		}
	}

	// 2. Probe Telegram direct listener if configured
	if topo.TgScenario == "direct_fake_tls" || topo.TgScenario == "dual" {
		if topo.TgDirectPort > 0 {
			target := fmt.Sprintf("127.0.0.1:%d", topo.TgDirectPort)
			ready := false
			var lastErr error
			for time.Now().Before(deadline) {
				conn, err := dialFn("tcp", target, 200*time.Millisecond)
				if err == nil {
					_ = conn.Close()
					ready = true
					break
				}
				lastErr = err
				time.Sleep(probeInterval)
			}
			if !ready {
				return fmt.Errorf("telemt direct listener %s not ready: %w", target, lastErr)
			}
		}
	} else if topo.TgScenario == "cdn_http" && topo.TgDirectPort > 0 {
		if s.dialTimeout == nil {
			target := fmt.Sprintf("127.0.0.1:%d", topo.TgDirectPort)
			if conn, err := net.DialTimeout("tcp", target, 200*time.Millisecond); err == nil {
				_ = conn.Close()
				return fmt.Errorf("unexpected listener on telemt direct port %s during cdn_http scenario", target)
			}
		}
	}

	// 3. Probe Telegram web listener if configured
	if topo.TgScenario == "cdn_http" || topo.TgScenario == "dual" {
		if topo.TgWebPort > 0 {
			target := fmt.Sprintf("127.0.0.1:%d", topo.TgWebPort)
			ready := false
			var lastErr error
			for time.Now().Before(deadline) {
				conn, err := dialFn("tcp", target, 200*time.Millisecond)
				if err == nil {
					_ = conn.Close()
					ready = true
					break
				}
				lastErr = err
				time.Sleep(probeInterval)
			}
			if !ready {
				return fmt.Errorf("tproxy web listener %s not ready: %w", target, lastErr)
			}
		}
	} else if topo.TgScenario == "direct_fake_tls" && topo.TgWebPort > 0 {
		if s.dialTimeout == nil {
			target := fmt.Sprintf("127.0.0.1:%d", topo.TgWebPort)
			if conn, err := net.DialTimeout("tcp", target, 200*time.Millisecond); err == nil {
				_ = conn.Close()
				return fmt.Errorf("unexpected listener on web proxy port %s during direct_fake_tls scenario", target)
			}
		}
	}

	// 4. Probe CDN Dispatcher if configured
	dispPort := topo.DispatcherPort
	if dispPort <= 0 {
		dispPort = 9009
	}
	if topo.XrayPublicHostname != "" || topo.TgPublicHostname != "" || topo.PublicHostname != "" {
		target := fmt.Sprintf("127.0.0.1:%d", dispPort)
		ready := false
		var lastErr error
		for time.Now().Before(deadline) {
			conn, err := dialFn("tcp", target, 200*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				ready = true
				break
			}
			lastErr = err
			time.Sleep(probeInterval)
		}
		if !ready {
			return fmt.Errorf("cdn dispatcher %s not ready: %w", target, lastErr)
		}

		// Strict HTTP route identity check
		checkXray := (topo.XrayEnabled || topo.XrayPort > 0 && topo.XrayPublicHostname != "") && (topo.XrayPublicHostname != "" || (topo.PublicHostname != "" && topo.XrayEnabled))
		checkTg := topo.TgPublicHostname != "" || (topo.PublicHostname != "" && (topo.TgEnabled || topo.TgWebPort > 0))

		if checkXray || checkTg {
			xrayHost := topo.XrayPublicHostname
			if xrayHost == "" {
				xrayHost = topo.PublicHostname
			}
			xrayPath := cdndispatcher.NormalizePathPrefix(topo.XrayPathPrefix)

			tgHost := topo.TgPublicHostname
			if tgHost == "" {
				tgHost = topo.PublicHostname
			}

			client := s.httpDoer
			if client == nil {
				client = &http.Client{
					Transport: &http.Transport{
						DisableKeepAlives: true,
						DialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
							if dialFn != nil {
								return dialFn(network, addr, 200*time.Millisecond)
							}
							var d net.Dialer
							d.Timeout = 200 * time.Millisecond
							return d.DialContext(dialCtx, network, addr)
						},
					},
					Timeout: 500 * time.Millisecond,
				}
			}

			httpReady := false
			var lastHttpErr error
			for time.Now().Before(deadline) {
				if ctx.Err() != nil {
					break
				}

				attemptOk := true

				if checkXray {
					xrayUrl := fmt.Sprintf("http://127.0.0.1:%d%s", dispPort, xrayPath)
					hReq, err := http.NewRequestWithContext(ctx, "GET", xrayUrl, nil)
					if err != nil {
						return fmt.Errorf("create xray probe request: %w", err)
					}
					hReq.Host = xrayHost
					hResp, err := client.Do(hReq)
					if err != nil {
						lastHttpErr = fmt.Errorf("http get xray probe (%s): %w", xrayUrl, err)
						attemptOk = false
					} else {
						routeHeader := hResp.Header.Get("X-CDN-Route")
						_ = hResp.Body.Close()
						if routeHeader != "xray" {
							lastHttpErr = fmt.Errorf("unexpected or missing X-CDN-Route header for Xray: got %q, want %q", routeHeader, "xray")
							attemptOk = false
						}
					}
				}

				if attemptOk && checkTg {
					tgUrl := fmt.Sprintf("http://127.0.0.1:%d/", dispPort)
					hReq, err := http.NewRequestWithContext(ctx, "GET", tgUrl, nil)
					if err != nil {
						return fmt.Errorf("create tg probe request: %w", err)
					}
					hReq.Host = tgHost
					hResp, err := client.Do(hReq)
					if err != nil {
						lastHttpErr = fmt.Errorf("http get tg probe (%s): %w", tgUrl, err)
						attemptOk = false
					} else {
						routeHeader := hResp.Header.Get("X-CDN-Route")
						_ = hResp.Body.Close()
						if routeHeader != "tgwebproxy" {
							lastHttpErr = fmt.Errorf("unexpected or missing X-CDN-Route header for Telegram: got %q, want %q", routeHeader, "tgwebproxy")
							attemptOk = false
						}
					}
				}

				if attemptOk {
					httpReady = true
					break
				}

				time.Sleep(probeInterval)
			}

			if !httpReady {
				if lastHttpErr != nil {
					return fmt.Errorf("cdn dispatcher route identity probe failed: %w", lastHttpErr)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return errors.New("cdn dispatcher route identity probe failed: timeout waiting for ready route")
			}
		}
	}

	return nil
}

func (s *WizardService) GetJob(ctx context.Context, jobID, sessionID string) (JobStatusResponse, error) {
	return s.jobRunner.GetJob(jobID, sessionID)
}

// CancelJob attempts to cancel a job before committing phase.
func (s *WizardService) CancelJob(ctx context.Context, jobID, sessionID string) error {
	return s.jobRunner.CancelJob(jobID, sessionID)
}

// Reveal exposes transient credentials for a completed job.
func (s *WizardService) Reveal(ctx context.Context, jobID, sessionID string) (*RevealCredentials, error) {
	return s.jobRunner.RevealSecrets(jobID, sessionID)
}
