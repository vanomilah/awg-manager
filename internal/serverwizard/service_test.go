package serverwizard

import (
	"context"
	"errors"
	"io"
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
	"github.com/hoaxisr/awg-manager/internal/serverwizard/egress"
	"github.com/hoaxisr/awg-manager/internal/sys/procnet"
	"github.com/hoaxisr/awg-manager/internal/tgwebproxy"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

type mockCoordinatorBridge struct {
	lockCount      int
	applyXrayErr   error
	applyTgErr     error
	txParams       serveringress.IngressTransactionParams
	txErr          error
	recoveryNeed   bool
	recoveryReason string
}

func (m *mockCoordinatorBridge) WithIngressLock(txID string, fn func() error) error {
	m.lockCount++
	return fn()
}

func (m *mockCoordinatorBridge) ExecuteIngressTransaction(ctx context.Context, params serveringress.IngressTransactionParams) error {
	m.lockCount++
	m.txParams = params
	if m.txErr != nil {
		return m.txErr
	}
	if params.FingerprintFunc != nil && params.ExpectedFingerprint != "" {
		curFP, err := params.FingerprintFunc(ctx)
		if err != nil {
			return err
		}
		if curFP != params.ExpectedFingerprint {
			return serveringress.ErrPlanStale
		}
	}
	if params.ReadinessProbe != nil {
		if err := params.ReadinessProbe(ctx, serveringress.IngressTopology{}); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockCoordinatorBridge) IsRecoveryRequired() (bool, string) {
	return m.recoveryNeed, m.recoveryReason
}

type mockTestConn struct {
	net.Conn
}

func (m *mockTestConn) Close() error {
	return nil
}

type mockHTTPDoer struct {
	doFn func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if m.doFn != nil {
		return m.doFn(req)
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
	}
	if strings.Contains(req.URL.Path, "xray") || strings.Contains(req.URL.Path, "cdn-bridge") {
		resp.Header.Set("X-CDN-Route", "xray")
	} else {
		resp.Header.Set("X-CDN-Route", "tgwebproxy")
	}
	return resp, nil
}

type mockXraySvcFull struct {
	mockXrayReader
	links *xrayserver.ShareLinks
}

func (m *mockXraySvcFull) GenerateLinks(clientID string) (*xrayserver.ShareLinks, error) {
	return m.links, nil
}

type mockTgSvcFull struct {
	mockTgReader
	rev tgwebproxy.RevealData
}

func (m *mockTgSvcFull) RevealSecret() (tgwebproxy.RevealData, error) {
	return m.rev, nil
}

type mockCatalog struct{}

func (m *mockCatalog) ListAll(ctx context.Context) []routing.TunnelEntry {
	return nil
}

func setupTestWizard(t *testing.T) (*WizardService, *mockCoordinatorBridge, *mockXraySvcFull, *mockTgSvcFull, *mockDispReader) {
	xray := &mockXraySvcFull{
		mockXrayReader: mockXrayReader{
			cfg: xrayserver.Config{Enabled: false, ListenPort: 9008},
			st:  xrayserver.Status{Installed: true, Configured: false},
		},
	}
	tg := &mockTgSvcFull{
		mockTgReader: mockTgReader{
			cfg: tgwebproxy.PublicConfig{Enabled: false, ListenPort: 8085},
			st:  tgwebproxy.Status{Installed: true, Running: false},
		},
	}
	disp := &mockDispReader{
		cfg:     cdndispatcher.Config{ListenAddr: ":9009"},
		running: false,
	}
	coord := &mockCoordinatorBridge{}

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
	procDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(procDir, "net"), 0755)
	_ = os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(""), 0644)
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

	return svc, coord, xray, tg, disp
}

func TestWizardService_CapabilitiesAndPreflight(t *testing.T) {
	svc, _, _, _, _ := setupTestWizard(t)
	ctx := context.Background()

	caps := svc.GetCapabilities(ctx, "tgwebproxy")
	if len(caps.Profiles) == 0 || len(caps.Scenarios) == 0 {
		t.Fatalf("expected non-empty profiles and scenarios, got %+v", caps)
	}

	preflight := svc.Preflight(ctx, WizardPlanRequest{
		Kind:         "tgwebproxy",
		DirectHost:   "192.168.1.1",
		DirectPort:   8443,
		ListenPort:   8085,
		TlsDomain:    "gateway.icloud.com",
		CdnProfileID: "cdn_get",
	})
	if !preflight.CanProceed {
		t.Fatalf("expected preflight CanProceed=true, got %+v", preflight.Checks)
	}
	if preflight.Fingerprint == "" {
		t.Errorf("expected non-empty fingerprint in preflight")
	}
}

