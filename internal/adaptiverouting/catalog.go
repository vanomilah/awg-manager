package adaptiverouting

import (
	"context"
	"fmt"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/mihomonative"
)

type TunnelProvider interface {
	ListTunnels() []TunnelInfo
}

type TunnelInfo struct {
	ID        string
	Name      string
	Interface string
	Active    bool
	Kind      string // "awg" | "native" | "wdtt" | "freeturn"
}

type Catalog struct {
	nativeStore    *mihomonative.Store
	tunnelProvider TunnelProvider
}

func NewCatalog(nativeStore *mihomonative.Store, tunnelProvider TunnelProvider) *Catalog {
	return &Catalog{
		nativeStore:    nativeStore,
		tunnelProvider: tunnelProvider,
	}
}

func (c *Catalog) ListEgresses(ctx context.Context) ([]ResolvedEgress, error) {
	var results []ResolvedEgress

	// 1. Kernel Tunnels
	if c.tunnelProvider != nil {
		for _, t := range c.tunnelProvider.ListTunnels() {
			if strings.HasPrefix(t.Name, "awg-manager:") || strings.HasPrefix(t.ID, "awg-manager:") ||
				strings.HasPrefix(t.Interface, "Proxy") || strings.HasPrefix(t.ID, "wan:") {
				continue
			}
			available := t.Active && t.Interface != ""
			reason := ""
			if !available {
				reason = "Туннель выключен или интерфейс не поднят"
			}
			results = append(results, ResolvedEgress{
				Ref: EgressRef{
					Kind:       EgressKindKernelTunnel,
					ResourceID: t.ID,
					Engine:     EngineSystem,
				},
				DisplayName: t.Name,
				Interface:   t.Interface,
				Capabilities: Capabilities{
					TCP:  true,
					UDP:  true,
					ICMP: true,
					IPv4: true,
				},
				Available:         available,
				UnavailableReason: reason,
			})
		}
	}

	// 2. Mihomo Native Resources
	if c.nativeStore != nil {
		// Proxies
		for _, p := range c.nativeStore.ListProxies() {
			available := p.Enabled
			reason := ""
			if !available {
				reason = "Прокси-узел отключен"
			}
			results = append(results, ResolvedEgress{
				Ref: EgressRef{
					Kind:       EgressKindMihomoProxy,
					ResourceID: p.ID,
					Engine:     EngineMihomo,
				},
				DisplayName: p.Name,
				Interface:   "awgsus0",
				Capabilities: Capabilities{
					TCP:  true,
					UDP:  true,
					IPv4: true,
				},
				Available:         available,
				UnavailableReason: reason,
			})
		}

		// Subscriptions
		for _, s := range c.nativeStore.ListSubscriptions() {
			available := s.Enabled
			reason := ""
			if !available {
				reason = "Подписка отключена"
			}
			results = append(results, ResolvedEgress{
				Ref: EgressRef{
					Kind:       EgressKindMihomoSubscription,
					ResourceID: s.ID,
					Engine:     EngineMihomo,
				},
				DisplayName: s.Name,
				Interface:   "awgsus0",
				Capabilities: Capabilities{
					TCP:  true,
					UDP:  true,
					IPv4: true,
				},
				Available:         available,
				UnavailableReason: reason,
			})
		}

		// Groups
		for _, g := range c.nativeStore.ListGroups() {
			available := g.Enabled
			reason := ""
			if !available {
				reason = "Группа отключена"
			} else if len(g.Proxies) == 0 && len(g.Use) == 0 {
				available = false
				reason = "В группе нет доступных узлов"
			}
			results = append(results, ResolvedEgress{
				Ref: EgressRef{
					Kind:       EgressKindMihomoGroup,
					ResourceID: g.ID,
					Engine:     EngineMihomo,
				},
				DisplayName: g.Name,
				Interface:   "awgsus0",
				Capabilities: Capabilities{
					TCP:  true,
					UDP:  !g.DisableUDP,
					IPv4: true,
				},
				Available:         available,
				UnavailableReason: reason,
			})
		}
	}

	return results, nil
}

func (c *Catalog) Resolve(ctx context.Context, ref EgressRef) (ResolvedEgress, error) {
	all, err := c.ListEgresses(ctx)
	if err != nil {
		return ResolvedEgress{}, err
	}

	for _, eg := range all {
		if eg.Ref.Kind == ref.Kind && eg.Ref.Engine == ref.Engine {
			if eg.Ref.ResourceID == ref.ResourceID || strings.EqualFold(eg.DisplayName, ref.ResourceID) {
				return eg, nil
			}
		}
	}

	return ResolvedEgress{}, fmt.Errorf("egress target %s/%s not found", ref.Kind, ref.ResourceID)
}
