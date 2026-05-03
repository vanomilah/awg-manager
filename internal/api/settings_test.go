package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

// newSettingsHandlerForTest returns a SettingsHandler backed by an
// isolated SettingsStore in a temp directory. The store is preloaded so
// defaults (including the v16 UsageLevel) are populated.
//
// nil AppLogger is intentionally passed — see internal/logging/applogger.go:
// "Safe to use with nil appLogger — all methods become no-ops."
func newSettingsHandlerForTest(t *testing.T) (*SettingsHandler, *storage.SettingsStore) {
	t.Helper()
	tmp := t.TempDir()
	store := storage.NewSettingsStore(tmp)
	if _, err := store.Load(); err != nil {
		t.Fatalf("seed Load: %v", err)
	}
	h := NewSettingsHandler(store, nil)
	return h, store
}

// TestUpdateUsageLevelAccepted verifies that a valid usageLevel value
// (here: "expert") is accepted and persisted by the Update handler.
func TestUpdateUsageLevelAccepted(t *testing.T) {
	h, store := newSettingsHandlerForTest(t)
	current, _ := store.Get()
	// store.Get() returns a pointer into the cache; copy by value so
	// payload mutations do not retroactively alias the cached state read
	// back as oldSettings inside the handler.
	payload := *current
	payload.UsageLevel = storage.UsageLevelExpert
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/settings/update", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.Get()
	if got.UsageLevel != storage.UsageLevelExpert {
		t.Errorf("UsageLevel after update = %q, want expert", got.UsageLevel)
	}
}

// TestUpdateUsageLevelInvalidRejected verifies that an unknown
// usageLevel value is rejected with 400 and the INVALID_USAGE_LEVEL
// error code, instead of being silently coerced to a default.
func TestUpdateUsageLevelInvalidRejected(t *testing.T) {
	h, _ := newSettingsHandlerForTest(t)
	body := []byte(`{
		"schemaVersion": 16,
		"authEnabled": false,
		"server": {"port": 2222, "interface": "br0"},
		"pingCheck": {"enabled": false, "defaults": {"method":"http","target":"8.8.8.8","interval":45,"deadInterval":120,"failThreshold":3}},
		"logging": {"enabled": true, "maxAge": 2},
		"disableMemorySaving": false,
		"updates": {"checkEnabled": true},
		"dnsRoute": {"autoRefreshEnabled": false},
		"usageLevel": "garbage",
		"singboxRouter": {"enabled": false, "policyName": "", "refreshMode": "interval", "refreshIntervalHours": 24}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/settings/update", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "INVALID_USAGE_LEVEL") {
		t.Errorf("body missing error code:\n%s", rec.Body.String())
	}
}

// TestUpdateUsageLevelEmptyPreserves verifies that a payload that
// OMITS usageLevel preserves the previously stored value. This is
// the partial-update defense after the SettingsPatch refactor:
// nil pointer in the patch DTO means "field absent in payload, keep
// existing value." An EXPLICIT empty string is now correctly rejected
// as INVALID_USAGE_LEVEL (separate test).
func TestUpdateUsageLevelEmptyPreserves(t *testing.T) {
	h, store := newSettingsHandlerForTest(t)

	// Pre-seed with expert.
	current, _ := store.Get()
	seed := *current
	seed.UsageLevel = storage.UsageLevelExpert
	if err := store.Save(&seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Build a payload that OMITS usageLevel entirely. Marshalling the
	// struct with UsageLevel="" would still serialize the field (no
	// omitempty), so we hand-craft the raw JSON instead.
	body := []byte(`{
		"schemaVersion": 16,
		"authEnabled": false,
		"server": {"port": 2222, "interface": "br0"},
		"pingCheck": {"enabled": false, "defaults": {"method":"http","target":"8.8.8.8","interval":45,"deadInterval":120,"failThreshold":3}},
		"logging": {"enabled": true, "maxAge": 2},
		"disableMemorySaving": false,
		"updates": {"checkEnabled": true},
		"dnsRoute": {"autoRefreshEnabled": false},
		"singboxRouter": {"enabled": false, "policyName": "", "refreshMode": "interval", "refreshIntervalHours": 24}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/settings/update", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.Get()
	if got.UsageLevel != storage.UsageLevelExpert {
		t.Errorf("UsageLevel = %q after omitted update, want expert (preserved)", got.UsageLevel)
	}
}
