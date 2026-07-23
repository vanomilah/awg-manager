package vlink

import (
	"encoding/json"
	"strings"
	"testing"
)

const trustTunnelTOMLSample = `loglevel = "info"
vpn_mode = "general"
killswitch_enabled = true
killswitch_allow_ports =[]
post_quantum_group_enabled = true
exclusions =["*.ru", "*.su", "*.рф", "*.yandex.net", "*.yandex.com", "*.vk.com", "*.vk.me"]
dns_upstreams =["https://dns.adguard-dns.com/dns-query", "quic://dns.adguard-dns.com"]

[endpoint]
hostname = "nl3.trutun.online"
addresses =["nl3.trutun.online:443"]
has_ipv6 = false
username = "user_1353818979"
password = "8eOprVpaxQx6"
skip_verification = false
upstream_protocol = "http3"
upstream_fallback_protocol = "http2"
anti_dpi = true
custom_sni = "nl3.trutun.online"
client_random_prefix = "cb52cee8"

[listener]
[listener.tun]
bound_if = ""
included_routes =["0.0.0.0/0", "2000::/3"]
excluded_routes =["0.0.0.0/8", "10.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/3"]
mtu_size = 1280
change_system_dns = true
`

func TestIsTrustTunnelTOML_Sample(t *testing.T) {
	if !IsTrustTunnelTOML([]byte(trustTunnelTOMLSample)) {
		t.Fatal("canonical sample must be detected as TrustTunnel TOML")
	}
}

func TestIsTrustTunnelTOML_ShortInline(t *testing.T) {
	inline := `[endpoint]
hostname = "nl3.trutun.online"
addresses = ["nl3.trutun.online:443"]
username = "user_1353818979"
password = "8eOprVpaxQx6"
`
	if !IsTrustTunnelTOML([]byte(inline)) {
		t.Fatal("short inline starting with [endpoint] must be detected")
	}
}

func TestParseTrustTunnelTOML_Sample(t *testing.T) {
	res := ParseTrustTunnelTOML([]byte(trustTunnelTOMLSample))
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %+v", res.Errors)
	}
	if len(res.Outbounds) != 1 {
		t.Fatalf("outbounds=%d want 1", len(res.Outbounds))
	}
	p := res.Outbounds[0]
	if p.Protocol != "trusttunnel" || p.Server != "nl3.trutun.online" || p.Port != 443 {
		t.Fatalf("unexpected parsed outbound: %+v", p)
	}
	var ob map[string]any
	if err := json.Unmarshal(p.Outbound, &ob); err != nil {
		t.Fatalf("outbound json: %v", err)
	}
	if ob["type"] != "trusttunnel" {
		t.Fatalf("type=%v", ob["type"])
	}
	if ob["username"] != "user_1353818979" || ob["password"] != "8eOprVpaxQx6" {
		t.Fatalf("auth fields: %+v", ob)
	}
	if ob["quic"] != true {
		t.Fatalf("quic=%v want true for http3", ob["quic"])
	}
	tls, _ := ob["tls"].(map[string]any)
	if tls == nil || tls["enabled"] != true || tls["server_name"] != "nl3.trutun.online" {
		t.Fatalf("tls=%+v", tls)
	}
}

func TestParseTrustTunnelTOML_HTTP2(t *testing.T) {
	body := strings.Replace(trustTunnelTOMLSample, `upstream_protocol = "http3"`, `upstream_protocol = "http2"`, 1)
	res := ParseTrustTunnelTOML([]byte(body))
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %+v", res.Errors)
	}
	var ob map[string]any
	if err := json.Unmarshal(res.Outbounds[0].Outbound, &ob); err != nil {
		t.Fatal(err)
	}
	if _, hasQUIC := ob["quic"]; hasQUIC {
		t.Fatalf("http2 must not set quic, got %+v", ob)
	}
}

func TestParseTrustTunnelTOML_SkipVerification(t *testing.T) {
	body := strings.Replace(trustTunnelTOMLSample, `skip_verification = false`, `skip_verification = true`, 1)
	res := ParseTrustTunnelTOML([]byte(body))
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %+v", res.Errors)
	}
	var ob map[string]any
	if err := json.Unmarshal(res.Outbounds[0].Outbound, &ob); err != nil {
		t.Fatal(err)
	}
	tls, _ := ob["tls"].(map[string]any)
	if tls["insecure"] != true {
		t.Fatalf("insecure=%v want true", tls["insecure"])
	}
}

func TestIsTrustTunnelTOML_Negatives(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"json", `{"outbounds":[]}`},
		{"mieru json", `{"profiles":[]}`},
		{"clash", "proxies:\n  - type: vless\n    name: x\n    server: h\n    port: 443\n    uuid: u"},
		{"no endpoint", `loglevel = "info"`},
		{"endpoint without auth", "[endpoint]\naddresses = [\"h:443\"]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsTrustTunnelTOML([]byte(tc.body)) {
				t.Fatalf("IsTrustTunnelTOML(%q) = true, want false", tc.body)
			}
		})
	}
}

func TestParseTrustTunnelTOML_MissingAddresses(t *testing.T) {
	body := `[endpoint]
username = "u"
password = "p"
`
	res := ParseTrustTunnelTOML([]byte(body))
	if len(res.Errors) != 1 {
		t.Fatalf("errors=%+v want 1", res.Errors)
	}
}
