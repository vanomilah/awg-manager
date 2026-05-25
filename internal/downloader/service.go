package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	singboxorch "github.com/hoaxisr/awg-manager/internal/singbox/orchestrator"
)

const (
	downloadProxyInboundTag  = "awgm-download-in"
	downloadProxySelectorTag = "awgm-download-selector"
	downloadProxyListenHost  = "127.0.0.1"
	downloadProxyListenPort  = 11998
	downloadCleanupTimeout   = 5 * time.Second
)

type Deps struct {
	Outbounds     OutboundsProvider
	Singbox       SingboxOperator
	Slot          SlotController
	RouteProvider RouteProvider
}

type Service struct {
	mu sync.Mutex

	depsMu    sync.RWMutex
	outbounds OutboundsProvider
	singbox   SingboxOperator
	slot      SlotController
	routeProv RouteProvider
}

func NewService(d Deps) *Service {
	return &Service{
		outbounds: d.Outbounds,
		singbox:   d.Singbox,
		slot:      d.Slot,
		routeProv: d.RouteProvider,
	}
}

func (s *Service) SetOutboundsProvider(p OutboundsProvider) {
	s.depsMu.Lock()
	defer s.depsMu.Unlock()
	s.outbounds = p
}

func (s *Service) SetSingboxOperator(op SingboxOperator) {
	s.depsMu.Lock()
	defer s.depsMu.Unlock()
	s.singbox = op
}

func (s *Service) SetSlotController(slot SlotController) {
	s.depsMu.Lock()
	defer s.depsMu.Unlock()
	s.slot = slot
}

func (s *Service) SetRouteProvider(p RouteProvider) {
	s.depsMu.Lock()
	defer s.depsMu.Unlock()
	s.routeProv = p
}

func (s *Service) snapshotDeps() (OutboundsProvider, SingboxOperator, SlotController, RouteProvider) {
	s.depsMu.RLock()
	defer s.depsMu.RUnlock()
	return s.outbounds, s.singbox, s.slot, s.routeProv
}

func (s *Service) ListOutbounds(ctx context.Context) []Outbound {
	outbounds, singbox, slot, _ := s.snapshotDeps()
	if outbounds == nil {
		return []Outbound{{
			Tag:       "direct",
			Kind:      "direct",
			Label:     "Direct (WAN)",
			Detail:    "без туннеля",
			Available: true,
		}}
	}

	items := outbounds.ListDownloadOutbounds(ctx)
	sbRunning := false
	if singbox != nil {
		sbRunning, _ = singbox.IsRunning()
	}
	downloadReady := sbRunning && slot != nil

	out := make([]Outbound, 0, len(items))
	for _, item := range items {
		available := item.Tag == "direct" || (downloadReady && strings.TrimSpace(item.Tag) != "")
		item.Available = available
		out = append(out, item)
	}
	return out
}

func (s *Service) DescribeRoute(ctx context.Context, route *Route) RouteInfo {
	outbounds, _, _, _ := s.snapshotDeps()
	return describeRouteWithProvider(ctx, outbounds, route)
}

func describeRouteWithProvider(ctx context.Context, outbounds OutboundsProvider, route *Route) RouteInfo {
	tag := "direct"
	if route != nil && strings.TrimSpace(route.Tag) != "" {
		tag = strings.TrimSpace(route.Tag)
	}
	if tag == "direct" {
		return RouteInfo{Tag: "direct", Kind: "direct", Label: "Direct (WAN)", Detail: "без туннеля"}
	}
	if outbounds != nil {
		for _, ob := range outbounds.ListDownloadOutbounds(ctx) {
			if ob.Tag == tag {
				return RouteInfo{Tag: ob.Tag, Kind: ob.Kind, Label: ob.Label, Detail: ob.Detail}
			}
		}
	}
	kind := ""
	if route != nil {
		kind = strings.TrimSpace(route.Kind)
	}
	return RouteInfo{Tag: tag, Kind: kind}
}

