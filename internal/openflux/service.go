package openflux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/childproc"
	"github.com/hoaxisr/awg-manager/internal/sys/routerinfo"
)

var (
	ErrArchNotSupported  = errors.New("openflux is not supported on this router architecture")
	ErrNotInstalled      = errors.New("openflux binary is not installed")
	ErrChecksumMismatch  = errors.New("downloaded binary checksum does not match pinned SHA256")
	ErrDiskSpace         = errors.New("insufficient disk space for openflux installation")
	ErrAlreadyInstalling = errors.New("openflux installation is already in progress")
)

// ClientStatus represents the operational state of the OpenFlux client.
type ClientStatus struct {
	Installed  bool          `json:"installed"`
	Running    bool          `json:"running"`
	PID        int           `json:"pid,omitempty"`
	UptimeS    int64         `json:"uptimeS,omitempty"`
	Socks5Port int           `json:"socks5Port,omitempty"`
	Socks5URI  string        `json:"socks5Uri,omitempty"`
	HttpURI    string        `json:"httpUri,omitempty"`
	Error      string        `json:"error,omitempty"`
	Config     *ClientConfig `json:"config,omitempty"`
	LogTail    string        `json:"logTail,omitempty"`
}

// Status represents the operational and installation state of OpenFlux.
type Status struct {
	Installed     bool          `json:"installed"`
	Running       bool          `json:"running"`
	PID           int           `json:"pid,omitempty"`
	UptimeS       int64         `json:"uptimeS,omitempty"`
	Version       string        `json:"version,omitempty"`
	LatestVersion string        `json:"latestVersion,omitempty"`
	ArchSupported bool          `json:"archSupported"`
	Binary        string        `json:"binary"`
	Arch          string        `json:"arch"`
	Link          string        `json:"link,omitempty"`
	Error         string        `json:"error,omitempty"`
	Config        *Config       `json:"config,omitempty"`
	LogTail       string        `json:"logTail,omitempty"`
	Client        *ClientStatus `json:"client,omitempty"`
}

// Service manages the lifecycle, configuration, installation, and link generation for OpenFlux.
type Service struct {
	mu              sync.Mutex
	isInstalling    bool
	dataDir         string
	openfluxDir     string
	binPath         string
	cfgPath         string
	keyPath         string
	pidPath         string
	logPath         string
	clientCfgPath   string
	clientKeyPath   string
	clientPidPath   string
	clientLogPath   string
	arch            string
	spec            BinarySpec
	httpClient      *http.Client
	freeDisk        func(path string) (int64, bool)
	config          Config
	startedAt       time.Time
	openedPort      int
	clientConfig    ClientConfig
	clientStartedAt time.Time
	clientLastErr   string
	clientStopCh    chan struct{}
	getPublicHost   func(context.Context) string
}

// New creates a new OpenFlux service.
func New(dataDir string, arch string, getPublicHost func(context.Context) string) *Service {
	if dataDir == "" {
		dataDir = "/opt/etc/awg-manager"
	}
	ofDir := filepath.Join(dataDir, "openflux")
	_ = os.MkdirAll(ofDir, 0755)

	normArch := NormalizeArch(arch)
	spec := EmbeddedBinaries[normArch]

	binPath := "/opt/bin/openflux"
	if dataDir != "/opt/etc/awg-manager" {
		binPath = filepath.Join(ofDir, "openflux")
	}

	s := &Service{
		dataDir:       dataDir,
		openfluxDir:   ofDir,
		binPath:       binPath,
		cfgPath:       filepath.Join(ofDir, "config.json"),
		keyPath:       filepath.Join(ofDir, "secret.key"),
		pidPath:       filepath.Join(ofDir, "openflux.pid"),
		logPath:       filepath.Join(ofDir, "openflux.log"),
		clientCfgPath: filepath.Join(ofDir, "client_config.json"),
		clientKeyPath: filepath.Join(ofDir, "client_secret.key"),
		clientPidPath: filepath.Join(ofDir, "openflux_client.pid"),
		clientLogPath: filepath.Join(ofDir, "openflux_client.log"),
		arch:          normArch,
		spec:          spec,
		httpClient:    &http.Client{Timeout: 3 * time.Minute},
		freeDisk:      routerinfo.FreeBytes,
		config:        DefaultConfig(),
		clientConfig:  DefaultClientConfig(),
		getPublicHost: getPublicHost,
	}

	s.loadConfig()
	s.loadClientConfig()
	return s
}

