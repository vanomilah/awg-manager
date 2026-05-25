package downloader

import (
	"context"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/deviceproxy"
	"github.com/hoaxisr/awg-manager/internal/singbox"
	"github.com/hoaxisr/awg-manager/internal/storage"
)

type deviceProxyOutboundsProvider struct {
	svc *deviceproxy.Service
}

type singboxOperatorAdapter struct {
	op *singbox.Operator
}

type settingsRouteProvider struct {
	store *storage.SettingsStore
}

func NewDeviceProxyOutboundsProvider(svc *deviceproxy.Service) OutboundsProvider {
	if svc == nil {
		return nil
	}
	return &deviceProxyOutboundsProvider{svc: svc}
}

func NewSingboxOperatorAdapter(op *singbox.Operator) SingboxOperator {
	if op == nil {
		return nil
	}
	return &singboxOperatorAdapter{op: op}
}

func NewSettingsRouteProvider(store *storage.SettingsStore) RouteProvider {
	if store == nil {
		return nil
	}
	return &settingsRouteProvider{store: store}
}

func NewSettingsBackedService(
	deviceProxySvc *deviceproxy.Service,
	singboxOp *singbox.Operator,
	slot SlotController,
	store *storage.SettingsStore,
) *Service {
	return NewService(Deps{
		Outbounds:     NewDeviceProxyOutboundsProvider(deviceProxySvc),
		Singbox:       NewSingboxOperatorAdapter(singboxOp),
		Slot:          slot,
		RouteProvider: NewSettingsRouteProvider(store),
	})
}

func (a *deviceProxyOutboundsProvider) ListDownloadOutbounds(ctx context.Context) []Outbound {
	if a == nil || a.svc == nil {
		return nil
	}
	src := a.svc.ListOutbounds(ctx)
	out := make([]Outbound, 0, len(src))
	for _, ob := range src {
		out = append(out, Outbound{
			Tag:    ob.Tag,
			Kind:   ob.Kind,
			Label:  ob.Label,
			Detail: ob.Detail,
		})
	}
	return out
}

func (a *singboxOperatorAdapter) IsRunning() (bool, int) {
	return a.op.IsRunningPublic()
}

func (a *singboxOperatorAdapter) SetSelectorDefault(ctx context.Context, selectorTag, memberTag string) error {
	return a.op.SetSelectorDefault(ctx, selectorTag, memberTag)
}

func (a *singboxOperatorAdapter) GetSelectorActive(ctx context.Context, selectorTag string) (string, error) {
	return a.op.GetSelectorActive(ctx, selectorTag)
}

func (p *settingsRouteProvider) GetDownloadRoute(ctx context.Context) (*Route, error) {
	if p == nil || p.store == nil {
		return &Route{Tag: "direct"}, nil
	}
	st, err := p.store.Get()
	if err != nil {
		return nil, err
	}
	tag := strings.TrimSpace(st.Download.RouteTag)
	if tag == "" {
		tag = "direct"
	}
	return &Route{Tag: tag}, nil
}
