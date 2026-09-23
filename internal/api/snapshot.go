package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	ndms "github.com/hoaxisr/awg-manager/internal/ndms"
	"github.com/hoaxisr/awg-manager/internal/tunnel/external"
)

type ManagedTunnelsLister interface {
	listItems(ctx context.Context) ([]tunnelItem, error)
}

type ExternalTunnelsLister interface {
	listExternal(ctx context.Context) ([]external.TunnelInfo, error)
}

type SystemTunnelsLister interface {
	listSystemTunnels(ctx context.Context) ([]ndms.SystemWireguardTunnel, error)
}

type singleflightCall struct {
	done chan struct{}
	val  interface{}
	err  error
}

type singleflightGroup struct {
	mu sync.Mutex
	m  map[string]*singleflightCall
}

func (g *singleflightGroup) DoContext(ctx context.Context, key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*singleflightCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		select {
		case <-c.done:
			return c.val, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &singleflightCall{done: make(chan struct{})}
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	close(c.done)

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}

func cloneExternalList(in []external.TunnelInfo) []external.TunnelInfo {
	if in == nil {
		return nil
	}
	out := make([]external.TunnelInfo, len(in))
	copy(out, in)
	return out
}

func cloneSystemList(in []ndms.SystemWireguardTunnel) []ndms.SystemWireguardTunnel {
	if in == nil {
		return nil
	}
	out := make([]ndms.SystemWireguardTunnel, len(in))
	copy(out, in)
	return out
}

// TunnelsSnapshotBuilder composes the {tunnels, external, system}
// payload used by GET /api/tunnels/all and by the hook-driven
// resource:invalidated refresher. It is the single assembly point
// for the composite tunnels list the polling store reads.
type TunnelsSnapshotBuilder struct {
	tunnels   ManagedTunnelsLister
	external  ExternalTunnelsLister
	systemTun SystemTunnelsLister

	sf singleflightGroup

	cacheMu          sync.RWMutex
	cachedExternal   []external.TunnelInfo
	cachedExternalAt time.Time
	cachedSystem     []ndms.SystemWireguardTunnel
	cachedSystemAt   time.Time
	cacheTTL         time.Duration
}

// NewTunnelsSnapshotBuilder creates a new builder with no handlers
// wired. Use the setters to wire the three handler references.
func NewTunnelsSnapshotBuilder() *TunnelsSnapshotBuilder {
	return &TunnelsSnapshotBuilder{
		cacheTTL: 15 * time.Second,
	}
}

// SetTunnelsHandler sets the tunnels handler reference.
func (b *TunnelsSnapshotBuilder) SetTunnelsHandler(h ManagedTunnelsLister) { b.tunnels = h }

// SetExternalHandler sets the external tunnels handler reference.
func (b *TunnelsSnapshotBuilder) SetExternalHandler(h ExternalTunnelsLister) { b.external = h }

// SetSystemTunnelsHandler sets the system tunnels handler reference.
func (b *TunnelsSnapshotBuilder) SetSystemTunnelsHandler(h SystemTunnelsLister) { b.systemTun = h }

// SetCacheTTL overrides the default 15-second TTL for system/external caches.
func (b *TunnelsSnapshotBuilder) SetCacheTTL(ttl time.Duration) {
	b.cacheMu.Lock()
	defer b.cacheMu.Unlock()
	b.cacheTTL = ttl
}

// InvalidateCaches forces the next Build call to query fresh data from system and external sources.
func (b *TunnelsSnapshotBuilder) InvalidateCaches() {
	b.cacheMu.Lock()
	defer b.cacheMu.Unlock()
	b.cachedExternalAt = time.Time{}
	b.cachedSystemAt = time.Time{}
}

type managedResult struct {
	items []tunnelItem
	err   error
	dur   time.Duration
}

type externalResult struct {
	items []external.TunnelInfo
	stale bool
	dur   time.Duration
}

type systemResult struct {
	items []ndms.SystemWireguardTunnel
	stale bool
	dur   time.Duration
}

// Build composes the snapshot payload for the polling store. Returns
// nil when no TunnelsHandler is wired, or when its listItems call
// errors — in both cases there's nothing safe to return.
func (b *TunnelsSnapshotBuilder) Build(ctx context.Context) map[string]interface{} {
	if b.tunnels == nil {
		return nil
	}

	startTotal := time.Now()
	managedCtx, cancelManaged := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancelManaged()
	externalCtx, cancelExternal := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancelExternal()
	systemCtx, cancelSystem := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancelSystem()

	// 1. Managed tunnels (critical path, max 2.5s)
	managedCh := make(chan managedResult, 1)
	go func() {
		start := time.Now()
		items, err := b.tunnels.listItems(managedCtx)
		managedCh <- managedResult{items: items, err: err, dur: time.Since(start)}
	}()

	// 2. External tunnels (with singleflight, last-good cache & max 1.5s timeout)
	var extCh chan externalResult
	if b.external != nil {
		extCh = make(chan externalResult, 1)
		go func() {
			b.cacheMu.RLock()
			cached := cloneExternalList(b.cachedExternal)
			cachedAt := b.cachedExternalAt
			ttl := b.cacheTTL
			b.cacheMu.RUnlock()

			if cached != nil && time.Since(cachedAt) < ttl {
				extCh <- externalResult{items: cached, stale: false, dur: 0}
				return
			}

			start := time.Now()
			val, err := b.sf.DoContext(externalCtx, "external", func() (interface{}, error) {
				return b.external.listExternal(externalCtx)
			})
			dur := time.Since(start)

			if err == nil && val != nil {
				extList, ok := val.([]external.TunnelInfo)
				if ok && extList != nil {
					cloned := cloneExternalList(extList)
					b.cacheMu.Lock()
					b.cachedExternal = cloned
					b.cachedExternalAt = time.Now()
					b.cacheMu.Unlock()
					extCh <- externalResult{items: cloneExternalList(cloned), stale: false, dur: dur}
					return
				}
			}

			// Timeout or error: fall back to stale cache
			if cached != nil {
				extCh <- externalResult{items: cached, stale: true, dur: dur}
			} else {
				extCh <- externalResult{items: []external.TunnelInfo{}, stale: false, dur: dur}
			}
		}()
	}

	// 3. System tunnels (NDMS/RCI, with singleflight, last-good cache & max 1.5s timeout)
	var sysCh chan systemResult
	if b.systemTun != nil {
		sysCh = make(chan systemResult, 1)
		go func() {
			b.cacheMu.RLock()
			cached := cloneSystemList(b.cachedSystem)
			cachedAt := b.cachedSystemAt
			ttl := b.cacheTTL
			b.cacheMu.RUnlock()

			if cached != nil && time.Since(cachedAt) < ttl {
				sysCh <- systemResult{items: cached, stale: false, dur: 0}
				return
			}

			start := time.Now()
			val, err := b.sf.DoContext(systemCtx, "system", func() (interface{}, error) {
				return b.systemTun.listSystemTunnels(systemCtx)
			})
			dur := time.Since(start)

			if err == nil && val != nil {
				sysList, ok := val.([]ndms.SystemWireguardTunnel)
				if ok && sysList != nil {
					cloned := cloneSystemList(sysList)
					b.cacheMu.Lock()
					b.cachedSystem = cloned
					b.cachedSystemAt = time.Now()
					b.cacheMu.Unlock()
					sysCh <- systemResult{items: cloneSystemList(cloned), stale: false, dur: dur}
					return
				}
			}

			// Timeout or error: fall back to stale cache
			if cached != nil {
				sysCh <- systemResult{items: cached, stale: true, dur: dur}
			} else {
				sysCh <- systemResult{items: []ndms.SystemWireguardTunnel{}, stale: false, dur: dur}
			}
		}()
	}

	// Wait for Managed tunnels (with hard 2500ms deadline)
	var mRes managedResult
	select {
	case mRes = <-managedCh:
	case <-managedCtx.Done():
		return nil
	}

	if mRes.err != nil || mRes.items == nil {
		return nil
	}

	// Wait for External tunnels (with hard 1500ms deadline)
	var eRes externalResult
	if extCh != nil {
		select {
		case eRes = <-extCh:
		case <-externalCtx.Done():
			b.cacheMu.RLock()
			eRes = externalResult{items: cloneExternalList(b.cachedExternal), stale: true, dur: 1500 * time.Millisecond}
			b.cacheMu.RUnlock()
			if eRes.items == nil {
				eRes.items = []external.TunnelInfo{}
			}
		}
	}

	// Wait for System tunnels (with hard 1500ms deadline)
	var sRes systemResult
	if sysCh != nil {
		select {
		case sRes = <-sysCh:
		case <-systemCtx.Done():
			b.cacheMu.RLock()
			sRes = systemResult{items: cloneSystemList(b.cachedSystem), stale: true, dur: 1500 * time.Millisecond}
			b.cacheMu.RUnlock()
			if sRes.items == nil {
				sRes.items = []ndms.SystemWireguardTunnel{}
			}
		}
	}

	total := time.Since(startTotal)
	serverTiming := fmt.Sprintf("managed;dur=%d, external;dur=%d, system;dur=%d, total;dur=%d",
		mRes.dur.Milliseconds(), eRes.dur.Milliseconds(), sRes.dur.Milliseconds(), total.Milliseconds())

	payload := map[string]interface{}{
		"tunnels":       mRes.items,
		"_serverTiming": serverTiming,
	}
	if b.external != nil {
		payload["external"] = eRes.items
		if eRes.stale {
			payload["externalStale"] = true
		}
	}
	if b.systemTun != nil {
		payload["system"] = sRes.items
		if sRes.stale {
			payload["systemStale"] = true
		}
	}
	return payload
}