func (s *Service) loadConfig() {
	data, err := os.ReadFile(s.cfgPath)
	if err == nil {
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err == nil {
			if strings.TrimSpace(cfg.Secret) == "" {
				cfg.Secret, _ = GenerateSecret()
			}
			s.config = cfg
		}
	} else {
		// Save default configuration
		_ = s.saveConfigLocked()
	}
}

func (s *Service) saveConfigLocked() error {
	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.cfgPath, data, 0644)
}

func (s *Service) loadClientConfig() {
	data, err := os.ReadFile(s.clientCfgPath)
	if err == nil {
		var cfg ClientConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			if strings.TrimSpace(cfg.Socks5Listen) == "" {
				cfg.Socks5Listen = fmt.Sprintf("127.0.0.1:%d", DefaultClientSocks5Port)
			}
			if strings.TrimSpace(cfg.Name) == "" {
				cfg.Name = "OpenFlux Выход"
			}
			s.clientConfig = cfg
		}
	} else {
		_ = s.saveClientConfigLocked()
	}
}

func (s *Service) saveClientConfigLocked() error {
	data, err := json.MarshalIndent(s.clientConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.clientCfgPath, data, 0644)
}

// IsInstalled checks if the binary exists and is executable.
func (s *Service) IsInstalled() bool {
	info, err := os.Stat(s.binPath)
	if err != nil {
		// Fallback check in openfluxDir
		fallback := filepath.Join(s.openfluxDir, "openflux")
		if fi, fe := os.Stat(fallback); fe == nil && !fi.IsDir() {
			s.binPath = fallback
			return true
		}
		return false
	}
	return !info.IsDir()
}

// GetRunningState inspects the PID file and process table.
func (s *Service) GetRunningState() (bool, int) {
	return s.checkRunningState(s.pidPath)
}

// GetClientRunningState inspects the client PID file and process table.
func (s *Service) GetClientRunningState() (bool, int) {
	return s.checkRunningState(s.clientPidPath)
}

func (s *Service) checkRunningState(pidPath string) (bool, int) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false, 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, 0
	}

	if !childproc.IsAlive(pid) {
		// Process is dead, clean up stale PID file
		_ = os.Remove(pidPath)
		return false, 0
	}

	if !childproc.MatchesBinary(pid, s.binPath) && !childproc.MatchesBinary(pid, "openflux") {
		_ = os.Remove(pidPath)
		return false, 0
	}

	return true, pid
}

func (s *Service) getClientStatusLocked() ClientStatus {
	installed := s.IsInstalled()
	running, pid := s.GetClientRunningState()
	var uptimeS int64
	if running {
		startedAt := s.clientStartedAt
		if startedAt.IsZero() {
			if fi, err := os.Stat(s.clientPidPath); err == nil {
				startedAt = fi.ModTime()
			}
		}
		if !startedAt.IsZero() {
			uptimeS = int64(time.Since(startedAt).Seconds())
		}
		if uptimeS < 1 {
			uptimeS = 1
		}
	}

	cfgCopy := s.clientConfig
	logTail := s.getFileLogTailLocked(s.clientLogPath, 100)
	errStr := s.clientLastErr
	if running && strings.Contains(logTail, "[SOCKS5] Listening on") {
		errStr = ""
	}

	return ClientStatus{
		Installed:  installed,
		Running:    running,
		PID:        pid,
		UptimeS:    uptimeS,
		Socks5Port: s.clientConfig.Socks5Port(),
		Socks5URI:  s.clientConfig.Socks5URI(),
		HttpURI:    s.clientConfig.HttpURI(),
		Error:      errStr,
		Config:     &cfgCopy,
		LogTail:    logTail,
	}
}

