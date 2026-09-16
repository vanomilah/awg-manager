package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/mihomo"
	"github.com/hoaxisr/awg-manager/internal/mihomonative"
)

// gatedBridgeRegistrar makes BridgeManager's allocation/reconciliation pass
// two-phase without coupling the native store to process management. While the
// gate is closed, BridgeManager may persist ports and ProxyN indices, but its
// final Ensure call is intentionally suppressed. The gate is opened only after
// Mihomo has loaded that exact listener configuration and passed readiness.
type bridgeProxyRegistrar interface {
	NextFreeIndex(context.Context, map[int]bool) (int, error)
	ReleaseProxyIndex(int)
	LookupProxy(context.Context, int) (string, bool, error)
	EnsureProxy(context.Context, int, int, string) error
	EnsureProxyIfOwned(context.Context, int, int, string, ...string) (bool, error)
	RemoveProxyIfOwned(context.Context, int, string, ...string) (bool, error)
}

type gatedBridgeRegistrar struct {
	base bridgeProxyRegistrar

	mu    sync.RWMutex
	ready bool
}

func (g *gatedBridgeRegistrar) setReady(ready bool) {
	g.mu.Lock()
	g.ready = ready
	g.mu.Unlock()
}

func (g *gatedBridgeRegistrar) isReady() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ready
}

func (g *gatedBridgeRegistrar) NextFreeIndex(ctx context.Context, reserved map[int]bool) (int, error) {
	return g.base.NextFreeIndex(ctx, reserved)
}

func (g *gatedBridgeRegistrar) ReleaseProxyIndex(index int) {
	g.base.ReleaseProxyIndex(index)
}

func (g *gatedBridgeRegistrar) LookupProxy(ctx context.Context, index int) (string, bool, error) {
	return g.base.LookupProxy(ctx, index)
}

func (g *gatedBridgeRegistrar) EnsureProxy(ctx context.Context, index, port int, description string) error {
	if !g.isReady() {
		// NextFreeIndex owns an in-process reservation until EnsureProxy. A
		// prepared bridge is already persisted and therefore reserved by the
		// next scan, so release the transient reservation while advertisement
		// is deliberately deferred.
		g.base.ReleaseProxyIndex(index)
		return nil
	}
	return g.base.EnsureProxy(ctx, index, port, description)
}

func (g *gatedBridgeRegistrar) IsGated() bool {
	return !g.isReady()
}

func (g *gatedBridgeRegistrar) EnsureProxyIfOwned(
	ctx context.Context,
	index, port int,
	owner string,
	legacyOwners ...string,
) (bool, error) {
	if !g.isReady() {
		g.base.ReleaseProxyIndex(index)
		desc, exists, err := g.base.LookupProxy(ctx, index)
		if err != nil {
			return false, err
		}
		if !exists {
			return true, nil
		}
		if desc == owner {
			return true, nil
		}
		for _, leg := range legacyOwners {
			if leg != "" && desc == leg {
				return true, nil
			}
		}
		return false, nil
	}
	return g.base.EnsureProxyIfOwned(ctx, index, port, owner, legacyOwners...)
}

func (g *gatedBridgeRegistrar) RemoveProxyIfOwned(ctx context.Context, index int, owner string, legacyOwners ...string) (bool, error) {
	return g.base.RemoveProxyIfOwned(ctx, index, owner, legacyOwners...)
}

// mihomoBridgeRuntime serializes the config/runtime/NDMS publication edge.
// BridgeManager remains responsible for allocation and ownership; this layer
// only controls when those allocations become externally routable.
type mihomoBridgeRuntime struct {
	mu      sync.Mutex
	store   *mihomonative.Store
	manager *mihomonative.BridgeManager
	gate    *gatedBridgeRegistrar
}

func newMihomoBridgeRuntime(
	store *mihomonative.Store,
	manager *mihomonative.BridgeManager,
	gate *gatedBridgeRegistrar,
) *mihomoBridgeRuntime {
	return &mihomoBridgeRuntime{store: store, manager: manager, gate: gate}
}

