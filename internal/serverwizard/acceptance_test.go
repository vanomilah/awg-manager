package serverwizard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/routing"
	"github.com/hoaxisr/awg-manager/internal/serveringress"
	"github.com/hoaxisr/awg-manager/internal/serverwizard/cdn"
	"github.com/hoaxisr/awg-manager/internal/serverwizard/egress"
	"github.com/hoaxisr/awg-manager/internal/sys/procnet"
	"github.com/hoaxisr/awg-manager/internal/tgwebproxy"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

type acceptanceXray struct {
	cfg            xrayserver.Config
	st             xrayserver.Status
	prepareCalls   []string
	commitCalls    []string
	finalizeCalls  []string
	rollbackCalls  []string
	preparedCfg    *xrayserver.Config
	committedCfg   *xrayserver.Config
}

func (m *acceptanceXray) GetConfig() xrayserver.Config { return m.cfg }
func (m *acceptanceXray) GetStatus() xrayserver.Status { return m.st }
func (m *acceptanceXray) GenerateLinks(clientID string) (*xrayserver.ShareLinks, error) {
	return &xrayserver.ShareLinks{
		VlessURL: "vless://" + clientID + "@cdn.example.com:443",
		UUID:     clientID,
	}, nil
}
func (m *acceptanceXray) PrepareCandidate(txID string, candidate xrayserver.Config) (string, error) {
	m.prepareCalls = append(m.prepareCalls, txID)
	m.preparedCfg = &candidate
	return "cand-" + txID, nil
}
func (m *acceptanceXray) CommitPrepared(txID string) error {
	m.commitCalls = append(m.commitCalls, txID)
	if m.preparedCfg != nil {
		m.cfg = *m.preparedCfg
		m.committedCfg = m.preparedCfg
	}
	return nil
}
func (m *acceptanceXray) FinalizePrepared(txID string) error {
	m.finalizeCalls = append(m.finalizeCalls, txID)
	m.preparedCfg = nil
	return nil
}
func (m *acceptanceXray) RollbackPrepared(txID string) error {
	m.rollbackCalls = append(m.rollbackCalls, txID)
	m.preparedCfg = nil
	return nil
}
func (m *acceptanceXray) Restart() error { return nil }

type acceptanceDispatcher struct {
	cfg       cdndispatcher.Config
	candidate cdndispatcher.Candidate
	running   bool
}

func (m *acceptanceDispatcher) GetConfig() cdndispatcher.Config { return m.cfg }
func (m *acceptanceDispatcher) IsRunning() bool                 { return m.running }
func (m *acceptanceDispatcher) Start() error {
	m.running = true
	return nil
}
func (m *acceptanceDispatcher) Stop() error {
	m.running = false
	return nil
}
func (m *acceptanceDispatcher) ApplyConfig(cfg cdndispatcher.Config) error {
	m.cfg = cfg
	return nil
}
func (m *acceptanceDispatcher) Reconfigure(cfg cdndispatcher.Config) error {
	m.cfg = cfg
	return nil
}
func (m *acceptanceDispatcher) PrepareCandidate(txID string, candidate cdndispatcher.Candidate) (string, error) {
	m.candidate = candidate
	return txID, nil
}
func (m *acceptanceDispatcher) CommitPrepared(txID string) error {
	m.cfg = m.candidate.Config
	m.running = m.candidate.Enabled
	return nil
}
func (m *acceptanceDispatcher) RollbackPrepared(txID string) error {
	return nil
}
func (m *acceptanceDispatcher) FinalizePrepared(txID string) error {
	return nil
}

type acceptanceTg struct {
	cfg       tgwebproxy.Config
	candidate tgwebproxy.Config
	st        tgwebproxy.Status
	updateCfg *tgwebproxy.Config
}

func (m *acceptanceTg) GetConfig() tgwebproxy.PublicConfig {
	return tgwebproxy.PublicConfig{
		Enabled:        m.cfg.Enabled,
		ListenPort:     m.cfg.ListenPort,
		PublicHostname: m.cfg.PublicHostname,
		DirectPort:     m.cfg.DirectPort,
		DirectHost:     m.cfg.DirectHost,
	}
}
func (m *acceptanceTg) GetInternalConfig() tgwebproxy.Config { return m.cfg }
func (m *acceptanceTg) GetStatus() tgwebproxy.Status         { return m.st }
func (m *acceptanceTg) ApplyManagedIngress(cfg tgwebproxy.ManagedIngressConfig) error {
	return nil
}
func (m *acceptanceTg) UpdateConfig(cfg tgwebproxy.Config) error {
	m.cfg = cfg
	m.updateCfg = &cfg
	return nil
}
func (m *acceptanceTg) PrepareCandidate(txID string, candidate tgwebproxy.Config) (string, error) {
	m.candidate = candidate
	return txID, nil
}
func (m *acceptanceTg) CommitPrepared(txID string) error {
	m.cfg = m.candidate
	m.updateCfg = &m.candidate
	return nil
}
func (m *acceptanceTg) RollbackPrepared(txID string) error {
	return nil
}
func (m *acceptanceTg) FinalizePrepared(txID string) error {
	return nil
}
func (m *acceptanceTg) RevealSecret() (tgwebproxy.RevealData, error) {
	return tgwebproxy.RevealData{
		Secret:     "11223344556677889900aabbccddeeff",
		MtproxyURL: "tg://proxy?server=test.com&port=8443",
		TgURL:      "tg://webproxy?server=cdn.com&secret=123",
	}, nil
}

