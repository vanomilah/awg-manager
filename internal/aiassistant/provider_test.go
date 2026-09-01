package aiassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/downloader"
)

func TestResponsesClientUsesStatelessReadOnlyPrompt(t *testing.T) {
	var request map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output_text":"Проверьте default route"}`))
	}))
	defer srv.Close()

	client := NewResponsesClient()
	client.Endpoint = srv.URL + "/responses"
	answer, err := client.Analyze(context.Background(), ModelConfig{
		Enabled: true, Provider: "openai", Model: "test-model", APIKey: "test-secret",
	}, "нет интернета на 192.168.1.25, ключ sk-supersecret", []byte(`{"tests":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Проверьте default route" {
		t.Fatalf("answer = %q", answer)
	}
	if request["store"] != false {
		t.Fatalf("store = %#v, want false", request["store"])
	}
	if request["model"] != "test-model" {
		t.Fatalf("model = %#v", request["model"])
	}
	instructions, _ := request["instructions"].(string)
	if !strings.Contains(instructions, "недоверенные") {
		t.Fatalf("prompt injection guard is missing: %q", instructions)
	}
	input, _ := request["input"].(string)
	if strings.Contains(input, "192.168.1.25") || strings.Contains(input, "sk-supersecret") {
		t.Fatalf("question was not sanitized: %q", input)
	}
}

func TestResponsesClientDoesNotLeakProviderBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`api_key=test-secret raw private response`))
	}))
	defer srv.Close()
	client := NewResponsesClient()
	client.Endpoint = srv.URL
	_, err := client.Analyze(context.Background(), ModelConfig{Enabled: true, Model: "m", APIKey: "test-secret"}, "q", nil)
	if err == nil || err.Error() != "model returned HTTP 401: request failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResponsesClientChatCompletionsFormat(t *testing.T) {
	var request map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer deepseek-key" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Все в порядке"}}]}`))
	}))
	defer srv.Close()

	client := NewResponsesClient()
	answer, err := client.Analyze(context.Background(), ModelConfig{
		Enabled:  true,
		Provider: "deepseek",
		BaseURL:  srv.URL + "/v1",
		Model:    "deepseek-chat",
		APIKey:   "deepseek-key",
	}, "вопрос", []byte(`{"tests":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Все в порядке" {
		t.Fatalf("answer = %q, want 'Все в порядке'", answer)
	}
}

func TestResponsesClientGoogleGeminiFormat(t *testing.T) {
	var request struct {
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"system_instruction"`
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
			ThinkingConfig struct {
				ThinkingLevel string `json:"thinkingLevel"`
			} `json:"thinkingConfig"`
		} `json:"generationConfig"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-goog-api-key"); got != "google-secret" {
			t.Errorf("x-goog-api-key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("unexpected authorization header = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Проверьте DNS"},{"text":"и маршрут."}]}}]}`))
	}))
	defer srv.Close()

	client := NewResponsesClient()
	client.Endpoint = srv.URL + "/v1beta/models/gemini-test:generateContent"
	answer, err := client.Analyze(context.Background(), ModelConfig{
		Enabled: true, Provider: "google", Model: "gemini-test", APIKey: "google-secret",
	}, "почему нет интернета", []byte(`{"tests":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Проверьте DNS\nи маршрут." {
		t.Fatalf("answer = %q", answer)
	}
	if len(request.SystemInstruction.Parts) != 1 || !strings.Contains(request.SystemInstruction.Parts[0].Text, "недоверенные") {
		t.Fatalf("system instruction missing: %+v", request.SystemInstruction)
	}
	if len(request.Contents) != 1 || request.Contents[0].Role != "user" {
		t.Fatalf("contents = %+v", request.Contents)
	}
	if request.GenerationConfig.MaxOutputTokens != 1000 {
		t.Fatalf("maxOutputTokens = %d", request.GenerationConfig.MaxOutputTokens)
	}
	if request.GenerationConfig.ThinkingConfig.ThinkingLevel != "minimal" {
		t.Fatalf("thinkingLevel = %q", request.GenerationConfig.ThinkingConfig.ThinkingLevel)
	}
}

func TestResolveGoogleEndpointEscapesModel(t *testing.T) {
	got := resolveEndpoint(ModelConfig{Provider: "google", Model: "gemini test"})
	want := DefaultGoogleBaseURL + "/models/gemini%20test:generateContent"
	if got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

func TestResponsesClientRetriesTransientGoogleFailure(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"high demand"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Готово"}]}}]}`))
	}))
	defer srv.Close()

	client := NewResponsesClient()
	client.Endpoint = srv.URL
	client.RetryBaseDelay = time.Millisecond
	answer, err := client.Analyze(context.Background(), ModelConfig{
		Enabled: true, Provider: "google", Model: "gemini-test", APIKey: "secret",
	}, "вопрос", nil)
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Готово" || attempts != 3 {
		t.Fatalf("answer = %q, attempts = %d", answer, attempts)
	}
}

func TestResponsesClientRejectsUnavailableSelectedRoute(t *testing.T) {
	client := NewRoutedResponsesClient(downloader.NewService(downloader.Deps{}))
	client.Endpoint = "http://127.0.0.1:1"
	_, err := client.Analyze(context.Background(), ModelConfig{
		Enabled: true, Provider: "google", Model: "gemini-test", APIKey: "secret",
		RouteTag: "missing-tunnel", RouteKind: "singbox",
	}, "question", nil)
	if err == nil || !strings.Contains(err.Error(), "model route") || !strings.Contains(err.Error(), "missing-tunnel") {
		t.Fatalf("error = %v", err)
	}
}
