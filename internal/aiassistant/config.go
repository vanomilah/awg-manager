package aiassistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

const (
	OpenAIResponsesEndpoint  = "https://api.openai.com/v1/responses"
	DefaultOpenAIBaseURL     = "https://api.openai.com/v1"
	DefaultDeepSeekBaseURL   = "https://api.deepseek.com/v1"
	DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	DefaultGoogleBaseURL     = "https://generativelanguage.googleapis.com/v1beta"
	DefaultOllamaBaseURL     = "http://127.0.0.1:11434/v1"
	DefaultEmbeddedBaseURL   = "http://127.0.0.1:11435/v1"
)

type LocalEngineConfig struct {
	Enabled         bool   `json:"enabled"`
	BinaryPath      string `json:"binaryPath,omitempty"`
	ModelPath       string `json:"modelPath,omitempty"`
	ContextSize     int    `json:"contextSize,omitempty"`
	Threads         int    `json:"threads,omitempty"`
	Port            int    `json:"port,omitempty"`
	AutoStopMinutes int    `json:"autoStopMinutes,omitempty"`
}

type ModelConfig struct {
	Enabled     bool              `json:"enabled"`
	AutoFix     bool              `json:"autoFix"`
	Provider    string            `json:"provider"`
	BaseURL     string            `json:"baseUrl,omitempty"`
	Model       string            `json:"model"`
	APIKey      string            `json:"apiKey,omitempty"`
	RouteTag    string            `json:"routeTag,omitempty"`
	RouteKind   string            `json:"routeKind,omitempty"`
	LocalEngine LocalEngineConfig `json:"localEngine,omitempty"`
	UpdatedAt   time.Time         `json:"updatedAt,omitempty"`
}

type PublicModelConfig struct {
	Enabled     bool              `json:"enabled"`
	AutoFix     bool              `json:"autoFix"`
	Provider    string            `json:"provider"`
	BaseURL     string            `json:"baseUrl,omitempty"`
	Model       string            `json:"model"`
	APIKeySet   bool              `json:"apiKeySet"`
	RouteTag    string            `json:"routeTag,omitempty"`
	RouteKind   string            `json:"routeKind,omitempty"`
	LocalEngine LocalEngineConfig `json:"localEngine,omitempty"`
	UpdatedAt   time.Time         `json:"updatedAt,omitempty"`
}

type ConfigUpdate struct {
	Enabled     bool               `json:"enabled"`
	AutoFix     bool               `json:"autoFix"`
	Provider    string             `json:"provider"`
	BaseURL     string             `json:"baseUrl,omitempty"`
	Model       string             `json:"model"`
	APIKey      string             `json:"apiKey,omitempty"`
	ClearAPIKey bool               `json:"clearApiKey,omitempty"`
	RouteTag    string             `json:"routeTag,omitempty"`
	RouteKind   string             `json:"routeKind,omitempty"`
	LocalEngine *LocalEngineConfig `json:"localEngine,omitempty"`
}

type ConfigStore struct {
	path string
	mu   sync.RWMutex
	cfg  ModelConfig
}

func NewConfigStore(path string) (*ConfigStore, error) {
	s := &ConfigStore{path: path, cfg: ModelConfig{
		Provider: "openai",
		LocalEngine: LocalEngineConfig{
			Port:            11435,
			ContextSize:     1536,
			Threads:         2,
			AutoStopMinutes: 10,
		},
	}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("AI config: read: %w", err)
	}
	if err := json.Unmarshal(b, &s.cfg); err != nil {
		return nil, fmt.Errorf("AI config: decode: %w", err)
	}
	if err := validateModelConfig(s.cfg); err != nil {
		return nil, fmt.Errorf("AI config: %w", err)
	}
	return s, nil
}

func (s *ConfigStore) Get() ModelConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *ConfigStore) Public() PublicModelConfig {
	cfg := s.Get()
	return PublicModelConfig{
		Enabled:     cfg.Enabled,
		AutoFix:     cfg.AutoFix,
		Provider:    cfg.Provider,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		APIKeySet:   cfg.APIKey != "",
		RouteTag:    cfg.RouteTag,
		RouteKind:   cfg.RouteKind,
		LocalEngine: cfg.LocalEngine,
		UpdatedAt:   cfg.UpdatedAt,
	}
}