func setupAcceptanceCoordinatorAndWizard(t *testing.T) (
	*WizardService,
	*serveringress.Coordinator,
	*acceptanceXray,
	*acceptanceTg,
	*acceptanceDispatcher,
	*FingerprintEngine,
) {
	dataDir := t.TempDir()
	procDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(procDir, "net"), 0755)
	_ = os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(""), 0644)

	xray := &acceptanceXray{
		cfg: xrayserver.Config{Enabled: false, ListenPort: 9008, OutboundMode: "socks"},
		st:  xrayserver.Status{Installed: true, Configured: false},
	}
	tg := &acceptanceTg{
		cfg: tgwebproxy.Config{Enabled: false, ListenPort: 8085, DirectPort: 8443},
		st:  tgwebproxy.Status{Installed: true, Running: false},
	}
	disp := &acceptanceDispatcher{
		cfg: cdndispatcher.Config{ListenAddr: ":9009"},
	}

	coord := serveringress.New(dataDir, xray, disp, tg)
	coord.SetProcDir(procDir)
	coord.SetLegacyInitPaths(
		filepath.Join(dataDir, "init.active"),
		filepath.Join(dataDir, "init.disabled"),
	)

	egressAdp := egress.NewAdapter(&mockCatalog{})
	recReader := &mockRecReader{required: false}

	preflight := NewPreflightEngine(xray, disp, tg, recReader, egressAdp)
	preflight.statFn = func(path string) (os.FileInfo, error) {
		return mockFileInfo{name: path, size: 1000}, nil
	}
	preflight.findProcFn = func(procDir, addr string, port int) (procnet.ListenerLookup, error) {
		return procnet.ListenerLookup{SocketFound: false}, nil
	}

	fingerprint := NewFingerprintEngine(xray, disp, tg, recReader, egressAdp)
	fingerprint.SetProcDir(procDir)
	fingerprint.SetInitScripts([]string{})

	planStore := NewPlanStore()
	jobRunner := NewJobRunner()

	svc := NewWizardService(
		coord,
		xray,
		tg,
		disp,
		preflight,
		fingerprint,
		planStore,
		jobRunner,
		egressAdp,
	)
	svc.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		return &mockTestConn{}, nil
	})
	svc.SetHTTPDoer(&mockHTTPDoer{})

	return svc, coord, xray, tg, disp, fingerprint
}

func TestWizardApplyRealCoordinatorDoesNotDeadlock(t *testing.T) {
	svc, coord, xray, _, disp, _ := setupAcceptanceCoordinatorAndWizard(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	planRec, err := svc.Plan(ctx, "session-deadlock", WizardPlanRequest{
		Kind:         "xray",
		DeviceType:   "phone",
		PublicDomain: "cdn.example.com",
		Path:         "/cdn-bridge/",
		Mode:         "xhttp_get",
		ClientRemark: "iPhone Test",
	})
	if err != nil {
		t.Fatalf("Plan creation failed: %v", err)
	}

	jobID, err := svc.Apply(ctx, "session-deadlock", planRec.PlanID)
	if err != nil {
		t.Fatalf("Apply initiation failed: %v", err)
	}

	// Poll job status until completion or timeout
	completed := false
	var finalJob JobStatusResponse
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("DEADLOCK DETECTED: job execution did not finish within timeout")
		case <-time.After(50 * time.Millisecond):
			job, err := svc.GetJob(ctx, jobID, "session-deadlock")
			if err != nil {
				t.Fatalf("failed to get job: %v", err)
			}
			if job.Phase == JobPhaseSucceeded || job.Phase == JobPhaseFailed {
				finalJob = job
				completed = true
				break
			}
		}
		if completed {
			break
		}
	}

	if finalJob.Phase != JobPhaseSucceeded {
		t.Fatalf("expected job Succeeded without deadlock, got %s: %s", finalJob.Phase, finalJob.Error)
	}

	// Verify coordinator executed the candidate commits
	if len(xray.commitCalls) != 1 {
		t.Errorf("expected 1 commitCall on xray, got %d", len(xray.commitCalls))
	}
	if len(xray.finalizeCalls) != 1 {
		t.Errorf("expected 1 finalizeCall on xray, got %d", len(xray.finalizeCalls))
	}
	if !disp.running {
		t.Errorf("expected dispatcher to be running after successful apply")
	}

	// Verify no stale lock remains
	locked := false
	err = coord.WithIngressLock("check-lock", func() error {
		locked = true
		return nil
	})
	if err != nil || !locked {
		t.Errorf("failed to acquire lock after apply finished: %v", err)
	}
}