func TestWizardService_PlanAndApply_HappyPath(t *testing.T) {
	svc, coord, _, _, _ := setupTestWizard(t)
	ctx := context.Background()
	sessionID := "user-session-abc"

	req := WizardPlanRequest{
		Kind:         "tgwebproxy",
		Scenario:     "dual",
		DirectHost:   "192.168.1.1",
		DirectPort:   8443,
		ListenPort:   8085,
		PublicDomain: "tg.example.com",
		TlsDomain:    "gateway.icloud.com",
		CdnProfileID: "cdn_get",
	}

	// 1. Create plan
	rec, err := svc.Plan(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("failed to create plan: %v", err)
	}
	if rec.PlanID == "" || rec.StateFingerprint == "" {
		t.Fatalf("invalid plan record: %+v", rec)
	}

	// 2. Apply plan
	jobID, err := svc.Apply(ctx, sessionID, rec.PlanID)
	if err != nil {
		t.Fatalf("failed to apply plan: %v", err)
	}

	// 3. Wait for async job completion
	var jobResp JobStatusResponse
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		jobResp, err = svc.GetJob(ctx, jobID, sessionID)
		if err != nil {
			t.Fatalf("failed to poll job: %v", err)
		}
		if jobResp.Phase == JobPhaseSucceeded || jobResp.Phase == JobPhaseFailed {
			break
		}
	}

	if jobResp.Phase != JobPhaseSucceeded {
		t.Fatalf("expected job Succeeded, got %s (err: %s)", jobResp.Phase, jobResp.Error)
	}
	if !jobResp.ResultAvailable {
		t.Errorf("expected ResultAvailable=true")
	}

	// 4. Reveal credentials
	creds, err := svc.Reveal(ctx, jobID, sessionID)
	if err != nil {
		t.Fatalf("failed to reveal credentials: %v", err)
	}
	if creds.Kind != "tgwebproxy" || creds.TgSecret == "" || creds.DirectLink == "" {
		t.Fatalf("unexpected reveal credentials: %+v", creds)
	}

	if coord.lockCount < 1 {
		t.Errorf("expected at least 1 ingress lock acquisition, got %d", coord.lockCount)
	}
}

func TestWizardService_StaleFingerprintRejection(t *testing.T) {
	svc, _, xray, _, _ := setupTestWizard(t)
	ctx := context.Background()
	sessionID := "user-session-stale"

	req := WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "vpn.example.com",
		Path:         "/cdn-bridge/",
		ClientRemark: "Test Phone",
	}

	rec, err := svc.Plan(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("failed to create plan: %v", err)
	}

	// Mutate state to cause fingerprint drift before apply
	xray.cfg.ListenPort = 9999

	jobID, err := svc.Apply(ctx, sessionID, rec.PlanID)
	if err != nil {
		t.Fatalf("unexpected apply error: %v", err)
	}

	var jobResp JobStatusResponse
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		jobResp, err = svc.GetJob(ctx, jobID, sessionID)
		if err != nil {
			t.Fatalf("failed to poll job: %v", err)
		}
		if jobResp.Phase == JobPhaseSucceeded || jobResp.Phase == JobPhaseFailed {
			break
		}
	}

	if jobResp.Phase != JobPhaseFailed || jobResp.ErrorCode != ErrCodePlanStale {
		t.Fatalf("expected job failed with PLAN_STALE on drifted state, got %+v", jobResp)
	}
}

func TestWizardService_DuplicateApplyRejection(t *testing.T) {
	svc, _, _, _, _ := setupTestWizard(t)
	ctx := context.Background()
	sessionID := "user-session-dup"

	req := WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "vpn.example.com",
		Path:         "/cdn-bridge/",
	}

	rec, err := svc.Plan(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("failed to create plan: %v", err)
	}

	// First apply succeeds
	_, err = svc.Apply(ctx, sessionID, rec.PlanID)
	if err != nil {
		t.Fatalf("first apply failed: %v", err)
	}

	// Second apply should fail with ErrOperationInProgress or ErrPlanAlreadyUsed
	_, err = svc.Apply(ctx, sessionID, rec.PlanID)
	if err == nil || (!errors.Is(err, ErrOperationInProgress) && !errors.Is(err, ErrPlanAlreadyUsed)) {
		t.Fatalf("expected ErrOperationInProgress or ErrPlanAlreadyUsed on duplicate apply, got %v", err)
	}
}

