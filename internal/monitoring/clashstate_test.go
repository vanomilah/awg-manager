package monitoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeClashProxiesServer responds to GET /proxies with the provided body.
func fakeClashProxiesServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/proxies") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestClashState_LatencyForOutbound_HappyPath(t *testing.T) {
	upstream := fakeClashProxiesServer(t, `{
		"proxies": {
			"vless-1": {"name":"vless-1","type":"VLESS","history":[{"delay":45}]},
			"vless-2": {"name":"vless-2","type":"VLESS","history":[{"delay":78},{"delay":92}]}
		}
	}`)
	t.Cleanup(upstream.Close)

	cs := NewClashState(func() string { return upstream.URL }, upstream.Client())
	cs.cacheTTL = 1 * time.Second

	delay, ok := cs.LatencyForOutbound(context.Background(), "vless-1")
	if !ok || delay != 45 {
		t.Fatalf("vless-1: got (%d, %v), want (45, true)", delay, ok)
	}
	delay, ok = cs.LatencyForOutbound(context.Background(), "vless-2")
	if !ok || delay != 92 {
		t.Fatalf("vless-2: got (%d, %v), want (92, true)", delay, ok)
	}
	delay, ok = cs.LatencyForOutbound(context.Background(), "missing-tag")
	if ok || delay != 0 {
		t.Errorf("missing-tag: got (%d, %v), want (0, false)", delay, ok)
	}
}