func TestWizardFingerprintRevalidatedUnderMutationLock(t *testing.T) {
	svc, _, xray, _, _, _ := setupAcceptanceCoordinatorAndWizard(t)
	ctx := context.Background()

	// 1. Create a plan based on initial state
	planRec, err := svc.Plan(ctx, "session-stale", WizardPlanRequest{
		Kind:         "xray",
		DeviceType:   "phone",
		PublicDomain: "cdn.example.com",
		Path:         "/cdn-bridge/",
		Mode:         "xhttp_get",
		ClientRemark: "iPhone Test",
	})
	if err != nil {
		t.Fatalf("Plan creation failed: %v", err)
	}

	// 2. Mutate system state externally before apply
	xray.cfg.ListenPort = 9999

	// 3. Apply the plan - coordinator must detect fingerprint mismatch under mutation lock
	jobID, err := svc.Apply(ctx, "session-stale", planRec.PlanID)
	if err != nil {
		// Stale error may be returned synchronously or asynchronously via job runner
		if !errors.Is(err, ErrPlanStale) && !strings.Contains(err.Error(), "stale") {
			t.Fatalf("unexpected synchronous error: %v", err)
		}
		return
	}

	// Poll job runner to ensure asynchronous failure
	var finalJob JobStatusResponse
	completed := false
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		job, err := svc.GetJob(ctx, jobID, "session-stale")
		if err == nil && (job.Phase == JobPhaseSucceeded || job.Phase == JobPhaseFailed) {
			finalJob = job
			completed = true
			break
		}
	}

	if !completed {
		t.Fatalf("job did not complete")
	}

	if finalJob.Phase != JobPhaseFailed {
		t.Fatalf("expected job to fail due to stale fingerprint, got %s", finalJob.Phase)
	}

	if !strings.Contains(strings.ToLower(finalJob.Error), "stale") &&
		!strings.Contains(strings.ToLower(finalJob.Error), "устарел") &&
		!strings.Contains(strings.ToLower(finalJob.Error), "fingerprint") {
		t.Errorf("expected error message to indicate stale plan or fingerprint mismatch, got: %s", finalJob.Error)
	}

	// Ensure no candidate commit occurred
	if len(xray.commitCalls) > 0 {
		t.Errorf("expected 0 commit calls on stale plan, got %d", len(xray.commitCalls))
	}
}

func TestNoPersonalDomainsInDefaults(t *testing.T) {
	// 1. Check tgwebproxy default TLS domain
	if tgwebproxy.DefaultTlsDomain == "ya.ru" || strings.Contains(tgwebproxy.DefaultTlsDomain, "vinvanvlad") {
		t.Errorf("tgwebproxy.DefaultTlsDomain contains personal or ya.ru domain: %s", tgwebproxy.DefaultTlsDomain)
	}

	// 2. Check cdn default profiles
	profiles := cdn.ListProfiles()
	for _, p := range profiles {
		if strings.Contains(p.Description, "vinvanvlad") || strings.Contains(p.Instructions, "vinvanvlad") {
			t.Errorf("CDN profile %s contains vinvanvlad in description/instructions", p.ID)
		}
	}

	// 3. Scan production Go source code (excluding tests and sys/traffic domain classifier)
	forbiddenWords := []string{"vinvanvlad.crazedns.ru", "cdn.vinvanvladnet.ru"}
	rootDirs := []string{".", "../serveringress", "../tgwebproxy", "../xrayserver"}

	for _, dir := range rootDirs {
		_ = filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil // test fixtures are excluded
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			contentStr := string(content)
			for _, word := range forbiddenWords {
				if strings.Contains(contentStr, word) {
					t.Errorf("production file %s contains forbidden personal domain %q", path, word)
				}
			}
			return nil
		})
	}
}

type mockCatalogEntries struct {
	entries []routing.TunnelEntry
}

func (m *mockCatalogEntries) ListAll(ctx context.Context) []routing.TunnelEntry {
	return m.entries
}

