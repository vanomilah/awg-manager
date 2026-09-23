package main

import (
	"context"
	"fmt"

	"github.com/hoaxisr/awg-manager/internal/adaptiverouting"
	"github.com/hoaxisr/awg-manager/internal/api"
	ndmsquery "github.com/hoaxisr/awg-manager/internal/ndms/query"
	"github.com/hoaxisr/awg-manager/internal/tunnel"
)

type adaptiveTunnelAdapter struct {
	app *app
}

func (a *adaptiveTunnelAdapter) ListTunnels() []adaptiverouting.TunnelInfo {
	if a.app == nil {
		return nil
	}
	var out []adaptiverouting.TunnelInfo
	seenIDs := make(map[string]bool)

	if a.app.catalog != nil {
		for _, e := range a.app.catalog.ListAll(context.Background()) {
			seenIDs[e.ID] = true
			if e.Iface != "" {
				seenIDs[e.Iface] = true
			}
			out = append(out, adaptiverouting.TunnelInfo{
				ID:        e.ID,
				Name:      e.Name,
				Interface: e.Iface,
				Active:    e.Available && e.Status != "disabled" && e.Status != "stopped",
				Kind:      e.Type,
			})
		}
	}

	if a.app.tunnelService != nil {
		if tunnels, err := a.app.tunnelService.List(context.Background()); err == nil {
			for _, t := range tunnels {
				if seenIDs[t.ID] || (t.InterfaceName != "" && seenIDs[t.InterfaceName]) {
					continue
				}
				seenIDs[t.ID] = true
				out = append(out, adaptiverouting.TunnelInfo{
					ID:        t.ID,
					Name:      t.Name,
					Interface: t.InterfaceName,
					Active:    t.State == tunnel.StateRunning,
					Kind:      string(t.Backend),
				})
			}
		}
	}
	return out
}

func (a *app) setupAdaptiveRouting() {
	store, err := adaptiverouting.NewStore(a.dataDir)
	if err != nil {
		panic(fmt.Sprintf("initialize Susanin store: %v", err))
	}
	a.adaptiveRoutingStore = store

	tunnelAdapter := &adaptiveTunnelAdapter{app: a}
	catalog := adaptiverouting.NewCatalog(a.mihomoNativeStore, tunnelAdapter)
	refChecker := adaptiverouting.NewReferenceChecker(store)

	if a.mihomoNativeStore != nil {
		a.mihomoNativeStore.SetInUseChecker(refChecker.InUseChecker())
	}

	svc := adaptiverouting.NewService(a.dataDir, catalog, refChecker, store)

	if a.ndmsTransportClient != nil {
		markStore := ndmsquery.NewPolicyMarkStore(a.ndmsTransportClient, nil)
		svc.SetPolicyMarkResolver(markStore)
	}

	systemExec := adaptiverouting.NewSystemExecutor()
	mihomoExec := adaptiverouting.NewMihomoExecutor(a.mihomoNativeStore, nil)
	singboxExec := adaptiverouting.NewSingboxExecutor(a.sbOrch)
	svc.SetExecutors(systemExec, mihomoExec, singboxExec)
	a.adaptiveRoutingMihomoExec = mihomoExec

	handler := api.NewAdaptiveRoutingHandler(svc)
	a.adaptiveRoutingSvc = svc
	a.adaptiveRoutingHandler = handler
}
