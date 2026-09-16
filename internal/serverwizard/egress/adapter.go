package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hoaxisr/awg-manager/internal/routing"
)

type EgressKind string

const (
	EgressKindDirect         EgressKind = "direct"
	EgressKindInterface      EgressKind = "interface"
	EgressKindSocks          EgressKind = "socks"
	EgressKindRouterOutbound EgressKind = "router_outbound"
)

type EgressOption struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Kind        EgressKind `json:"kind"`
	Owner       string     `json:"owner"`
	Available   bool       `json:"available"`
	DegradedMsg string     `json:"degraded_msg,omitempty"`
	SupportsTCP bool       `json:"supports_tcp"`
	SupportsUDP bool       `json:"supports_udp"`
	Generation  uint64     `json:"generation"`
}

type TunnelCatalog interface {
	ListAll(ctx context.Context) []routing.TunnelEntry
}

// Resolver resolves an egress request for a server kind into concrete outbound parameters.
type Resolver interface {
	Resolve(ctx context.Context, req ResolveRequest) (ResolvedEgress, error)
}

type Adapter struct {
	catalog        TunnelCatalog
	generation     atomic.Uint64
	socksCacheMu   sync.Mutex
	socksCachedAt  time.Time
	socksAvailable bool
	dialTimeout    func(network, address string, timeout time.Duration) (net.Conn, error)
}

func NewAdapter(catalog TunnelCatalog) *Adapter {
	a := &Adapter{
		catalog:     catalog,
		dialTimeout: net.DialTimeout,
	}
	a.generation.Store(1)
	return a
}

// SetDialTimeout allows customizing the dialer function for tests.
func (a *Adapter) SetDialTimeout(dialFn func(network, address string, timeout time.Duration) (net.Conn, error)) {
	a.socksCacheMu.Lock()
	defer a.socksCacheMu.Unlock()
	a.dialTimeout = dialFn
	a.socksCachedAt = time.Time{}
}

func (a *Adapter) checkSocksAvailable() bool {
	a.socksCacheMu.Lock()
	defer a.socksCacheMu.Unlock()

	if !a.socksCachedAt.IsZero() && time.Since(a.socksCachedAt) < 2*time.Second {
		return a.socksAvailable
	}

	dialFn := a.dialTimeout
	if dialFn == nil {
		dialFn = net.DialTimeout
	}

	conn, err := dialFn("tcp", "127.0.0.1:1099", 200*time.Millisecond)
	a.socksCachedAt = time.Now()
	if err == nil {
		_ = conn.Close()
		a.socksAvailable = true
	} else {
		a.socksAvailable = false
	}
	return a.socksAvailable
}

// BumpGeneration increments the generation counter when routing state changes.
func (a *Adapter) BumpGeneration() uint64 {
	return a.generation.Add(1)
}

// ListOptions aggregates all system, tunnel, and proxy egress possibilities.
func (a *Adapter) ListOptions(ctx context.Context) []EgressOption {
	gen := a.generation.Load()
	options := make([]EgressOption, 0)

	// 1. Direct WAN
	options = append(options, EgressOption{
		ID:          "direct",
		Name:        "Прямой выход в интернет (WAN)",
		Kind:        EgressKindDirect,
		Owner:       "system",
		Available:   true,
		SupportsTCP: true,
		SupportsUDP: true,
		Generation:  gen,
	})

	// 2. Mihomo / Singbox Proxy Outbound (:1099 cached probe)
	socksOk := a.checkSocksAvailable()
	socksDegraded := ""
	if !socksOk {
		socksDegraded = "Mihomo SOCKS listener :1099 not responding"
	}
	options = append(options, EgressOption{
		ID:          "mihomo:1099",
		Name:        "Политики маршрутизации Mihomo (:1099)",
		Kind:        EgressKindSocks,
		Owner:       "mihomo",
		Available:   socksOk,
		DegradedMsg: socksDegraded,
		SupportsTCP: true,
		SupportsUDP: true,
		Generation:  gen,
	})

	// 3. Tunnels from Catalog
	if a.catalog != nil {
		tunnels := a.catalog.ListAll(ctx)
		for _, t := range tunnels {
			degradedMsg := ""
			if !t.Available {
				degradedMsg = fmt.Sprintf("Туннель недоступен (статус: %s)", t.Status)
			}
			name := t.Name
			if name == "" {
				name = t.ID
			}
			options = append(options, EgressOption{
				ID:          t.ID,
				Name:        fmt.Sprintf("%s (%s)", name, t.Iface),
				Kind:        EgressKindInterface,
				Owner:       t.Type,
				Available:   t.Available,
				DegradedMsg: degradedMsg,
				SupportsTCP: true,
				SupportsUDP: true,
				Generation:  gen,
			})
		}
	}

	// Sort stable: direct first, socks second, then by name
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Kind == EgressKindDirect {
			return true
		}
		if options[j].Kind == EgressKindDirect {
			return false
		}
		if options[i].Kind == EgressKindSocks {
			return true
		}
		if options[j].Kind == EgressKindSocks {
			return false
		}
		return options[i].Name < options[j].Name
	})

	return options
}