func TestSharedIngressTopologyUnion(t *testing.T) {
	svc, _, xray, tg, disp, _ := setupAcceptanceCoordinatorAndWizard(t)
	ctx := context.Background()

	// 1. First setup Xray server
	xrayReq := WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "xray.example.org",
		Path:         "/xray-bridge/",
		ListenPort:   9008,
		CdnProfileID: "cdn_get",
		ClientRemark: "Client-Xray",
	}
	planXray, err := svc.Plan(ctx, "session-union", xrayReq)
	if err != nil {
		t.Fatalf("Plan xray failed: %v", err)
	}
	jobIDXray, err := svc.Apply(ctx, "session-union", planXray.PlanID)
	if err != nil {
		t.Fatalf("Apply xray failed: %v", err)
	}

	// Wait for Xray job
	for i := 0; i < 40; i++ {
		time.Sleep(25 * time.Millisecond)
		job, _ := svc.GetJob(ctx, jobIDXray, "session-union")
		if job.Phase == JobPhaseSucceeded {
			break
		}
	}
	if xray.cfg.PublicDomain != "xray.example.org" || xray.cfg.ListenPort != 9008 {
		t.Fatalf("xray config not applied properly: %+v", xray.cfg)
	}

	// 2. Now setup Telegram in dual mode
	tgReq := WizardPlanRequest{
		Kind:         "tgwebproxy",
		Scenario:     "dual",
		DirectHost:   "tg-direct.example.org",
		DirectPort:   8443,
		ListenPort:   8085,
		PublicDomain: "tg-cdn.example.org",
		TlsDomain:    "gateway.icloud.com",
		CdnProfileID: "cdn_get",
	}
	planTG, err := svc.Plan(ctx, "session-union", tgReq)
	if err != nil {
		t.Fatalf("Plan tg failed: %v", err)
	}
	jobIDTG, err := svc.Apply(ctx, "session-union", planTG.PlanID)
	if err != nil {
		t.Fatalf("Apply tg failed: %v", err)
	}

	// Wait for TG job
	for i := 0; i < 40; i++ {
		time.Sleep(25 * time.Millisecond)
		job, _ := svc.GetJob(ctx, jobIDTG, "session-union")
		if job.Phase == JobPhaseSucceeded {
			break
		}
	}

	// 3. Verify union: Xray settings must NOT be overwritten!
	if xray.cfg.PublicDomain != "xray.example.org" || xray.cfg.ListenPort != 9008 {
		t.Errorf("Telegram setup corrupted Xray config! Got %+v", xray.cfg)
	}
	// Telegram settings must be applied
	if tg.cfg.PublicHostname != "tg-cdn.example.org" || tg.cfg.DirectPort != 8443 || tg.cfg.ListenPort != 8085 {
		t.Errorf("Telegram config not applied properly: %+v", tg.cfg)
	}
	// Dispatcher config must maintain routes for both
	if disp.cfg.XrayPublicHost != "xray.example.org" && disp.cfg.PublicHostname != "xray.example.org" {
		t.Errorf("expected disp Xray hostname xray.example.org, got %s", disp.cfg.XrayPublicHost)
	}
	if disp.cfg.TgPublicHost != "tg-cdn.example.org" && disp.cfg.PublicHostname != "tg-cdn.example.org" {
		t.Errorf("expected disp TG hostname tg-cdn.example.org, got %s", disp.cfg.TgPublicHost)
	}
}

func TestStrictCDNProfileModeMatrix(t *testing.T) {
	cat := &mockCatalog{}
	adp := egress.NewAdapter(cat)
	ctx := context.Background()

	// 1. Valid: cdn_get + xhttp_get
	cfg, err := BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_get",
		Mode:         "xhttp_get",
	})
	if err != nil || cfg.Transport != "xhttp" || cfg.UplinkMethod != "GET" {
		t.Fatalf("expected cdn_get + xhttp_get to succeed, got %+v, %v", cfg, err)
	}

	// 2. Valid: cdn_get + "" (default)
	cfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_get",
	})
	if err != nil || cfg.Transport != "xhttp" || cfg.UplinkMethod != "GET" {
		t.Fatalf("expected cdn_get default to succeed, got %+v, %v", cfg, err)
	}

	// 3. Valid: cdn_ws + ws
	cfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_ws",
		Mode:         "ws",
	})
	if err != nil || cfg.Transport != "ws" {
		t.Fatalf("expected cdn_ws + ws to succeed, got %+v, %v", cfg, err)
	}

	// 4. Valid: cdn_full + xhttp_get
	cfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_full",
		Mode:         "xhttp_get",
	})
	if err != nil || cfg.Transport != "xhttp" {
		t.Fatalf("expected cdn_full + xhttp_get to succeed, got %+v, %v", cfg, err)
	}

	// 5. Valid: cdn_full + ws
	cfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_full",
		Mode:         "ws",
	})
	if err != nil || cfg.Transport != "ws" {
		t.Fatalf("expected cdn_full + ws to succeed, got %+v, %v", cfg, err)
	}

	// 6. Invalid: cdn_get + ws -> REJECT
	_, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_get",
		Mode:         "ws",
	})
	if err == nil {
		t.Fatalf("expected cdn_get + ws to be rejected")
	}

	// 7. Invalid: cdn_ws + xhttp_get -> REJECT
	_, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_ws",
		Mode:         "xhttp_get",
	})
	if err == nil {
		t.Fatalf("expected cdn_ws + xhttp_get to be rejected")
	}

	// 8. Invalid: unknown profile -> REJECT
	_, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.org",
		CdnProfileID: "cdn_post",
	})
	if err == nil {
		t.Fatalf("expected unknown profile cdn_post to be rejected")
	}
}