func (r *mihomoBridgeRuntime) prepare(ctx context.Context, previous []mihomonative.BridgeRef) error {
	if r == nil || r.manager == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gate.setReady(false)
	return r.manager.Reconcile(ctx, previous)
}

func (r *mihomoBridgeRuntime) activate(ctx context.Context) error {
	if r == nil || r.manager == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.manager.Enabled() {
		return r.deactivateLocked(ctx)
	}
	if err := waitForMihomoBridgeListeners(ctx, r.store.ConfigBridgeListeners()); err != nil {
		r.gate.setReady(false)
		if cleanupErr := r.deactivateLocked(ctx); cleanupErr != nil {
			return fmt.Errorf("Mihomo bridge readiness: %v; withdraw exports: %w", err, cleanupErr)
		}
		return err
	}
	r.gate.setReady(true)
	// A second idempotent reconciliation is the publication commit. Passing
	// the current snapshot prevents it from treating any allocation as stale.
	if err := r.manager.Reconcile(ctx, r.store.ListBridges()); err != nil {
		r.gate.setReady(false)
		if cleanupErr := r.deactivateLocked(ctx); cleanupErr != nil {
			return fmt.Errorf("activate Mihomo bridges: %v; withdraw partial exports: %w", err, cleanupErr)
		}
		return fmt.Errorf("activate Mihomo bridges: %w", err)
	}
	return nil
}

