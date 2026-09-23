package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/aiassistant"
	"github.com/hoaxisr/awg-manager/internal/api"
	"github.com/hoaxisr/awg-manager/internal/mihomo"
	"github.com/hoaxisr/awg-manager/internal/mihomonative"
	singboxorch "github.com/hoaxisr/awg-manager/internal/singbox/orchestrator"
	"github.com/hoaxisr/awg-manager/internal/singbox/router"
	"github.com/hoaxisr/awg-manager/internal/singbox/subscription"
	"github.com/hoaxisr/awg-manager/internal/storage"
)

type degradedMockMutationApplier struct {
	degraded atomic.Bool
	calls    atomic.Int32
}

func (m *degradedMockMutationApplier) IsDegraded() bool {
	return m.degraded.Load()
}

func (m *degradedMockMutationApplier) CheckMutationAllowed() error {
	if m.degraded.Load() {
		return api.ErrRecoveryRequired
	}
	return nil
}

func (m *degradedMockMutationApplier) ApplyNativeMutation(_ context.Context, fn func() error) error {
	if err := m.CheckMutationAllowed(); err != nil {
		return err
	}
	m.calls.Add(1)
	return fn()
}

func (m *degradedMockMutationApplier) ApplyNativeMutationWithOutcome(_ context.Context, fn func() error) (*mihomo.MutationOutcome, error) {
	if err := m.CheckMutationAllowed(); err != nil {
		return nil, err
	}
	m.calls.Add(1)
	return &mihomo.MutationOutcome{ApplyPath: mihomo.ApplyPathHotReload}, fn()
}

func (m *degradedMockMutationApplier) ApplyDraftOnly(_ context.Context, fn func() error) error {
	if err := m.CheckMutationAllowed(); err != nil {
		return err
	}
	m.calls.Add(1)
	return fn()
}

func (m *degradedMockMutationApplier) ApplyPendingDraft(_ context.Context) error {
	if err := m.CheckMutationAllowed(); err != nil {
		return err
	}
	m.calls.Add(1)
	return nil
}

func (m *degradedMockMutationApplier) Reconcile(_ context.Context, _ string, _ bool) error {
	return nil
}

func (m *degradedMockMutationApplier) ExportEvidence(_ context.Context) (*mihomo.RecoveryEvidenceDTO, error) {
	return nil, nil
}

type degradedMockRouterService struct {
	router.Service
	settings    storage.SingboxRouterSettings
	modeCalls   int
	updateCalls int
}

func (m *degradedMockRouterService) GetSettings(context.Context) (storage.SingboxRouterSettings, error) {
	return m.settings, nil
}

func (m *degradedMockRouterService) UpdateSettings(_ context.Context, s storage.SingboxRouterSettings) error {
	m.updateCalls++
	m.settings = s
	return nil
}

func (m *degradedMockRouterService) SwitchRoutingMode(_ context.Context, mode string) error {
	m.modeCalls++
	m.settings.RoutingMode = mode
	return nil
}

type degradedMockPresenceProbe struct{}

func (degradedMockPresenceProbe) IsPresent() bool { return true }

type degradedMockSingboxProc struct {
	reloadCalls atomic.Int32
	stopCalls   atomic.Int32
}

func (p *degradedMockSingboxProc) Reload() error {
	p.reloadCalls.Add(1)
	return nil
}

func (p *degradedMockSingboxProc) Start() error { return nil }
func (p *degradedMockSingboxProc) Stop() error {
	p.stopCalls.Add(1)
	return nil
}
func (p *degradedMockSingboxProc) IsRunning() (bool, int) { return true, 1234 }

type degradedNoopMutator struct{}

func (degradedNoopMutator) AllocListenPort() (uint16, error)                    { return 11000, nil }
func (degradedNoopMutator) AllocProxyIndex(context.Context) (int, error)        { return 1, nil }
func (degradedNoopMutator) AddOutbound(string, []byte) error                    { return nil }
func (degradedNoopMutator) UpdateOutbound(string, []byte) error                 { return nil }
func (degradedNoopMutator) RemoveOutbound(string) error                         { return nil }
func (degradedNoopMutator) AddInbound(string, []byte) error                     { return nil }
func (degradedNoopMutator) RemoveInbound(string) error                          { return nil }
func (degradedNoopMutator) AddRouteRule([]byte) error                           { return nil }
func (degradedNoopMutator) RemoveRouteRule(string, string) error                { return nil }
func (degradedNoopMutator) EnsureProxy(context.Context, int, int, string) error { return nil }
func (degradedNoopMutator) RemoveProxy(context.Context, int) error              { return nil }
func (degradedNoopMutator) Reload(context.Context) error                        { return nil }
func (degradedNoopMutator) Rollback()                                           {}
func (degradedNoopMutator) SelectClashProxy(string, string) error               { return nil }
func (degradedNoopMutator) GetClashSelectorActive(string) (string, error)       { return "", nil }
func (degradedNoopMutator) DeclaredOutboundTags() []string                      { return nil }
func (degradedNoopMutator) SubscriptionOutbounds() []map[string]any             { return nil }