func TestEgressResolutionAndApplication(t *testing.T) {
	cat := &mockCatalogEntries{
		entries: []routing.TunnelEntry{
			{ID: "Wireguard0", Iface: "nwg1", Available: true, Status: "running"},
			{ID: "Degraded0", Iface: "nwg2", Available: false, Status: "down"},
		},
	}
	adp := egress.NewAdapter(cat)
	adp.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		return &mockTestConn{}, nil
	})
	ctx := context.Background()

	// Telegram egress tests
	// 1. Telegram direct -> empty UpstreamDevice and OutboundInterface (NO nwg1!)
	cfg, err := BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:           "tgwebproxy",
		Scenario:       "dual",
		DirectHost:     "tg.example.org",
		PublicDomain:   "cdn.example.org",
		TlsDomain:      "gateway.icloud.com",
		UpstreamDevice: "direct",
	})
	if err != nil {
		t.Fatalf("BuildDesiredConfig failed for telegram direct: %v", err)
	}
	if cfg.UpstreamDevice != "" || cfg.OutboundInterface != "" || cfg.OutboundMode != "direct" {
		t.Fatalf("expected empty UpstreamDevice and direct mode, got: %+v", cfg)
	}

	// 2. Telegram interface -> Wireguard0 -> nwg1
	cfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:           "tgwebproxy",
		Scenario:       "dual",
		DirectHost:     "tg.example.org",
		PublicDomain:   "cdn.example.org",
		TlsDomain:      "gateway.icloud.com",
		UpstreamDevice: "Wireguard0",
	})
	if err != nil || cfg.UpstreamDevice != "nwg1" || cfg.OutboundInterface != "nwg1" {
		t.Fatalf("expected nwg1 for Wireguard0, got: %+v, %v", cfg, err)
	}

	// 3. Telegram socks -> REJECT
	_, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:           "tgwebproxy",
		Scenario:       "dual",
		DirectHost:     "tg.example.org",
		PublicDomain:   "cdn.example.org",
		TlsDomain:      "gateway.icloud.com",
		UpstreamDevice: "mihomo:1099",
	})
	if err == nil {
		t.Fatalf("expected telegram with SOCKS egress to fail")
	}

	// 4. Telegram degraded tunnel -> REJECT
	_, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:           "tgwebproxy",
		Scenario:       "dual",
		DirectHost:     "tg.example.org",
		PublicDomain:   "cdn.example.org",
		TlsDomain:      "gateway.icloud.com",
		UpstreamDevice: "Degraded0",
	})
	if err == nil {
		t.Fatalf("expected telegram with degraded tunnel to fail")
	}

	// Xray egress tests
	// 5. Xray direct -> OutboundMode: "direct", OutboundInterface: ""
	xCfg, err := BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:           "xray",
		PublicDomain:   "cdn.example.org",
		UpstreamDevice: "direct",
	})
	if err != nil || xCfg.OutboundMode != "direct" || xCfg.OutboundInterface != "" {
		t.Fatalf("expected xray direct outbound, got: %+v, %v", xCfg, err)
	}

	// 6. Xray socks -> OutboundMode: "socks", OutboundSocksPort: 1099
	xCfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:           "xray",
		PublicDomain:   "cdn.example.org",
		UpstreamDevice: "mihomo:1099",
	})
	if err != nil || xCfg.OutboundMode != "socks" || xCfg.OutboundSocksPort != 1099 {
		t.Fatalf("expected xray socks outbound, got: %+v, %v", xCfg, err)
	}

	// 7. Xray interface -> OutboundMode: "interface", OutboundInterface: "nwg1"
	xCfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:           "xray",
		PublicDomain:   "cdn.example.org",
		UpstreamDevice: "Wireguard0",
	})
	if err != nil || xCfg.OutboundMode != "interface" || xCfg.OutboundInterface != "nwg1" {
		t.Fatalf("expected xray interface outbound, got: %+v, %v", xCfg, err)
	}
}

