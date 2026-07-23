// Package vlink: поддержка канонического TOML-конфига клиента TrustTunnel.
//
// TrustTunnel CLI и мобильные клиенты экспортируют настройки endpoint как
// TOML-документ с секцией [endpoint] (hostname, addresses, username,
// password, upstream_protocol, ...). Клиентские поля (vpn_mode, killswitch,
// dns_upstreams, exclusions, [listener]) для sing-box-туннеля не нужны —
// конвертируем только endpoint в outbound type=trusttunnel.
//
// Entry points: IsTrustTunnelTOML детектирует формат; ParseTrustTunnelTOML
// возвращает BatchResult, по форме идентичный ParseMieruClientJSON /
// ParseSingboxBody.
package vlink

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/BurntSushi/toml"
)

type trustTunnelClientConfig struct {
	Endpoint trustTunnelEndpointConfig `toml:"endpoint"`
}

type trustTunnelEndpointConfig struct {
	Hostname                 string   `toml:"hostname"`
	Addresses                []string `toml:"addresses"`
	Username                 string   `toml:"username"`
	Password                 string   `toml:"password"`
	SkipVerification         bool     `toml:"skip_verification"`
	Certificate              string   `toml:"certificate"`
	UpstreamProtocol         string   `toml:"upstream_protocol"`
	UpstreamFallbackProtocol string   `toml:"upstream_fallback_protocol"`
	AntiDPI                  bool     `toml:"anti_dpi"`
	CustomSNI                string   `toml:"custom_sni"`
	ClientRandom             string   `toml:"client_random"`
	ClientRandomPrefix       string   `toml:"client_random_prefix"`
}

// IsTrustTunnelTOML reports whether body looks like a TrustTunnel client
// config TOML: секция [endpoint] с addresses, username и password. JSON и
// Clash YAML отсеиваются до разбора TOML.
func IsTrustTunnelTOML(body []byte) bool {
	trimmed := trimLeadingSpace(stripUTF8BOM(body))
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '{' {
		return false
	}
	if LooksLikeJSON(body) {
		return false
	}
	if IsClashYAML(body) {
		return false
	}
	if !strings.Contains(string(trimmed), "[endpoint]") {
		return false
	}
	var probe trustTunnelClientConfig
	if err := toml.Unmarshal(trimmed, &probe); err != nil {
		return false
	}
	ep := probe.Endpoint
	return len(ep.Addresses) > 0 && ep.Username != "" && ep.Password != ""
}

// ParseTrustTunnelTOML парсит канонический TrustTunnel client config TOML и
// возвращает BatchResult с одним trusttunnel outbound.
func ParseTrustTunnelTOML(body []byte) BatchResult {
	out := BatchResult{}
	fail := func(msg string) BatchResult {
		out.Errors = append(out.Errors, ParseError{LineIdx: 0, Scheme: "trusttunnel-toml", Message: msg})
		return out
	}
	var cfg trustTunnelClientConfig
	if _, err := toml.Decode(string(stripUTF8BOM(body)), &cfg); err != nil {
		return fail(fmt.Sprintf("не удалось разобрать TrustTunnel TOML: %s", err))
	}
	parsed, err := trustTunnelEndpointToOutbound(cfg.Endpoint)
	if err != nil {
		return fail(fmt.Sprintf("не удалось обработать TrustTunnel конфиг: %s", err))
	}
	out.Outbounds = []ParsedOutbound{*parsed}
	return out
}

func trustTunnelEndpointToOutbound(ep trustTunnelEndpointConfig) (*ParsedOutbound, error) {
	if len(ep.Addresses) == 0 {
		return nil, fmt.Errorf("в [endpoint] нет addresses")
	}
	if ep.Username == "" {
		return nil, fmt.Errorf("в [endpoint] нет username")
	}
	if ep.Password == "" {
		return nil, fmt.Errorf("в [endpoint] нет password")
	}
	server, port, err := parseTrustTunnelAddress(ep.Addresses[0])
	if err != nil {
		return nil, fmt.Errorf("addresses[0]: %w", err)
	}
	sni := strings.TrimSpace(ep.CustomSNI)
	if sni == "" {
		sni = strings.TrimSpace(ep.Hostname)
	}
	if sni == "" {
		sni = server
	}

	quic := trustTunnelUsesQUIC(ep.UpstreamProtocol)
	tagHost := strings.TrimSpace(ep.Hostname)
	if tagHost == "" {
		tagHost = server
	}
	tag := fmt.Sprintf("trusttunnel-%s-%d", sanitizeTagPart(tagHost), port)

	ob := map[string]any{
		"type":        "trusttunnel",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"username":    ep.Username,
		"password":    ep.Password,
		"network":     []any{"tcp", "udp"},
		"tls": map[string]any{
			"enabled":     true,
			"server_name": sni,
		},
	}
	if ep.SkipVerification {
		tls := ob["tls"].(map[string]any)
		tls["insecure"] = true
	}
	if cert := strings.TrimSpace(ep.Certificate); cert != "" {
		tls := ob["tls"].(map[string]any)
		tls["certificate"] = cert
	}
	if quic {
		ob["quic"] = true
	}

	raw, err := json.Marshal(ob)
	if err != nil {
		return nil, fmt.Errorf("marshal outbound: %w", err)
	}
	label := tagHost
	if port != 443 {
		label = fmt.Sprintf("%s:%d", tagHost, port)
	}
	return &ParsedOutbound{
		Tag:      tag,
		Protocol: "trusttunnel",
		Server:   server,
		Port:     port,
		Outbound: raw,
		Label:    label,
	}, nil
}

func parseTrustTunnelAddress(raw string) (host string, port uint16, err error) {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return "", 0, fmt.Errorf("пустой address")
	}
	if !strings.Contains(addr, ":") {
		return addr, 443, nil
	}
	hostPart, portPart, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return "", 0, splitErr
	}
	hostPart = strings.TrimSpace(hostPart)
	if hostPart == "" {
		return "", 0, fmt.Errorf("пустой host в %q", raw)
	}
	portN, convErr := parsePort(portPart)
	if convErr != nil {
		return "", 0, convErr
	}
	return hostPart, portN, nil
}

func trustTunnelUsesQUIC(upstreamProtocol string) bool {
	switch strings.ToLower(strings.TrimSpace(upstreamProtocol)) {
	case "http3", "h3", "quic":
		return true
	default:
		return false
	}
}

func parsePort(raw string) (uint16, error) {
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &port); err != nil {
		return 0, fmt.Errorf("некорректный port %q", raw)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d вне диапазона", port)
	}
	return uint16(port), nil
}
