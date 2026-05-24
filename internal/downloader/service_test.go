package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	singboxorch "github.com/hoaxisr/awg-manager/internal/singbox/orchestrator"
)

type fakeOutboundsProvider struct {
	items []Outbound
}

func (f *fakeOutboundsProvider) ListDownloadOutbounds(context.Context) []Outbound {
	out := make([]Outbound, len(f.items))
	copy(out, f.items)
	return out
}

type fakeRouteProvider struct {
	route *Route
	err   error
}

func (f *fakeRouteProvider) GetDownloadRoute(context.Context) (*Route, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.route, nil
}

type fakeSingbox struct {
	running bool

	selectorCalls []string
	selectorErrs  []error

	activeNow  string
	activeErrs []error
}

func (f *fakeSingbox) IsRunning() (bool, int) {
	if f.running {
		return true, 123
	}
	return false, 0
}

func (f *fakeSingbox) SetSelectorDefault(_ context.Context, selectorTag, memberTag string) error {
	f.selectorCalls = append(f.selectorCalls, selectorTag+"="+memberTag)
	f.activeNow = memberTag
	if len(f.selectorErrs) == 0 {
		return nil
	}
	err := f.selectorErrs[0]
	f.selectorErrs = f.selectorErrs[1:]
	return err
}

func (f *fakeSingbox) GetSelectorActive(_ context.Context, _ string) (string, error) {
	if len(f.activeErrs) > 0 {
		err := f.activeErrs[0]
		f.activeErrs = f.activeErrs[1:]
		if err != nil {
			return "", err
		}
	}
	return f.activeNow, nil
}

type fakeSlot struct {
	saveCalls   int
	enableCalls []bool
	reloadCalls int

	saveErr        error
	enableErr      error
	enableTrueErr  error
	enableFalseErr error
	reloadErr      error

	lastSlot singboxorch.Slot
	lastJSON string
}

func (f *fakeSlot) SaveSilent(slot singboxorch.Slot, b []byte) error {
	f.saveCalls++
	f.lastSlot = slot
	f.lastJSON = string(b)
	return f.saveErr
}

func (f *fakeSlot) SetEnabledSilent(slot singboxorch.Slot, enabled bool) error {
	f.lastSlot = slot
	f.enableCalls = append(f.enableCalls, enabled)
	if enabled && f.enableTrueErr != nil {
		return f.enableTrueErr
	}
	if !enabled && f.enableFalseErr != nil {
		return f.enableFalseErr
	}
	return f.enableErr
}

func (f *fakeSlot) Reload() error {
	f.reloadCalls++
	return f.reloadErr
}

func TestListOutbounds_NoProvider(t *testing.T) {
	svc := NewService(Deps{})
	got := svc.ListOutbounds(context.Background())
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Tag != "direct" || !got[0].Available {
		t.Fatalf("unexpected fallback outbound: %+v", got[0])
	}
}

func TestResolveClient_Direct(t *testing.T) {
	svc := NewService(Deps{})
	lease, err := svc.ResolveClient(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolve direct nil route: %v", err)
	}
	if lease == nil || lease.Client == nil {
		t.Fatal("direct route should return lease with non-nil client")
	}
	if lease.Route.Tag != "direct" {
		t.Fatalf("route tag: got %q want direct", lease.Route.Tag)
	}
	lease.Close()
}

func TestResolveClient_NonDirectWithoutSingbox(t *testing.T) {
	svc := NewService(Deps{})
	_, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-a"})
	if err == nil || !strings.Contains(err.Error(), "operator is not configured") {
		t.Fatalf("expected operator not configured error, got %v", err)
	}
}

func TestResolveClient_NonDirectWithoutSlot(t *testing.T) {
	svc := NewService(Deps{
		Singbox: &fakeSingbox{running: true},
	})
	_, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-a"})
	if err == nil || !strings.Contains(err.Error(), "orchestrator is not configured") {
		t.Fatalf("expected orchestrator not configured error, got %v", err)
	}
}

func TestResolveClient_NonDirectSingboxNotRunning(t *testing.T) {
	svc := NewService(Deps{
		Singbox: &fakeSingbox{running: false},
		Slot:    &fakeSlot{},
	})
	_, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-a"})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected not running error, got %v", err)
	}
}