// GetClientStatus returns the current OpenFlux client status.
func (s *Service) GetClientStatus() ClientStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getClientStatusLocked()
}

// GetStatus returns the current service status (both server and client).
func (s *Service) GetStatus(ctx context.Context) Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	installed := s.IsInstalled()
	running, pid := s.GetRunningState()

	var uptimeS int64
	if running {
		startedAt := s.startedAt
		if startedAt.IsZero() {
			if fi, err := os.Stat(s.pidPath); err == nil {
				startedAt = fi.ModTime()
			}
		}
		if !startedAt.IsZero() {
			uptimeS = int64(time.Since(startedAt).Seconds())
		}
		if uptimeS < 1 {
			uptimeS = 1
		}
	}

	host := ""
	if s.getPublicHost != nil {
		host = s.getPublicHost(ctx)
	}

	link, _ := GenerateLink(s.config, host)

	supported := IsArchSupported(s.arch)
	cfgCopy := s.config
	clientSt := s.getClientStatusLocked()

	return Status{
		Installed:     installed,
		Running:       running,
		PID:           pid,
		UptimeS:       uptimeS,
		Version:       s.spec.Version,
		LatestVersion: s.spec.Version,
		ArchSupported: supported,
		Binary:        s.binPath,
		Arch:          s.arch,
		Link:          link,
		Config:        &cfgCopy,
		LogTail:       s.getLogTailLocked(100),
		Client:        &clientSt,
	}
}

// SaveConfig validates and saves the configuration, applying changes if needed.
func (s *Service) SaveConfig(ctx context.Context, cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := cfg.Validate(); err != nil {
		return err
	}

	s.config = cfg
	if err := s.saveConfigLocked(); err != nil {
		return err
	}

	running, _ := s.GetRunningState()
	if running {
		if !s.config.Enabled {
			_ = s.stopProcess(ctx)
		} else {
			_ = s.restartProcess(ctx)
		}
	} else if s.config.Enabled && s.IsInstalled() {
		_ = s.startProcess(ctx)
	}

	return nil
}

// Start launches the OpenFlux process.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.startProcess(ctx); err != nil {
		return err
	}
	s.config.Enabled = true
	_ = s.saveConfigLocked()
	return nil
}

// Stop stops the OpenFlux process.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.stopProcess(ctx); err != nil {
		return err
	}
	s.config.Enabled = false
	_ = s.saveConfigLocked()
	return nil
}

// Restart restarts the OpenFlux process.
func (s *Service) Restart(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.restartProcess(ctx)
}

