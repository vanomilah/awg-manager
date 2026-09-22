package api

import (
	"context"
	"sync"
	"time"

	ndms "github.com/hoaxisr/awg-manager/internal/ndms"
	"github.com/hoaxisr/awg-manager/internal/tunnel/external"
)

// TunnelsSnapshotBuilder composes the {tunnels, external, system}
// payload used by GET /api/tunnels/all and by the hook-driven
// resource:invalidated refresher. It is the single assembly point
// for the composite tunnels list the polling store reads.
//
// The struct is a thin holder for the three handler references;
// callers wire whichever handlers are available and call Build.
// Missing handlers produce empty slices in the relevant keys.
type TunnelsSnapshotBuilder struct {
	tunnels   *TunnelsHandler
	external  *ExternalTunnelsHandler
	systemTun *SystemTunnelsHandler
}

// NewTunnelsSnapshotBuilder creates a new builder with no handlers
// wired. Use the setters to wire the three handler references.
func NewTunnelsSnapshotBuilder() *TunnelsSnapshotBuilder {
	return &TunnelsSnapshotBuilder{}
}

// SetTunnelsHandler sets the tunnels handler reference.
func (b *TunnelsSnapshotBuilder) SetTunnelsHandler(h *TunnelsHandler) { b.tunnels = h }

// SetExternalHandler sets the external tunnels handler reference.
func (b *TunnelsSnapshotBuilder) SetExternalHandler(h *ExternalTunnelsHandler) { b.external = h }

// SetSystemTunnelsHandler sets the system tunnels handler reference.
func (b *TunnelsSnapshotBuilder) SetSystemTunnelsHandler(h *SystemTunnelsHandler) { b.systemTun = h }

// Build composes the snapshot payload for the polling store. Returns
// nil when no TunnelsHandler is wired, or when its listItems call
// errors — in both cases there's nothing safe to return.
func (b *TunnelsSnapshotBuilder) Build(ctx context.Context) map[string]interface{} {
	if b.tunnels == nil {
		return nil
	}

	var wg sync.WaitGroup
	var items []tunnelItem
	var itemsErr error
	var externalList []external.TunnelInfo
	var systemList []ndms.SystemWireguardTunnel

	wg.Add(1)
	go func() {
		defer wg.Done()
		items, itemsErr = b.tunnels.listItems(ctx)
	}()

	if b.external != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			ext, err := b.external.listExternal(subCtx)
			if err == nil && ext != nil {
				externalList = ext
			} else {
				externalList = []external.TunnelInfo{}
			}
		}()
	}

	if b.systemTun != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			sys, err := b.systemTun.listSystemTunnels(subCtx)
			if err == nil && sys != nil {
				systemList = sys
			} else {
				systemList = []ndms.SystemWireguardTunnel{}
			}
		}()
	}

	wg.Wait()

	if itemsErr != nil || items == nil {
		return nil
	}

	payload := map[string]interface{}{"tunnels": items}
	if b.external != nil {
		payload["external"] = externalList
	}
	if b.systemTun != nil {
		payload["system"] = systemList
	}
	return payload
}
