package xrayserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestService_CleanInitialization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := New(tmpDir, nil)
	cfg := svc.GetConfig()

	if cfg.Enabled {
		t.Errorf("expected Enabled=false on clean start, got true")
	}
	if cfg.PublicDomain != "" {
		t.Errorf("expected empty PublicDomain on clean start, got %q", cfg.PublicDomain)
	}
	if len(cfg.Clients) != 0 {
		t.Errorf("expected empty Clients on clean start, got %d clients", len(cfg.Clients))
	}
	if cfg.ListenPort != 9008 {
		t.Errorf("expected default ListenPort=9008, got %d", cfg.ListenPort)
	}

	st := svc.GetStatus()
	if st.Configured {
		t.Errorf("expected Configured=false on clean start, got true")
	}
	if st.ClientsCount != 0 {
		t.Errorf("expected ClientsCount=0, got %d", st.ClientsCount)
	}
}

func TestService_LoadExistingConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	xrayDir := filepath.Join(tmpDir, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatalf("mkdir xray: %v", err)
	}

	existing := Config{
		Enabled:            true,
		ListenPort:         9010,
		DispatcherPort:     9011,
		PublicDomain:       "existing.myrouter.org",
		PublicPort:         8443,
		Path:               "/custom-path/",
		Mode:               "packet-up",
		UplinkMethod:       "GET",
		XmuxMaxConnections: 4,
		OutboundSocksPort:  1099,
		Clients: []Client{
			{
				ID:        "11111111-2222-3333-4444-555555555555",
				Remark:    "Телефон",
				Enabled:   true,
				CreatedAt: "2026-09-01T10:00:00Z",
			},
			{
				ID:        "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				Remark:    "Дача",
				Enabled:   false,
				CreatedAt: "2026-09-02T10:00:00Z",
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(filepath.Join(xrayDir, "xray-server-settings.json"), data, 0600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	svc := New(tmpDir, nil)
	cfg := svc.GetConfig()

	if !cfg.Enabled {
		t.Errorf("expected Enabled=true from existing config")
	}
	if cfg.PublicDomain != "existing.myrouter.org" {
		t.Errorf("expected domain 'existing.myrouter.org', got %q", cfg.PublicDomain)
	}
	if cfg.ListenPort != 9010 {
		t.Errorf("expected port 9010, got %d", cfg.ListenPort)
	}
	if len(cfg.Clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(cfg.Clients))
	}
	if cfg.Clients[0].Remark != "Телефон" || cfg.Clients[1].Remark != "Дача" {
		t.Errorf("clients remarks mismatch: %+v", cfg.Clients)
	}

	st := svc.GetStatus()
	if !st.Configured {
		t.Errorf("expected Configured=true for existing valid config")
	}
}

func TestService_SaveConfigPermissions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := New(tmpDir, nil)
	client, err := svc.AddClient("Новый клиент")
	if err != nil {
		t.Fatalf("AddClient failed: %v", err)
	}
	if client.ID == "" || client.Remark != "Новый клиент" {
		t.Errorf("invalid added client: %+v", client)
	}

	cfgPath := filepath.Join(tmpDir, "xray", "xray-server-settings.json")
	fi, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config file failed: %v", err)
	}

	if runtime.GOOS != "windows" {
		perm := fi.Mode().Perm()
		if perm != 0600 {
			t.Errorf("expected file mode 0600, got %04o", perm)
		}
	}
}

