package api

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	ndms "github.com/hoaxisr/awg-manager/internal/ndms"
	"github.com/hoaxisr/awg-manager/internal/tunnel/external"
)

type mockManagedLister struct {
	items []tunnelItem
	err   error
}

func (m *mockManagedLister) listItems(ctx context.Context) ([]tunnelItem, error) {
	return m.items, m.err
}

type mockExternalLister struct {
	callCount int
	timeout   bool
	delay     time.Duration
	items     []external.TunnelInfo
	err       error
}

func (m *mockExternalLister) listExternal(ctx context.Context) ([]external.TunnelInfo, error) {
	m.callCount++
	if m.timeout {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.items, m.err
}

type mockSystemLister struct {
	callCount int
	timeout   bool
	items     []ndms.SystemWireguardTunnel
	err       error
}

func (m *mockSystemLister) listSystemTunnels(ctx context.Context) ([]ndms.SystemWireguardTunnel, error) {
	m.callCount++
	if m.timeout {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.items, m.err
}

func TestTunnelsSnapshotBuilder_CacheAndStale(t *testing.T) {
	b := NewTunnelsSnapshotBuilder()

	th := &mockManagedLister{
		items: []tunnelItem{{ID: "t1", Name: "Tunnel 1"}},
	}
	b.SetTunnelsHandler(th)

	extMock := &mockExternalLister{
		items: []external.TunnelInfo{{InterfaceName: "ext1"}},
	}
	b.SetExternalHandler(extMock)

	sysMock := &mockSystemLister{
		items: []ndms.SystemWireguardTunnel{{ID: "sys1"}},
	}
	b.SetSystemTunnelsHandler(sysMock)

	ctx := context.Background()

	// 1. First build — queries external and system handlers
	snap1 := b.Build(ctx)
	if snap1 == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if extMock.callCount != 1 {
		t.Fatalf("expected 1 ext call, got %d", extMock.callCount)
	}
	if sysMock.callCount != 1 {
		t.Fatalf("expected 1 sys call, got %d", sysMock.callCount)
	}

	timing, ok := snap1["_serverTiming"].(string)
	if !ok || !strings.Contains(timing, "managed;") || !strings.Contains(timing, "external;") || !strings.Contains(timing, "system;") {
		t.Fatalf("expected _serverTiming header, got %v", snap1["_serverTiming"])
	}

	// 2. Second build within TTL — should use cache, not increment callCounts
	snap2 := b.Build(ctx)
	if snap2 == nil {
		t.Fatal("expected non-nil snapshot on second call")
	}
	if extMock.callCount != 1 {
		t.Fatalf("expected cache hit with 1 ext call, got %d", extMock.callCount)
	}
	if sysMock.callCount != 1 {
		t.Fatalf("expected cache hit with 1 sys call, got %d", sysMock.callCount)
	}

	// 3. Invalidate caches — should query handlers again
	b.InvalidateCaches()
	snap3 := b.Build(ctx)
	if snap3 == nil {
		t.Fatal("expected non-nil snapshot on third call")
	}
	if extMock.callCount != 2 {
		t.Fatalf("expected cache miss after invalidate, got %d calls", extMock.callCount)
	}
	if sysMock.callCount != 2 {
		t.Fatalf("expected cache miss after invalidate, got %d calls", sysMock.callCount)
	}
}

func TestTunnelsSnapshotBuilder_TimeoutFallback(t *testing.T) {
	b := NewTunnelsSnapshotBuilder()
	b.SetCacheTTL(1 * time.Millisecond) // force expiration

	th := &mockManagedLister{
		items: []tunnelItem{{ID: "t1"}},
	}
	b.SetTunnelsHandler(th)

	extMock := &mockExternalLister{
		items: []external.TunnelInfo{{InterfaceName: "cached-ext"}},
	}
	b.SetExternalHandler(extMock)

	ctx := context.Background()

	// Initial populate
	snap1 := b.Build(ctx)
	if snap1 == nil || len(snap1["external"].([]external.TunnelInfo)) != 1 {
		t.Fatal("failed initial populate")
	}

	time.Sleep(5 * time.Millisecond) // expire cache
	extMock.timeout = true

	// Build with timeout should fallback to stale cached data
	start := time.Now()
	snap2 := b.Build(ctx)
	dur := time.Since(start)

	if snap2 == nil {
		t.Fatal("expected non-nil snapshot with stale fallback")
	}
	if dur > 2500*time.Millisecond {
		t.Fatalf("expected timeout within ~1.5s, took %v", dur)
	}
	if snap2["externalStale"] != true {
		t.Fatalf("expected externalStale: true, got %v", snap2["externalStale"])
	}
	extList := snap2["external"].([]external.TunnelInfo)
	if len(extList) != 1 || extList[0].InterfaceName != "cached-ext" {
		t.Fatalf("expected stale cached data returned, got %v", extList)
	}
}

func TestTunnelsSnapshotBuilder_Singleflight(t *testing.T) {
	b := NewTunnelsSnapshotBuilder()

	th := &mockManagedLister{
		items: []tunnelItem{{ID: "t1"}},
	}
	b.SetTunnelsHandler(th)

	extMock := &mockExternalLister{
		items: []external.TunnelInfo{{InterfaceName: "ext-sf"}},
		delay: 50 * time.Millisecond,
	}
	b.SetExternalHandler(extMock)

	const concurrency = 25
	var wg sync.WaitGroup
	wg.Add(concurrency)

	ctx := context.Background()
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			snap := b.Build(ctx)
			if snap == nil {
				t.Errorf("expected non-nil snapshot")
				return
			}
			extList, ok := snap["external"].([]external.TunnelInfo)
			if !ok || len(extList) != 1 {
				t.Errorf("expected 1 external tunnel, got %v", extList)
			}
		}()
	}

	wg.Wait()

	// Due to singleflight, even with 25 concurrent cold requests, callCount should be 1
	if extMock.callCount != 1 {
		t.Fatalf("expected exactly 1 external call due to singleflight, got %d", extMock.callCount)
	}
}

