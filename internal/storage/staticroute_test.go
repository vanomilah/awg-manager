package storage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStaticRouteList_IconURL_RoundTrip(t *testing.T) {
	t.Run("iconUrl survives marshal/unmarshal", func(t *testing.T) {
		orig := StaticRouteList{
			ID:       "sr_1",
			Name:     "datacenter",
			TunnelID: "tun_de",
			IconURL:  "https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Cloudflare.png",
			Subnets:  []string{"10.0.0.0/8"},
			Enabled:  true,
		}
		raw, err := json.Marshal(orig)
		if err != nil {
			t.Fatal(err)
		}
		var got StaticRouteList
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.IconURL != orig.IconURL {
			t.Errorf("IconURL = %q, want %q", got.IconURL, orig.IconURL)
		}
	})

	t.Run("empty iconUrl is omitted in JSON", func(t *testing.T) {
		raw, err := json.Marshal(StaticRouteList{ID: "sr_1", Name: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "iconUrl") {
			t.Errorf("expected iconUrl to be omitted, got: %s", raw)
		}
	})

	t.Run("legacy JSON without iconUrl unmarshals fine", func(t *testing.T) {
		legacy := []byte(`{"id":"sr_1","name":"x","tunnelID":"t","subnets":["10.0.0.0/8"],"enabled":true,"createdAt":"","updatedAt":""}`)
		var got StaticRouteList
		if err := json.Unmarshal(legacy, &got); err != nil {
			t.Fatal(err)
		}
		if got.IconURL != "" {
			t.Errorf("IconURL = %q, want empty string", got.IconURL)
		}
	})
}