func TestAIActionCallbacks_DegradedMihomo_RestartAndReload(t *testing.T) {
	ctx := context.Background()
	applier := &degradedMockMutationApplier{}
	applier.degraded.Store(true)

	mh := api.NewMihomoHandler(nil)
	mh.SetMutationApplier(applier)

	restartCalls := 0
	mh.SetRestartFunc(func() error {
		restartCalls++
		return nil
	})

	reloadCalls := 0
	mh.SetReloadFunc(func() error {
		reloadCalls++
		return nil
	})

	s := &Server{mihomoHandler: mh}
	handlers := s.buildAIActionHandlers(nil)
	registry := aiassistant.NewActionRegistry(handlers)

	// 1. mihomo.restart rejected
	err := registry.Apply(ctx, "mihomo.restart", "")
	if err == nil || !errors.Is(err, api.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired for mihomo.restart, got: %v", err)
	}
	if restartCalls != 0 {
		t.Fatalf("expected 0 restart calls beneath gate, got %d", restartCalls)
	}

	// 2. mihomo.reload rejected
	err = registry.Apply(ctx, "mihomo.reload", "")
	if err == nil || !errors.Is(err, api.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired for mihomo.reload, got: %v", err)
	}
	if reloadCalls != 0 {
		t.Fatalf("expected 0 reload calls beneath gate, got %d", reloadCalls)
	}
}

