//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/api"
	"github.com/hoaxisr/awg-manager/internal/events"
	"github.com/hoaxisr/awg-manager/internal/firc"
	"github.com/hoaxisr/awg-manager/internal/logging"
	"github.com/hoaxisr/awg-manager/internal/singbox"
	"github.com/hoaxisr/awg-manager/internal/singbox/router"
	"github.com/hoaxisr/awg-manager/internal/storage"
)

// Self-exec fake awgm-box/sing-box binary hook so Process.matchesBinary (/proc/<pid>/cmdline
// argv0 basename == "awgm-box"), `version`, `check`, and
// Clash API readiness probing work without an external awgm-box executable.
func init() {
	base := filepath.Base(os.Args[0])
	if base != "awgm-box" && base != "sing-box" {
		return
	}
	sub := ""
	if len(os.Args) > 1 {
		sub = os.Args[1]
	}
	switch sub {
	case "version":
		fmt.Println("sing-box version 1.14.0")
		os.Exit(0)
	case "check":
		os.Exit(0)
	case "run":
		configDir := ""
		for i := 2; i < len(os.Args)-1; i++ {
			if os.Args[i] == "-C" || os.Args[i] == "-D" {
				configDir = os.Args[i+1]
				break
			}
		}
		clashAddr := strings.TrimSpace(os.Getenv("AWGM_TEST_SINGBOX_CLASH_ADDR"))
		if configDir != "" {
			if raw, err := os.ReadFile(filepath.Join(configDir, "00-base.json")); err == nil {
				var cfg struct {
					Experimental struct {
						ClashAPI struct {
							ExternalController string `json:"external_controller"`
						} `json:"clash_api"`
					} `json:"experimental"`
				}
				if json.Unmarshal(raw, &cfg) == nil && strings.TrimSpace(cfg.Experimental.ClashAPI.ExternalController) != "" {
					clashAddr = strings.TrimSpace(cfg.Experimental.ClashAPI.ExternalController)
				}
			}
		}
		var srv *http.Server
		if clashAddr != "" {
			ln, err := net.Listen("tcp", clashAddr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "FATAL[0000] clash listen %s: %v\n", clashAddr, err)
				os.Exit(1)
			}
			srv = &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"version":"1.14.0","proxies":{}}`))
				}),
			}
			go func() { _ = srv.Serve(ln) }()
		}
		_, _ = fmt.Fprintln(os.Stderr, "INFO[0000] sing-box started")

		sigCh := make(chan os.Signal, 4)
		signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
		for sig := range sigCh {
			if sig == syscall.SIGHUP {
				continue
			}
			if srv != nil {
				shutCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				_ = srv.Shutdown(shutCtx)
				cancel()
			}
			os.Exit(0)
		}
	default:
		os.Exit(0)
	}
}

type lifecycleNoopWANCollector struct{}

func (lifecycleNoopWANCollector) Collect(_ context.Context) ([]string, error) {
	return []string{"203.0.113.2/32"}, nil
}

type faultyRouterCoord struct {
	inner      *router.ServiceImpl
	failPark   atomic.Bool
	failResume atomic.Bool
}

func (f *faultyRouterCoord) ParkForExternalEngine(ctx context.Context) error {
	if f.failPark.Load() {
		return errors.New("injected router park failure")
	}
	return f.inner.ParkForExternalEngine(ctx)
}

func (f *faultyRouterCoord) ResumeFromExternalEngine(ctx context.Context) error {
	if f.failResume.Load() {
		return errors.New("injected router resume failure")
	}
	return f.inner.ResumeFromExternalEngine(ctx)
}

type lifecycleHarness struct {
	t             *testing.T
	dataDir       string
	singboxDir    string
	clashPort     int
	app           *app
	singboxH      *api.SingboxHandler
	fircSrv       *httptest.Server
	fircRunning   *atomic.Bool
	failFircStart atomic.Bool
	routerFault   *faultyRouterCoord
	mux           *http.ServeMux
	closed        bool
}

func pickFreeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ephemeral port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func seedLifecycleDiskState(t *testing.T, rootDir string, clashPort int) (dataDir, singboxDir string) {
	t.Helper()
	dataDir = filepath.Join(rootDir, "data")
	singboxDir = filepath.Join(rootDir, "singbox")
	cfgDir := filepath.Join(singboxDir, "config.d")
	fircDir := filepath.Join(dataDir, "firc")
	for _, d := range []string{dataDir, cfgDir, fircDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	sbBin := filepath.Join(singboxDir, "awgm-box")
	if err := os.Symlink(selfExe, sbBin); err != nil {
		t.Fatalf("symlink stub awgm-box: %v", err)
	}

	store := storage.NewSettingsStore(dataDir)
	if _, err := store.Load(); err != nil {
		t.Fatalf("seed settings load: %v", err)
	}
	if err := store.Update(func(s *storage.Settings) error {
		s.CreateNDMSProxyForSingbox = false
		s.SingboxClashPort = clashPort
		s.SingboxRouter = storage.SingboxRouterSettings{
			Enabled:       true,
			PolicyName:    "Policy0",
			DeviceMode:    "all",
			RoutingMode:   "tproxy",
			WANAutoDetect: true,
			RoutingEngine: "sing-box",
		}
		return nil
	}); err != nil {
		t.Fatalf("seed settings update: %v", err)
	}

	tunnelsJSON := `{"outbounds":[{"type":"vless","tag":"tun-a","server":"198.51.100.10","server_port":443}]}` + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "10-tunnels.json"), []byte(tunnelsJSON), 0600); err != nil {
		t.Fatalf("write 10-tunnels.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fircDir, "tunnels.yaml"), []byte("[]\n"), 0600); err != nil {
		t.Fatalf("write empty firc tunnels.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fircDir, "groups.yaml"), []byte("[]\n"), 0600); err != nil {
		t.Fatalf("write empty firc groups.yaml: %v", err)
	}
	return dataDir, singboxDir
}

func startLifecycleApp(t *testing.T, dataDir, singboxDir string, clashPort int, sharedFircRunning *atomic.Bool) *lifecycleHarness {
	t.Helper()
	t.Setenv("AWGM_TEST_SINGBOX_CLASH_ADDR", fmt.Sprintf("127.0.0.1:%d", clashPort))
	restorePaths(t)
	applyDataDir(dataDir)

	settingsStore := storage.NewSettingsStore(dataDir)
	snap, err := settingsStore.Load()
	if err != nil {
		t.Fatalf("settingsStore.Load: %v", err)
	}
	logSvc := logging.NewService(settingsStore)
	bootLog := logging.NewScopedLogger(logSvc, logging.GroupSystem, logging.SubBoot)

	a := &app{
		dataDir:        dataDir,
		singboxDir:     singboxDir,
		settingsStore:  settingsStore,
		settings:       snap,
		eventBus:       events.NewBus(),
		loggingService: logSvc,
		bootLog:        bootLog,
	}
	a.setupSingboxRuntime()

	routerSvc := router.NewService(router.Deps{
		AppLog:             logSvc,
		Settings:           settingsStore,
		Singbox:            a.singboxOp,
		Orch:               a.sbOrch,
		Events:             a.eventBus,
		Bus:                a.eventBus,
		IPTables:           router.NewNoopIPTables(),
		WANIPCollector:     lifecycleNoopWANCollector{},
		NetfilterPreflight: func(context.Context) error { return nil },
		XtDscpProbe:        func(context.Context) bool { return false },
		ListeningProbe: func() bool {
			r, _ := a.singboxOp.IsRunning()
			return r
		},
	})
	a.routerSvc = routerSvc
	rc := &faultyRouterCoord{inner: routerSvc}
	a.routerCoord = rc

	if sharedFircRunning == nil {
		sharedFircRunning = &atomic.Bool{}
	}
	h := &lifecycleHarness{
		t:           t,
		dataDir:     dataDir,
		singboxDir:  singboxDir,
		clashPort:   clashPort,
		app:         a,
		singboxH:    api.NewSingboxHandler(a.singboxOp, a.eventBus, nil, nil, logSvc),
		fircRunning: sharedFircRunning,
		routerFault: rc,
	}

	var fircMu sync.Mutex
	var extCfg firc.ExtConfig
	h.fircSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.fircRunning.Load() {
			http.Error(w, "fircd stopped", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/netfilter":
			_, _ = w.Write([]byte(`{"available":true,"chainsPresent":true,"rulesCount":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tunnels":
			_, _ = w.Write([]byte(`{"tunnels":[]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/tunnels":
			_, _ = w.Write([]byte(`{"tunnels":[],"applied":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/groups":
			_, _ = w.Write([]byte(`{"groups":[]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/groups":
			_, _ = w.Write([]byte(`{"groups":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/settings":
			_, _ = w.Write([]byte(`{"settings":{}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/system/settings":
			_, _ = w.Write([]byte(`{"applied":[],"pendingRestart":[]}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/ext"):
			fircMu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&extCfg)
			fircMu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))

	a.fircOp = firc.NewOperator(firc.Options{
		ConfigDir:   filepath.Join(dataDir, "firc"),
		InboundPort: firc.DefaultMixedInboundPort,
	})
	a.fircOp.SetClient(firc.NewHTTPClient(h.fircSrv.URL, h.fircSrv.Client()))
	a.fircOp.SetLifecycleHooks(
		func(_ context.Context) error {
			if h.failFircStart.Load() {
				return errors.New("injected firc start failure")
			}
			h.fircRunning.Store(true)
			return nil
		},
		func(_ context.Context) error {
			h.fircRunning.Store(false)
			return nil
		},
	)

	a.fircHandler = api.NewFircHandler(a.fircOp)
	a.fircHandler.SetSettingsStore(settingsStore)
	a.fircHandler.SetEventBus(a.eventBus)
	a.fircHandler.SetSingboxConfigDir(filepath.Join(singboxDir, "config.d"))
	a.wireFircEngineCoordinator()
	a.completeFircStartup()

	h.mux = http.NewServeMux()
	passthrough := func(fn http.HandlerFunc) http.HandlerFunc { return fn }
	a.fircHandler.RegisterRoutes(h.mux, passthrough)
	h.mux.HandleFunc("POST /api/singbox/control", h.singboxH.Control)

	t.Cleanup(func() {
		h.Close(true)
	})
	return h
}

func (h *lifecycleHarness) Close(stopChild bool) {
	if h.closed {
		return
	}
	h.closed = true
	if h.app != nil && h.app.sbOrch != nil {
		h.app.sbOrch.Close()
	}
	if stopChild && h.app != nil && h.app.singboxOp != nil {
		_ = h.app.singboxOp.Stop()
		if proc := h.app.singboxOp.Process(); proc != nil {
			proc.Close()
		}
	}
	if h.app != nil && h.app.loggingService != nil {
		h.app.loggingService.Stop()
	}
	if h.fircSrv != nil {
		h.fircSrv.Close()
	}
}

func (h *lifecycleHarness) runWatchdogTick(ctx context.Context) {
	h.t.Helper()
	wd := singbox.NewWatchdog(h.app.singboxOp, nil, nil)
	wCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	wd.Run(wCtx)
}

func (h *lifecycleHarness) postSwitchEngine(engine string, standby bool) (int, string) {
	h.t.Helper()
	body, err := json.Marshal(map[string]any{
		"engine":         engine,
		"singboxStandby": standby,
	})
	if err != nil {
		h.t.Fatalf("marshal switch request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/firc/engine/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (h *lifecycleHarness) postSingboxControl(action string) (int, string) {
	h.t.Helper()
	body, err := json.Marshal(api.SingboxControlRequest{Action: action})
	if err != nil {
		h.t.Fatalf("marshal control request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/singbox/control", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (h *lifecycleHarness) loadSettings() *storage.Settings {
	h.t.Helper()
	s, err := h.app.settingsStore.Load()
	if err != nil {
		h.t.Fatalf("settingsStore.Load: %v", err)
	}
	return s
}

func (h *lifecycleHarness) bootstrapRunningSingboxWithActiveRouter(ctx context.Context) {
	h.t.Helper()
	if err := h.app.singboxOp.Start(); err != nil {
		h.t.Fatalf("singboxOp.Start: %v", err)
	}
	if err := h.app.routerSvc.Reconcile(ctx); err != nil {
		h.t.Fatalf("initial routerSvc.Reconcile: %v", err)
	}
	if running, _ := h.app.singboxOp.IsRunning(); !running {
		h.t.Fatalf("expected sing-box running after bootstrap")
	}
	st, err := h.app.routerSvc.GetStatus(ctx)
	if err != nil {
		h.t.Fatalf("initial routerSvc.GetStatus: %v", err)
	}
	if !st.Active || st.ParkedByExternalEngine {
		h.t.Fatalf("expected router active & unparked after bootstrap, got %+v", st)
	}
}

func TestFircEngineLifecycle_SwitchAndStop_SurvivesWatchdogTicksAndDaemonRestart(t *testing.T) {
	ctx := context.Background()
	clashPort := pickFreeTCPPort(t)
	dataDir, singboxDir := seedLifecycleDiskState(t, t.TempDir(), clashPort)

	sharedFirc := &atomic.Bool{}
	h := startLifecycleApp(t, dataDir, singboxDir, clashPort, sharedFirc)
	h.bootstrapRunningSingboxWithActiveRouter(ctx)

	if code, body := h.postSwitchEngine("firc", false); code != http.StatusOK {
		t.Fatalf("switch_and_stop failed: HTTP %d: %s", code, body)
	}

	if !sharedFirc.Load() {
		t.Fatalf("expected fircd running after switch_and_stop")
	}
	if running, _ := h.app.singboxOp.IsRunning(); running {
		t.Fatalf("expected sing-box stopped after switch_and_stop")
	}
	if !h.app.singboxOp.IsStoppedByEngine() || h.app.singboxOp.IsManuallyStopped() || !h.app.singboxOp.IsAutoStartSuppressed() {
		t.Fatalf("unexpected singbox flags after switch_and_stop: byEngine=%v manual=%v suppressed=%v",
			h.app.singboxOp.IsStoppedByEngine(), h.app.singboxOp.IsManuallyStopped(), h.app.singboxOp.IsAutoStartSuppressed())
	}
	s := h.loadSettings()
	if s.SingboxRouter.RoutingEngine != "firc" || !s.SingboxStoppedByEngine || s.SingboxManuallyStopped {
		t.Fatalf("unexpected persisted settings after switch_and_stop: engine=%q byEngine=%v manual=%v",
			s.SingboxRouter.RoutingEngine, s.SingboxStoppedByEngine, s.SingboxManuallyStopped)
	}
	st, err := h.app.routerSvc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus after switch_and_stop: %v", err)
	}
	if !st.ParkedByExternalEngine || st.Active || st.RoutingEngine != "firc" {
		t.Fatalf("expected router parked on firc, got %+v", st)
	}

	// Watchdog ticks & router/singbox reconciles must not revive sing-box or unpark router.
	for i := 0; i < 3; i++ {
		h.runWatchdogTick(ctx)
		if err := h.app.singboxOp.Reconcile(ctx); err != nil {
			t.Fatalf("singbox Reconcile[%d]: %v", i, err)
		}
		if err := h.app.routerSvc.Reconcile(ctx); err != nil {
			t.Fatalf("router Reconcile[%d]: %v", i, err)
		}
		if running, _ := h.app.singboxOp.IsRunning(); running {
			t.Fatalf("watchdog/reconcile[%d] cold-started sing-box while stopped by engine", i)
		}
		st, err = h.app.routerSvc.GetStatus(ctx)
		if err != nil {
			t.Fatalf("GetStatus[%d]: %v", i, err)
		}
		if !st.ParkedByExternalEngine || st.Active {
			t.Fatalf("reconcile[%d] unparked router while engine is firc: %+v", i, st)
		}
	}

	// Simulate full awg-manager daemon restart on the same persisted dataDir/singboxDir.
	h.Close(false)
	h2 := startLifecycleApp(t, dataDir, singboxDir, clashPort, sharedFirc)
	if !h2.app.singboxOp.IsStoppedByEngine() || h2.app.singboxOp.IsManuallyStopped() || !h2.app.singboxOp.IsAutoStartSuppressed() {
		t.Fatalf("flags lost across daemon restart: byEngine=%v manual=%v suppressed=%v",
			h2.app.singboxOp.IsStoppedByEngine(), h2.app.singboxOp.IsManuallyStopped(), h2.app.singboxOp.IsAutoStartSuppressed())
	}
	h2.runWatchdogTick(ctx)
	if err := h2.app.singboxOp.Reconcile(ctx); err != nil {
		t.Fatalf("post-restart singbox Reconcile: %v", err)
	}
	if err := h2.app.routerSvc.Reconcile(ctx); err != nil {
		t.Fatalf("post-restart router Reconcile: %v", err)
	}
	if running, _ := h2.app.singboxOp.IsRunning(); running {
		t.Fatalf("post-restart watchdog cold-started sing-box while SingboxStoppedByEngine=true")
	}
	st2, err := h2.app.routerSvc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("post-restart router GetStatus: %v", err)
	}
	if !st2.ParkedByExternalEngine || st2.Active || st2.RoutingEngine != "firc" {
		t.Fatalf("expected post-restart router parked on firc, got %+v", st2)
	}
}

func TestFircEngineLifecycle_SwitchStandby_KeepsNonRouterSingboxWhileRouterParked(t *testing.T) {
	ctx := context.Background()
	clashPort := pickFreeTCPPort(t)
	dataDir, singboxDir := seedLifecycleDiskState(t, t.TempDir(), clashPort)

	sharedFirc := &atomic.Bool{}
	h := startLifecycleApp(t, dataDir, singboxDir, clashPort, sharedFirc)
	h.bootstrapRunningSingboxWithActiveRouter(ctx)

	if code, body := h.postSwitchEngine("firc", true); code != http.StatusOK {
		t.Fatalf("switch_standby failed: HTTP %d: %s", code, body)
	}

	if !sharedFirc.Load() {
		t.Fatalf("expected fircd running after switch_standby")
	}
	if running, _ := h.app.singboxOp.IsRunning(); !running {
		t.Fatalf("expected sing-box kept running in standby mode")
	}
	if h.app.singboxOp.IsStoppedByEngine() {
		t.Fatalf("expected SingboxStoppedByEngine=false in standby mode")
	}
	st, err := h.app.routerSvc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus in standby: %v", err)
	}
	if !st.ParkedByExternalEngine || st.Active || st.RoutingEngine != "firc" {
		t.Fatalf("expected router parked while sing-box in standby, got %+v", st)
	}

	// Reconcile/watchdog keep standby sing-box alive and keep router parked.
	h.runWatchdogTick(ctx)
	if err := h.app.routerSvc.Reconcile(ctx); err != nil {
		t.Fatalf("standby router Reconcile: %v", err)
	}
	st, _ = h.app.routerSvc.GetStatus(ctx)
	if !st.ParkedByExternalEngine || st.Active {
		t.Fatalf("router failed to stay parked on standby reconcile: %+v", st)
	}
	if running, _ := h.app.singboxOp.IsRunning(); !running {
		t.Fatalf("standby sing-box stopped unexpectedly")
	}

	// Same-engine convergence: switch to firc with singboxStandby=false stops sing-box.
	if code, body := h.postSwitchEngine("firc", false); code != http.StatusOK {
		t.Fatalf("same-engine switch_and_stop failed: HTTP %d: %s", code, body)
	}
	if running, _ := h.app.singboxOp.IsRunning(); running {
		t.Fatalf("expected sing-box stopped after same-engine switch_and_stop")
	}
	if !h.app.singboxOp.IsStoppedByEngine() {
		t.Fatalf("expected SingboxStoppedByEngine=true after same-engine switch_and_stop")
	}
}

func TestFircEngineLifecycle_ReturnToSingbox_And_ManualControlIsolation(t *testing.T) {
	ctx := context.Background()
	clashPort := pickFreeTCPPort(t)
	dataDir, singboxDir := seedLifecycleDiskState(t, t.TempDir(), clashPort)

	sharedFirc := &atomic.Bool{}
	h := startLifecycleApp(t, dataDir, singboxDir, clashPort, sharedFirc)
	h.bootstrapRunningSingboxWithActiveRouter(ctx)

	// Part A: switch_and_stop -> firc, then switch back -> sing-box resumes both process and router.
	if code, body := h.postSwitchEngine("firc", false); code != http.StatusOK {
		t.Fatalf("switch_and_stop to firc failed: HTTP %d: %s", code, body)
	}
	if code, body := h.postSwitchEngine("sing-box", false); code != http.StatusOK {
		t.Fatalf("return to sing-box failed: HTTP %d: %s", code, body)
	}
	if sharedFirc.Load() {
		t.Fatalf("expected fircd stopped after returning to sing-box")
	}
	if running, _ := h.app.singboxOp.IsRunning(); !running {
		t.Fatalf("expected sing-box resumed after returning from switch_and_stop")
	}
	if h.app.singboxOp.IsStoppedByEngine() || h.app.singboxOp.IsManuallyStopped() {
		t.Fatalf("unexpected stop flags after return to sing-box: byEngine=%v manual=%v",
			h.app.singboxOp.IsStoppedByEngine(), h.app.singboxOp.IsManuallyStopped())
	}
	st, err := h.app.routerSvc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus after return: %v", err)
	}
	if st.ParkedByExternalEngine || !st.Active || st.RoutingEngine != "sing-box" {
		t.Fatalf("expected router resumed and active after return to sing-box, got %+v", st)
	}

	// Part B: Manual stop isolation — manual stop must remain honored across firc round-trip,
	// while engine return must not clear SingboxManuallyStopped.
	if code, body := h.postSingboxControl("stop"); code != http.StatusOK {
		t.Fatalf("manual control stop failed: HTTP %d: %s", code, body)
	}
	if !h.app.singboxOp.IsManuallyStopped() || h.app.singboxOp.IsStoppedByEngine() {
		t.Fatalf("expected manual=true, byEngine=false after Control(stop); got manual=%v byEngine=%v",
			h.app.singboxOp.IsManuallyStopped(), h.app.singboxOp.IsStoppedByEngine())
	}
	if code, body := h.postSwitchEngine("firc", false); code != http.StatusOK {
		t.Fatalf("switch to firc while manually stopped failed: HTTP %d: %s", code, body)
	}
	if !h.app.singboxOp.IsManuallyStopped() {
		t.Fatalf("switching to firc clobbered SingboxManuallyStopped")
	}
	if code, body := h.postSwitchEngine("sing-box", false); code != http.StatusOK {
		t.Fatalf("return to sing-box while manually stopped failed: HTTP %d: %s", code, body)
	}
	if !h.app.singboxOp.IsManuallyStopped() {
		t.Fatalf("returning to sing-box cleared SingboxManuallyStopped")
	}
	if h.app.singboxOp.IsStoppedByEngine() {
		t.Fatalf("expected SingboxStoppedByEngine=false after returning to sing-box")
	}
	h.runWatchdogTick(ctx)
	if running, _ := h.app.singboxOp.IsRunning(); running {
		t.Fatalf("manually stopped sing-box started despite SingboxManuallyStopped=true")
	}

	// Explicit manual start restores process and clears SingboxManuallyStopped.
	if code, body := h.postSingboxControl("start"); code != http.StatusOK {
		t.Fatalf("manual control start failed: HTTP %d: %s", code, body)
	}
	if running, _ := h.app.singboxOp.IsRunning(); !running {
		t.Fatalf("expected sing-box running after manual Control(start)")
	}
	if h.app.singboxOp.IsManuallyStopped() {
		t.Fatalf("expected SingboxManuallyStopped cleared after manual Control(start)")
	}
}

func TestFircEngineLifecycle_BidirectionalRollback_RestoresExactState(t *testing.T) {
	ctx := context.Background()
	clashPort := pickFreeTCPPort(t)
	dataDir, singboxDir := seedLifecycleDiskState(t, t.TempDir(), clashPort)

	sharedFirc := &atomic.Bool{}
	h := startLifecycleApp(t, dataDir, singboxDir, clashPort, sharedFirc)
	h.bootstrapRunningSingboxWithActiveRouter(ctx)

	// Direction 1A: sing-box -> firc fails after CommitSettings & sing-box stop when router ParkForExternalEngine fails.
	h.routerFault.failPark.Store(true)
	if code, _ := h.postSwitchEngine("firc", false); code == http.StatusOK {
		t.Fatalf("expected switch sing-box -> firc to fail on injected park error")
	}
	h.routerFault.failPark.Store(false)

	if sharedFirc.Load() {
		t.Fatalf("rollback failed to stop candidate fircd")
	}
	s := h.loadSettings()
	if s.SingboxRouter.RoutingEngine != "sing-box" || s.SingboxStoppedByEngine || s.SingboxManuallyStopped {
		t.Fatalf("rollback failed to restore sing-box settings: engine=%q byEngine=%v manual=%v",
			s.SingboxRouter.RoutingEngine, s.SingboxStoppedByEngine, s.SingboxManuallyStopped)
	}
	if running, _ := h.app.singboxOp.IsRunning(); !running {
		t.Fatalf("rollback failed to restore running sing-box")
	}
	st, err := h.app.routerSvc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus after sing-box -> firc rollback: %v", err)
	}
	if st.ParkedByExternalEngine || !st.Active {
		t.Fatalf("rollback failed to keep router active & unparked: %+v", st)
	}

	// Direction 1B: Manual-stop state preserved on rollback from sing-box -> firc.
	if code, body := h.postSingboxControl("stop"); code != http.StatusOK {
		t.Fatalf("manual stop before rollback check failed: HTTP %d: %s", code, body)
	}
	h.routerFault.failPark.Store(true)
	if code, _ := h.postSwitchEngine("firc", true); code == http.StatusOK {
		t.Fatalf("expected switch_standby to fail on injected park error")
	}
	h.routerFault.failPark.Store(false)

	s = h.loadSettings()
	if s.SingboxRouter.RoutingEngine != "sing-box" || s.SingboxStoppedByEngine || !s.SingboxManuallyStopped {
		t.Fatalf("rollback failed to preserve manual-stop state: engine=%q byEngine=%v manual=%v",
			s.SingboxRouter.RoutingEngine, s.SingboxStoppedByEngine, s.SingboxManuallyStopped)
	}
	if running, _ := h.app.singboxOp.IsRunning(); running {
		t.Fatalf("rollback unexpectedly started manually stopped sing-box")
	}

	// Restore running sing-box, switch cleanly to firc with switch_and_stop, then test Direction 2: firc -> sing-box rollback.
	if code, body := h.postSingboxControl("start"); code != http.StatusOK {
		t.Fatalf("control start failed: HTTP %d: %s", code, body)
	}
	if err := h.app.routerSvc.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile before clean firc switch: %v", err)
	}
	if code, body := h.postSwitchEngine("firc", false); code != http.StatusOK {
		t.Fatalf("clean switch_and_stop to firc failed: HTTP %d: %s", code, body)
	}

	h.routerFault.failResume.Store(true)
	if code, _ := h.postSwitchEngine("sing-box", false); code == http.StatusOK {
		t.Fatalf("expected switch firc -> sing-box to fail on injected resume error")
	}
	h.routerFault.failResume.Store(false)

	if !sharedFirc.Load() {
		t.Fatalf("rollback failed to restart previous fircd")
	}
	s = h.loadSettings()
	if s.SingboxRouter.RoutingEngine != "firc" || !s.SingboxStoppedByEngine || s.SingboxManuallyStopped {
		t.Fatalf("rollback failed to restore firc + stopped-by-engine state: engine=%q byEngine=%v manual=%v",
			s.SingboxRouter.RoutingEngine, s.SingboxStoppedByEngine, s.SingboxManuallyStopped)
	}
	if running, _ := h.app.singboxOp.IsRunning(); running {
		t.Fatalf("rollback left candidate sing-box running")
	}
	st, err = h.app.routerSvc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus after firc -> sing-box rollback: %v", err)
	}
	if !st.ParkedByExternalEngine || st.Active || st.RoutingEngine != "firc" {
		t.Fatalf("rollback failed to keep router parked on firc: %+v", st)
	}
}

func TestFircEngineLifecycle_StopEngineSerializesWithInflightColdStart(t *testing.T) {
	runScenario := func(t *testing.T, triggerStop func(ctx context.Context, h *lifecycleHarness)) {
		t.Helper()
		ctx := context.Background()
		clashPort := pickFreeTCPPort(t)
		dataDir, singboxDir := seedLifecycleDiskState(t, t.TempDir(), clashPort)

		sharedFirc := &atomic.Bool{}
		h := startLifecycleApp(t, dataDir, singboxDir, clashPort, sharedFirc)
		h.bootstrapRunningSingboxWithActiveRouter(ctx)

		if err := h.app.singboxOp.Stop(); err != nil {
			t.Fatalf("initial Stop: %v", err)
		}

		checked := make(chan struct{})
		proceed := make(chan struct{})
		h.app.sbOrch.SetShouldRun(func() bool {
			allowed := !h.app.singboxOp.IsAutoStartSuppressed()
			close(checked)
			<-proceed
			return allowed
		})

		reloadDone := make(chan error, 1)
		go func() {
			reloadDone <- h.app.sbOrch.Reload()
		}()

		select {
		case <-checked:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for in-flight sbOrch.Reload() shouldRun check")
		}

		triggerStop(ctx, h)
		close(proceed)

		select {
		case err := <-reloadDone:
			if err != nil {
				t.Fatalf("sbOrch.Reload() returned unexpected error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sbOrch.Reload() completion")
		}

		if running, _ := h.app.singboxOp.IsRunning(); running {
			t.Fatalf("in-flight cold-start resurrected sing-box after engine stop (stoppedByEngine=%v)",
				h.app.singboxOp.IsStoppedByEngine())
		}
		if !h.app.singboxOp.IsStoppedByEngine() {
			t.Fatalf("expected SingboxStoppedByEngine=true")
		}
	}

	t.Run("paused_in_should_run", func(t *testing.T) {
		runScenario(t, func(ctx context.Context, h *lifecycleHarness) {
			hooks := h.app.buildProductionEngineSwitchHooks()
			if err := hooks.StopEngine(ctx, "sing-box"); err != nil {
				t.Fatalf("hooks.StopEngine(sing-box): %v", err)
			}
		})
	})

	t.Run("switch_and_stop_http_versus_inflight_cold_start", func(t *testing.T) {
		runScenario(t, func(_ context.Context, h *lifecycleHarness) {
			if code, body := h.postSwitchEngine("firc", false); code != http.StatusOK {
				t.Fatalf("postSwitchEngine(firc, false) failed: HTTP %d: %s", code, body)
			}
			if !h.fircRunning.Load() {
				t.Fatalf("expected fircd started after engine switch")
			}
		})
	})
}
