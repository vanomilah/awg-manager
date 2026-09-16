package query

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hoaxisr/awg-manager/internal/ndms/cache"
	"github.com/hoaxisr/awg-manager/internal/ndms/transport"
)

const keenDNSTTL = 60 * time.Second

// KeenDNSInfo holds the router's KeenDNS domain registration and active cloud relay endpoints.
type KeenDNSInfo struct {
	Domain       string   `json:"domain"`
	Enabled      bool     `json:"enabled"`
	Address      string   `json:"address"`
	RelayIPs     []string `json:"relay_ips,omitempty"`
	RelayDomains []string `json:"relay_domains,omitempty"`
}

// KeenDNSStore caches KeenDNS status from NDMS.
type KeenDNSStore struct {
	*cache.KeyedStore[string, *KeenDNSInfo]
	getter Getter
	log    Logger
}

func NewKeenDNSStore(g Getter, log Logger) *KeenDNSStore {
	s := &KeenDNSStore{getter: g, log: log}
	s.KeyedStore = cache.NewKeyedStore(keenDNSTTL, log, "keendns", s.fetch)
	return s
}

// Get returns the current KeenDNS registration. Missing/unconfigured → nil, nil.
func (s *KeenDNSStore) Get(ctx context.Context) (*KeenDNSInfo, error) {
	return s.KeyedStore.Get(ctx, "status")
}

func (s *KeenDNSStore) fetch(ctx context.Context, _ string) (*KeenDNSInfo, error) {
	// Только /show/ndns — авторитетный эндпоинт KeenDNS на всех поддерживаемых
	// прошивках. 404 означает, что подсистема отсутствует на этой OS → KeenDNS
	// не настроен (а не ошибка), без него поллер сыпал бы ошибками каждый тик.
	raw, err := s.getter.GetRaw(ctx, "/show/ndns")
	if err != nil {
		var httpErr *transport.HTTPError
		if errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return parseKeenDNS(raw), nil
}

// parseKeenDNS строит FQDN доступа из полей booked + domain ответа /show/ndns
// (например booked="impod", domain="crazedns.ru" → "impod.crazedns.ru").
// Также извлекает активные реле-серверы из блока ttp.tunnel (IP-адреса и домены).
func parseKeenDNS(raw []byte) *KeenDNSInfo {
	var v struct {
		Booked  string `json:"booked"`
		Domain  string `json:"domain"`
		Address string `json:"address"`
		TTP     struct {
			Tunnel []struct {
				Target       string `json:"target"`
				TargetRemote string `json:"target-remote"`
			} `json:"tunnel"`
		} `json:"ttp"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	booked := strings.TrimSpace(v.Booked)
	domain := strings.TrimSpace(v.Domain)
	if booked == "" || domain == "" {
		return nil
	}

	var relayIPs []string
	var relayDomains []string
	seenIP := make(map[string]struct{})
	seenDomain := make(map[string]struct{})

	for _, t := range v.TTP.Tunnel {
		if remote := strings.TrimSpace(t.TargetRemote); remote != "" {
			host := remote
			if h, _, err := net.SplitHostPort(remote); err == nil {
				host = h
			}
			if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
				ipStr := ip.String()
				if _, ok := seenIP[ipStr]; !ok {
					seenIP[ipStr] = struct{}{}
					relayIPs = append(relayIPs, ipStr)
				}
			}
		}
		if target := strings.TrimSpace(t.Target); target != "" {
			host := target
			if h, _, err := net.SplitHostPort(target); err == nil {
				host = h
			}
			host = strings.ToLower(strings.TrimSpace(host))
			if host != "" {
				if _, ok := seenDomain[host]; !ok {
					seenDomain[host] = struct{}{}
					relayDomains = append(relayDomains, host)
				}
			}
		}
	}

	return &KeenDNSInfo{
		Domain:       booked + "." + domain,
		Enabled:      true,
		Address:      strings.TrimSpace(v.Address),
		RelayIPs:     relayIPs,
		RelayDomains: relayDomains,
	}
}
