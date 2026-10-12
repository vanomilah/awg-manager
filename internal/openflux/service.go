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

// Status represents the operational and installation state of OpenFlux.
type Status struct {
	Installed       bool    `json:"installed"`
	Running         bool    `json:"running"`
	PID             int     `json:"pid,omitempty"`
	UptimeS         int64   `json:"uptimeS,omitempty"`
	Version         string  `json:"version,omitempty"`
	LatestVersion   string  `json:"latestVersion,omitempty"`
	ArchSupported   bool    `json:"archSupported"`
	Binary          string  `json:"binary"`
	Arch            string  `json:"arch"`
	Link            string  `json:"link,omitempty"`
	Error           string  `json:"error,omitempty"`
	Config          *Config `json:"config,omitempty"`
	LogTail         string  `json:"logTail,omitempty"`
}

// Service manages the lifecycle, configuration, installation, and link generation for OpenFlux.
type Service struct {
	mu            sync.Mutex
	isInstalling  bool
	dataDir       string
	openfluxDir   string
	binPath       string
	cfgPath       string
	keyPath       string
	pidPath       string
	logPath       string
	arch          string
	spec          BinarySpec
	httpClient    *http.Client
	freeDisk      func(path string) (int64, bool)
	config        Config
	startedAt     time.Time
	openedPort    int
	getPublicHost func(context.Context) string
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
		arch:          normArch,
		spec:          spec,
		httpClient:    &http.Client{Timeout: 3 * time.Minute},
		freeDisk:      routerinfo.FreeBytes,
		config:        DefaultConfig(),
		getPublicHost: getPublicHost,
	}

	s.loadConfig()
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
	data, err := os.ReadFile(s.pidPath)
	if err != nil {
		return false, 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, 0
	}

	if !childproc.IsAlive(pid) {
		// Process is dead, clean up stale PID file
		_ = os.Remove(s.pidPath)
		return false, 0
	}

	if !childproc.MatchesBinary(pid, s.binPath) && !childproc.MatchesBinary(pid, "openflux") {
		_ = os.Remove(s.pidPath)
		return false, 0
	}

	return true, pid
}

// GetStatus returns the current service status.
func (s *Service) GetStatus(ctx context.Context) Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	installed := s.IsInstalled()
	running, pid := s.GetRunningState()

	var uptimeS int64
	if running && !s.startedAt.IsZero() {
		uptimeS = int64(time.Since(s.startedAt).Seconds())
	}

	host := ""
	if s.getPublicHost != nil {
		host = s.getPublicHost(ctx)
	}

	link, _ := GenerateLink(s.config, host)

	supported := IsArchSupported(s.arch)
	cfgCopy := s.config

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

// StartIfEnabled starts the service if configured as enabled and installed.
func (s *Service) StartIfEnabled(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config.Enabled && s.IsInstalled() {
		return s.startProcess(ctx)
	}
	return nil
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

	// Graceful termination
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
		// Force kill if still running
		_ = childproc.Kill(pid)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = childproc.Kill(pid)
	}

	_ = os.Remove(s.pidPath)
	return nil
}

func (s *Service) restartProcess(ctx context.Context) error {
	_ = s.stopProcess(ctx)
	time.Sleep(500 * time.Millisecond)
	return s.startProcess(ctx)
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

// GetLogTail returns the last N lines from the process log file.
func (s *Service) GetLogTail(maxLines int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLogTailLocked(maxLines)
}

func (s *Service) getLogTailLocked(maxLines int) string {
	if maxLines <= 0 {
		maxLines = 100
	}
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}