func TestReadinessProbe_HTTPRouteIdentity_SharedHostDualPaths(t *testing.T) {
	ctx := context.Background()

	sharedTopo := serveringress.IngressTopology{
		PublicHostname:     "shared.example.com",
		XrayPublicHostname: "shared.example.com",
		XrayEnabled:        true,
		XrayPort:           9008,
		XrayPathPrefix:     "/cdn-bridge",
		TgPublicHostname:   "shared.example.com",
		TgEnabled:          true,
		TgWebPort:          8085,
		TgScenario:         "dual",
		DispatcherPort:     9009,
	}

	tests := []struct {
		name       string
		xrayHeader string
		tgHeader   string
		tgHttpErr  error
		wantErr    bool
		errSubstr  string
	}{
		{
			name:       "BothSucceed",
			xrayHeader: "xray",
			tgHeader:   "tgwebproxy",
			wantErr:    false,
		},
		{
			name:       "XrayHeaderWrong",
			xrayHeader: "invalid-xray",
			tgHeader:   "tgwebproxy",
			wantErr:    true,
			errSubstr:  "unexpected or missing X-CDN-Route header for Xray",
		},
		{
			name:       "XrayHeaderMissing",
			xrayHeader: "",
			tgHeader:   "tgwebproxy",
			wantErr:    true,
			errSubstr:  "unexpected or missing X-CDN-Route header for Xray",
		},
		{
			name:       "TgHeaderWrong",
			xrayHeader: "xray",
			tgHeader:   "invalid-tg",
			wantErr:    true,
			errSubstr:  "unexpected or missing X-CDN-Route header for Telegram",
		},
		{
			name:       "TgHeaderMissing",
			xrayHeader: "xray",
			tgHeader:   "",
			wantErr:    true,
			errSubstr:  "unexpected or missing X-CDN-Route header for Telegram",
		},
		{
			name:       "TgHTTPError",
			xrayHeader: "xray",
			tgHttpErr:  errors.New("connection reset by peer"),
			wantErr:    true,
			errSubstr:  "http get tg probe",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _, _ := setupTestWizard(t)
			svc.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
				return &mockTestConn{}, nil
			})
			svc.SetHTTPDoer(&mockHTTPDoer{
				doFn: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Path, "cdn-bridge") {
						resp := &http.Response{
							StatusCode: http.StatusOK,
							Header:     make(http.Header),
							Body:       io.NopCloser(strings.NewReader("ok")),
						}
						if tc.xrayHeader != "" {
							resp.Header.Set("X-CDN-Route", tc.xrayHeader)
						}
						return resp, nil
					}
					if tc.tgHttpErr != nil {
						return nil, tc.tgHttpErr
					}
					resp := &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("ok")),
					}
					if tc.tgHeader != "" {
						resp.Header.Set("X-CDN-Route", tc.tgHeader)
					}
					return resp, nil
				},
			})

			probeCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
			defer cancel()

			err := svc.probeTopologyReadiness(probeCtx, sharedTopo)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tc.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
			}
		})
	}
}

func TestReadinessProbe_SocketVsHTTPIndependence(t *testing.T) {
	svc, _, _, _, _ := setupTestWizard(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Socket dialer fails on dispatcher port 9009
	dialErr := errors.New("connection refused on 9009")
	svc.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		if strings.Contains(address, "9009") {
			return nil, dialErr
		}
		return &mockTestConn{}, nil
	})

	// HTTP doer is configured to return valid 200 OK with correct header
	httpCalled := false
	svc.SetHTTPDoer(&mockHTTPDoer{
		doFn: func(req *http.Request) (*http.Response, error) {
			httpCalled = true
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
			}
			resp.Header.Set("X-CDN-Route", "xray")
			return resp, nil
		},
	})

	topo := serveringress.IngressTopology{
		PublicHostname:     "cdn.example.com",
		XrayPublicHostname: "cdn.example.com",
		XrayEnabled:        true,
		XrayPort:           9008,
		XrayPathPrefix:     "/cdn-bridge",
		DispatcherPort:     9009,
	}

	err := svc.probeTopologyReadiness(ctx, topo)
	if err == nil {
		t.Fatalf("expected socket probe failure, got nil")
	}
	if !strings.Contains(err.Error(), "cdn dispatcher") || !strings.Contains(err.Error(), "not ready") {
		t.Errorf("expected dispatcher socket not ready error, got: %v", err)
	}
	if httpCalled {
		t.Errorf("expected HTTP probe NOT to be called when socket readiness failed")
	}
}