func TestCancellationBoundarySynchronized(t *testing.T) {
	runner := NewJobRunner()
	planStore := NewPlanStore()

	// 1. Job in Preparing can be cancelled
	_, cancel1 := context.WithCancel(context.Background())
	job1 := runner.CreateJobWithID("job-cancel-1", "xray", "session-1", cancel1)
	planRec := planStore.SavePlan(WizardPlanRequest{Kind: "xray"}, DesiredWizardConfig{ServerKind: "xray"}, "session-1", ChangePlan{}, "fp1", DefaultPlanTTL)
	_, err := planStore.Reserve(planRec.PlanID, "session-1", job1.ID)
	if err != nil {
		t.Fatalf("failed to reserve plan: %v", err)
	}

	runner.UpdateStep(job1.ID, JobPhasePreparing, 20, "Preparing...")
	err = runner.CancelJob(job1.ID, "session-1")
	if err != nil {
		t.Fatalf("expected cancel to succeed in preparing phase: %v", err)
	}
	if !runner.IsCancelRequested(job1.ID) {
		t.Errorf("expected cancelRequested=true")
	}
	runner.MarkCancelled(job1.ID)
	_ = planStore.Release(planRec.PlanID, job1.ID)

	// Verify plan was released and can be re-reserved
	_, err = planStore.Reserve(planRec.PlanID, "session-1", "job-new")
	if err != nil {
		t.Fatalf("expected released plan to be re-reservable: %v", err)
	}

	// 2. Job in Committing (point of no return) CANNOT be cancelled
	_, cancel2 := context.WithCancel(context.Background())
	job2 := runner.CreateJobWithID("job-cancel-2", "xray", "session-1", cancel2)
	runner.SetPhaseNonCancellable(job2.ID, JobPhaseCommitting, 80, "Committing...")

	err = runner.CancelJob(job2.ID, "session-1")
	if !errors.Is(err, ErrCannotCancel) {
		t.Fatalf("expected ErrCannotCancel in Committing phase, got %v", err)
	}
	status, _ := runner.GetJob(job2.ID, "session-1")
	if status.Phase != JobPhaseCommitting {
		t.Errorf("job phase should remain Committing, got %s", status.Phase)
	}
}

func TestTelegramScenariosAppliedCorrectly(t *testing.T) {
	cat := &mockCatalog{}
	adp := egress.NewAdapter(cat)
	ctx := context.Background()

	// 1. direct_fake_tls without TlsDomain must FAIL
	_, err := BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:       "tgwebproxy",
		Scenario:   "direct_fake_tls",
		DirectHost: "tg.example.org",
	})
	if err == nil {
		t.Fatalf("expected direct_fake_tls without TlsDomain to fail")
	}

	// 2. direct_fake_tls with TlsDomain succeeds
	cfg, err := BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:       "tgwebproxy",
		Scenario:   "direct_fake_tls",
		DirectHost: "tg.example.org",
		TlsDomain:  "gateway.icloud.com",
	})
	if err != nil {
		t.Fatalf("expected direct_fake_tls with TlsDomain to succeed: %v", err)
	}
	if cfg.TlsDomain != "gateway.icloud.com" || cfg.DirectPort != 8443 {
		t.Fatalf("unexpected direct_fake_tls config: %+v", cfg)
	}

	// 3. cdn_http without TlsDomain succeeds (TLS domain not needed for CDN HTTP)
	cfg, err = BuildDesiredConfig(ctx, adp, WizardPlanRequest{
		Kind:         "tgwebproxy",
		Scenario:     "cdn_http",
		PublicDomain: "cdn.example.org",
	})
	if err != nil {
		t.Fatalf("expected cdn_http to succeed without TlsDomain: %v", err)
	}
	if cfg.TlsDomain != "" {
		t.Fatalf("expected empty TlsDomain for cdn_http, got %s", cfg.TlsDomain)
	}
}