func TestResolveClient_UsesRouteProviderWhenRouteNil(t *testing.T) {
	svc := NewService(Deps{
		RouteProvider: &fakeRouteProvider{route: &Route{Tag: "direct"}},
	})
	lease, err := svc.ResolveClient(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolve with route provider: %v", err)
	}
	if lease.Route.Tag != "direct" {
		t.Fatalf("route tag = %q, want direct", lease.Route.Tag)
	}
	lease.Close()
}

func TestResolveClient_ExplicitRouteOverridesProvider(t *testing.T) {
	svc := NewService(Deps{
		RouteProvider: &fakeRouteProvider{route: &Route{Tag: "awg-test"}},
	})
	lease, err := svc.ResolveClient(context.Background(), &Route{Tag: "direct"})
	if err != nil {
		t.Fatalf("resolve explicit route: %v", err)
	}
	if lease.Route.Tag != "direct" {
		t.Fatalf("route tag = %q, want direct", lease.Route.Tag)
	}
	lease.Close()
}

func TestResolveClient_RouteProviderError(t *testing.T) {
	svc := NewService(Deps{
		RouteProvider: &fakeRouteProvider{err: errors.New("settings read failed")},
	})
	_, err := svc.ResolveClient(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "load download route settings") {
		t.Fatalf("expected route provider error, got %v", err)
	}
}

