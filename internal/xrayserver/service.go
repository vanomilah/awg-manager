package xrayserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/childproc"
	"github.com/hoaxisr/awg-manager/internal/xrayconfig"
)

var (
	ErrRecoveryRequired = errors.New("xray server configuration is corrupt: manual recovery required")
)

// PIDRecord stores structured process identity information to prevent stale/reused PID attacks.
type PIDRecord struct {
	PID               int    `json:"pid"`
	StartTime         uint64 `json:"start_time"`
	ConfigPath        string `json:"config_path"`
	BinaryFingerprint string `json:"binary_fingerprint"`
}

type Client struct {
	ID        string `json:"id"`
	Remark    string `json:"remark"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

type Config struct {
	Enabled            bool     `json:"enabled"`
	ListenAddress      string   `json:"listen_address"`       // Default: "127.0.0.1"
	ListenPort         int      `json:"listen_port"`          // Default: 9008
	DispatcherPort     int      `json:"dispatcher_port"`      // Default: 9009
	PublicDomain       string   `json:"public_domain"`        // Domain configured for public ingress
	PublicPort         int      `json:"public_port"`          // Ingress public port (usually 443)
	Path               string   `json:"path"`                 // Path (e.g. /cdn-bridge/)
	Transport          string   `json:"transport,omitempty"`  // "xhttp" (default) or "ws"
	Mode               string   `json:"mode"`                 // Transport mode (packet-up for xhttp)
	UplinkMethod       string   `json:"uplink_method"`        // HTTP uplink method (GET for xhttp)
	XmuxMaxConnections int      `json:"xmux_max_connections"` // Default: 2
	OutboundMode       string   `json:"outbound_mode,omitempty"`      // "direct", "socks", "interface"
	OutboundInterface  string   `json:"outbound_interface,omitempty"` // network device name (e.g. "nwg1")
	OutboundSocksPort  int      `json:"outbound_socks_port"`          // Forwarding port (e.g., 1099 for mihomo/singbox policy routing, 0 for direct)
	Clients            []Client `json:"clients"`
}

type Status struct {
	Installed           bool     `json:"installed"`
	Running             bool     `json:"running"`
	PID                 int      `json:"pid"`
	ListenAddress       string   `json:"listen_address,omitempty"`
	Port                int      `json:"port"`
	Version             string   `json:"version"`
	Domain              string   `json:"domain"`
	Configured          bool     `json:"configured"`
	ClientsCount        int      `json:"clients_count"`
	Clients             []Client `json:"clients"`
	RecoveryRequired    bool     `json:"recovery_required,omitempty"`
	RecoveryReason      string   `json:"recovery_reason,omitempty"`
	RecoveryFingerprint string   `json:"recovery_fingerprint,omitempty"`
}

type ShareLinks struct {
	VlessURL    string `json:"vless_url"`
	HappJSON    string `json:"happ_json"`
	SingboxJSON string `json:"singbox_json"`
	MihomoYAML  string `json:"mihomo_yaml"`
	Remark      string `json:"remark"`
	UUID        string `json:"uuid"`
}

type Service struct {
	mu                  sync.Mutex
	dataDir             string
	binPath             string
	config              Config
	proc                *managedProc
	onReload            func()
	recoveryRequired    bool
	recoveryReason      string
	recoveryFingerprint string
	probe               ListenerOwnershipProbe
	secrets             SecretStore
	profiles            ProfileStore
	compiler            *xrayconfig.Compiler
	validator           *xrayconfig.Validator
	redactor            *xrayconfig.Redactor
	parser              *xrayconfig.Parser
}

func New(dataDir string, onReload func()) *Service {
	s := &Service{
		dataDir:   dataDir,
		binPath:   "/opt/sbin/xray",
		onReload:  onReload,
		probe:     NewDefaultListenerProbe(),
		compiler:  xrayconfig.NewCompiler(),
		validator: xrayconfig.NewValidator(),
		redactor:  xrayconfig.NewRedactor(),
		parser:    xrayconfig.NewParser(),
	}

	secStore, err := NewDiskSecretStore(filepath.Join(dataDir, "xray", "secrets"))
	if err == nil {
		s.secrets = secStore
	}

	profStore, err := NewDiskProfileStore(filepath.Join(dataDir, "xray", "profiles"))
	if err == nil {
		s.profiles = profStore
	}

	s.loadConfig()
	_ = s.RecoverPendingTransactions()
	return s
}

func (s *Service) Secrets() SecretStore {
	return s.secrets
}

func (s *Service) Profiles() ProfileStore {
	return s.profiles
}

func (s *Service) Compiler() *xrayconfig.Compiler {
	return s.compiler
}

func (s *Service) Validator() *xrayconfig.Validator {
	return s.validator
}

func (s *Service) Redactor() *xrayconfig.Redactor {
	return s.redactor
}

func (s *Service) Parser() *xrayconfig.Parser {
	return s.parser
}

func (s *Service) DataDir() string {
	return s.dataDir
}

func (s *Service) SetListenerProbe(probe ListenerOwnershipProbe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe = probe
}

func (s *Service) SetBinaryPath(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binPath = p
}

func (s *Service) BinPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.binPath
}

func (s *Service) ClearRecoveryRequired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryRequired = false
	s.recoveryReason = ""
	s.recoveryFingerprint = ""
}

func (s *Service) IsRecoveryRequired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoveryRequired
}

func (s *Service) RecoveryInfo() (required bool, reason string, fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoveryRequired, s.recoveryReason, s.recoveryFingerprint
}

func (s *Service) cfgPath() string {
	return filepath.Join(s.dataDir, "xray", "xray-server-settings.json")
}

func (s *Service) xrayDir() string {
	return filepath.Join(s.dataDir, "xray")
}

func (s *Service) xrayRuntimeConfigPath() string {
	return filepath.Join(s.xrayDir(), "config.json")
}

func (s *Service) pidPath() string {
	return filepath.Join(s.xrayDir(), "xray-server.pid")
}

func (s *Service) loadConfig() {
	// Safe blank defaults without hardcoded personal domains, IPs, or pre-baked clients
	s.config = Config{
		Enabled:            false,
		ListenAddress:      "127.0.0.1",
		ListenPort:         9008,
		DispatcherPort:     9009,
		PublicDomain:       "",
		PublicPort:         443,
		Path:               "",
		Mode:               "packet-up",
		UplinkMethod:       "GET",
		XmuxMaxConnections: 2,
		OutboundSocksPort:  1099,
		Clients:            []Client{},
	}

	data, err := os.ReadFile(s.cfgPath())
	if err != nil {
		if os.IsNotExist(err) {
			s.recoveryRequired = false
			return
		}
		s.recoveryRequired = true
		s.recoveryReason = fmt.Sprintf("read config: %v", err)
		return
	}

	if len(data) == 0 {
		return
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		// Side-effect-free: DO NOT rename file, DO NOT overwrite with defaults
		sum := sha256.Sum256(data)
		s.recoveryRequired = true
		s.recoveryReason = fmt.Sprintf("malformed JSON: %v", err)
		s.recoveryFingerprint = hex.EncodeToString(sum[:])
		return
	}

	s.config = loaded
	s.recoveryRequired = false
	s.recoveryReason = ""
	s.recoveryFingerprint = ""

	if s.config.ListenAddress == "" {
		s.config.ListenAddress = "127.0.0.1"
	}
	if s.config.ListenPort == 0 {
		s.config.ListenPort = 9008
	}
	if s.config.DispatcherPort == 0 {
		s.config.DispatcherPort = 9009
	}
	if s.config.PublicPort == 0 {
		s.config.PublicPort = 443
	}
	if s.config.Transport == "" {
		s.config.Transport = "xhttp"
	}
	if s.config.Mode == "" {
		s.config.Mode = "packet-up"
	}
	if s.config.UplinkMethod == "" {
		s.config.UplinkMethod = "GET"
	}
	if s.config.XmuxMaxConnections <= 0 {
		s.config.XmuxMaxConnections = 2
	}
	if s.config.OutboundMode == "" {
		if s.config.OutboundSocksPort > 0 {
			s.config.OutboundMode = "socks"
		} else if s.config.OutboundInterface != "" {
			s.config.OutboundMode = "interface"
		} else {
			s.config.OutboundMode = "direct"
		}
	}
	if s.config.Clients == nil {
		s.config.Clients = []Client{}
	}
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmpFile.Chmod(perm); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	tmpFile = nil

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename to %s: %w", path, err)
	}

	if dirF, err := os.Open(dir); err == nil {
		_ = dirF.Sync()
		_ = dirF.Close()
	}

	return nil
}

func (s *Service) saveConfig() error {
	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.cfgPath(), data, 0600)
}

func (s *Service) GetConfig() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.config
	if s.config.Clients != nil {
		cfg.Clients = make([]Client, len(s.config.Clients))
		copy(cfg.Clients, s.config.Clients)
	}
	return cfg
}

func (s *Service) UpdateConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recoveryRequired {
		return ErrRecoveryRequired
	}

	s.config.Enabled = cfg.Enabled
	if cfg.ListenAddress != "" {
		s.config.ListenAddress = strings.TrimSpace(cfg.ListenAddress)
	}
	if cfg.ListenPort > 0 {
		s.config.ListenPort = cfg.ListenPort
	}
	if cfg.DispatcherPort > 0 {
		s.config.DispatcherPort = cfg.DispatcherPort
	}
	s.config.PublicDomain = strings.TrimSpace(cfg.PublicDomain)
	if cfg.PublicPort > 0 {
		s.config.PublicPort = cfg.PublicPort
	}
	s.config.Path = strings.TrimSpace(cfg.Path)
	if cfg.Transport != "" {
		s.config.Transport = cfg.Transport
	}
	if cfg.Mode != "" {
		s.config.Mode = cfg.Mode
	}
	if cfg.UplinkMethod != "" {
		s.config.UplinkMethod = cfg.UplinkMethod
	}
	if cfg.XmuxMaxConnections > 0 {
		s.config.XmuxMaxConnections = cfg.XmuxMaxConnections
	}
	if cfg.OutboundMode != "" {
		s.config.OutboundMode = cfg.OutboundMode
	}
	if cfg.OutboundInterface != "" {
		s.config.OutboundInterface = cfg.OutboundInterface
	}
	s.config.OutboundSocksPort = cfg.OutboundSocksPort
	if cfg.Clients != nil {
		s.config.Clients = make([]Client, len(cfg.Clients))
		copy(s.config.Clients, cfg.Clients)
	}

	if err := s.saveConfig(); err != nil {
		return err
	}

	var err error
	if s.config.Enabled {
		err = s.restartLocked()
	} else {
		err = s.stopLocked()
	}
	s.notifyReload()
	return err
}

func (s *Service) GetStatus() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	installed := fileExists(s.binPath)
	running, pid := s.checkRunningLocked()
	version := ""
	if installed {
		out, err := exec.Command(s.binPath, "version").Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 0 {
				version = strings.TrimSpace(lines[0])
			}
		}
	}

	configured := s.config.PublicDomain != "" && len(s.config.Clients) > 0

	clients := make([]Client, len(s.config.Clients))
	copy(clients, s.config.Clients)

	return Status{
		Installed:           installed,
		Running:             running,
		PID:                 pid,
		ListenAddress:       s.config.ListenAddress,
		Port:                s.config.ListenPort,
		Version:             version,
		Domain:              s.config.PublicDomain,
		Configured:          configured,
		ClientsCount:        len(clients),
		Clients:             clients,
		RecoveryRequired:    s.recoveryRequired,
		RecoveryReason:      s.recoveryReason,
		RecoveryFingerprint: s.recoveryFingerprint,
	}
}

func computeFileSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func readPIDRecord(path string) (*PIDRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec PIDRecord
	if err := json.Unmarshal(data, &rec); err == nil && rec.PID > 0 {
		return &rec, nil
	}
	// Fallback to legacy plain integer pid format
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
		return &PIDRecord{PID: pid}, nil
	}
	return nil, errors.New("invalid pid file content")
}

func (s *Service) verifyProcessIdentityLocked(rec *PIDRecord) bool {
	if rec == nil || rec.PID <= 0 {
		return false
	}
	// 1. Process must be alive
	if !childproc.IsAlive(rec.PID) {
		return false
	}
	// 2. Binary fingerprint must match current binary on disk
	if rec.BinaryFingerprint == "" {
		return false
	}
	currentFP := computeFileSHA256(s.binPath)
	if currentFP == "" || currentFP != rec.BinaryFingerprint {
		return false // Binary changed or unreadable
	}
	// 3. On Linux, verify StartTime and cmdline
	if runtime.GOOS == "linux" {
		if rec.StartTime == 0 {
			return false // Fail-closed: missing start time on Linux
		}
		curStartTime, ok := childproc.StartTime(rec.PID)
		if !ok || curStartTime != rec.StartTime {
			return false // Fail-closed: unreadable stat or reused PID
		}
		cmdlineBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", rec.PID))
		if err != nil {
			return false // Fail-closed: unreadable cmdline
		}
		cmdline := string(cmdlineBytes)
		if !strings.Contains(cmdline, "xray") {
			return false
		}
		if rec.ConfigPath != "" && !strings.Contains(cmdline, rec.ConfigPath) {
			return false
		}
	}
	return true
}

func (s *Service) writePIDRecord(pid int, configPath string) error {
	if pid <= 0 {
		return errors.New("invalid pid")
	}
	var st uint64
	if startTime, ok := childproc.StartTime(pid); ok && startTime > 0 {
		st = startTime
	} else if runtime.GOOS == "linux" {
		return fmt.Errorf("read process start time for pid %d failed", pid)
	}
	fp := computeFileSHA256(s.binPath)
	if fp == "" {
		return fmt.Errorf("compute binary fingerprint for %s failed", s.binPath)
	}
	rec := PIDRecord{
		PID:               pid,
		StartTime:         st,
		ConfigPath:        configPath,
		BinaryFingerprint: fp,
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pid record: %w", err)
	}
	if err := atomicWriteFile(s.pidPath(), data, 0600); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

func (s *Service) checkRunningLocked() (bool, int) {
	// 1. Direct managed process handle
	if s.proc != nil && s.proc.IsRunning() {
		return true, s.proc.PID()
	}

	// 2. PID file tracking
	rec, err := readPIDRecord(s.pidPath())
	if err != nil {
		return false, 0
	}

	// 3. Fail-closed process identity check
	if !s.verifyProcessIdentityLocked(rec) {
		_ = os.Remove(s.pidPath())
		return false, 0
	}

	return true, rec.PID
}

func (s *Service) AddClient(remark string) (*Client, error) {
	s.mu.Lock()
	if s.recoveryRequired {
		s.mu.Unlock()
		return nil, ErrRecoveryRequired
	}

	id, err := generateUUID()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	if strings.TrimSpace(remark) == "" {
		remark = fmt.Sprintf("Клиент %d", len(s.config.Clients)+1)
	}

	c := Client{
		ID:        id,
		Remark:    strings.TrimSpace(remark),
		Enabled:   true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	candidate := s.cloneConfigLocked()
	candidate.Clients = append(candidate.Clients, c)
	s.mu.Unlock()

	txID, err := s.PrepareCandidate("", candidate)
	if err != nil {
		return nil, fmt.Errorf("prepare client candidate: %w", err)
	}

	if err := s.CommitPrepared(txID); err != nil {
		return nil, fmt.Errorf("commit client candidate: %w", err)
	}
	_ = s.FinalizePrepared(txID)

	return &c, nil
}

func (s *Service) DeleteClient(id string) error {
	s.mu.Lock()
	if s.recoveryRequired {
		s.mu.Unlock()
		return ErrRecoveryRequired
	}

	idx := -1
	for i, c := range s.config.Clients {
		if c.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return errors.New("client not found")
	}

	candidate := s.cloneConfigLocked()
	candidate.Clients = append(candidate.Clients[:idx], candidate.Clients[idx+1:]...)
	s.mu.Unlock()

	txID, err := s.PrepareCandidate("", candidate)
	if err != nil {
		return fmt.Errorf("prepare delete candidate: %w", err)
	}

	if err := s.CommitPrepared(txID); err != nil {
		return err
	}
	return s.FinalizePrepared(txID)
}

func (s *Service) ToggleClient(id string, enabled bool) error {
	s.mu.Lock()
	if s.recoveryRequired {
		s.mu.Unlock()
		return ErrRecoveryRequired
	}

	found := false
	candidate := s.cloneConfigLocked()
	for i := range candidate.Clients {
		if candidate.Clients[i].ID == id {
			candidate.Clients[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		s.mu.Unlock()
		return errors.New("client not found")
	}
	s.mu.Unlock()

	txID, err := s.PrepareCandidate("", candidate)
	if err != nil {
		return fmt.Errorf("prepare toggle candidate: %w", err)
	}

	if err := s.CommitPrepared(txID); err != nil {
		return err
	}
	return s.FinalizePrepared(txID)
}

func (s *Service) GenerateLinks(clientID string) (*ShareLinks, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var client *Client
	for _, c := range s.config.Clients {
		if c.ID == clientID {
			client = &c
			break
		}
	}
	if client == nil {
		return nil, errors.New("client not found")
	}

	return BuildShareLinks(s.config, *client)
}

// BuildShareLinks generates client share links from configuration and client metadata.
// It is a pure function that does not depend on service runtime state.
func BuildShareLinks(cfg Config, client Client) (*ShareLinks, error) {
	if client.ID == "" {
		return nil, errors.New("client UUID is required")
	}
	domain := strings.TrimSpace(cfg.PublicDomain)
	if domain == "" {
		return nil, errors.New("public domain is required")
	}
	port := cfg.PublicPort
	if port <= 0 {
		port = 443
	}
	path := cfg.Path
	if path == "" {
		path = "/cdn-bridge"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "packet-up"
	}
	method := cfg.UplinkMethod
	if method == "" {
		method = "GET"
	}
	isWS := cfg.Transport == "ws"

	var vlessURL, mihomoYAML string
	var happJSON, singboxJSON []byte

	if isWS {
		// WebSocket transport format
		vlessURL = fmt.Sprintf(
			"vless://%s@%s:%d?encryption=none&security=tls&sni=%s&type=ws&host=%s&path=%s#%s",
			client.ID,
			domain,
			port,
			domain,
			domain,
			url.QueryEscape(path),
			url.QueryEscape(client.Remark),
		)

		happObj := map[string]interface{}{
			"protocol": "vless",
			"address":  domain,
			"port":     port,
			"uuid":     client.ID,
			"security": "tls",
			"sni":      domain,
			"network":  "ws",
			"ws": map[string]interface{}{
				"path": path,
				"host": domain,
			},
			"remark": client.Remark,
		}
		happJSON, _ = json.MarshalIndent(happObj, "", "  ")

		singboxObj := map[string]interface{}{
			"type":        "vless",
			"tag":         "proxy-cdn",
			"server":      domain,
			"server_port": port,
			"uuid":        client.ID,
			"tls": map[string]interface{}{
				"enabled":     true,
				"server_name": domain,
			},
			"transport": map[string]interface{}{
				"type": "ws",
				"path": path,
				"headers": map[string]interface{}{
					"Host": domain,
				},
			},
		}
		singboxJSON, _ = json.MarshalIndent(singboxObj, "", "  ")

		mihomoYAML = fmt.Sprintf(`- name: "%s"
  type: vless
  server: %s
  port: %d
  uuid: %s
  cipher: auto
  tls: true
  servername: %s
  network: ws
  ws-opts:
    path: "%s"
    headers:
      Host: "%s"`, client.Remark, domain, port, client.ID, domain, path, domain)
	} else {
		// Standard VLESS URL format for XHTTP
		vlessURL = fmt.Sprintf(
			"vless://%s@%s:%d?encryption=none&security=tls&sni=%s&type=xhttp&host=%s&path=%s&mode=%s#%s",
			client.ID,
			domain,
			port,
			domain,
			domain,
			url.QueryEscape(path),
			mode,
			url.QueryEscape(client.Remark),
		)

		happObj := map[string]interface{}{
			"protocol": "vless",
			"address":  domain,
			"port":     port,
			"uuid":     client.ID,
			"security": "tls",
			"sni":      domain,
			"network":  "xhttp",
			"xhttp": map[string]interface{}{
				"path":             path,
				"host":             domain,
				"mode":             mode,
				"uplinkHTTPMethod": method,
				"enableXmux":       true,
			},
			"remark": client.Remark,
		}
		happJSON, _ = json.MarshalIndent(happObj, "", "  ")

		singboxObj := map[string]interface{}{
			"type":        "vless",
			"tag":         "proxy-cdn",
			"server":      domain,
			"server_port": port,
			"uuid":        client.ID,
			"tls": map[string]interface{}{
				"enabled":     true,
				"server_name": domain,
			},
			"transport": map[string]interface{}{
				"type": "xhttp",
				"path": path,
				"host": domain,
				"mode": mode,
			},
		}
		singboxJSON, _ = json.MarshalIndent(singboxObj, "", "  ")

		mihomoYAML = fmt.Sprintf(`- name: "%s"
  type: vless
  server: %s
  port: %d
  uuid: %s
  cipher: auto
  tls: true
  servername: %s
  network: xhttp
  xhttp-opts:
    mode: %s
    path: "%s"
    headers:
      Host: "%s"`, client.Remark, domain, port, client.ID, domain, mode, path, domain)
	}

	return &ShareLinks{
		VlessURL:    vlessURL,
		HappJSON:    string(happJSON),
		SingboxJSON: string(singboxJSON),
		MihomoYAML:  mihomoYAML,
		Remark:      client.Remark,
		UUID:        client.ID,
	}, nil
}

func (s *Service) notifyReload() {
	if s.onReload != nil {
		go s.onReload()
	}
}

func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recoveryRequired {
		return ErrRecoveryRequired
	}

	s.config.Enabled = true
	_ = s.saveConfig()
	s.notifyReload()
	return s.restartLocked()
}

// StartConfigured starts the xray process if configured Enabled, without modifying config or saving to disk.
func (s *Service) StartConfigured() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recoveryRequired {
		return ErrRecoveryRequired
	}

	if !s.config.Enabled {
		return nil
	}

	return s.restartLocked()
}

func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Enabled = false
	_ = s.saveConfig()
	s.notifyReload()
	return s.stopLocked()
}

// Shutdown stops the running process gracefully without changing Config.Enabled on disk
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

// ShutdownRuntime stops the running process gracefully without modifying s.config.Enabled or saving.
func (s *Service) ShutdownRuntime(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

// IsRunning reports whether the xray process is actively running.
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	running, _ := s.checkRunningLocked()
	return running
}

func (s *Service) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recoveryRequired {
		return ErrRecoveryRequired
	}

	s.notifyReload()
	return s.restartLocked()
}

func (s *Service) stopLocked() error {
	if s.proc != nil {
		_ = s.proc.Stop(3 * time.Second)
		s.proc = nil
	}

	rec, _ := readPIDRecord(s.pidPath())
	if rec != nil && rec.PID > 0 {
		// Strict repeated identity check before SIGTERM
		if s.verifyProcessIdentityLocked(rec) {
			_ = childproc.Terminate(rec.PID)
			for i := 0; i < 30; i++ {
				time.Sleep(100 * time.Millisecond)
				if !childproc.IsAlive(rec.PID) {
					break
				}
				// Strict repeated identity check before SIGKILL
				if i == 29 && s.verifyProcessIdentityLocked(rec) {
					_ = childproc.Kill(rec.PID)
				}
			}
		}
	}

	_ = os.Remove(s.pidPath())
	return nil
}

// RenderRuntimeConfig transforms Config into the standard Xray vless inbound JSON
func RenderRuntimeConfig(cfg Config) ([]byte, error) {
	clientsList := []map[string]interface{}{}
	for _, c := range cfg.Clients {
		if c.Enabled {
			clientsList = append(clientsList, map[string]interface{}{
				"id": c.ID,
			})
		}
	}

	path := cfg.Path
	if path == "" {
		path = "/cdn-bridge/"
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "packet-up"
	}
	method := cfg.UplinkMethod
	if method == "" {
		method = "GET"
	}
	conns := cfg.XmuxMaxConnections
	if conns <= 0 {
		conns = 2
	}
	port := cfg.ListenPort
	if port <= 0 {
		port = 9008
	}
	listenAddr := cfg.ListenAddress
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}

	var streamSettings map[string]interface{}
	if cfg.Transport == "ws" {
		wsSettings := map[string]interface{}{
			"path": path,
		}
		if cfg.PublicDomain != "" {
			wsSettings["headers"] = map[string]interface{}{
				"Host": cfg.PublicDomain,
			}
		}
		streamSettings = map[string]interface{}{
			"network":    "ws",
			"wsSettings": wsSettings,
		}
	} else {
		streamSettings = map[string]interface{}{
			"network": "xhttp",
			"xhttpSettings": map[string]interface{}{
				"path":             path,
				"mode":             mode,
				"uplinkHTTPMethod": method,
				"xmux": map[string]interface{}{
					"maxConcurrency":   0,
					"maxConnections":   conns,
					"cMaxReuseTimes":   0,
					"hMaxRequestTimes": "100-200",
					"hMaxReusableSecs": "300-600",
					"hKeepAlivePeriod": 0,
				},
				"enableXmux": true,
			},
		}
	}

	xrayConfig := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
			"access":   "/opt/var/log/xray-access.log",
			"error":    "/opt/var/log/xray-error.log",
		},
		"inbounds": []map[string]interface{}{
			{
				"listen":   listenAddr,
				"port":     port,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients":    clientsList,
					"decryption": "none",
				},
				"streamSettings": streamSettings,
			},
		},
		"outbounds": []map[string]interface{}{},
	}

	if cfg.OutboundMode == "interface" && cfg.OutboundInterface != "" {
		xrayConfig["outbounds"] = []map[string]interface{}{
			{
				"protocol": "freedom",
				"tag":      "direct",
				"settings": map[string]interface{}{},
				"streamSettings": map[string]interface{}{
					"sockopt": map[string]interface{}{
						"interface": cfg.OutboundInterface,
					},
				},
			},
		}
	} else if cfg.OutboundMode == "socks" || (cfg.OutboundMode == "" && cfg.OutboundSocksPort > 0) {
		socksPort := cfg.OutboundSocksPort
		if socksPort <= 0 {
			socksPort = 1099
		}
		xrayConfig["outbounds"] = []map[string]interface{}{
			{
				"protocol": "socks",
				"tag":      "proxy-awgm",
				"settings": map[string]interface{}{
					"servers": []map[string]interface{}{
						{
							"address": "127.0.0.1",
							"port":    socksPort,
						},
					},
				},
			},
			{
				"protocol": "freedom",
				"tag":      "direct",
			},
		}
	} else {
		xrayConfig["outbounds"] = []map[string]interface{}{
			{
				"protocol": "freedom",
				"tag":      "direct",
			},
		}
	}

	return json.MarshalIndent(xrayConfig, "", "  ")
}

// TestConfig writes a temporary configuration and validates it with xray test options
func (s *Service) TestConfig(cfg Config) error {
	if !fileExists(s.binPath) {
		return errors.New("xray binary not found at " + s.binPath)
	}

	rendered, err := RenderRuntimeConfig(cfg)
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}

	tmpFile := filepath.Join(s.xrayDir(), fmt.Sprintf("test-config-%d.json", time.Now().UnixNano()))
	if err := atomicWriteFile(tmpFile, rendered, 0600); err != nil {
		return fmt.Errorf("write test config: %w", err)
	}
	defer func() {
		_ = os.Remove(tmpFile)
	}()

	cmd := exec.Command(s.binPath, "run", "-test", "-c", tmpFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		cmd2 := exec.Command(s.binPath, "-test", "-config", tmpFile)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("config test failed: %s (fallback: %s)", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return nil
}

func (s *Service) restartLocked() error {
	if !s.config.Enabled {
		return s.stopLocked()
	}

	if !fileExists(s.binPath) {
		return errors.New("xray binary not found at " + s.binPath)
	}

	rendered, err := RenderRuntimeConfig(s.config)
	if err != nil {
		return fmt.Errorf("render xray config: %w", err)
	}

	// Safe testing before deployment if binary is runnable
	if err := s.TestConfig(s.config); err != nil {
		return fmt.Errorf("invalid xray configuration: %w", err)
	}

	_ = s.stopLocked()

	configPath := s.xrayRuntimeConfigPath()
	if err := atomicWriteFile(configPath, rendered, 0600); err != nil {
		return fmt.Errorf("write xray config: %w", err)
	}

	cmd := exec.Command(s.binPath, "run", "-c", configPath)
	proc := newManagedProc(cmd)
	if err := proc.Start(); err != nil {
		return fmt.Errorf("start xray: %w", err)
	}
	s.proc = proc

	pid := proc.PID()
	if pid <= 0 {
		_ = s.stopLocked()
		return errors.New("xray process has invalid pid")
	}
	if err := s.writePIDRecord(pid, configPath); err != nil {
		_ = s.stopLocked()
		return fmt.Errorf("record process identity: %w", err)
	}

	// Readiness check via ListenerOwnershipProbe
	readyTimeout := 3 * time.Second
	probeInterval := 50 * time.Millisecond
	deadline := time.Now().Add(readyTimeout)
	isReady := false

	listenAddr := s.config.ListenAddress
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}
	listenPort := s.config.ListenPort

	for time.Now().Before(deadline) {
		if !proc.IsRunning() {
			exitErr := proc.ExitError()
			_ = s.stopLocked()
			return fmt.Errorf("xray exited prematurely with code %v", exitErr)
		}

		if s.probe != nil {
			owned, err := s.probe.IsAddressPortOwnedByPID(pid, listenAddr, listenPort)
			if err == nil && owned {
				isReady = true
				break
			}
		} else {
			isReady = true
			break
		}

		time.Sleep(probeInterval)
	}

	if !isReady {
		_ = s.stopLocked()
		return fmt.Errorf("xray process %d started but failed to bind %s:%d within %v", pid, listenAddr, listenPort, readyTimeout)
	}

	return nil
}

func generateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand read: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
