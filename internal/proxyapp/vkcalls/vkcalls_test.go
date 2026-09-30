package vkcalls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

type memorySettings struct {
	cfg storage.VKCallsSettings
}

func (m *memorySettings) GetVKCallsSettings() storage.VKCallsSettings {
	return m.cfg
}

func (m *memorySettings) SetVKCallsSettings(s storage.VKCallsSettings) error {
	m.cfg = s
	return nil
}

func TestVKCallsGenerateSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/method/calls.start", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			http.Error(w, `{"error":{"error_code":5,"error_msg":"auth failed"}}`, http.StatusOK)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"call_id":   "c-12345",
				"join_link": "https://vk.ru/call/join/test_hash_abc123",
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	svc := New(Config{
		Settings:   &memorySettings{},
		Client:     server.Client(),
		APIBaseURL: server.URL,
	})

	// Override endpoint by mocking host
	resp, err := svc.Generate(context.Background(), GenerateRequest{
		Token: "test-token",
		Count: 2,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(resp.Links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(resp.Links))
	}
	if resp.Hashes[0] != "test_hash_abc123" {
		t.Fatalf("unexpected hash: %s", resp.Hashes[0])
	}
}

func TestVKCallsCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("act") == "get_anonym_token" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"access_token": "anon-123",
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/method/calls.getCallPreview", func(w http.ResponseWriter, r *http.Request) {
		link := r.FormValue("vk_join_link")
		if link == "https://vk.ru/call/join/expired" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"error_code": 100,
					"error_msg":  "call not found",
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"ok_join_link": "https://ok.ru/live/123",
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	svc := New(Config{
		Client:       server.Client(),
		APIBaseURL:   server.URL,
		LoginBaseURL: server.URL,
	})

	res, err := svc.Check(context.Background(), CheckRequest{
		Links: []string{"https://vk.ru/call/join/active", "https://vk.ru/call/join/expired"},
	})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.Results))
	}
	if !res.Results[0].Alive {
		t.Errorf("expected link 0 to be alive")
	}
	if res.Results[1].Alive {
		t.Errorf("expected link 1 to be dead")
	}
}

func TestExtractVKHash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://vk.ru/call/join/Abc123_xyz", "Abc123_xyz"},
		{"https://vk.com/call/join/Abc123_xyz?extra=1", "Abc123_xyz"},
		{"Abc123_xyz", "Abc123_xyz"},
		{"vk.ru/call/join/Abc123_xyz#anchor", "Abc123_xyz"},
	}

	for _, tc := range tests {
		got := ExtractVKHash(tc.input)
		if got != tc.want {
			t.Errorf("ExtractVKHash(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