// SaveClientConfig validates and saves the client configuration, applying changes if needed.
func (s *Service) SaveClientConfig(ctx context.Context, cfg ClientConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(cfg.Socks5Listen) == "" {
		cfg.Socks5Listen = fmt.Sprintf("127.0.0.1:%d", DefaultClientSocks5Port)
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "OpenFlux Выход"
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	s.clientConfig = cfg
	if err := s.saveClientConfigLocked(); err != nil {
		return err
	}

	running, _ := s.GetClientRunningState()
	if running {
		if !s.clientConfig.Enabled {
			return s.stopClientProcess(ctx)
		}
		return s.restartClientProcess(ctx)
	} else if s.clientConfig.Enabled && s.IsInstalled() {
		if err := s.startClientProcess(ctx, true); err != nil {
			s.clientConfig.Enabled = false
			_ = s.saveClientConfigLocked()
			return err
		}
	}

	return nil
}

// StartClient launches the OpenFlux client process.
func (s *Service) StartClient(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.startClientProcess(ctx, true); err != nil {
		return err
	}
	s.clientConfig.Enabled = true
	_ = s.saveClientConfigLocked()
	return nil
}

// StopClient stops the OpenFlux client process.
func (s *Service) StopClient(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.stopClientProcess(ctx); err != nil {
		return err
	}
	s.clientConfig.Enabled = false
	s.clientLastErr = ""
	_ = s.saveClientConfigLocked()
	return nil
}

// RestartClient restarts the OpenFlux client process.
func (s *Service) RestartClient(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.restartClientProcess(ctx)
}

// ParseClientLink decodes an openflux://v1/... link and builds a ClientConfig preserving local listen settings.
func (s *Service) ParseClientLink(link string) (ShareConfig, ClientConfig, error) {
	sc, err := Decode(link)
	if err != nil {
		return ShareConfig{}, ClientConfig{}, err
	}
	s.mu.Lock()
	base := s.clientConfig
	s.mu.Unlock()

	cfg := ClientConfigFromShare(sc, base)
	return sc, cfg, nil
}

// Shutdown stops running server and client processes on daemon exit without clearing their Enabled flags.
func (s *Service) Shutdown(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.stopProcess(ctx)
	_ = s.stopClientProcess(ctx)
}

// StartIfEnabled starts server and client processes if configured as enabled and installed.
func (s *Service) StartIfEnabled(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsInstalled() {
		return nil
	}
	var firstErr error
	if s.config.Enabled {
		if err := s.startProcess(ctx); err != nil {
			firstErr = err
		}
	}
	if s.clientConfig.Enabled {
		if err := s.startClientProcess(ctx, true); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) startProcess(ctx context.Context) error {
	if !s.IsInstalled() {
		return ErrNotInstalled
	}

	running, _ := s.GetRunningState()
	if running {
		s.applyFirewall(ctx)
		return nil
	}

	if err := s.config.Validate(); err != nil {
		return fmt.Errorf("ошибка валидации настроек: %w", err)
	}

	// Write secret file
	_ = os.MkdirAll(s.openfluxDir, 0755)
	if err := os.WriteFile(s.keyPath, []byte(strings.TrimSpace(s.config.Secret)+"\n"), 0600); err != nil {
		return fmt.Errorf("не удалось записать ключ шифрования: %w", err)
	}

	// Clean old PID file
	_ = os.Remove(s.pidPath)

	args := s.config.BuildArgs(s.keyPath)

	logFile, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("не удалось открыть файл журнала: %w", err)
	}

	cmd := exec.Command(s.binPath, args...)
	cmd.Dir = s.openfluxDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("не удалось запустить openflux: %w", err)
	}

	pid := cmd.Process.Pid
	_ = os.WriteFile(s.pidPath, []byte(strconv.Itoa(pid)+"\n"), 0644)
	s.startedAt = time.Now()

	// Detach process handling
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()

	s.applyFirewall(ctx)
	return nil
}

func (s *Service) stopProcess(ctx context.Context) error {
	running, pid := s.GetRunningState()
	if !running {
		_ = os.Remove(s.pidPath)
		s.removeFirewall(ctx)
		return nil
	}

	s.removeFirewall(ctx)
	terminatePID(pid, s.pidPath)
	return nil
}

func (s *Service) restartProcess(ctx context.Context) error {
	_ = s.stopProcess(ctx)
	time.Sleep(500 * time.Millisecond)
	return s.startProcess(ctx)
}

func extractLastErrorFromLog(logTail string) string {
	lines := strings.Split(strings.TrimSpace(logTail), "\n")
	lastNonEmpty := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if lastNonEmpty == "" {
			lastNonEmpty = line
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "failed") || strings.Contains(lower, "error") || strings.Contains(lower, "handshake not complete") || strings.Contains(lower, "fatal") || strings.Contains(lower, "bind:") || strings.Contains(lower, "panic") {
			return line
		}
	}
	return lastNonEmpty
}

