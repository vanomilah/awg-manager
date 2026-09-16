package xrayserver

import (
	"fmt"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/xrayconfig"
)

// LegacyToManagedConfig translates legacy single-inbound Config into the comprehensive ManagedConfig.
func LegacyToManagedConfig(legacy Config) *xrayconfig.ManagedConfig {
	listenAddr := strings.TrimSpace(legacy.ListenAddress)
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}
	listenPort := legacy.ListenPort
	if listenPort <= 0 {
		listenPort = 9008
	}

	transport := strings.ToLower(strings.TrimSpace(legacy.Transport))
	if transport == "" {
		transport = "xhttp"
	}

	path := legacy.Path
	if path == "" {
		path = "/cdn-bridge/"
	}

	// 1. Build Inbound
	clients := make([]xrayconfig.Client, 0, len(legacy.Clients))
	for _, c := range legacy.Clients {
		email := c.Remark
		if email == "" {
			email = fmt.Sprintf("client-%s@awgm", c.ID[:8])
		}
		clients = append(clients, xrayconfig.Client{
			ID:        c.ID,
			UUID:      c.ID,
			Remark:    c.Remark,
			Email:     email,
			Enabled:   c.Enabled,
			CreatedAt: c.CreatedAt,
		})
	}

	inbound := xrayconfig.Inbound{
		Tag:       "vless-in",
		Listen:    listenAddr,
		Port:      listenPort,
		Protocol:  "vless",
		Transport: transport,
		Security:  "none",
		Path:      path,
		Host:      legacy.PublicDomain,
		Clients:   clients,
		Sniffing: &xrayconfig.SniffingConfig{
			Enabled:      true,
			DestOverride: []string{"http", "tls"},
		},
	}

	// 2. Build Outbounds & Routing
	outbounds := make([]xrayconfig.Outbound, 0, 2)
	var primaryOutboundTag string

	switch legacy.OutboundMode {
	case "socks":
		socksPort := legacy.OutboundSocksPort
		if socksPort <= 0 {
			socksPort = 1099
		}
		primaryOutboundTag = "mihomo-proxy"
		outbounds = append(outbounds, xrayconfig.Outbound{
			Tag:      primaryOutboundTag,
			Protocol: "socks",
			Server:   "127.0.0.1",
			Port:     socksPort,
		})
		outbounds = append(outbounds, xrayconfig.Outbound{
			Tag:      "direct",
			Protocol: "freedom",
		})

	case "interface":
		if legacy.OutboundInterface != "" {
			primaryOutboundTag = "interface-out"
			outbounds = append(outbounds, xrayconfig.Outbound{
				Tag:         primaryOutboundTag,
				Protocol:    "freedom",
				SendThrough: legacy.OutboundInterface,
			})
			outbounds = append(outbounds, xrayconfig.Outbound{
				Tag:      "direct",
				Protocol: "freedom",
			})
		} else {
			primaryOutboundTag = "direct"
			outbounds = append(outbounds, xrayconfig.Outbound{
				Tag:      "direct",
				Protocol: "freedom",
			})
		}

	default:
		if legacy.OutboundSocksPort > 0 {
			primaryOutboundTag = "mihomo-proxy"
			outbounds = append(outbounds, xrayconfig.Outbound{
				Tag:      primaryOutboundTag,
				Protocol: "socks",
				Server:   "127.0.0.1",
				Port:     legacy.OutboundSocksPort,
			})
			outbounds = append(outbounds, xrayconfig.Outbound{
				Tag:      "direct",
				Protocol: "freedom",
			})
		} else {
			primaryOutboundTag = "direct"
			outbounds = append(outbounds, xrayconfig.Outbound{
				Tag:      "direct",
				Protocol: "freedom",
			})
		}
	}

	routingRules := []xrayconfig.RoutingRule{
		{
			Type:        "field",
			InboundTag:  []string{"vless-in"},
			OutboundTag: primaryOutboundTag,
		},
	}

	return &xrayconfig.ManagedConfig{
		LogLevel:     "warning",
		StatsEnabled: true,
		Inbounds:     []xrayconfig.Inbound{inbound},
		Outbounds:    outbounds,
		RoutingRules: routingRules,
	}
}
