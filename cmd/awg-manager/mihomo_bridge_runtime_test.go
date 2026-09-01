package main

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/mihomonative"
)

type flakyBridgeProxyRegistrar struct {
	failOnce map[int]bool
	active   map[int]bool
	attempts map[int]int
	removed  []int
}

func (f *flakyBridgeProxyRegistrar) NextFreeIndex(context.Context, map[int]bool) (int, error) {
	return 0, nil
}
func (f *flakyBridgeProxyRegistrar) ReleaseProxyIndex(int) {}
func (f *flakyBridgeProxyRegistrar) LookupProxy(context.Context, int) (string, bool, error) {
	return "", false, nil
}
func (f *flakyBridgeProxyRegistrar) EnsureProxy(context.Context, int, int, string) error {
	return nil
}
func (f *flakyBridgeProxyRegistrar) EnsureProxyIfOwned(context.Context, int, int, string, ...string) (bool, error) {
	return true, nil
}
func (f *flakyBridgeProxyRegistrar) RemoveProxyIfOwned(_ context.Context, index int, _ string, _ ...string) (bool, error) {
	f.attempts[index]++
	if f.failOnce[index] {
		delete(f.failOnce, index)
		return false, errors.New("transient NDMS remove failure")
	}
	if !f.active[index] {
		return true, nil
	}
	delete(f.active, index)
	f.removed = append(f.removed, index)
	return true, nil
}

func TestWaitForMihomoBridgeListenersRequiresBoundListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := waitForMihomoBridgeListeners(context.Background(), []mihomonative.BridgeListener{{Port: port}}); err != nil {
		t.Fatalf("ready listener rejected: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForMihomoBridgeListeners(cancelled, []mihomonative.BridgeListener{{Port: port + 1}}); err == nil {
		t.Fatal("unbound listener unexpectedly passed readiness")
	}
}

