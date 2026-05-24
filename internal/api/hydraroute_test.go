package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/deviceproxy"
	"github.com/hoaxisr/awg-manager/internal/downloader"
	"github.com/hoaxisr/awg-manager/internal/storage"
)

func TestToDownloaderRoute(t *testing.T) {
	if got := toDownloaderRoute(nil); got != nil {
		t.Fatalf("nil route: got %+v", got)
	}
	got := toDownloaderRoute(&DownloadRouteDTO{Tag: "awg-a", Kind: "awg"})
	if got == nil {
		t.Fatal("expected non-nil route")
	}
	if got.Tag != "awg-a" || got.Kind != "awg" {
		t.Fatalf("unexpected route: %+v", got)
	}
}

func TestListDownloadOutbounds_NoDeviceProxy(t *testing.T) {
	h := NewHydraRouteHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/download/outbounds", nil)
	rr := httptest.NewRecorder()

	h.ListDownloadOutbounds(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"tag":"direct"`) {
		t.Fatalf("expected direct tag in response: %s", body)
	}
	if !strings.Contains(body, `"available":true`) {
		t.Fatalf("expected direct available=true in response: %s", body)
	}
}

func TestDownloadDeviceProxyAdapter(t *testing.T) {
	svc := &deviceproxy.Service{}
	adapter := &downloadDeviceProxyAdapter{svc: svc}
	list := adapter.ListDownloadOutbounds(context.Background())
	if len(list) == 0 {
		t.Fatal("expected at least direct outbound")
	}
	if list[0].Tag != "direct" {
		t.Fatalf("first outbound tag: got %q want direct", list[0].Tag)
	}
}

func TestDownloadSingboxInterfaceAssertion(t *testing.T) {
	var _ downloader.SingboxOperator = (*downloadSingboxAdapter)(nil)
}

func TestDownloadSettingsRouteProvider_DefaultDirect(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewSettingsStore(dir)
	if _, err := store.Load(); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	p := &downloadSettingsRouteProvider{store: store}
	route, err := p.GetDownloadRoute(context.Background())
	if err != nil {
		t.Fatalf("get route: %v", err)
	}
	if route == nil || route.Tag != "direct" {
		t.Fatalf("route = %+v, want direct", route)
	}
}

func TestDownloadSettingsRouteProvider_UsesStoredTag(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewSettingsStore(dir)
	st, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	st.Download.RouteTag = "awg-test"
	if err := store.Save(st); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	p := &downloadSettingsRouteProvider{store: store}
	route, err := p.GetDownloadRoute(context.Background())
	if err != nil {
		t.Fatalf("get route: %v", err)
	}
	if route == nil || route.Tag != "awg-test" {
		t.Fatalf("route = %+v, want awg-test", route)
	}

	// Ensure empty value is normalized to direct.
	st.Download.RouteTag = ""
	if err := store.Save(st); err != nil {
		t.Fatalf("save empty routeTag: %v", err)
	}
	route, err = p.GetDownloadRoute(context.Background())
	if err != nil {
		t.Fatalf("get route after empty: %v", err)
	}
	if route == nil || route.Tag != "direct" {
		t.Fatalf("route after empty = %+v, want direct", route)
	}

}
