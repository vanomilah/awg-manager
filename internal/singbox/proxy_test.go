package singbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/ndms"
)

func TestProxyManagerNextFreeIndexReservesAcrossConcurrentCallers(t *testing.T) {
	pm := &ProxyManager{
		pending: make(map[int]time.Time),
		listInterfaces: func(context.Context) ([]ndms.Interface, error) {
			return nil, nil
		},
	}
	const callers = 16
	results := make(chan int, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			index, err := pm.NextFreeIndex(context.Background(), nil)
			if err != nil {
				errs <- err
				return
			}
			results <- index
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[int]bool)
	for index := range results {
		if seen[index] {
			t.Fatalf("duplicate pending Proxy%d reservation", index)
		}
		seen[index] = true
	}
	if len(seen) != callers {
		t.Fatalf("reserved %d unique indices, want %d", len(seen), callers)
	}
	pm.ReleaseProxyIndex(0)
	index, err := pm.NextFreeIndex(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if index != 0 {
		t.Fatalf("released reservation yielded Proxy%d, want Proxy0", index)
	}
}

func TestProxyManagerNextFreeIndexSkipsPersistedReservations(t *testing.T) {
	pm := &ProxyManager{
		pending: make(map[int]time.Time),
		listInterfaces: func(context.Context) ([]ndms.Interface, error) {
			return nil, nil
		},
	}
	pm.SetReservedIndices(func() map[int]bool {
		return map[int]bool{0: true, 2: true}
	})
	index, err := pm.NextFreeIndex(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatalf("NextFreeIndex = %d, want 1 (Proxy0 and Proxy2 persisted)", index)
	}
}

func TestProxyManagerOwnedMutationPreservesForeignProxy(t *testing.T) {
	description := "user-created"
	createCalls := 0
	deleteCalls := 0
	pm := &ProxyManager{
		pending:      make(map[int]time.Time),
		hasComponent: func() bool { return true },
		getProxy: func(_ context.Context, _ string) (*ndms.ProxyInfo, error) {
			return &ndms.ProxyInfo{Exists: true, Description: description}, nil
		},
		createProxy: func(_ context.Context, _, nextDescription, _ string, _ int, _ bool) error {
			createCalls++
			description = nextDescription
			return nil
		},
		downProxy: func(context.Context, string) error { return nil },
		deleteProxy: func(context.Context, string) error {
			deleteCalls++
			return nil
		},
	}

	owner := "awg-manager:singbox:subscription:sub-id"
	owned, err := pm.EnsureProxyIfOwned(context.Background(), 4, 11004, owner, "old label")
	if err != nil {
		t.Fatal(err)
	}
	if owned || createCalls != 0 || description != "user-created" {
		t.Fatalf("foreign ensure mutated Proxy4: owned=%v createCalls=%d description=%q", owned, createCalls, description)
	}
	removed, err := pm.RemoveProxyIfOwnedBy(context.Background(), 4, owner, "old label")
	if err != nil {
		t.Fatal(err)
	}
	if removed || deleteCalls != 0 {
		t.Fatalf("foreign remove mutated Proxy4: removed=%v deleteCalls=%d", removed, deleteCalls)
	}

	// A legacy label is accepted once and rewritten to the stable owner token.
	description = "old label"
	owned, err = pm.EnsureProxyIfOwned(context.Background(), 4, 11004, owner, "old label")
	if err != nil {
		t.Fatal(err)
	}
	if !owned || createCalls != 1 || description != owner {
		t.Fatalf("legacy migration: owned=%v createCalls=%d description=%q", owned, createCalls, description)
	}
	removed, err = pm.RemoveProxyIfOwnedBy(context.Background(), 4, owner)
	if err != nil {
		t.Fatal(err)
	}
	if !removed || deleteCalls != 1 {
		t.Fatalf("owned remove: removed=%v deleteCalls=%d", removed, deleteCalls)
	}
}

// proxyIsOurs decides whether an NDMS ProxyN belongs to awg-manager's sing-box
// management, so disable/orphan-cleanup removes it. Subscription composites are
// the regression case: their proxy carries the subscription *label* as the
// interface description (not a tunnel tag), so the tag/slot heuristics miss it —
// they must be recognised via their explicitly-tracked proxy index.
func TestProxyIsOurs(t *testing.T) {
	tunnelTags := map[string]bool{"vless-1": true}
	ourPortSlots := map[int]bool{3: true}
	subProxyIdx := map[int]bool{7: true}

	cases := []struct {
		name string
		idx  int
		desc string
		want bool
	}{
		{"tunnel matched by description tag", 0, "vless-1", true},
		{"tunnel matched by port slot (empty desc)", 3, "", true},
		{"subscription composite (label description)", 7, "Моя подписка", true},
		{"foreign proxy with description", 9, "some-other-app", false},
		{"foreign proxy empty desc unknown slot", 5, "", false},
	}
	for _, c := range cases {
		got := proxyIsOurs(c.idx, c.desc, tunnelTags, ourPortSlots, subProxyIdx)
		if got != c.want {
			t.Errorf("%s: proxyIsOurs(%d, %q) = %v, want %v", c.name, c.idx, c.desc, got, c.want)
		}
	}
}

// nativeProxyKernelNames must return kernel names of ONLY the proxies that are
// not ours — the KeenOS-native SOCKS proxies a user can bind a router outbound
// to (#323). Ours (by tunnel tag or by port slot) are excluded.
func TestNativeProxyKernelNames(t *testing.T) {
	proxies := []proxyEntry{
		{idx: 0, desc: "My-Socks5", kernel: "t2s0"},      // native — keep
		{idx: 1, desc: "vless-1", kernel: "t2s1"},        // ours by tunnel tag — drop
		{idx: 2, desc: "", kernel: "t2s2"},               // ours by port slot — drop
		{idx: 3, desc: "another-native", kernel: "t2s3"}, // native — keep
	}
	got := nativeProxyKernelNames(proxies,
		map[string]bool{"vless-1": true}, // tunnelTags
		map[int]bool{2: true},            // ourPortSlots
		map[int]bool{},                   // subProxyIdx
	)
	if len(got) != 2 {
		t.Fatalf("want 2 native, got %d: %v", len(got), got)
	}
	want := map[string]bool{"t2s0": true, "t2s3": true}
	for _, k := range got {
		if !want[k] {
			t.Errorf("unexpected native proxy %q in %v", k, got)
		}
	}
}