func (s *Service) startClientProcess(ctx context.Context, resetStopCh bool) error {
	if !s.IsInstalled() {
		return ErrNotInstalled
	}

	running, _ := s.GetClientRunningState()
	if running {
		return nil
	}

	if err := s.clientConfig.Validate(); err != nil {
		return fmt.Errorf("ошибка валидации настроек клиента: %w", err)
	}
	if err := s.clientConfig.ValidateForStart(); err != nil {
		return err
	}

	if resetStopCh {
		if s.clientStopCh != nil {
			close(s.clientStopCh)
		}
		s.clientStopCh = make(chan struct{})
		s.clientLastErr = ""
	}
	stopCh := s.clientStopCh
	if stopCh == nil {
		stopCh = make(chan struct{})
		s.clientStopCh = stopCh
	}

	_ = os.MkdirAll(s.openfluxDir, 0755)
	if strings.TrimSpace(s.clientConfig.Secret) != "" {
		if err := os.WriteFile(s.clientKeyPath, []byte(strings.TrimSpace(s.clientConfig.Secret)+"\n"), 0600); err != nil {
			return fmt.Errorf("не удалось записать ключ шифрования клиента: %w", err)
		}
	}

	_ = os.Remove(s.clientPidPath)

	args := s.clientConfig.BuildArgs(s.clientKeyPath)

	// Truncate log if manual start or if file exceeds 128 KB
	openFlags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if resetStopCh {
		openFlags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	} else if fi, stErr := os.Stat(s.clientLogPath); stErr == nil && fi.Size() > 128*1024 {
		openFlags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}

	logFile, err := os.OpenFile(s.clientLogPath, openFlags, 0644)
	if err != nil {
		return fmt.Errorf("не удалось открыть файл журнала клиента: %w", err)
	}

	cmd := exec.Command(s.binPath, args...)
	cmd.Dir = s.openfluxDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("не удалось запустить клиент openflux: %w", err)
	}

	pid := cmd.Process.Pid
	_ = os.WriteFile(s.clientPidPath, []byte(strconv.Itoa(pid)+"\n"), 0644)
	s.clientStartedAt = time.Now()

	waitDone := make(chan error, 1)
	go func(stop <-chan struct{}) {
		waitErr := cmd.Wait()
		_ = logFile.Close()
		waitDone <- waitErr

		s.mu.Lock()
		defer s.mu.Unlock()

		select {
		case <-stop:
			return
		default:
		}

		if waitErr != nil {
			if msg := extractLastErrorFromLog(s.getFileLogTailLocked(s.clientLogPath, 30)); msg != "" {
				s.clientLastErr = msg
			} else {
				s.clientLastErr = fmt.Sprintf("процесс завершился: %v", waitErr)
			}
		}

		if s.clientConfig.Enabled && s.clientConfig.AutoReconnect {
			go func() {
				select {
				case <-stop:
					return
				case <-time.After(5 * time.Second):
				}
				s.mu.Lock()
				defer s.mu.Unlock()
				select {
				case <-stop:
					return
				default:
				}
				if s.clientConfig.Enabled {
					_ = s.startClientProcess(context.Background(), false)
				}
			}()
		}
	}(stopCh)

	if resetStopCh {
		select {
		case waitErr := <-waitDone:
			_ = os.Remove(s.clientPidPath)
			msg := extractLastErrorFromLog(s.getFileLogTailLocked(s.clientLogPath, 30))
			if msg == "" && waitErr != nil {
				msg = fmt.Sprintf("процесс завершился сразу после запуска: %v", waitErr)
			} else if msg == "" {
				msg = "процесс завершился сразу после запуска"
			}
			s.clientLastErr = msg
			return fmt.Errorf("%s", msg)
		case <-time.After(150 * time.Millisecond):
		}
	}

	return nil
}

func (s *Service) stopClientProcess(ctx context.Context) error {
	if s.clientStopCh != nil {
		close(s.clientStopCh)
		s.clientStopCh = nil
	}

	running, pid := s.GetClientRunningState()
	if !running {
		_ = os.Remove(s.clientPidPath)
		return nil
	}

	terminatePID(pid, s.clientPidPath)
	return nil
}

func (s *Service) restartClientProcess(ctx context.Context) error {
	_ = s.stopClientProcess(ctx)
	time.Sleep(500 * time.Millisecond)
	return s.startClientProcess(ctx, true)
}

func terminatePID(pid int, pidPath string) {
	_ = childproc.Terminate(pid)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			if !childproc.IsAlive(pid) {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = childproc.Kill(pid)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = childproc.Kill(pid)
	}

	_ = os.Remove(pidPath)
}

