package vlink

import "testing"

func TestIsClashYAML(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"proxies header", "proxies:\n  - name: a\n    type: vless\n", true},
		{"proxies header with leading whitespace lines", "\n# comment\nproxies:\n  - name: a\n", true},
		{"share-link vless", "vless://uuid@host:443?security=tls\n", false},
		{"html", "<!DOCTYPE html><html></html>", false},
		{"base64", "dmxlc3M6Ly91dWlkQGhvc3Q6NDQz", false},
		{"empty", "", false},
		{"plain trojan link", "trojan://pass@host:443?sni=h", false},
		{"clash-like but no proxies", "rules:\n  - DOMAIN,a.com,DIRECT\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsClashYAML([]byte(tc.body)); got != tc.want {
				t.Errorf("IsClashYAML(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}