func (s *ConfigStore) Save(update ConfigUpdate) (PublicModelConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.cfg
	next.Enabled = update.Enabled
	next.AutoFix = update.AutoFix
	provider := strings.TrimSpace(update.Provider)
	if provider == "" {
		provider = "openai"
	}
	providerChanged := next.Provider != "" && next.Provider != provider
	next.Provider = provider
	next.BaseURL = strings.TrimSpace(update.BaseURL)
	next.Model = strings.TrimSpace(update.Model)
	next.RouteTag = strings.TrimSpace(update.RouteTag)
	next.RouteKind = strings.TrimSpace(update.RouteKind)
	if next.RouteTag == "" {
		next.RouteTag = "direct"
		next.RouteKind = "direct"
	}
	// A credential belongs to one provider. Never silently reuse (and send)
	// the previous provider's secret after switching endpoints.
	if providerChanged && strings.TrimSpace(update.APIKey) == "" {
		next.APIKey = ""
	}
	if key := strings.TrimSpace(update.APIKey); key != "" {
		next.APIKey = key
	}
	if update.ClearAPIKey {
		next.APIKey = ""
	}
	if update.LocalEngine != nil {
		next.LocalEngine = *update.LocalEngine
	}
	next.UpdatedAt = time.Now()
	if err := validateModelConfig(next); err != nil {
		return PublicModelConfig{}, err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return PublicModelConfig{}, err
	}
	if err := storage.AtomicWritePerm(s.path, append(b, '\n'), 0o600); err != nil {
		return PublicModelConfig{}, fmt.Errorf("AI config: save: %w", err)
	}
	s.cfg = next
	return PublicModelConfig{
		Enabled:     next.Enabled,
		AutoFix:     next.AutoFix,
		Provider:    next.Provider,
		BaseURL:     next.BaseURL,
		Model:       next.Model,
		APIKeySet:   next.APIKey != "",
		RouteTag:    next.RouteTag,
		RouteKind:   next.RouteKind,
		LocalEngine: next.LocalEngine,
		UpdatedAt:   next.UpdatedAt,
	}, nil
}

func validateModelConfig(cfg ModelConfig) error {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	allowedProviders := map[string]bool{
		"openai":         true,
		"deepseek":       true,
		"openrouter":     true,
		"google":         true,
		"ollama":         true,
		"local_embedded": true,
		"custom":         true,
	}
	if !allowedProviders[cfg.Provider] {
		return fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
	if len(cfg.Model) > 120 || strings.ContainsAny(cfg.Model, "\r\n") {
		return errors.New("invalid model name")
	}
	if len(cfg.BaseURL) > 512 || strings.ContainsAny(cfg.BaseURL, "\r\n") {
		return errors.New("invalid base URL")
	}
	if err := validateProviderURL(cfg.Provider, cfg.BaseURL); err != nil {
		return err
	}
	if len(cfg.APIKey) > 512 || strings.ContainsAny(cfg.APIKey, "\r\n") {
		return errors.New("invalid API key")
	}
	if len(cfg.RouteTag) > 160 || len(cfg.RouteKind) > 40 || strings.ContainsAny(cfg.RouteTag+cfg.RouteKind, "\r\n") {
		return errors.New("invalid model route")
	}
	if err := validateLocalEngine(cfg.LocalEngine); err != nil {
		return err
	}
	if cfg.Enabled {
		if cfg.Model == "" && cfg.Provider != "local_embedded" {
			return errors.New("model name is required when model analysis is enabled")
		}
		// Cloud providers require API key; local/ollama do not
		if (cfg.Provider == "openai" || cfg.Provider == "deepseek" || cfg.Provider == "openrouter" || cfg.Provider == "google") && cfg.APIKey == "" {
			return errors.New("API key is required for cloud model provider")
		}
	}
	return nil
}

func validateProviderURL(provider, raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return errors.New("invalid base URL")
	}
	host := strings.ToLower(u.Hostname())
	switch provider {
	case "openai":
		if u.Scheme != "https" || host != "api.openai.com" {
			return errors.New("OpenAI base URL must use https://api.openai.com; use custom provider for another endpoint")
		}
	case "deepseek":
		if u.Scheme != "https" || host != "api.deepseek.com" {
			return errors.New("DeepSeek base URL must use https://api.deepseek.com; use custom provider for another endpoint")
		}
	case "openrouter":
		if u.Scheme != "https" || host != "openrouter.ai" {
			return errors.New("OpenRouter base URL must use https://openrouter.ai; use custom provider for another endpoint")
		}
	case "google":
		if u.Scheme != "https" || host != "generativelanguage.googleapis.com" {
			return errors.New("Google base URL must use https://generativelanguage.googleapis.com")
		}
	case "local_embedded":
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("embedded model base URL must use a loopback address")
		}
	}
	return nil
}

func validateLocalEngine(cfg LocalEngineConfig) error {
	if cfg.Port != 0 && (cfg.Port < 1024 || cfg.Port > 65535) {
		return errors.New("local model port must be between 1024 and 65535")
	}
	if cfg.ContextSize != 0 && (cfg.ContextSize < 256 || cfg.ContextSize > 4096) {
		return errors.New("local model context size must be between 256 and 4096")
	}
	if cfg.Threads != 0 && (cfg.Threads < 1 || cfg.Threads > 3) {
		return errors.New("local model threads must be between 1 and 3")
	}
	if cfg.AutoStopMinutes != 0 && (cfg.AutoStopMinutes < 1 || cfg.AutoStopMinutes > 60) {
		return errors.New("local model auto-stop must be between 1 and 60 minutes")
	}
	for _, configuredPath := range []string{cfg.BinaryPath, cfg.ModelPath} {
		if configuredPath == "" {
			continue
		}
		clean := path.Clean(configuredPath)
		if !strings.HasPrefix(clean, "/") || (clean != "/opt" && !strings.HasPrefix(clean, "/opt/")) {
			return errors.New("local model paths must be absolute paths under /opt")
		}
	}
	return nil
}
