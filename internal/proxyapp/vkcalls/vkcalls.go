package vkcalls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

const (
	defaultVKCallAppID      = "6287487"
	defaultVKCallAPIVersion = "5.280"
	defaultVKCallWebHost    = "vk.ru"
	defaultVKCallUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"
)

type SettingsStore interface {
	GetVKCallsSettings() storage.VKCallsSettings
	SetVKCallsSettings(storage.VKCallsSettings) error
}

// Service manages VK Calls generation and health checks.
type Service struct {
	settings     SettingsStore
	client       *http.Client
	webHost      string
	appID        string
	apiBaseURL   string
	loginBaseURL string
}

// Config holds optional overrides for Service.
type Config struct {
	Settings     SettingsStore
	Client       *http.Client
	WebHost      string
	AppID        string
	APIBaseURL   string
	LoginBaseURL string
}

// New creates a new VK Calls service.
func New(cfg Config) *Service {
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	host := cfg.WebHost
	if host == "" {
		host = defaultVKCallWebHost
	}
	appID := cfg.AppID
	if appID == "" {
		appID = defaultVKCallAppID
	}
	return &Service{
		settings:     cfg.Settings,
		client:       c,
		webHost:      host,
		appID:        appID,
		apiBaseURL:   cfg.APIBaseURL,
		loginBaseURL: cfg.LoginBaseURL,
	}
}

func (s *Service) apiURL(path string) string {
	if s.apiBaseURL != "" {
		return strings.TrimRight(s.apiBaseURL, "/") + path
	}
	return fmt.Sprintf("https://api.%s%s", s.webHost, path)
}

func (s *Service) loginURL(path string) string {
	if s.loginBaseURL != "" {
		return strings.TrimRight(s.loginBaseURL, "/") + path
	}
	return fmt.Sprintf("https://login.%s%s", s.webHost, path)
}


// GenerateRequest holds parameters for creating new VK calls.
type GenerateRequest struct {
	Token     string `json:"token,omitempty"`
	GroupID   int64  `json:"groupId,omitempty"`
	Count     int    `json:"count,omitempty"`
	SaveToken bool   `json:"saveToken,omitempty"`
}

// GenerateResponse holds the generated join links.
type GenerateResponse struct {
	Success bool     `json:"success"`
	Links   []string `json:"links"`
	Hashes  []string `json:"hashes"`
	CallIDs []string `json:"callIds,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// CheckRequest holds a list of links or hashes to verify.
type CheckRequest struct {
	Links []string `json:"links"`
}

// CheckItemResult represents the status of a single VK call link.
type CheckItemResult struct {
	Link  string `json:"link"`
	Hash  string `json:"hash"`
	Alive bool   `json:"alive"`
	Error string `json:"error,omitempty"`
}

// CheckResponse holds the results of verifying links.
type CheckResponse struct {
	Results []CheckItemResult `json:"results"`
}

// ConfigResponse exposes whether credentials are saved.
type ConfigResponse struct {
	HasToken    bool   `json:"hasToken"`
	MaskedToken string `json:"maskedToken,omitempty"`
	GroupID     int64  `json:"groupId,omitempty"`
}

// GetConfig returns the stored VK credentials status (token is masked).
func (s *Service) GetConfig() ConfigResponse {
	if s.settings == nil {
		return ConfigResponse{}
	}
	st := s.settings.GetVKCallsSettings()
	hasToken := strings.TrimSpace(st.Token) != ""
	masked := ""
	if hasToken {
		tok := strings.TrimSpace(st.Token)
		if len(tok) > 10 {
			masked = tok[:6] + "..." + tok[len(tok)-4:]
		} else {
			masked = "***"
		}
	}
	return ConfigResponse{
		HasToken:    hasToken,
		MaskedToken: masked,
		GroupID:     st.GroupID,
	}
}

// SaveConfig updates the stored token and group ID.
func (s *Service) SaveConfig(token string, groupID int64) error {
	if s.settings == nil {
		return errors.New("настройки недоступны")
	}
	return s.settings.SetVKCallsSettings(storage.VKCallsSettings{
		Token:   strings.TrimSpace(token),
		GroupID: groupID,
	})
}

// Generate creates 1 to 5 new VK Calls rooms and returns their join links.
func (s *Service) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	token := strings.TrimSpace(req.Token)
	groupID := req.GroupID

	if token == "" && s.settings != nil {
		saved := s.settings.GetVKCallsSettings()
		token = strings.TrimSpace(saved.Token)
		if groupID == 0 {
			groupID = saved.GroupID
		}
	}

	if token == "" {
		return nil, errors.New("VK Access Token не указан (укажите токен или сохраните его в настройках)")
	}

	if req.SaveToken && s.settings != nil && req.Token != "" {
		_ = s.settings.SetVKCallsSettings(storage.VKCallsSettings{
			Token:   token,
			GroupID: groupID,
		})
	}

	count := req.Count
	if count < 1 {
		count = 1
	}
	if count > 5 {
		count = 5
	}

	links := make([]string, 0, count)
	hashes := make([]string, 0, count)
	callIDs := make([]string, 0, count)

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if i > 0 {
			time.Sleep(200 * time.Millisecond)
		}

		callID, joinLink, err := s.createOneCall(ctx, token, groupID)
		if err != nil {
			if len(links) > 0 {
				break
			}
			return nil, err
		}

		hash := ExtractVKHash(joinLink)
		links = append(links, joinLink)
		hashes = append(hashes, hash)
		callIDs = append(callIDs, callID)
	}

	return &GenerateResponse{
		Success: true,
		Links:   links,
		Hashes:  hashes,
		CallIDs: callIDs,
	}, nil
}

func (s *Service) createOneCall(ctx context.Context, token string, groupID int64) (string, string, error) {
	endpoint := s.apiURL("/method/calls.start")
	form := url.Values{
		"v": {defaultVKCallAPIVersion},
	}
	if groupID != 0 {
		form.Set("group_id", fmt.Sprintf("%d", groupID))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", defaultVKCallUserAgent)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("запрос calls.start не удался: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("ошибка чтения ответа calls.start: %w", err)
	}

	var parsed struct {
		Response struct {
			CallID   string `json:"call_id"`
			JoinLink string `json:"join_link"`
		} `json:"response"`
		Error struct {
			Code int    `json:"error_code"`
			Msg  string `json:"error_msg"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("ошибка разбора ответа VK: %w", err)
	}

	if parsed.Error.Code != 0 {
		return "", "", fmt.Errorf("VK API ошибка %d: %s", parsed.Error.Code, parsed.Error.Msg)
	}

	if parsed.Response.JoinLink == "" {
		return "", "", errors.New("VK API вернул пустой join_link")
	}

	return parsed.Response.CallID, parsed.Response.JoinLink, nil
}

