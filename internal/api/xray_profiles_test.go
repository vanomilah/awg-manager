package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/xrayconfig"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

func setupTestXrayServerHandler(t *testing.T) (*XrayServerHandler, string) {
	tempDir, err := os.MkdirTemp("", "api-xray-prof-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	svc := xrayserver.New(tempDir, func() {})
	handler := NewXrayServerHandler(svc, nil)
	return handler, tempDir
}

func TestXrayProfiles_CRUDAndImportExport(t *testing.T) {
	handler, tempDir := setupTestXrayServerHandler(t)
	defer os.RemoveAll(tempDir)

	// 1. Create Profile
	createReq := CreateProfileRequest{
		Name:    "Test Server Profile",
		Role:    xrayconfig.RoleServer,
		Enabled: true,
		Config: &xrayconfig.ManagedConfig{
			LogLevel: "warning",
			Inbounds: []xrayconfig.Inbound{
				{
					Tag:       "vless-in",
					Listen:    "127.0.0.1",
					Port:      9008,
					Protocol:  "vless",
					Transport: "xhttp",
					Security:  "reality",
					Clients: []xrayconfig.Client{
						{UUID: "27c69d32-f887-479b-84aa-cb997437844e", Email: "test@awgm", Enabled: true},
					},
					Reality: &xrayconfig.RealityConfig{
						PrivateKey:  "secret-priv-key-canary",
						PublicKey:   "pub-key-canary",
						Target:      "ya.ru:443",
						ServerNames: []string{"ya.ru"},
					},
				},
			},
			Outbounds: []xrayconfig.Outbound{
				{Tag: "direct", Protocol: "freedom"},
			},
		},
	}

	bodyBytes, _ := json.Marshal(createReq)
	req := httptest.NewRequest(http.MethodPost, "/api/servers/xray/profiles", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.RouteProfiles(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var createdResp struct {
		Success bool             `json:"success"`
		Data    ProfileDetailDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createdResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	profileID := createdResp.Data.ID
	if profileID == "" {
		t.Fatal("expected non-empty profile ID")
	}

	// Verify secret in createdResp is redacted
	if createdResp.Data.Config.Inbounds[0].Reality.PrivateKey != "[REDACTED]" {
		t.Errorf("created response leaked private key: %s", createdResp.Data.Config.Inbounds[0].Reality.PrivateKey)
	}

	// 2. List Profiles
	listReq := httptest.NewRequest(http.MethodGet, "/api/servers/xray/profiles", nil)
	listRec := httptest.NewRecorder()
	handler.RouteProfiles(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", listRec.Code, listRec.Body.String())
	}

	// 3. Get Profile Details
	getReq := httptest.NewRequest(http.MethodGet, "/api/servers/xray/profiles/"+profileID, nil)
	getRec := httptest.NewRecorder()
	handler.RouteProfiles(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// 4. Preview Profile Diff
	prevReq := httptest.NewRequest(http.MethodPost, "/api/servers/xray/profiles/"+profileID+"/preview", nil)
	prevRec := httptest.NewRecorder()
	handler.RouteProfiles(prevRec, prevReq)
	if prevRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for preview, got %d: %s", prevRec.Code, prevRec.Body.String())
	}
	if !strings.Contains(prevRec.Body.String(), "summary") {
		t.Errorf("expected diff summary in preview response: %s", prevRec.Body.String())
	}

	// 5. Redacted Export (GET)
	expRedactedReq := httptest.NewRequest(http.MethodGet, "/api/servers/xray/profiles/"+profileID+"/export", nil)
	expRedactedRec := httptest.NewRecorder()
	handler.RouteProfiles(expRedactedRec, expRedactedReq)
	if expRedactedRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for redacted export, got %d: %s", expRedactedRec.Code, expRedactedRec.Body.String())
	}
	redactedBody := expRedactedRec.Body.String()
	if strings.Contains(redactedBody, "secret-priv-key-canary") {
		t.Errorf("redacted export leaked canary secret key: %s", redactedBody)
	}

	// 6. Private Export (POST /export-private) - Mandatory Constraint 2
	expPrivReq := httptest.NewRequest(http.MethodPost, "/api/servers/xray/profiles/"+profileID+"/export-private", nil)
	expPrivRec := httptest.NewRecorder()
	handler.RouteProfiles(expPrivRec, expPrivReq)
	if expPrivRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for private export, got %d: %s", expPrivRec.Code, expPrivRec.Body.String())
	}

	// Verify required caching and disposition headers
	if cc := expPrivRec.Header().Get("Cache-Control"); cc != "no-store, private" {
		t.Errorf("expected Cache-Control 'no-store, private', got %q", cc)
	}
	if pragma := expPrivRec.Header().Get("Pragma"); pragma != "no-cache" {
		t.Errorf("expected Pragma 'no-cache', got %q", pragma)
	}
	if cd := expPrivRec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("expected Content-Disposition attachment, got %q", cd)
	}

	// Verify unredacted canary secret is present in download payload
	privBody := expPrivRec.Body.String()
	if !strings.Contains(privBody, "secret-priv-key-canary") {
		t.Errorf("expected unredacted private key in private export: %s", privBody)
	}

	// Verify GET to export-private is REJECTED
	getPrivReq := httptest.NewRequest(http.MethodGet, "/api/servers/xray/profiles/"+profileID+"/export-private", nil)
	getPrivRec := httptest.NewRecorder()
	handler.RouteProfiles(getPrivRec, getPrivReq)
	if getPrivRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed for GET /export-private, got %d", getPrivRec.Code)
	}

	// 7. Import Profile
	importPayload := `{
		"content": "{\"log\":{\"loglevel\":\"info\"},\"inbounds\":[{\"tag\":\"imp-in\",\"port\":1080,\"protocol\":\"socks\"}],\"outbounds\":[{\"tag\":\"direct\",\"protocol\":\"freedom\"}]}"
	}`
	impReq := httptest.NewRequest(http.MethodPost, "/api/servers/xray/profiles/import", strings.NewReader(importPayload))
	impRec := httptest.NewRecorder()
	handler.RouteProfiles(impRec, impReq)
	if impRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for import, got %d: %s", impRec.Code, impRec.Body.String())
	}

	// 8. Capabilities Endpoint
	capsReq := httptest.NewRequest(http.MethodGet, "/api/servers/xray/capabilities", nil)
	capsRec := httptest.NewRecorder()
	handler.GetCapabilities(capsRec, capsReq)
	if capsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for capabilities, got %d: %s", capsRec.Code, capsRec.Body.String())
	}
	if !strings.Contains(capsRec.Body.String(), "supports_xhttp") {
		t.Errorf("expected supports_xhttp in capabilities response: %s", capsRec.Body.String())
	}

	// 9. Recovery Resolve Endpoint
	recReq := httptest.NewRequest(http.MethodPost, "/api/servers/xray/recovery/resolve", strings.NewReader(`{"strategy":"clear"}`))
	recRec := httptest.NewRecorder()
	handler.ResolveRecovery(recRec, recReq)
	if recRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for recovery resolve, got %d: %s", recRec.Code, recRec.Body.String())
	}
}