func TestAIActionCallbacks_DegradedMihomo_RoutingReapply(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	settingsStore := storage.NewSettingsStore(tmpDir)

	applier := &degradedMockMutationApplier{}
	applier.degraded.Store(true)

	mh := api.NewMihomoHandler(nil)
	mh.SetMutationApplier(applier)

	reloadCalls := 0
	mh.SetReloadFunc(func() error {
		reloadCalls++
		return nil
	})

	sbProc := &degradedMockSingboxProc{}
	sbOrch := singboxorch.New(filepath.Join(tmpDir, "sb"), sbProc)

	s := &Server{
		settings:      settingsStore,
		mihomoHandler: mh,
		singboxOrch:   sbOrch,
	}
	handlers := s.buildAIActionHandlers(nil)
	registry := aiassistant.NewActionRegistry(handlers)

	// Case A: routing engine is mihomo -> rejected
	err := settingsStore.Update(func(cfg *storage.Settings) error {
		cfg.SingboxRouter.RoutingEngine = "mihomo"
		return nil
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	err = registry.Apply(ctx, "routing.reapply", "")
	if err == nil || !errors.Is(err, api.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired for routing.reapply on mihomo, got: %v", err)
	}
	if reloadCalls != 0 {
		t.Fatalf("expected 0 reload calls, got %d", reloadCalls)
	}

	// Case B: routing engine is sing-box -> delegates to sing-box reload, unaffected by degraded mihomo
	err = settingsStore.Update(func(cfg *storage.Settings) error {
		cfg.SingboxRouter.RoutingEngine = "sing-box"
		return nil
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	err = registry.Apply(ctx, "routing.reapply", "")
	if err != nil {
		t.Fatalf("expected nil error for routing.reapply on sing-box, got: %v", err)
	}
	if sbProc.stopCalls.Load() == 0 && sbProc.reloadCalls.Load() == 0 {
		t.Fatalf("expected sing-box process controller to be invoked by orchestrator reload")
	}
}

func TestAIActionCallbacks_DegradedMihomo_SubscriptionUpdate(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Sing-box subscription setup with one inline subscription
	subStore, err := subscription.NewStore(filepath.Join(tmpDir, "sub.json"))
	if err != nil {
		t.Fatalf("subscription.NewStore: %v", err)
	}
	subSvc := subscription.NewService(subStore, degradedNoopMutator{})
	singboxSub, err := subSvc.Create(ctx, subscription.CreateInput{
		Label:   "Test Singbox Sub",
		Inline:  "vless://00000000-0000-0000-0000-000000000001@example.com:443?security=tls#tag-1",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("subSvc.Create: %v", err)
	}
	singboxSubID := singboxSub.ID

	subHandler := api.NewSubscriptionHandler(subSvc, degradedMockPresenceProbe{})

	// Degraded Mihomo handler
	applier := &degradedMockMutationApplier{}
	applier.degraded.Store(true)

	mh := api.NewMihomoHandler(nil)
	mh.SetMutationApplier(applier)
	nativeStorePath := filepath.Join(tmpDir, "mihomo_native.json")
	nativeStore, err := mihomonative.NewStore(nativeStorePath)
	if err != nil {
		t.Fatalf("mihomonative.NewStore: %v", err)
	}
	mh.SetNativeStore(nativeStore)

	s := &Server{
		subscriptionHandler: subHandler,
		mihomoHandler:       mh,
	}
	handlers := s.buildAIActionHandlers(nil)
	registry := aiassistant.NewActionRegistry(handlers)

	// 1. Sing-box subscription update succeeds and is NOT blocked by Mihomo degradation
	err = registry.Apply(ctx, "subscription.update", singboxSubID)
	if err != nil {
		t.Fatalf("expected nil error for sing-box subscription update, got: %v", err)
	}

	// 2. Native Mihomo subscription (not found in sing-box) falls through and is rejected by degraded gate
	err = registry.Apply(ctx, "subscription.update", "sub-mihomo-native")
	if err == nil || !errors.Is(err, api.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired for native mihomo subscription, got: %v", err)
	}
	if applier.calls.Load() != 0 {
		t.Fatalf("expected 0 applier calls, got %d", applier.calls.Load())
	}
}

func TestAIActionCallbacks_DegradedMihomo_SwitchRoutingEngineAndMode(t *testing.T) {
	ctx := context.Background()
	applier := &degradedMockMutationApplier{}
	applier.degraded.Store(true)

	routerMock := &degradedMockRouterService{
		settings: storage.SingboxRouterSettings{
			Enabled:       true,
			RoutingEngine: "sing-box",
			RoutingMode:   "tproxy",
		},
	}

	mh := api.NewMihomoHandler(nil)
	mh.SetMutationApplier(applier)
	mh.SetRouterService(routerMock)

	s := &Server{mihomoHandler: mh}
	handlers := s.buildAIActionHandlers(nil)
	registry := aiassistant.NewActionRegistry(handlers)

	// 1. Switching to mihomo while degraded -> rejected
	err := registry.Apply(ctx, "routing.switch_engine", "mihomo")
	if err == nil || !errors.Is(err, api.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired when switching to mihomo, got: %v", err)
	}
	if routerMock.updateCalls != 0 {
		t.Fatalf("expected 0 UpdateSettings calls, got %d", routerMock.updateCalls)
	}

	// 2. Switching mode while active engine is sing-box -> allowed
	err = registry.Apply(ctx, "routing.switch_mode", "fakeip-tun")
	if err != nil {
		t.Fatalf("expected nil error when switching mode in sing-box, got: %v", err)
	}
	if routerMock.modeCalls != 1 {
		t.Fatalf("expected 1 SwitchRoutingMode call, got %d", routerMock.modeCalls)
	}

	// 3. Set active engine to mihomo
	routerMock.settings.RoutingEngine = "mihomo"

	// Switching mode while active engine is mihomo -> rejected
	err = registry.Apply(ctx, "routing.switch_mode", "policy-tun")
	if err == nil || !errors.Is(err, api.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired when switching mode with active mihomo, got: %v", err)
	}
	if routerMock.modeCalls != 1 {
		t.Fatalf("expected 0 additional SwitchRoutingMode calls, got %d", routerMock.modeCalls)
	}

	// Switching engine away from mihomo while mihomo is degraded -> rejected
	err = registry.Apply(ctx, "routing.switch_engine", "sing-box")
	if err == nil || !errors.Is(err, api.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired when switching away from degraded mihomo, got: %v", err)
	}
	if routerMock.updateCalls != 0 {
		t.Fatalf("expected 0 UpdateSettings calls, got %d", routerMock.updateCalls)
	}
}

func TestAIActionCallbacks_HealthyMihomo_AllowsMutations(t *testing.T) {
	ctx := context.Background()
	applier := &degradedMockMutationApplier{}
	applier.degraded.Store(false) // healthy

	routerMock := &degradedMockRouterService{
		settings: storage.SingboxRouterSettings{
			Enabled:       true,
			RoutingEngine: "mihomo",
			RoutingMode:   "tproxy",
		},
	}

	restartCalled := false
	reloadCalled := false

	mh := api.NewMihomoHandler(nil)
	mh.SetMutationApplier(applier)
	mh.SetRouterService(routerMock)
	mh.SetRestartFunc(func() error {
		restartCalled = true
		return nil
	})
	mh.SetReloadFunc(func() error {
		reloadCalled = true
		return nil
	})

	s := &Server{mihomoHandler: mh}
	handlers := s.buildAIActionHandlers(nil)
	registry := aiassistant.NewActionRegistry(handlers)

	// Healthy restart
	if err := registry.Apply(ctx, "mihomo.restart", ""); err != nil {
		t.Fatalf("expected healthy restart to succeed, got: %v", err)
	}
	if !restartCalled {
		t.Fatal("expected restart to be called")
	}

	// Healthy reload
	if err := registry.Apply(ctx, "mihomo.reload", ""); err != nil {
		t.Fatalf("expected healthy reload to succeed, got: %v", err)
	}
	if !reloadCalled {
		t.Fatal("expected reload to be called")
	}

	// Healthy mode switch
	if err := registry.Apply(ctx, "routing.switch_mode", "fakeip-tun"); err != nil {
		t.Fatalf("expected healthy mode switch to succeed, got: %v", err)
	}
	if routerMock.modeCalls != 1 {
		t.Fatalf("expected 1 mode call, got %d", routerMock.modeCalls)
	}
}