func TestWizardService_ConsumeErrorAfterCommitBoundary(t *testing.T) {
	svc, coord, xray, _, _, _ := setupAcceptanceCoordinatorAndWizard(t)
	ctx := context.Background()

	// 1. Create a plan
	planRec, err := svc.Plan(ctx, "session-consume-err", WizardPlanRequest{
		Kind:         "xray",
		DeviceType:   "phone",
		PublicDomain: "cdn.example.com",
		Path:         "/cdn-bridge/",
		Mode:         "xhttp_get",
		ClientRemark: "Test Client",
	})
	if err != nil {
		t.Fatalf("Plan creation failed: %v", err)
	}

	// 2. Inject fault: MarkConsumed fails in PlanStore
	forcedConsumeErr := errors.New("simulated disk write failure during consume")
	svc.planStore.SetMarkConsumedSeamForTest(func(planID, jobID string) (*ServerPlanRecord, error) {
		return nil, forcedConsumeErr
	})

	// 3. Apply the plan
	jobID, err := svc.Apply(ctx, "session-consume-err", planRec.PlanID)
	if err != nil {
		t.Fatalf("Apply initiation failed: %v", err)
	}

	// 4. Poll job status until it reaches terminal phase
	var finalJob JobStatusResponse
	completed := false
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		job, err := svc.GetJob(ctx, jobID, "session-consume-err")
		if err == nil && (job.Phase == JobPhaseRecoveryRequired || job.Phase == JobPhaseFailed || job.Phase == JobPhaseSucceeded) {
			finalJob = job
			completed = true
			break
		}
	}
	if !completed {
		t.Fatalf("job did not complete within timeout")
	}

	// 5. Verify assertions per closure requirements:
	// a. Coordinator/job transitioned to recovery_required
	if finalJob.Phase != JobPhaseRecoveryRequired {
		t.Fatalf("expected job phase %s, got %s (err: %s)", JobPhaseRecoveryRequired, finalJob.Phase, finalJob.Error)
	}

	// b. Diagnostic reason contains "plan_consume_failed_after_commit_boundary"
	if !strings.Contains(finalJob.Error, "plan_consume_failed_after_commit_boundary") {
		t.Errorf("expected job error to contain 'plan_consume_failed_after_commit_boundary', got error=%q", finalJob.Error)
	}
	if finalJob.ErrorCode != ErrCodeRecoveryRequired {
		t.Errorf("expected job error code %s, got %s", ErrCodeRecoveryRequired, finalJob.ErrorCode)
	}

	// c. Coordinator itself is in recoveryNeeded state
	isRec, reason := coord.IsRecoveryRequired()
	if !isRec {
		t.Errorf("expected coordinator to be in recoveryNeeded state")
	}
	if !strings.Contains(reason, "plan_consume_failed_after_commit_boundary") {
		t.Errorf("expected coordinator recovery reason to contain 'plan_consume_failed_after_commit_boundary', got %q", reason)
	}

	// d. Component rollback was NOT called (we are past point of no return)
	if len(xray.rollbackCalls) > 0 {
		t.Errorf("expected 0 rollback calls on xray after commit boundary, got %d", len(xray.rollbackCalls))
	}

	// e. Plan was NOT released and is NOT re-reservable by another job
	// Reset the seam so we can check real store state
	svc.planStore.SetMarkConsumedSeamForTest(nil)
	storedPlan, err := svc.planStore.GetPlan(planRec.PlanID, "session-consume-err")
	if err != nil {
		t.Fatalf("failed to retrieve stored plan: %v", err)
	}
	if storedPlan.State != PlanStateReserved {
		t.Errorf("expected plan state to remain Reserved (not released to Available), got %s", storedPlan.State)
	}
	// Attempting to reserve with another job must fail
	_, err = svc.planStore.Reserve(planRec.PlanID, "session-consume-err", "another-job")
	if err == nil {
		t.Errorf("expected reservation by another job to fail because plan is not available")
	}
}

func TestWizardService_ConsumeErrorAfterCommitBoundary_WithConcurrentCancelRequest(t *testing.T) {
	svc, coord, xray, _, _, _ := setupAcceptanceCoordinatorAndWizard(t)
	ctx := context.Background()
	sessionID := "session-consume-cancel-race"

	// 1. Create a plan
	planRec, err := svc.Plan(ctx, sessionID, WizardPlanRequest{
		Kind:         "xray",
		DeviceType:   "phone",
		PublicDomain: "cdn.example.com",
		Path:         "/cdn-bridge/",
		Mode:         "xhttp_get",
		ClientRemark: "Test Client",
	})
	if err != nil {
		t.Fatalf("Plan creation failed: %v", err)
	}

	// 2. Inject fault: MarkConsumed triggers a cancel request right before returning error
	forcedConsumeErr := errors.New("simulated disk write failure during consume")
	svc.planStore.SetMarkConsumedSeamForTest(func(planID, jobID string) (*ServerPlanRecord, error) {
		// Attempt to request cancellation concurrently right during consume error
		_ = svc.CancelJob(ctx, jobID, sessionID)
		return nil, forcedConsumeErr
	})

	// 3. Apply the plan
	jobID, err := svc.Apply(ctx, sessionID, planRec.PlanID)
	if err != nil {
		t.Fatalf("Apply initiation failed: %v", err)
	}

	// 4. Poll job status until it reaches terminal phase
	var finalJob JobStatusResponse
	completed := false
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		job, err := svc.GetJob(ctx, jobID, sessionID)
		if err == nil && (job.Phase == JobPhaseRecoveryRequired || job.Phase == JobPhaseFailed || job.Phase == JobPhaseSucceeded || job.Phase == JobPhaseCancelled) {
			finalJob = job
			completed = true
			break
		}
	}
	if !completed {
		t.Fatalf("job did not complete within timeout")
	}

	// 5. Verify assertions per audit requirements:
	// a. Final job phase must be STRICTLY recovery_required, NEVER cancelled!
	if finalJob.Phase != JobPhaseRecoveryRequired {
		t.Fatalf("expected job phase %s, got %s (err: %s)", JobPhaseRecoveryRequired, finalJob.Phase, finalJob.Error)
	}

	// b. Diagnostic reason contains "plan_consume_failed_after_commit_boundary"
	if !strings.Contains(finalJob.Error, "plan_consume_failed_after_commit_boundary") {
		t.Errorf("expected job error to contain 'plan_consume_failed_after_commit_boundary', got error=%q", finalJob.Error)
	}
	if finalJob.ErrorCode != ErrCodeRecoveryRequired {
		t.Errorf("expected job error code %s, got %s", ErrCodeRecoveryRequired, finalJob.ErrorCode)
	}

	// c. Coordinator itself is in recoveryNeeded state
	isRec, reason := coord.IsRecoveryRequired()
	if !isRec {
		t.Errorf("expected coordinator to be in recoveryNeeded state")
	}
	if !strings.Contains(reason, "plan_consume_failed_after_commit_boundary") {
		t.Errorf("expected coordinator recovery reason to contain 'plan_consume_failed_after_commit_boundary', got %q", reason)
	}

	// d. Component rollback was NOT called (we are past point of no return)
	if len(xray.rollbackCalls) > 0 {
		t.Errorf("expected 0 rollback calls on xray after commit boundary, got %d", len(xray.rollbackCalls))
	}

	// e. Plan remains reserved and is NOT released or re-reservable by another job
	svc.planStore.SetMarkConsumedSeamForTest(nil)
	storedPlan, err := svc.planStore.GetPlan(planRec.PlanID, sessionID)
	if err != nil {
		t.Fatalf("failed to retrieve stored plan: %v", err)
	}
	if storedPlan.State != PlanStateReserved {
		t.Errorf("expected plan state to remain Reserved (not released to Available), got %s", storedPlan.State)
	}

	// f. Explicit Release does NOT change state or make plan available
	_ = svc.planStore.Release(planRec.PlanID, "foreign-job")
	storedPlanAfterRelease, _ := svc.planStore.GetPlan(planRec.PlanID, sessionID)
	if storedPlanAfterRelease.State != PlanStateReserved {
		t.Errorf("expected plan state to remain Reserved after foreign release attempt, got %s", storedPlanAfterRelease.State)
	}

	// Attempting to reserve with another job must fail
	_, err = svc.planStore.Reserve(planRec.PlanID, sessionID, "another-job")
	if err == nil {
		t.Errorf("expected reservation by another job to fail because plan is not available")
	}
}

