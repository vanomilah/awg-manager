package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/xrayserver"
	"github.com/hoaxisr/awg-manager/internal/xrayserver/xraybin"
)

func TestXrayStatusSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	_ = os.MkdirAll(binDir, 0755)
	binPath := filepath.Join(binDir, "xray")
	res := xraybin.NewResolverWithPaths(tmpDir, filepath.Join(tmpDir, "none.list"), []string{binPath})

	h := NewXrayHandler()
	h.SetResolver(res)

	// 1. Uninstalled
	st := h.GetStatus(context.Background())
	if st.Installed {
		t.Errorf("expected not installed")
	}

	// 2. Mock binary
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755)
	st = h.GetStatus(context.Background())
	if !st.Installed {
		t.Errorf("expected installed")
	}

	// 3. Service configuration integration
	dataDir := filepath.Join(tmpDir, "data")
	svc := xrayserver.New(dataDir, nil)
	if err := svc.UpdateConfig(xrayserver.Config{
		Enabled:      false,
		ListenPort:   9008,
		Path:         "/test-path",
		PublicDomain: "cl-test.edgecdn.ru",
		UplinkMethod: "PUT",
		Clients: []xrayserver.Client{
			{ID: "11111111-2222-3333-4444-555555555555", Remark: "Test Client", Enabled: true},
		},
	}); err != nil {
		t.Fatalf("unexpected UpdateConfig error: %v", err)
	}
	h.SetService(svc)

	st = h.GetStatus(context.Background())
	if st.Path != "/test-path" {
		t.Errorf("expected path /test-path, got %s", st.Path)
	}
	if st.CDNHost != "cl-test.edgecdn.ru" {
		t.Errorf("expected CDNHost cl-test.edgecdn.ru, got %s", st.CDNHost)
	}
	if !strings.Contains(st.Link, "cl-test.edgecdn.ru") {
		t.Errorf("expected link to contain CDN host, got %s", st.Link)
	}
}

func TestXrayHTTPHandler(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xray")
	res := xraybin.NewResolverWithPaths(tmpDir, filepath.Join(tmpDir, "none.list"), []string{binPath})

	h := NewXrayHandler()
	h.SetResolver(res)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, func(hf http.HandlerFunc) http.HandlerFunc { return hf })

	// Test GET /api/xray/status
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/xray/status", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify old mutating endpoints are gone and return 404
	obsoleteEndpoints := []string{
		"/api/xray/start",
		"/api/xray/stop",
		"/api/xray/restart",
		"/api/xray/config",
	}

	for _, ep := range obsoleteEndpoints {
		r := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPost, ep, nil)
		mux.ServeHTTP(r, q)
		if r.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for obsolete endpoint %s, got %d", ep, r.Code)
		}
	}
}