func TestService_RenderRuntimeConfig(t *testing.T) {
	cfg := Config{
		Enabled:            true,
		ListenPort:         9008,
		PublicDomain:       "test.domain.com",
		Path:               "/ws-bridge/",
		Mode:               "packet-up",
		UplinkMethod:       "GET",
		XmuxMaxConnections: 2,
		OutboundSocksPort:  1099,
		Clients: []Client{
			{
				ID:      "test-uuid-1",
				Remark:  "Active",
				Enabled: true,
			},
			{
				ID:      "test-uuid-2",
				Remark:  "Disabled",
				Enabled: false,
			},
		},
	}

	data, err := RenderRuntimeConfig(cfg)
	if err != nil {
		t.Fatalf("RenderRuntimeConfig failed: %v", err)
	}

	var parsed struct {
		Inbounds []struct {
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					ID string `json:"id"`
				} `json:"clients"`
			} `json:"settings"`
			StreamSettings struct {
				Network       string `json:"network"`
				XhttpSettings struct {
					Path string `json:"path"`
					Mode string `json:"mode"`
				} `json:"xhttpSettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
		Outbounds []struct {
			Protocol string `json:"protocol"`
			Tag      string `json:"tag"`
		} `json:"outbounds"`
	}

	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse rendered config: %v", err)
	}

	if len(parsed.Inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(parsed.Inbounds))
	}
	in := parsed.Inbounds[0]
	if in.Port != 9008 || in.Protocol != "vless" {
		t.Errorf("unexpected inbound: port=%d, protocol=%s", in.Port, in.Protocol)
	}
	if len(in.Settings.Clients) != 1 || in.Settings.Clients[0].ID != "test-uuid-1" {
		t.Errorf("expected only active client in rendered config, got %+v", in.Settings.Clients)
	}
	if in.StreamSettings.XhttpSettings.Path != "/ws-bridge/" {
		t.Errorf("path mismatch: got %q", in.StreamSettings.XhttpSettings.Path)
	}

	if len(parsed.Outbounds) < 2 || parsed.Outbounds[0].Tag != "proxy-awgm" {
		t.Errorf("expected proxy-awgm outbound first, got %+v", parsed.Outbounds)
	}
}

func TestService_ClientCRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := New(tmpDir, nil)

	c1, err := svc.AddClient("Client 1")
	if err != nil {
		t.Fatalf("AddClient 1 failed: %v", err)
	}
	c2, err := svc.AddClient("Client 2")
	if err != nil {
		t.Fatalf("AddClient 2 failed: %v", err)
	}

	if len(svc.GetConfig().Clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(svc.GetConfig().Clients))
	}

	// Toggle client 1
	if err := svc.ToggleClient(c1.ID, false); err != nil {
		t.Fatalf("ToggleClient failed: %v", err)
	}
	if svc.GetConfig().Clients[0].Enabled {
		t.Errorf("expected client 1 to be disabled")
	}

	// Delete client 2
	if err := svc.DeleteClient(c2.ID); err != nil {
		t.Fatalf("DeleteClient failed: %v", err)
	}
	if len(svc.GetConfig().Clients) != 1 {
		t.Fatalf("expected 1 client after delete, got %d", len(svc.GetConfig().Clients))
	}

	// Toggle non-existent client
	if err := svc.ToggleClient("fake-id", true); err == nil {
		t.Errorf("expected error toggling non-existent client")
	}
}

func TestService_GenerateLinks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := New(tmpDir, nil)
	_ = svc.UpdateConfig(Config{
		PublicDomain: "vpn.example.com",
		PublicPort:   443,
		Path:         "/cdn-test/",
	})

	c, _ := svc.AddClient("Рабочий телефон")
	links, err := svc.GenerateLinks(c.ID)
	if err != nil {
		t.Fatalf("GenerateLinks failed: %v", err)
	}

	if links.UUID != c.ID {
		t.Errorf("UUID mismatch: got %q, want %q", links.UUID, c.ID)
	}
	if links.Remark != "Рабочий телефон" {
		t.Errorf("Remark mismatch: got %q", links.Remark)
	}
	if len(links.VlessURL) == 0 || len(links.HappJSON) == 0 || len(links.SingboxJSON) == 0 || len(links.MihomoYAML) == 0 {
		t.Errorf("one or more share links are empty: %+v", links)
	}

	var dummy map[string]interface{}
	if err := json.Unmarshal([]byte(links.HappJSON), &dummy); err != nil {
		t.Errorf("invalid HappJSON: %v", err)
	}
	if err := json.Unmarshal([]byte(links.SingboxJSON), &dummy); err != nil {
		t.Errorf("invalid SingboxJSON: %v", err)
	}
}

func TestService_MalformedConfigRecoveryRequired(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-corrupt-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	xrayDir := filepath.Join(tmpDir, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	corruptBytes := []byte("{\n  \"enabled\": true,\n  \"listen_port\": 9008,\n  INVALID_JSON_CORRUPT\n")
	cfgPath := filepath.Join(xrayDir, "xray-server-settings.json")
	if err := os.WriteFile(cfgPath, corruptBytes, 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	svc := New(tmpDir, nil)

	// 1. Must be in recovery required state
	if !svc.IsRecoveryRequired() {
		t.Fatalf("expected IsRecoveryRequired to be true")
	}

	req, reason, fingerprint := svc.RecoveryInfo()
	if !req || reason == "" || fingerprint == "" {
		t.Errorf("expected full recovery info, got req=%v, reason=%q, fp=%q", req, reason, fingerprint)
	}

	// 2. Disk file MUST NOT be modified or deleted
	diskBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read disk config: %v", err)
	}
	if string(diskBytes) != string(corruptBytes) {
		t.Errorf("corrupt file on disk was modified: got %q, want %q", string(diskBytes), string(corruptBytes))
	}

	// 3. Status must report recovery required
	st := svc.GetStatus()
	if !st.RecoveryRequired || st.RecoveryReason == "" || st.RecoveryFingerprint == "" {
		t.Errorf("status does not report recovery required: %+v", st)
	}

	// 4. Mutation operations must fail with ErrRecoveryRequired
	if err := svc.UpdateConfig(Config{Enabled: true}); !errors.Is(err, ErrRecoveryRequired) {
		t.Errorf("expected ErrRecoveryRequired on UpdateConfig, got: %v", err)
	}
	if _, err := svc.AddClient("test"); !errors.Is(err, ErrRecoveryRequired) {
		t.Errorf("expected ErrRecoveryRequired on AddClient, got: %v", err)
	}
	if err := svc.DeleteClient("any"); !errors.Is(err, ErrRecoveryRequired) {
		t.Errorf("expected ErrRecoveryRequired on DeleteClient, got: %v", err)
	}
	if err := svc.ToggleClient("any", true); !errors.Is(err, ErrRecoveryRequired) {
		t.Errorf("expected ErrRecoveryRequired on ToggleClient, got: %v", err)
	}
}

func TestService_DeepCopyImmutability(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-deepcopy-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := New(tmpDir, nil)
	c, err := svc.AddClient("Original Client")
	if err != nil {
		t.Fatalf("AddClient failed: %v", err)
	}

	cfg := svc.GetConfig()
	if len(cfg.Clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(cfg.Clients))
	}

	// Mutate the returned config's slice
	cfg.Clients[0].Remark = "Hacked Client"
	cfg.Clients = append(cfg.Clients, Client{ID: "fake", Remark: "Injected"})

	// Check that internal service config was not mutated
	freshCfg := svc.GetConfig()
	if freshCfg.Clients[0].Remark != "Original Client" {
		t.Errorf("internal client was mutated through GetConfig slice: %q", freshCfg.Clients[0].Remark)
	}
	if len(freshCfg.Clients) != 1 {
		t.Errorf("internal client slice length was modified through GetConfig: got %d, want 1", len(freshCfg.Clients))
	}

	// Test GetStatus deep copy as well
	st := svc.GetStatus()
	st.Clients[0].Remark = "Hacked in status"
	freshSt := svc.GetStatus()
	if freshSt.Clients[0].Remark != "Original Client" {
		t.Errorf("internal client was mutated through GetStatus slice: %q", freshSt.Clients[0].Remark)
	}
	_ = c
}

func TestService_RenderRuntimeConfig_ListenAddress(t *testing.T) {
	// 1. Default fallback to 127.0.0.1
	cfgDefault := Config{
		ListenPort: 9008,
	}
	data, err := RenderRuntimeConfig(cfgDefault)
	if err != nil {
		t.Fatalf("RenderRuntimeConfig failed: %v", err)
	}
	var parsedDefault struct {
		Inbounds []struct {
			Listen string `json:"listen"`
			Port   int    `json:"port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &parsedDefault); err != nil {
		t.Fatalf("unmarshal rendered config: %v", err)
	}
	if len(parsedDefault.Inbounds) == 0 || parsedDefault.Inbounds[0].Listen != "127.0.0.1" {
		t.Errorf("expected default listen address '127.0.0.1', got %q", parsedDefault.Inbounds[0].Listen)
	}

	// 2. Explicit custom ListenAddress
	cfgCustom := Config{
		ListenAddress: "192.168.1.1",
		ListenPort:    9008,
	}
	dataCustom, err := RenderRuntimeConfig(cfgCustom)
	if err != nil {
		t.Fatalf("RenderRuntimeConfig custom failed: %v", err)
	}
	var parsedCustom struct {
		Inbounds []struct {
			Listen string `json:"listen"`
			Port   int    `json:"port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(dataCustom, &parsedCustom); err != nil {
		t.Fatalf("unmarshal rendered config: %v", err)
	}
	if parsedCustom.Inbounds[0].Listen != "192.168.1.1" {
		t.Errorf("expected custom listen address '192.168.1.1', got %q", parsedCustom.Inbounds[0].Listen)
	}
}

func TestService_ShutdownVsStop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xray-shutdown-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := New(tmpDir, nil)
	// Manually enable in memory and save
	svc.config.Enabled = true
	if err := svc.saveConfig(); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	// 1. Shutdown: must NOT change config.Enabled
	if err := svc.Shutdown(nil); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
	if !svc.GetConfig().Enabled {
		t.Errorf("Shutdown modified config.Enabled in memory: got false, want true")
	}

	// Check on disk
	diskSvc := New(tmpDir, nil)
	if !diskSvc.GetConfig().Enabled {
		t.Errorf("Shutdown modified config.Enabled on disk: got false, want true")
	}

	// 2. Stop: MUST set config.Enabled = false
	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if svc.GetConfig().Enabled {
		t.Errorf("Stop did not set config.Enabled to false in memory")
	}

	diskSvcAfterStop := New(tmpDir, nil)
	if diskSvcAfterStop.GetConfig().Enabled {
		t.Errorf("Stop did not set config.Enabled to false on disk")
	}
}

func TestService_ProcessIdentityFailClosed(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xray")
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755)

	svc := New(tmpDir, nil)
	svc.SetBinaryPath(binPath)
	binFP := computeFileSHA256(binPath)

	// 1. Nil record
	if svc.verifyProcessIdentityLocked(nil) {
		t.Errorf("expected verifyProcessIdentityLocked(nil) to return false")
	}

	// 2. PID <= 0
	if svc.verifyProcessIdentityLocked(&PIDRecord{PID: 0, BinaryFingerprint: binFP}) {
		t.Errorf("expected verifyProcessIdentityLocked(PID:0) to return false")
	}
	if svc.verifyProcessIdentityLocked(&PIDRecord{PID: -5, BinaryFingerprint: binFP}) {
		t.Errorf("expected verifyProcessIdentityLocked(PID:-5) to return false")
	}

	// 3. Non-existent PID
	if svc.verifyProcessIdentityLocked(&PIDRecord{PID: 9999999, BinaryFingerprint: binFP}) {
		t.Errorf("expected verifyProcessIdentityLocked(dead PID) to return false")
	}

	// 4. Missing binary fingerprint
	myPID := os.Getpid()
	if svc.verifyProcessIdentityLocked(&PIDRecord{PID: myPID, BinaryFingerprint: ""}) {
		t.Errorf("expected verifyProcessIdentityLocked to reject empty BinaryFingerprint")
	}

	// 5. Changed binary fingerprint
	if svc.verifyProcessIdentityLocked(&PIDRecord{PID: myPID, BinaryFingerprint: "mismatched_sha256_hash_123456"}) {
		t.Errorf("expected verifyProcessIdentityLocked to reject mismatched BinaryFingerprint")
	}

	// 6. On Linux: missing StartTime (StartTime == 0) must be rejected
	if runtime.GOOS == "linux" {
		zeroTimeRec := &PIDRecord{
			PID:               myPID,
			StartTime:         0,
			BinaryFingerprint: binFP,
		}
		if svc.verifyProcessIdentityLocked(zeroTimeRec) {
			t.Errorf("expected fail-closed on missing StartTime (0) on Linux")
		}

		// 7. On Linux: mismatched start time on active process
		mismatchedRec := &PIDRecord{
			PID:               myPID,
			StartTime:         123456789, // arbitrary timestamp that won't match our actual boot time ticks
			BinaryFingerprint: binFP,
		}
		if svc.verifyProcessIdentityLocked(mismatchedRec) {
			t.Errorf("expected fail-closed on mismatched StartTime on Linux")
		}
	}

	// 8. writePIDRecord error handling
	if err := svc.writePIDRecord(0, "/tmp/cfg"); err == nil {
		t.Errorf("expected writePIDRecord to fail on PID=0")
	}
	svc.SetBinaryPath("/non/existent/path/xray")
	if err := svc.writePIDRecord(myPID, "/tmp/cfg"); err == nil {
		t.Errorf("expected writePIDRecord to fail on missing binary path")
	}
}

func TestCommitPrepared_RollbackErrorsPropagate(t *testing.T) {
	dataDir := t.TempDir()
	svc := New(dataDir, nil)
	initialCfg := Config{
		Enabled:    false,
		ListenPort: 9008,
	}
	_ = svc.UpdateConfig(initialCfg)

	cand := initialCfg
	cand.ListenPort = 9009
	txID, err := svc.PrepareCandidate("tx-err-prop", cand)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt the settings backup in snapshot to make rollback fail
	bakPath := filepath.Join(dataDir, "xray", "transactions", txID, "xray-server-settings.json.bak")
	_ = os.WriteFile(bakPath, []byte("corrupted_content_that_fails_checksum"), 0600)

	// Invalidate candidate runtime config to cause commit error
	candRtPath := filepath.Join(dataDir, "xray", "transactions", txID, "config.json.candidate")
	_ = os.Remove(candRtPath)

	commitErr := svc.CommitPrepared(txID)
	if commitErr == nil {
		t.Fatalf("expected CommitPrepared to fail")
	}

	// Verify the error contains both candidate runtime error AND rollback failure
	errMsg := commitErr.Error()
	if !strings.Contains(errMsg, "read candidate runtime") {
		t.Errorf("expected primary error to be present in: %s", errMsg)
	}
	if !strings.Contains(errMsg, "rollback failed") {
		t.Errorf("expected rollback error to be joined in: %s", errMsg)
	}
}

func TestRollbackLocked_FileRestoreFailurePreventsRestart(t *testing.T) {
	dataDir := t.TempDir()
	svc := New(dataDir, nil)
	initialCfg := Config{
		Enabled:    true,
		ListenPort: 9008,
	}
	_ = svc.UpdateConfig(initialCfg)

	cand := initialCfg
	cand.ListenPort = 9009
	txID, err := svc.PrepareCandidate("tx-restore-fail", cand)
	if err != nil {
		t.Fatal(err)
	}

	// Read manifest
	txPath := filepath.Join(dataDir, "xray", "transactions", txID)
	manifestData, _ := os.ReadFile(filepath.Join(txPath, "snapshot-manifest.json"))
	var manifest SnapshotManifest
	_ = json.Unmarshal(manifestData, &manifest)

	// Make settings backup file corrupt so checksum fails or make target dir read-only
	_ = os.WriteFile(filepath.Join(txPath, "xray-server-settings.json.bak"), []byte("bad"), 0600)

	svc.mu.Lock()
	rbErr := svc.rollbackLocked(txID, &manifest)
	svc.mu.Unlock()

	if rbErr == nil {
		t.Fatalf("expected rollbackLocked to return error on corrupt backup")
	}

	// Verify process is NOT running
	if svc.IsRunning() {
		t.Errorf("process must not be restarted when rollback fails")
	}
}

func TestBuildShareLinks_Pure(t *testing.T) {
	client := Client{
		ID:     "11111111-2222-3333-4444-555555555555",
		Remark: "Phone Client",
	}

	// 1. XHTTP test
	cfgXHTTP := Config{
		PublicDomain: "vpn.example.com",
		PublicPort:   443,
		Path:         "/cdn-bridge",
		Transport:    "xhttp",
		Mode:         "packet-up",
		UplinkMethod: "GET",
	}

	links, err := BuildShareLinks(cfgXHTTP, client)
	if err != nil {
		t.Fatalf("unexpected error for xhttp: %v", err)
	}
	if !strings.Contains(links.VlessURL, "11111111-2222-3333-4444-555555555555@vpn.example.com:443") {
		t.Errorf("unexpected vless url: %s", links.VlessURL)
	}
	if !strings.Contains(links.VlessURL, "type=xhttp") {
		t.Errorf("expected type=xhttp in url: %s", links.VlessURL)
	}
	if links.UUID != client.ID || links.Remark != client.Remark {
		t.Errorf("mismatched id/remark: %+v", links)
	}
	if !strings.Contains(links.MihomoYAML, "network: xhttp") {
		t.Errorf("expected mihomo yaml to contain network: xhttp, got: %s", links.MihomoYAML)
	}
	if !strings.Contains(links.SingboxJSON, "proxy-cdn") {
		t.Errorf("expected singbox json to contain tag proxy-cdn, got: %s", links.SingboxJSON)
	}
	if !strings.Contains(links.HappJSON, "protocol") {
		t.Errorf("expected happ json to contain protocol, got: %s", links.HappJSON)
	}

	// 2. WS test
	cfgWS := Config{
		PublicDomain: "vpn.example.com",
		PublicPort:   8443,
		Path:         "/ws-path",
		Transport:    "ws",
	}
	linksWS, err := BuildShareLinks(cfgWS, client)
	if err != nil {
		t.Fatalf("unexpected error for ws: %v", err)
	}
	if !strings.Contains(linksWS.VlessURL, "type=ws") {
		t.Errorf("expected type=ws in url: %s", linksWS.VlessURL)
	}
	if !strings.Contains(linksWS.MihomoYAML, "network: ws") {
		t.Errorf("expected network: ws in mihomo yaml")
	}

	// 3. Validation errors
	_, err = BuildShareLinks(cfgXHTTP, Client{ID: ""})
	if err == nil {
		t.Errorf("expected error on empty client ID")
	}

	_, err = BuildShareLinks(Config{PublicDomain: ""}, client)
	if err == nil {
		t.Errorf("expected error on empty domain")
	}
}

