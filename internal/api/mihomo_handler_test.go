package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

type fakeMihomoEngine struct {
	running     bool
	pid         int
	reloadCalls int
	reloadErr   error
	lastError   string
	configDir   string
}

func (f *fakeMihomoEngine) Reload() error                           { f.reloadCalls++; return f.reloadErr }
func (f *fakeMihomoEngine) IsRunning() (bool, int)                  { return f.running, f.pid }
func (f *fakeMihomoEngine) Start() error                            { return nil }
func (f *fakeMihomoEngine) Stop() error                             { return nil }
func (f *fakeMihomoEngine) ClearManualStop() error                  { return nil }
func (f *fakeMihomoEngine) ValidateConfigDir(context.Context) error { return nil }
func (f *fakeMihomoEngine) ConfigDir() string {
	if f.configDir != "" {
		return f.configDir
	}
	return "/tmp/mihomo"
}
func (f *fakeMihomoEngine) Binary() string                       { return "/usr/bin/mihomo" }
func (f *fakeMihomoEngine) LastError() string                    { return f.lastError }
func (f *fakeMihomoEngine) CrashStats() (int, string, time.Time) { return 0, "", time.Time{} }

func TestMihomoStatusReportsSelectedAndActive(t *testing.T) {
	store := newMihomoHandlerSettings(t, "mihomo", true)
	op := &fakeMihomoEngine{running: true, pid: 4321}
	h := NewMihomoHandler(op)
	h.SetSettingsStore(store)

	rec := httptest.NewRecorder()
	h.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/mihomo/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	var envelope struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !envelope.Success {
		t.Fatalf("success = false; response=%s", rec.Body.String())
	}
	got := envelope.Data
	for _, key := range []string{"running", "selected", "enabled", "active"} {
		if got[key] != true {
			t.Fatalf("status[%q] = %v, want true; status=%v", key, got[key], got)
		}
	}
	if got["pid"] != float64(4321) {
		t.Fatalf("status pid = %v, want 4321", got["pid"])
	}
}

func TestMihomoReloadUsesPreparedReload(t *testing.T) {
	store := newMihomoHandlerSettings(t, "mihomo", true)
	op := &fakeMihomoEngine{}
	h := NewMihomoHandler(op)
	h.SetSettingsStore(store)
	preparedCalls := 0
	h.SetReloadFunc(func() error {
		preparedCalls++
		return nil
	})

	rec := httptest.NewRecorder()
	h.handleReload(rec, httptest.NewRequest(http.MethodPost, "/api/mihomo/reload", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if preparedCalls != 1 || op.reloadCalls != 0 {
		t.Fatalf("reload calls: prepared=%d direct=%d, want 1/0", preparedCalls, op.reloadCalls)
	}
}

func TestMihomoReloadRejectsInactiveEngine(t *testing.T) {
	store := newMihomoHandlerSettings(t, "sing-box", true)
	op := &fakeMihomoEngine{}
	h := NewMihomoHandler(op)
	h.SetSettingsStore(store)
	called := false
	h.SetReloadFunc(func() error { called = true; return errors.New("must not run") })

	rec := httptest.NewRecorder()
	h.handleReload(rec, httptest.NewRequest(http.MethodPost, "/api/mihomo/reload", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status code = %d, want 409", rec.Code)
	}
	if called {
		t.Fatal("prepared reload called for inactive Mihomo")
	}
}

func TestMihomoRoutesAreGuarded(t *testing.T) {
	h := NewMihomoHandler(&fakeMihomoEngine{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "guarded", http.StatusUnauthorized)
		}
	})

	for _, path := range []string{"/api/mihomo/status", "/api/mihomo/config", "/api/mihomo/reload", "/api/mihomo/clash", "/api/mihomo/clash/proxies"} {
		rec := httptest.NewRecorder()
		method := http.MethodGet
		if path == "/api/mihomo/reload" {
			method = http.MethodPost
		}
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", path, rec.Code)
		}
	}
}

func TestMihomoConfigReturnsGeneratedYAML(t *testing.T) {
	dir := t.TempDir()
	want := []byte("mode: rule\nrules:\n  - MATCH,DIRECT\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), want, 0600); err != nil {
		t.Fatal(err)
	}
	h := NewMihomoHandler(&fakeMihomoEngine{configDir: dir})
	rec := httptest.NewRecorder()
	h.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/mihomo/config", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "MATCH,DIRECT") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func newMihomoHandlerSettings(t *testing.T, engine string, enabled bool) *storage.SettingsStore {
	t.Helper()
	store := storage.NewSettingsStore(t.TempDir())
	err := store.Update(func(s *storage.Settings) error {
		s.SingboxRouter.RoutingEngine = engine
		s.SingboxRouter.Enabled = enabled
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	return store
}