// FindOption returns an egress option by ID.
func (a *Adapter) FindOption(ctx context.Context, id string) (EgressOption, bool) {
	for _, opt := range a.ListOptions(ctx) {
		if opt.ID == id {
			return opt, true
		}
	}
	return EgressOption{}, false
}

// EgressSummary creates a deterministic summary string for state fingerprinting.
func (a *Adapter) EgressSummary(ctx context.Context) string {
	opts := a.ListOptions(ctx)
	summary := fmt.Sprintf("gen:%d", a.generation.Load())
	for _, o := range opts {
		summary += fmt.Sprintf("|%s:%t:%s", o.ID, o.Available, o.DegradedMsg)
	}
	return summary
}

// ResolveRequest represents a typed egress resolution request.
type ResolveRequest struct {
	ServerKind string `json:"server_kind"` // "xray" or "tgwebproxy"
	EgressID   string `json:"egress_id"`   // "direct", "mihomo:1099", "nwg1", etc.
}

// ResolvedEgress holds normalized outbound mode and target parameters.
type ResolvedEgress struct {
	Mode      string `json:"mode"`      // "direct", "socks", "interface"
	Interface string `json:"interface"` // e.g. "nwg1"
	SocksPort int    `json:"socks_port"` // e.g. 1099
}

// Resolve resolves an egress request for a target server kind into concrete outbound parameters.
func (a *Adapter) Resolve(ctx context.Context, req ResolveRequest) (ResolvedEgress, error) {
	serverKind := req.ServerKind
	egressID := req.EgressID
	if egressID == "" || egressID == "direct" {
		return ResolvedEgress{Mode: "direct"}, nil
	}

	if egressID == "mihomo:1099" || egressID == "socks" {
		if serverKind == "tgwebproxy" {
			return ResolvedEgress{}, errors.New("Telegram Proxy does not support SOCKS upstream egress; choose direct or an interface")
		}
		if !a.checkSocksAvailable() {
			return ResolvedEgress{}, fmt.Errorf("egress %s is unavailable: listener :1099 not responding", egressID)
		}
		return ResolvedEgress{Mode: "socks", SocksPort: 1099}, nil
	}

	if a.catalog != nil {
		tunnels := a.catalog.ListAll(ctx)
		for _, t := range tunnels {
			if t.ID == egressID || t.Iface == egressID {
				if !t.Available {
					return ResolvedEgress{}, fmt.Errorf("selected tunnel interface %s is degraded (%s)", t.Iface, t.Status)
				}
				if t.Iface == "" {
					return ResolvedEgress{}, fmt.Errorf("selected tunnel %s has no network interface", t.ID)
				}
				return ResolvedEgress{
					Mode:      "interface",
					Interface: t.Iface,
				}, nil
			}
		}
	}

	return ResolvedEgress{
		Mode:      "interface",
		Interface: egressID,
	}, nil
}