func waitForMihomoBridgeListeners(ctx context.Context, listeners []mihomonative.BridgeListener) error {
	if len(listeners) == 0 {
		return nil
	}
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pending := make(map[int]bool, len(listeners))
	for _, listener := range listeners {
		pending[listener.Port] = true
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	dialer := net.Dialer{Timeout: 200 * time.Millisecond}
	for len(pending) > 0 {
		for port := range pending {
			conn, err := dialer.DialContext(readyCtx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				_ = conn.Close()
				delete(pending, port)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("Mihomo bridge listeners not ready on ports %v: %w", sortedBridgePorts(pending), readyCtx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

func sortedBridgePorts(pending map[int]bool) []int {
	ports := make([]int, 0, len(pending))
	for port := range pending {
		ports = append(ports, port)
	}
	for i := 1; i < len(ports); i++ {
		for j := i; j > 0 && ports[j] < ports[j-1]; j-- {
			ports[j], ports[j-1] = ports[j-1], ports[j]
		}
	}
	return ports
}

func (r *mihomoBridgeRuntime) deactivate(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deactivateLocked(ctx)
}

// deactivateIf serializes the stale-exit guard with activation. If a newer
// Mihomo generation already activated, the old callback observes current=false
// while holding the same runtime mutex and cannot withdraw the new exports.
// If the old callback wins the lock, the later activation is necessarily the
// final operation and republishes them.
func (r *mihomoBridgeRuntime) deactivateIf(ctx context.Context, current func() bool) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if current != nil && !current() {
		return nil
	}
	return r.deactivateLocked(ctx)
}

// deactivateAfterUnexpectedExit closes the publication gate immediately and
// retries best-effort withdrawal for a bounded period. NDMS can transiently
// reject one interface mutation while still accepting the others; keeping the
// gate closed and retrying avoids leaving a dead ProxyN indefinitely.
func (r *mihomoBridgeRuntime) deactivateAfterUnexpectedExit(ctx context.Context, current func() bool) error {
	if r == nil {
		return nil
	}
	delays := [...]time.Duration{0, 100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond}
	var joined error
	for attempt, delay := range delays {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(joined, ctx.Err())
			case <-time.After(delay):
			}
		}
		err := r.deactivateIf(ctx, current)
		if err == nil {
			return nil
		}
		joined = errors.Join(joined, err)
	}
	return joined
}

func (r *mihomoBridgeRuntime) deactivateLocked(ctx context.Context) error {
	if r.gate != nil {
		r.gate.setReady(false)
	}
	if r.store == nil || r.gate == nil || r.gate.base == nil {
		return nil
	}
	var joined error
	for _, ref := range r.store.ListBridges() {
		if _, err := r.gate.base.RemoveProxyIfOwned(
			ctx,
			ref.Bridge.ProxyIndex,
			mihomonative.BridgeOwnershipDescription(ref.Kind, ref.ID),
		); err != nil {
			joined = errors.Join(joined, fmt.Errorf("withdraw Mihomo bridge %s/%s: %w", ref.Kind, ref.ID, err))
		}
	}
	return joined
}

// Ensure *mihomoBridgeRuntime satisfies mihomo.BridgeRuntime.
var _ mihomo.BridgeRuntime = (*mihomoBridgeRuntime)(nil)

// ApplyBridges ensures all requested bridges are active in the kernel/NDMS.
func (r *mihomoBridgeRuntime) ApplyBridges(ctx context.Context, bridges []mihomo.BridgeRef) error {
	if r == nil || r.manager == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gate.setReady(true)
	return r.manager.Reconcile(ctx, nil)
}

// WithdrawBridges withdraws the specified bridge interfaces from the kernel/NDMS.
func (r *mihomoBridgeRuntime) WithdrawBridges(ctx context.Context, bridges []mihomo.BridgeRef) error {
	if r == nil || r.gate == nil || r.gate.base == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var joined error
	for _, ref := range bridges {
		canonicalOwner := ""
		legacyOwners := []string{}
		if ref.LegacyOwner != "" {
			legacyOwners = append(legacyOwners, ref.LegacyOwner)
		}
		if r.store != nil {
			for _, nb := range r.store.ListBridges() {
				if nb.Bridge.ProxyIndex == ref.ProxyIndex {
					canonicalOwner = mihomonative.BridgeOwnershipDescription(nb.Kind, nb.ID)
					break
				}
			}
		}
		if canonicalOwner != "" {
			if _, err := r.gate.base.RemoveProxyIfOwned(ctx, ref.ProxyIndex, canonicalOwner, legacyOwners...); err != nil {
				joined = errors.Join(joined, fmt.Errorf("withdraw bridge Proxy%d: %w", ref.ProxyIndex, err))
			}
		}
	}
	return joined
}

// VerifyBridges verifies that Mihomo is listening on the required bridge ports.
func (r *mihomoBridgeRuntime) VerifyBridges(ctx context.Context, bridges []mihomo.BridgeRef) error {
	if r == nil || r.store == nil {
		return nil
	}
	listeners := r.store.ConfigBridgeListeners()
	if len(listeners) == 0 {
		return nil
	}
	targetPorts := make(map[int]bool)
	for _, b := range bridges {
		for _, nb := range r.store.ListBridges() {
			if nb.Bridge.ProxyIndex == b.ProxyIndex {
				targetPorts[nb.Bridge.ListenPort] = true
			}
		}
	}
	var requiredListeners []mihomonative.BridgeListener
	for _, l := range listeners {
		if targetPorts[l.Port] {
			requiredListeners = append(requiredListeners, l)
		}
	}
	if len(requiredListeners) > 0 {
		return waitForMihomoBridgeListeners(ctx, requiredListeners)
	}
	return nil
}

// ListActiveBridges returns all currently registered bridge interfaces.
func (r *mihomoBridgeRuntime) ListActiveBridges(ctx context.Context) ([]mihomo.BridgeRef, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	nativeBridges := r.store.ListBridges()
	out := make([]mihomo.BridgeRef, len(nativeBridges))
	for i, nb := range nativeBridges {
		out[i] = mihomo.BridgeRef{
			ProxyIndex:      nb.Bridge.ProxyIndex,
			ProxyInterface:  nb.Bridge.ProxyInterface,
			KernelInterface: nb.Bridge.KernelInterface,
			LegacyOwner:     nb.LegacyOwner,
		}
	}
	return out, nil
}