type unyieldingExternalLister struct {
	items []external.TunnelInfo
}

func (u *unyieldingExternalLister) listExternal(ctx context.Context) ([]external.TunnelInfo, error) {
	// Completely ignores ctx.Done() and sleeps past timeout
	time.Sleep(5 * time.Second)
	return u.items, nil
}

func TestTunnelsSnapshotBuilder_UnresponsiveProvider(t *testing.T) {
	b := NewTunnelsSnapshotBuilder()
	th := &mockManagedLister{
		items: []tunnelItem{{ID: "t1"}},
	}
	b.SetTunnelsHandler(th)

	// Inject unyielding provider
	b.SetExternalHandler(&unyieldingExternalLister{
		items: []external.TunnelInfo{{InterfaceName: "never-returns"}},
	})

	ctx := context.Background()
	start := time.Now()
	snap := b.Build(ctx)
	dur := time.Since(start)

	if snap == nil {
		t.Fatal("expected non-nil snapshot despite unyielding provider")
	}
	// Hard timeout is 1500ms; should return in ~1.5-1.8s, NOT 5s
	if dur > 2200*time.Millisecond {
		t.Fatalf("Build blocked on unresponsive provider for %v, want <2200ms", dur)
	}
}

func TestSingleflightGroup_WaitersHonorContext(t *testing.T) {
	var group singleflightGroup
	started := make(chan struct{})
	release := make(chan struct{})
	leaderDone := make(chan struct{})

	go func() {
		defer close(leaderDone)
		_, _ = group.DoContext(context.Background(), "external", func() (interface{}, error) {
			close(started)
			<-release
			return "ok", nil
		})
	}()
	<-started

	const waiters = 25
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			_, err := group.DoContext(ctx, "external", func() (interface{}, error) {
				t.Error("waiter unexpectedly became a second leader")
				return nil, nil
			})
			if err != context.DeadlineExceeded {
				t.Errorf("waiter error = %v, want context deadline exceeded", err)
			}
		}()
	}

	waitersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitersDone)
	}()
	select {
	case <-waitersDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("singleflight waiters remained blocked behind an unresponsive leader")
	}

	group.mu.Lock()
	entries := len(group.m)
	group.mu.Unlock()
	if entries != 1 {
		t.Fatalf("singleflight entries = %d, want only the active leader", entries)
	}

	close(release)
	select {
	case <-leaderDone:
	case <-time.After(time.Second):
		t.Fatal("singleflight leader did not finish after release")
	}
}

func TestTunnelsSnapshotBuilder_DefensiveCopy(t *testing.T) {
	b := NewTunnelsSnapshotBuilder()
	th := &mockManagedLister{
		items: []tunnelItem{{ID: "t1"}},
	}
	b.SetTunnelsHandler(th)

	extMock := &mockExternalLister{
		items: []external.TunnelInfo{{InterfaceName: "original"}},
	}
	b.SetExternalHandler(extMock)

	ctx := context.Background()
	snap1 := b.Build(ctx)
	extList1 := snap1["external"].([]external.TunnelInfo)
	// Mutate returned slice
	extList1[0].InterfaceName = "mutated"

	// Next call from cache
	snap2 := b.Build(ctx)
	extList2 := snap2["external"].([]external.TunnelInfo)
	if extList2[0].InterfaceName != "original" {
		t.Fatalf("cache was mutated by caller: got %q, want %q", extList2[0].InterfaceName, "original")
	}
}