func TestResolveClient_UsesRouteProviderForRoutedDownload(t *testing.T) {
	sb := &fakeSingbox{running: true}
	slot := &fakeSlot{}
	svc := NewService(Deps{
		Outbounds: &fakeOutboundsProvider{
			items: []Outbound{
				{Tag: "direct", Kind: "direct", Label: "Direct (WAN)"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test"},
			},
		},
		Singbox:       sb,
		Slot:          slot,
		RouteProvider: &fakeRouteProvider{route: &Route{Tag: "awg-test"}},
	})

	lease, err := svc.ResolveClient(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolve with routed provider: %v", err)
	}
	if lease == nil || lease.Client == nil {
		t.Fatal("expected non-nil lease and client")
	}
	if lease.Route.Tag != "awg-test" {
		t.Fatalf("route tag = %q, want awg-test", lease.Route.Tag)
	}
	if slot.saveCalls != 1 {
		t.Fatalf("SaveSilent calls: got %d want 1", slot.saveCalls)
	}
	if len(sb.selectorCalls) == 0 || !strings.Contains(sb.selectorCalls[0], "=awg-test") {
		t.Fatalf("expected selector set to awg-test, got %v", sb.selectorCalls)
	}

	lease.Close()
	if len(slot.enableCalls) < 2 || slot.enableCalls[len(slot.enableCalls)-1] != false {
		t.Fatalf("expected slot disable on close, got %v", slot.enableCalls)
	}
}

func TestResolveClient_HappyPath(t *testing.T) {
	sb := &fakeSingbox{running: true}
	slot := &fakeSlot{}
	svc := NewService(Deps{
		Outbounds: &fakeOutboundsProvider{
			items: []Outbound{
				{Tag: "direct", Kind: "direct", Label: "Direct (WAN)"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test duplicate"},
				{Tag: " ", Kind: "bad", Label: "bad"},
			},
		},
		Singbox: sb,
		Slot:    slot,
	})

	lease, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-test"})
	if err != nil {
		t.Fatalf("resolve routed lease: %v", err)
	}
	if lease == nil || lease.Client == nil {
		t.Fatal("expected non-nil lease and client")
	}
	if lease.Route.Tag != "awg-test" {
		t.Fatalf("route tag = %q, want awg-test", lease.Route.Tag)
	}
	if slot.saveCalls != 1 {
		t.Fatalf("SaveSilent calls: got %d want 1", slot.saveCalls)
	}
	if slot.lastSlot != singboxorch.SlotDownloadProxy {
		t.Fatalf("last slot: got %q want %q", slot.lastSlot, singboxorch.SlotDownloadProxy)
	}
	assertSlotJSON(t, slot.lastJSON)
	if len(slot.enableCalls) < 1 || !slot.enableCalls[0] {
		t.Fatalf("expected first enable call true, got %v", slot.enableCalls)
	}
	if slot.reloadCalls < 1 {
		t.Fatalf("expected reload on enable, got %d", slot.reloadCalls)
	}

	lease.Close()
	lease.Close()

	if len(sb.selectorCalls) < 2 || sb.selectorCalls[len(sb.selectorCalls)-1] != "awgm-download-selector=direct" {
		t.Fatalf("expected selector restore to direct, got %v", sb.selectorCalls)
	}
	if len(slot.enableCalls) < 2 || slot.enableCalls[len(slot.enableCalls)-1] != false {
		t.Fatalf("expected disable call on cleanup, got %v", slot.enableCalls)
	}
	if slot.reloadCalls < 2 {
		t.Fatalf("expected second reload on cleanup, got %d", slot.reloadCalls)
	}

	lease2, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-test"})
	if err != nil {
		t.Fatalf("second resolve should not deadlock: %v", err)
	}
	lease2.Close()
}

func TestLeaseClose_Idempotent(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	lease := &Lease{
		cleanup: func() {
			mu.Lock()
			defer mu.Unlock()
			calls++
		},
	}
	lease.Close()
	lease.Close()
	lease.Close()
	if calls != 1 {
		t.Fatalf("cleanup calls: got %d want 1", calls)
	}
}

func TestResolveClient_ReloadFailureDisablesSlotAndUnlocks(t *testing.T) {
	sb := &fakeSingbox{running: true}
	slot := &fakeSlot{reloadErr: errors.New("reload failed")}
	svc := NewService(Deps{
		Outbounds: &fakeOutboundsProvider{
			items: []Outbound{
				{Tag: "direct", Kind: "direct", Label: "Direct (WAN)"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test"},
			},
		},
		Singbox: sb,
		Slot:    slot,
	})

	_, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-test"})
	if err == nil || !strings.Contains(err.Error(), "reload sing-box with download transport slot") {
		t.Fatalf("expected reload error, got %v", err)
	}
	if len(slot.enableCalls) < 2 || slot.enableCalls[len(slot.enableCalls)-1] != false {
		t.Fatalf("expected disable call after reload failure, got %v", slot.enableCalls)
	}

	slot.reloadErr = nil
	lease2, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-test"})
	if err != nil {
		t.Fatalf("second resolve should not deadlock: %v", err)
	}
	lease2.Close()
}

func TestResolveClient_EnableFailureCleansUpAndUnlocks(t *testing.T) {
	sb := &fakeSingbox{running: true}
	slot := &fakeSlot{enableTrueErr: errors.New("enable failed")}
	svc := NewService(Deps{
		Outbounds: &fakeOutboundsProvider{
			items: []Outbound{
				{Tag: "direct", Kind: "direct", Label: "Direct (WAN)"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test"},
			},
		},
		Singbox: sb,
		Slot:    slot,
	})

	_, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-test"})
	if err == nil || !strings.Contains(err.Error(), "enable download transport slot") {
		t.Fatalf("expected enable error, got %v", err)
	}
	if len(slot.enableCalls) < 2 || !slot.enableCalls[0] || slot.enableCalls[1] {
		t.Fatalf("expected enable=true then enable=false, got %v", slot.enableCalls)
	}
	if slot.reloadCalls < 1 {
		t.Fatalf("expected cleanup reload call, got %d", slot.reloadCalls)
	}

	slot.enableTrueErr = nil
	lease2, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-test"})
	if err != nil {
		t.Fatalf("second resolve should not deadlock: %v", err)
	}
	lease2.Close()
}

func TestResolveClient_SelectFailureCleansUpAndUnlocks(t *testing.T) {
	sb := &fakeSingbox{
		running: true,
		selectorErrs: []error{
			errors.New("not ready"),
		},
	}
	slot := &fakeSlot{}
	svc := NewService(Deps{
		Outbounds: &fakeOutboundsProvider{
			items: []Outbound{
				{Tag: "direct", Kind: "direct", Label: "Direct (WAN)"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test"},
			},
		},
		Singbox: sb,
		Slot:    slot,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := svc.ResolveClient(ctx, &Route{Tag: "awg-test"})
	if err == nil || !strings.Contains(err.Error(), "select download outbound") {
		t.Fatalf("expected select error, got %v", err)
	}
	if len(slot.enableCalls) < 2 || !slot.enableCalls[0] || slot.enableCalls[len(slot.enableCalls)-1] {
		t.Fatalf("expected slot enable then disable, got %v", slot.enableCalls)
	}

	sb.selectorErrs = nil
	lease2, err := svc.ResolveClient(context.Background(), &Route{Tag: "awg-test"})
	if err != nil {
		t.Fatalf("second resolve should not deadlock: %v", err)
	}
	lease2.Close()
}

func TestListOutbounds_WithProviderNotRunning(t *testing.T) {
	svc := NewService(Deps{
		Outbounds: &fakeOutboundsProvider{
			items: []Outbound{
				{Tag: "direct", Kind: "direct", Label: "Direct (WAN)"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test"},
			},
		},
		Singbox: &fakeSingbox{running: false},
		Slot:    &fakeSlot{},
	})

	got := svc.ListOutbounds(context.Background())
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !got[0].Available {
		t.Fatalf("direct must be available: %+v", got[0])
	}
	if got[1].Available {
		t.Fatalf("non-direct must be unavailable when sing-box not running: %+v", got[1])
	}
}

func TestListOutbounds_WithProviderRunningAndSlot(t *testing.T) {
	svc := NewService(Deps{
		Outbounds: &fakeOutboundsProvider{
			items: []Outbound{
				{Tag: "direct", Kind: "direct", Label: "Direct (WAN)"},
				{Tag: "awg-test", Kind: "awg", Label: "AWG test"},
			},
		},
		Singbox: &fakeSingbox{running: true},
		Slot:    &fakeSlot{},
	})

	got := svc.ListOutbounds(context.Background())
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !got[0].Available || !got[1].Available {
		t.Fatalf("both direct and non-direct must be available: %+v", got)
	}
}

func assertSlotJSON(t *testing.T, raw string) {
	t.Helper()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("unmarshal slot json: %v", err)
	}

	inboundsAny, ok := parsed["inbounds"].([]interface{})
	if !ok || len(inboundsAny) != 1 {
		t.Fatalf("inbounds: got %T %v", parsed["inbounds"], parsed["inbounds"])
	}
	inbound, ok := inboundsAny[0].(map[string]interface{})
	if !ok {
		t.Fatalf("inbound type: %T", inboundsAny[0])
	}
	if inbound["type"] != "mixed" || inbound["tag"] != "awgm-download-in" || inbound["listen"] != "127.0.0.1" {
		t.Fatalf("unexpected inbound: %+v", inbound)
	}
	if int(inbound["listen_port"].(float64)) != 11998 {
		t.Fatalf("inbound listen_port = %v, want 11998", inbound["listen_port"])
	}

	outboundsAny, ok := parsed["outbounds"].([]interface{})
	if !ok || len(outboundsAny) != 1 {
		t.Fatalf("outbounds: got %T %v", parsed["outbounds"], parsed["outbounds"])
	}
	selector, ok := outboundsAny[0].(map[string]interface{})
	if !ok {
		t.Fatalf("selector type: %T", outboundsAny[0])
	}
	if selector["type"] != "selector" || selector["tag"] != "awgm-download-selector" {
		t.Fatalf("unexpected selector: %+v", selector)
	}
	if selector["default"] != "awg-test" {
		t.Fatalf("selector default = %v, want awg-test", selector["default"])
	}
	if selector["interrupt_exist_connections"] != false {
		t.Fatalf("interrupt_exist_connections = %v, want false", selector["interrupt_exist_connections"])
	}
	membersAny, ok := selector["outbounds"].([]interface{})
	if !ok {
		t.Fatalf("selector outbounds type: %T", selector["outbounds"])
	}
	members := make([]string, 0, len(membersAny))
	for _, m := range membersAny {
		members = append(members, m.(string))
	}
	if len(members) != 2 || members[0] != "direct" || members[1] != "awg-test" {
		t.Fatalf("selector members = %v, want [direct awg-test]", members)
	}

	routeAny, ok := parsed["route"].(map[string]interface{})
	if !ok {
		t.Fatalf("route type: %T", parsed["route"])
	}
	rulesAny, ok := routeAny["rules"].([]interface{})
	if !ok || len(rulesAny) != 1 {
		t.Fatalf("route.rules: got %T %v", routeAny["rules"], routeAny["rules"])
	}
	rule, ok := rulesAny[0].(map[string]interface{})
	if !ok {
		t.Fatalf("route rule type: %T", rulesAny[0])
	}
	if rule["inbound"] != "awgm-download-in" || rule["outbound"] != "awgm-download-selector" {
		t.Fatalf("unexpected route rule: %+v", rule)
	}
}