func (s *Service) ResolveClient(ctx context.Context, route *Route) (*Lease, error) {
	outbounds, singbox, slot, routeProvider := s.snapshotDeps()
	effectiveRoute := route
	if effectiveRoute == nil || strings.TrimSpace(effectiveRoute.Tag) == "" {
		if routeProvider != nil {
			providedRoute, err := routeProvider.GetDownloadRoute(ctx)
			if err != nil {
				return nil, fmt.Errorf("load download route settings: %w", err)
			}
			effectiveRoute = providedRoute
		}
	}
	info := describeRouteWithProvider(ctx, outbounds, effectiveRoute)
	if info.Tag == "direct" {
		return &Lease{
			Client: &http.Client{},
			Route:  info,
		}, nil
	}
	tag := info.Tag
	if singbox == nil {
		return nil, fmt.Errorf("selected outbound %q is unavailable: sing-box operator is not configured", tag)
	}
	if slot == nil {
		return nil, fmt.Errorf("selected outbound %q is unavailable: sing-box orchestrator is not configured", tag)
	}
	isRunning, _ := singbox.IsRunning()
	if !isRunning {
		return nil, fmt.Errorf("selected outbound %q is unavailable: sing-box is not running", tag)
	}

	members := []string{"direct"}
	found := false
	if outbounds != nil {
		for _, ob := range outbounds.ListDownloadOutbounds(ctx) {
			if ob.Tag == "" {
				continue
			}
			if ob.Tag != "direct" {
				members = append(members, ob.Tag)
			}
			if ob.Tag == tag {
				found = true
				info = RouteInfo{Tag: ob.Tag, Kind: ob.Kind, Label: ob.Label, Detail: ob.Detail}
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("selected outbound %q is unavailable for download transport", tag)
	}

	s.mu.Lock()
	proxyURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(downloadProxyListenHost, fmt.Sprintf("%d", downloadProxyListenPort)),
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	if dt, ok := http.DefaultTransport.(*http.Transport); ok && dt != nil {
		transport = dt.Clone()
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{
		Transport: transport,
	}
	lease := &Lease{
		Client: client,
		Route:  info,
	}
	lease.cleanup = func() {
		client.CloseIdleConnections()
		s.cleanupDownloadProxySlotLocked(singbox, slot)
		s.mu.Unlock()
	}

	selectedTag := selectedTagForSlot(tag)
	if err := s.applyDownloadProxySlotLocked(slot, members, selectedTag); err != nil {
		client.CloseIdleConnections()
		s.cleanupDownloadProxySlotLocked(singbox, slot)
		s.mu.Unlock()
		return nil, err
	}
	if err := s.selectDownloadOutboundWithRetry(ctx, singbox, selectedTag); err != nil {
		lease.Close()
		return nil, fmt.Errorf("select download outbound %q: %w", tag, err)
	}
	active, err := s.readSelectorActiveWithRetry(ctx, singbox, downloadProxySelectorTag)
	if err != nil {
		lease.Close()
		return nil, fmt.Errorf("verify download selector active member: %w", err)
	}
	slog.Info("downloader: selector state",
		"requestedTag", tag,
		"requestedKind", info.Kind,
		"activeTag", active,
		"members", members,
		"proxy", net.JoinHostPort(downloadProxyListenHost, fmt.Sprintf("%d", downloadProxyListenPort)),
	)
	if active != selectedTag {
		lease.Close()
		return nil, fmt.Errorf("download selector mismatch: requested %s, active %s", selectedTag, active)
	}
	return lease, nil
}

func selectedTagForSlot(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "direct"
	}
	return tag
}

func (s *Service) applyDownloadProxySlotLocked(slot SlotController, members []string, selectedTag string) error {
	if slot == nil {
		return fmt.Errorf("download proxy orchestrator is not configured")
	}
	uniq := uniqueDownloadMembers(members)
	slotPayload := map[string]any{
		"inbounds": []any{
			map[string]any{
				"type":        "mixed",
				"tag":         downloadProxyInboundTag,
				"listen":      downloadProxyListenHost,
				"listen_port": downloadProxyListenPort,
			},
		},
		"outbounds": []any{
			map[string]any{
				"type":                        "selector",
				"tag":                         downloadProxySelectorTag,
				"outbounds":                   uniq,
				"default":                     selectedTag,
				"interrupt_exist_connections": false,
			},
		},
		"route": map[string]any{
			"rules": []any{
				map[string]any{
					"inbound":  downloadProxyInboundTag,
					"outbound": downloadProxySelectorTag,
				},
			},
		},
	}
	raw, err := json.MarshalIndent(slotPayload, "", "  ")
	if err != nil {
		return fmt.Errorf("build download transport slot: %w", err)
	}
	if err := slot.SaveSilent(singboxorch.SlotDownloadProxy, raw); err != nil {
		return fmt.Errorf("save download transport slot: %w", err)
	}
	if err := slot.SetEnabledSilent(singboxorch.SlotDownloadProxy, true); err != nil {
		return fmt.Errorf("enable download transport slot: %w", err)
	}
	if err := slot.Reload(); err != nil {
		_ = slot.SetEnabledSilent(singboxorch.SlotDownloadProxy, false)
		_ = slot.Reload()
		return fmt.Errorf("reload sing-box with download transport slot: %w", err)
	}
	return nil
}

func uniqueDownloadMembers(members []string) []string {
	uniq := make([]string, 0, len(members))
	seen := map[string]struct{}{}
	uniq = append(uniq, "direct")
	seen["direct"] = struct{}{}
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		uniq = append(uniq, m)
	}
	return uniq
}

func (s *Service) selectDownloadOutboundWithRetry(ctx context.Context, op SingboxOperator, memberTag string) error {
	const (
		attempts = 20
		pause    = 250 * time.Millisecond
	)
	var lastErr error
	for i := 0; i < attempts; i++ {
		lastErr = op.SetSelectorDefault(ctx, downloadProxySelectorTag, memberTag)
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
	return lastErr
}

func (s *Service) readSelectorActiveWithRetry(ctx context.Context, op SingboxOperator, selectorTag string) (string, error) {
	const (
		attempts = 20
		pause    = 250 * time.Millisecond
	)
	var lastErr error
	for i := 0; i < attempts; i++ {
		active, err := op.GetSelectorActive(ctx, selectorTag)
		if err == nil && strings.TrimSpace(active) != "" {
			return active, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("empty active member")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pause):
		}
	}
	return "", lastErr
}

func (s *Service) cleanupDownloadProxySlotLocked(op SingboxOperator, slot SlotController) {
	if op != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), downloadCleanupTimeout)
		if err := op.SetSelectorDefault(cleanupCtx, downloadProxySelectorTag, "direct"); err != nil {
			slog.Warn("downloader: failed to restore selector to direct", "error", err)
		}
		cancel()
	}
	if slot == nil {
		return
	}
	if err := slot.SetEnabledSilent(singboxorch.SlotDownloadProxy, false); err != nil {
		slog.Warn("downloader: failed to disable download proxy slot", "error", err)
		return
	}
	if err := slot.Reload(); err != nil {
		slog.Warn("downloader: failed to reload after disabling download proxy slot", "error", err)
	}
}