func TestWizardAcceptance_CustomXrayPath_DispatcherAndReadinessConsistency(t *testing.T) {
	svc, _, _, _, disp, _ := setupAcceptanceCoordinatorAndWizard(t)
	ctx := context.Background()

	var probedURLs []string
	svc.SetHTTPDoer(&mockHTTPDoer{
		doFn: func(req *http.Request) (*http.Response, error) {
			probedURLs = append(probedURLs, req.URL.String())
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
			}
			if strings.Contains(req.URL.Path, "my-custom-vless-prefix") || strings.Contains(req.URL.Path, "xray") || strings.Contains(req.URL.Path, "cdn-bridge") {
				resp.Header.Set("X-CDN-Route", "xray")
			} else {
				resp.Header.Set("X-CDN-Route", "tgwebproxy")
			}
			return resp, nil
		},
	})

	// Input path has leading/trailing slashes and arbitrary prefix
	rawInputPath := "///my-custom-vless-prefix///"
	expectedCanonicalPath := "/my-custom-vless-prefix"

	planRec, err := svc.Plan(ctx, "session-custom-path", WizardPlanRequest{
		Kind:         "xray",
		DeviceType:   "phone",
		PublicDomain: "cdn.example.com",
		Path:         rawInputPath,
		Mode:         "xhttp_get",
		ClientRemark: "Custom Path Client",
	})
	if err != nil {
		t.Fatalf("Plan creation failed: %v", err)
	}

	// 1. Verify desired config in plan has normalized path
	if planRec.Desired.Path != expectedCanonicalPath {
		t.Errorf("expected plan desired path %q, got %q", expectedCanonicalPath, planRec.Desired.Path)
	}

	jobID, err := svc.Apply(ctx, "session-custom-path", planRec.PlanID)
	if err != nil {
		t.Fatalf("Apply initiation failed: %v", err)
	}

	// Wait for completion
	completed := false
	var lastJob JobStatusResponse
	for i := 0; i < 80; i++ {
		time.Sleep(50 * time.Millisecond)
		job, err := svc.GetJob(ctx, jobID, "session-custom-path")
		if err == nil {
			lastJob = job
			if job.Phase == JobPhaseSucceeded {
				completed = true
				break
			} else if job.Phase == JobPhaseFailed || job.Phase == JobPhaseRecoveryRequired {
				t.Fatalf("job failed with phase %s: %s (code: %s)", job.Phase, job.Error, job.ErrorCode)
			}
		}
	}
	if !completed {
		t.Fatalf("job did not succeed within timeout, last job state: %+v", lastJob)
	}

	// 2. Verify registered dispatcher prefix is canonical without trailing slashes
	if disp.cfg.XrayPathPrefix != expectedCanonicalPath {
		t.Errorf("expected dispatcher XrayPathPrefix %q, got %q", expectedCanonicalPath, disp.cfg.XrayPathPrefix)
	}

	// 3. Verify probed URL matches the registered dispatcher prefix
	expectedProbeURL := fmt.Sprintf("http://127.0.0.1:9009%s", expectedCanonicalPath)
	found := false
	for _, u := range probedURLs {
		if u == expectedProbeURL {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("readiness probe did not request canonical URL %q, probed URLs: %v", expectedProbeURL, probedURLs)
	}
}

