package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/aiassistant"
)

type fakeAIService struct {
	question string
	state    aiassistant.State
	err      error
}

func TestAIAssistantConfigNeverReturnsAPIKey(t *testing.T) {
	store, err := aiassistant.NewConfigStore(filepath.Join(t.TempDir(), "ai.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(aiassistant.ConfigUpdate{Enabled: true, Provider: "openai", Model: "test", APIKey: "top-secret"}); err != nil {
		t.Fatal(err)
	}
	h := NewAIAssistantHandler(&fakeAIService{})
	h.SetConfigStore(store)
	rec := httptest.NewRecorder()
	h.Config(rec, httptest.NewRequest(http.MethodGet, "/api/system/ai/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "top-secret") || !strings.Contains(rec.Body.String(), `"apiKeySet":true`) {
		t.Fatalf("unsafe config response: %s", rec.Body.String())
	}
}

func (f *fakeAIService) Start(question string) error { f.question = question; return f.err }
func (f *fakeAIService) Status() aiassistant.State   { return f.state }

func TestAIAssistantDiagnose(t *testing.T) {
	svc := &fakeAIService{state: aiassistant.State{Status: "running", ReadOnly: true}}
	h := NewAIAssistantHandler(svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/system/ai/diagnose", strings.NewReader(`{"question":"нет интернета"}`))
	h.Diagnose(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if svc.question != "нет интернета" {
		t.Fatalf("question = %q", svc.question)
	}
}

func TestAIAssistantDiagnoseRejectsUnknownFields(t *testing.T) {
	h := NewAIAssistantHandler(&fakeAIService{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/system/ai/diagnose", strings.NewReader(`{"command":"rm"}`))
	h.Diagnose(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAIAssistantEmbeddedStatus(t *testing.T) {
	store, err := aiassistant.NewConfigStore(filepath.Join(t.TempDir(), "ai.json"))
	if err != nil {
		t.Fatal(err)
	}
	embedded := aiassistant.NewEmbeddedManager(store)
	h := NewAIAssistantHandler(&fakeAIService{})
	h.SetEmbedded(embedded)

	rec := httptest.NewRecorder()
	h.Embedded(rec, httptest.NewRequest(http.MethodGet, "/api/system/ai/embedded", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"port":11435`) {
		t.Fatalf("expected port in response: %s", rec.Body.String())
	}
}