// Check verifies whether the given links or hashes correspond to active VK calls.
func (s *Service) Check(ctx context.Context, req CheckRequest) (*CheckResponse, error) {
	if len(req.Links) == 0 {
		return &CheckResponse{Results: []CheckItemResult{}}, nil
	}

	anonToken, err := s.getAnonToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("получение анонимного токена VK не удалось: %w", err)
	}

	results := make([]CheckItemResult, 0, len(req.Links))
	for _, raw := range req.Links {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		fullLink := trimmed
		if !strings.HasPrefix(fullLink, "http://") && !strings.HasPrefix(fullLink, "https://") {
			fullLink = "https://vk.ru/call/join/" + trimmed
		}

		hash := ExtractVKHash(fullLink)
		alive, checkErr := s.checkOneCall(ctx, anonToken, fullLink)
		res := CheckItemResult{
			Link:  fullLink,
			Hash:  hash,
			Alive: alive,
		}
		if checkErr != nil {
			res.Error = checkErr.Error()
		}
		results = append(results, res)
	}

	return &CheckResponse{Results: results}, nil
}

func (s *Service) getAnonToken(ctx context.Context) (string, error) {
	endpoint := s.loginURL("/?act=get_anonym_token")
	form := url.Values{
		"client_id": {s.appID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", defaultVKCallUserAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tok struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("разбор anon_token: %w", err)
	}
	if tok.Data.AccessToken == "" {
		return "", errors.New("пустой anon access_token от VK")
	}
	return tok.Data.AccessToken, nil
}

func (s *Service) checkOneCall(ctx context.Context, anonToken, link string) (bool, error) {
	endpoint := s.apiURL("/method/calls.getCallPreview")
	form := url.Values{
		"v":            {defaultVKCallAPIVersion},
		"vk_join_link": {link},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", defaultVKCallUserAgent)
	req.Header.Set("Authorization", "Bearer "+anonToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var res struct {
		Response struct {
			OKJoinLink string `json:"ok_join_link"`
		} `json:"response"`
		Error struct {
			Code int    `json:"error_code"`
			Msg  string `json:"error_msg"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &res); err != nil {
		return false, err
	}

	if res.Error.Code != 0 {
		return false, fmt.Errorf("VK API %d: %s", res.Error.Code, res.Error.Msg)
	}

	return true, nil
}

// ExtractVKHash strips prefix and query parameters to return the bare hash.
func ExtractVKHash(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	if idx := strings.Index(lower, "/call/join/"); idx >= 0 {
		s = s[idx+len("/call/join/"):]
	}
	if idx := strings.Index(s, "?"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "#"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