func (s *Service) applyFirewall(ctx context.Context) {
	if runtime.GOOS != "linux" {
		return
	}
	if !s.config.OpenFirewall {
		s.removeFirewall(ctx)
		return
	}
	port := s.config.DirectPort()
	if port <= 0 {
		return
	}
	portStr := strconv.Itoa(port)

	// Check if rule already exists
	checkCmd := exec.CommandContext(ctx, "iptables", "-C", "INPUT", "-p", "tcp", "--dport", portStr, "-j", "ACCEPT")
	if err := checkCmd.Run(); err != nil {
		// Rule does not exist, insert it
		addCmd := exec.CommandContext(ctx, "iptables", "-I", "INPUT", "1", "-p", "tcp", "--dport", portStr, "-j", "ACCEPT")
		_ = addCmd.Run()
	}
	s.openedPort = port
}

func (s *Service) removeFirewall(ctx context.Context) {
	if runtime.GOOS != "linux" || s.openedPort <= 0 {
		return
	}
	portStr := strconv.Itoa(s.openedPort)
	for {
		delCmd := exec.CommandContext(ctx, "iptables", "-D", "INPUT", "-p", "tcp", "--dport", portStr, "-j", "ACCEPT")
		if err := delCmd.Run(); err != nil {
			break
		}
	}
	s.openedPort = 0
}

// Install downloads and installs the pinned OpenFlux binary.
func (s *Service) Install(ctx context.Context) error {
	s.mu.Lock()
	if s.isInstalling {
		s.mu.Unlock()
		return ErrAlreadyInstalling
	}
	s.isInstalling = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isInstalling = false
		s.mu.Unlock()
	}()

	if !IsArchSupported(s.arch) {
		return ErrArchNotSupported
	}

	spec := s.spec
	if spec.URL == "" {
		return fmt.Errorf("нет адреса загрузки для архитектуры %s", s.arch)
	}

	_ = os.MkdirAll(s.openfluxDir, 0755)
	if s.freeDisk != nil {
		if free, ok := s.freeDisk(s.openfluxDir); ok && free < 30*1024*1024 {
			return ErrDiskSpace
		}
	}

	tmpFile, err := os.CreateTemp(s.openfluxDir, "openflux-download-*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	defer tmpFile.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "awg-manager-openflux-installer")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка загрузки: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("загрузка вернула HTTP %d", resp.StatusCode)
	}

	h := sha256.New()
	tee := io.TeeReader(resp.Body, h)
	if _, err := io.Copy(tmpFile, tee); err != nil {
		return fmt.Errorf("ошибка записи файла: %w", err)
	}
	_ = tmpFile.Close()

	computedSHA := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(computedSHA, spec.SHA256) {
		return fmt.Errorf("%w: ожидался %s, получен %s", ErrChecksumMismatch, spec.SHA256, computedSHA)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.stopProcess(ctx)
	_ = s.stopClientProcess(ctx)

	// Ensure target directory exists
	_ = os.MkdirAll(filepath.Dir(s.binPath), 0755)

	if err := os.Rename(tmpPath, s.binPath); err != nil {
		// Fallback: copy file
		in, openErr := os.Open(tmpPath)
		if openErr != nil {
			return fmt.Errorf("не удалось переместить бинарь: %w", err)
		}
		defer in.Close()
		out, createErr := os.OpenFile(s.binPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if createErr != nil {
			return fmt.Errorf("не удалось создать файл %s: %w", s.binPath, createErr)
		}
		defer out.Close()
		if _, copyErr := io.Copy(out, in); copyErr != nil {
			return fmt.Errorf("не удалось скопировать бинарь: %w", copyErr)
		}
	}

	return nil
}

// GetLogTail returns the last N lines from the server process log file.
func (s *Service) GetLogTail(maxLines int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLogTailLocked(maxLines)
}

// GetClientLogTail returns the last N lines from the client process log file.
func (s *Service) GetClientLogTail(maxLines int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getFileLogTailLocked(s.clientLogPath, maxLines)
}

func (s *Service) getLogTailLocked(maxLines int) string {
	return s.getFileLogTailLocked(s.logPath, maxLines)
}

func (s *Service) getFileLogTailLocked(path string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 100
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