func TestMihomoBridgeRuntimeActivateNDMSOffDoesNotWaitOrPublish(t *testing.T) {
	store, err := mihomonative.NewStore(filepath.Join(t.TempDir(), "native.json"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := store.CreateProxy("vless://11111111-1111-1111-1111-111111111111@native.example:443?type=xhttp#Native", mihomonative.EngineMihomo, mihomonative.EngineMihomo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetBridge("proxy", nodes[0].ID, mihomonative.ProxyBridge{
		ListenPort: 12999, ProxyIndex: 7, ProxyInterface: "Proxy7", KernelInterface: "t2s7",
	}); err != nil {
		t.Fatal(err)
	}
	gate := &gatedBridgeRegistrar{}
	manager := mihomonative.NewBridgeManager(store, gate, func() bool { return false })
	runtime := newMihomoBridgeRuntime(store, manager, gate)

	started := time.Now()
	if err := runtime.activate(context.Background()); err != nil {
		t.Fatalf("activate() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("NDMS-off activation waited for absent listener: %s", elapsed)
	}
	if gate.isReady() {
		t.Fatal("NDMS-off activation opened advertisement gate")
	}
}

func TestMihomoBridgeRuntimeStaleExitDoesNotCloseNewGenerationGate(t *testing.T) {
	gate := &gatedBridgeRegistrar{}
	gate.setReady(true)
	runtime := &mihomoBridgeRuntime{gate: gate}

	if err := runtime.deactivateIf(context.Background(), func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	if !gate.isReady() {
		t.Fatal("stale exit withdrew exports from a newer generation")
	}
	if err := runtime.deactivateIf(context.Background(), func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if gate.isReady() {
		t.Fatal("current generation exit did not close advertisement gate")
	}
}

func TestMihomoBridgeRuntimeUnexpectedExitWithdrawsAllAndRetriesFailures(t *testing.T) {
	store, err := mihomonative.NewStore(filepath.Join(t.TempDir(), "native.json"))
	if err != nil {
		t.Fatal(err)
	}
	firstURI := "vless://11111111-1111-1111-1111-111111111111@one.example:443#One"
	secondURI := "vless://22222222-2222-2222-2222-222222222222@two.example:443#Two"
	first, err := store.CreateProxy(firstURI, mihomonative.EngineMihomo, mihomonative.EngineMihomo)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateProxy(secondURI, mihomonative.EngineMihomo, mihomonative.EngineMihomo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetBridge("proxy", first[0].ID, mihomonative.ProxyBridge{ListenPort: 12003, ProxyIndex: 3, ProxyInterface: "Proxy3", KernelInterface: "t2s3"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetBridge("proxy", second[0].ID, mihomonative.ProxyBridge{ListenPort: 12004, ProxyIndex: 4, ProxyInterface: "Proxy4", KernelInterface: "t2s4"}); err != nil {
		t.Fatal(err)
	}
	// Fail-safe withdrawal must also clean a persisted disabled bridge: its
	// earlier disable transaction may have failed after flipping the store.
	if _, err := store.UpdateProxy(second[0].ID, mihomonative.UpdateProxyInput{
		URI: secondURI, EnginePreference: mihomonative.EngineMihomo,
		RoutingEngine: mihomonative.EngineMihomo, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	base := &flakyBridgeProxyRegistrar{
		failOnce: map[int]bool{3: true},
		active:   map[int]bool{3: true, 4: true},
		attempts: make(map[int]int),
	}
	gate := &gatedBridgeRegistrar{base: base}
	gate.setReady(true)
	runtime := &mihomoBridgeRuntime{store: store, gate: gate}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := runtime.deactivateAfterUnexpectedExit(ctx, func() bool { return true }); err != nil {
		t.Fatalf("deactivateAfterUnexpectedExit() error = %v", err)
	}
	if gate.isReady() {
		t.Fatal("unexpected exit left publication gate open")
	}
	if len(base.active) != 0 {
		t.Fatalf("active ProxyN after retry = %v", base.active)
	}
	if base.attempts[3] < 2 {
		t.Fatalf("failed first Proxy3 was not retried: attempts=%v", base.attempts)
	}
	if len(base.removed) != 2 || base.removed[0] != 4 || base.removed[1] != 3 {
		t.Fatalf("successful removal order = %v, want [4 3] (second removed before first retry)", base.removed)
	}
}

type fakeNDMSRegistrarWithOwnership struct {
	descriptions map[int]string
	occupied     map[int]bool
	ensured      map[int]string
}

func (f *fakeNDMSRegistrarWithOwnership) NextFreeIndex(context.Context, map[int]bool) (int, error) {
	return 99, nil
}
func (f *fakeNDMSRegistrarWithOwnership) ReleaseProxyIndex(int) {}
func (f *fakeNDMSRegistrarWithOwnership) LookupProxy(_ context.Context, index int) (string, bool, error) {
	if !f.occupied[index] {
		return "", false, nil
	}
	return f.descriptions[index], true, nil
}
func (f *fakeNDMSRegistrarWithOwnership) EnsureProxy(_ context.Context, index, port int, desc string) error {
	f.occupied[index] = true
	f.descriptions[index] = desc
	f.ensured[index] = desc
	return nil
}
func (f *fakeNDMSRegistrarWithOwnership) EnsureProxyIfOwned(ctx context.Context, index, port int, owner string, legacy ...string) (bool, error) {
	if f.occupied[index] {
		cur := f.descriptions[index]
		owned := cur == owner
		for _, leg := range legacy {
			if leg != "" && cur == leg {
				owned = true
				break
			}
		}
		if !owned {
			return false, nil
		}
	}
	return true, f.EnsureProxy(ctx, index, port, owner)
}
func (f *fakeNDMSRegistrarWithOwnership) RemoveProxyIfOwned(context.Context, int, string, ...string) (bool, error) {
	return true, nil
}

func TestMihomoBridgeRuntime_PrepareRetainsLegacyOwnerUntilActivate(t *testing.T) {
	store, err := mihomonative.NewStore(filepath.Join(t.TempDir(), "native.json"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := store.CreateProxy("vless://11111111-1111-1111-1111-111111111111@native.example:443?type=xhttp#LegacyNode", mihomonative.EngineMihomo, mihomonative.EngineMihomo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetBridge("proxy", nodes[0].ID, mihomonative.ProxyBridge{
		ListenPort: 12007, ProxyIndex: 7, ProxyInterface: "Proxy7", KernelInterface: "t2s7",
		LegacyOwner: "LegacyNode",
	}); err != nil {
		t.Fatal(err)
	}

	base := &fakeNDMSRegistrarWithOwnership{
		descriptions: map[int]string{7: "LegacyNode"},
		occupied:     map[int]bool{7: true},
		ensured:      make(map[int]string),
	}
	gate := &gatedBridgeRegistrar{base: base}
	manager := mihomonative.NewBridgeManager(store, gate, func() bool { return true })
	runtime := newMihomoBridgeRuntime(store, manager, gate)

	// Step 1: Prepare (gate is closed)
	if err := runtime.prepare(context.Background(), nil); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	// Verify that during prepare, LegacyOwner was NOT cleared and NDMS description was NOT overwritten yet
	pAfterPrepare, err := store.GetProxy(nodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if pAfterPrepare.Bridge.LegacyOwner != "LegacyNode" {
		t.Fatalf("prepare prematurely cleared LegacyOwner: %+v", pAfterPrepare.Bridge)
	}
	if base.descriptions[7] != "LegacyNode" {
		t.Fatalf("prepare prematurely updated NDMS description: %q", base.descriptions[7])
	}

	// Step 2: Open listener so readiness check passes
	listener, err := net.Listen("tcp", "127.0.0.1:12007")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	// Step 3: Readiness / Activate (gate opens, publication occurs)
	if err := runtime.activate(context.Background()); err != nil {
		t.Fatalf("activate() error = %v", err)
	}

	// Verify that after activate, NDMS description is updated to canonical token AND LegacyOwner is cleared
	canonicalOwner := mihomonative.BridgeOwnershipDescription("proxy", nodes[0].ID)
	if base.descriptions[7] != canonicalOwner {
		t.Fatalf("activate did not write canonical owner: %q, want %q", base.descriptions[7], canonicalOwner)
	}

	pAfterActivate, err := store.GetProxy(nodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if pAfterActivate.Bridge.LegacyOwner != "" {
		t.Fatalf("activate did not clear LegacyOwner: %+v", pAfterActivate.Bridge)
	}
	if pAfterActivate.Bridge.ProxyIndex != 7 {
		t.Fatalf("proxy index changed unnecessarily: got %d, want 7", pAfterActivate.Bridge.ProxyIndex)
	}
}
