package xrayserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrMigrationConflict = errors.New("migration conflict: legacy configuration has unsupported or ambiguous topology")
	ErrAlreadyMigrated   = errors.New("already migrated")
)

type LegacyTopology string

const (
	TopologyA LegacyTopology = "A" // Direct: origin -> legacy Xray public port
	TopologyB LegacyTopology = "B" // Loopback: Xray on 127.0.0.1 + CDN dispatcher
	TopologyC LegacyTopology = "C" // Ambiguous or conflicting
)

type LegacyDiscoveryResult struct {
	Found               bool              `json:"found"`
	ConfigPath          string            `json:"config_path"`
	InitScriptPath      string            `json:"init_script_path"`
	InitScriptIsSymlink bool              `json:"init_script_is_symlink"`
	Topology            LegacyTopology    `json:"topology"`
	ListenAddress       string            `json:"listen_address"`
	ListenPort          int               `json:"listen_port"`
	PublicDomain        string            `json:"public_domain"`
	Path                string            `json:"path"`
	Mode                string            `json:"mode"`
	UplinkMethod        string            `json:"uplink_method"`
	Clients             []Client          `json:"clients"`
	Checksums           map[string]string `json:"checksums"`
	ConflictReason      string            `json:"conflict_reason,omitempty"`
}

type MigrationMarker struct {
	MigratedAt           string         `json:"migrated_at"`
	SourcePath           string         `json:"source_path"`
	SourceChecksum       string         `json:"source_checksum"`
	Topology             LegacyTopology `json:"topology"`
	InitScriptRenamed    bool           `json:"init_script_renamed"`
	OriginPort           int            `json:"origin_port"`
	AssignedLoopbackPort int            `json:"assigned_loopback_port"`
}

type RuntimeDecision struct {
	ActiveGeneration  string `json:"active_generation"` // "new" | "legacy"
	MigrationStatus   string `json:"migration_status"`  // "migrated" | "deferred" | "conflict"
	DecidedAt         string `json:"decided_at"`
	GenerationCounter int    `json:"generation_counter"`
	Reason            string `json:"reason,omitempty"`
}

type legacyConfig struct {
	Inbounds []struct {
		Listen         string `json:"listen"`
		Port           int    `json:"port"`
		Protocol       string `json:"protocol"`
		Settings       struct {
			Clients []struct {
				ID string `json:"id"`
			} `json:"clients"`
		} `json:"settings"`
		StreamSettings struct {
			Network       string `json:"network"`
			XHTTPSettings struct {
				Host             string `json:"host"`
				Path             string `json:"path"`
				Mode             string `json:"mode"`
				UplinkHTTPMethod string `json:"uplinkHTTPMethod"`
			} `json:"xhttpSettings"`
		} `json:"streamSettings"`
	} `json:"inbounds"`
}

// DiscoverLegacy inspects the legacy configuration and init script without modifying the filesystem.
func DiscoverLegacy(cfgPath, initScriptPath string) (*LegacyDiscoveryResult, error) {
	res := &LegacyDiscoveryResult{
		ConfigPath:     cfgPath,
		InitScriptPath: initScriptPath,
		Checksums:      make(map[string]string),
	}

	if !fileExists(cfgPath) {
		return res, nil
	}

	res.Found = true

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("read legacy config: %w", err)
	}

	h := sha256.Sum256(data)
	res.Checksums["config.json"] = hex.EncodeToString(h[:])

	// Check init script symlink safety
	if initScriptPath != "" && fileExists(initScriptPath) {
		fi, err := os.Lstat(initScriptPath)
		if err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				res.InitScriptIsSymlink = true
				target, err := filepath.EvalSymlinks(initScriptPath)
				if err != nil {
					res.Topology = TopologyC
					res.ConflictReason = fmt.Sprintf("invalid init script symlink: %v", err)
					return res, nil
				}
				// Verify target is within /opt/etc/init.d
				cleanTarget := filepath.Clean(target)
				cleanInitDir := filepath.Clean(filepath.Dir(initScriptPath))
				rel, err := filepath.Rel(cleanInitDir, cleanTarget)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
					res.Topology = TopologyC
					res.ConflictReason = fmt.Sprintf("symlink target %q escapes trusted init.d directory", cleanTarget)
					return res, nil
				}
			}

			initData, _ := os.ReadFile(initScriptPath)
			initSum := sha256.Sum256(initData)
			res.Checksums["init_script"] = hex.EncodeToString(initSum[:])
		}
	}

	var parsed legacyConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		res.Topology = TopologyC
		res.ConflictReason = fmt.Sprintf("malformed legacy JSON: %v", err)
		return res, nil
	}

	// Locate VLESS inbound
	var vlessInbounds []int
	for idx, in := range parsed.Inbounds {
		if strings.EqualFold(in.Protocol, "vless") {
			vlessInbounds = append(vlessInbounds, idx)
		}
	}

	if len(vlessInbounds) == 0 {
		res.Topology = TopologyC
		res.ConflictReason = "no VLESS inbound found in legacy config"
		return res, nil
	}

	if len(vlessInbounds) > 1 {
		res.Topology = TopologyC
		res.ConflictReason = "multiple VLESS inbounds found: ambiguous topology"
		return res, nil
	}

	in := parsed.Inbounds[vlessInbounds[0]]
	res.ListenAddress = in.Listen
	res.ListenPort = in.Port
	res.PublicDomain = in.StreamSettings.XHTTPSettings.Host
	res.Path = in.StreamSettings.XHTTPSettings.Path
	res.Mode = in.StreamSettings.XHTTPSettings.Mode
	res.UplinkMethod = in.StreamSettings.XHTTPSettings.UplinkHTTPMethod

	for idx, cl := range in.Settings.Clients {
		if cl.ID != "" {
			res.Clients = append(res.Clients, Client{
				ID:        cl.ID,
				Remark:    fmt.Sprintf("Client %d (Legacy)", idx+1),
				Enabled:   true,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			})
		}
	}

	// Topology Classification
	if res.ListenAddress == "" || res.ListenAddress == "0.0.0.0" {
		res.Topology = TopologyA
	} else if res.ListenAddress == "127.0.0.1" {
		res.Topology = TopologyB
	} else {
		res.Topology = TopologyC
		res.ConflictReason = fmt.Sprintf("unsupported listen address %q", res.ListenAddress)
	}

	return res, nil
}

// MigrateLegacy converts discovered legacy configuration into managed xrayserver.Config and marks migration complete.
func MigrateLegacy(ctx context.Context, disc *LegacyDiscoveryResult, dataDir string, loopbackPort int) (*Config, error) {
	if disc == nil || !disc.Found {
		return nil, errors.New("no legacy configuration discovered")
	}

	if disc.Topology == TopologyC {
		return nil, fmt.Errorf("%w: %s", ErrMigrationConflict, disc.ConflictReason)
	}

	if loopbackPort <= 0 {
		loopbackPort = 9008
	}

	xrayDir := filepath.Join(dataDir, "xray")
	if err := os.MkdirAll(xrayDir, 0700); err != nil {
		return nil, fmt.Errorf("create xray dir: %w", err)
	}

	markerPath := filepath.Join(xrayDir, "migration-marker.json")
	if fileExists(markerPath) {
		return nil, ErrAlreadyMigrated
	}

	// Backup legacy config into xray directory
	if disc.ConfigPath != "" && fileExists(disc.ConfigPath) {
		cfgData, _ := os.ReadFile(disc.ConfigPath)
		_ = atomicWriteFile(filepath.Join(xrayDir, "legacy-config.json.bak"), cfgData, 0600)
	}

	// Handle init script rename safely
	var initRenamed bool
	if disc.InitScriptPath != "" && fileExists(disc.InitScriptPath) {
		disabledPath := disc.InitScriptPath + ".disabled"
		if err := os.Rename(disc.InitScriptPath, disabledPath); err != nil {
			return nil, fmt.Errorf("rename legacy init script: %w", err)
		}
		initRenamed = true
	}

	// Build new Config
	newCfg := &Config{
		Enabled:            true,
		ListenAddress:      "127.0.0.1",
		ListenPort:         loopbackPort,
		DispatcherPort:     disc.ListenPort,
		PublicDomain:       disc.PublicDomain,
		PublicPort:         443,
		Path:               disc.Path,
		Mode:               disc.Mode,
		UplinkMethod:       disc.UplinkMethod,
		XmuxMaxConnections: 2,
		OutboundSocksPort:  1099,
		Clients:            disc.Clients,
	}

	if disc.Topology == TopologyB {
		newCfg.ListenPort = disc.ListenPort
		newCfg.DispatcherPort = 9009
	}

	if newCfg.Path == "" {
		newCfg.Path = "/cdn-bridge/"
	}
	if newCfg.Mode == "" {
		newCfg.Mode = "packet-up"
	}
	if newCfg.UplinkMethod == "" {
		newCfg.UplinkMethod = "GET"
	}

	// Save settings
	settingsPath := filepath.Join(xrayDir, "xray-server-settings.json")
	var origSettings []byte
	var origPerm os.FileMode = 0600
	settingsExisted := false
	if fi, err := os.Stat(settingsPath); err == nil && !fi.IsDir() {
		settingsExisted = true
		origPerm = fi.Mode().Perm()
		if data, err := os.ReadFile(settingsPath); err == nil {
			origSettings = data
		}
	}

	rollbackSettings := func() {
		if settingsExisted && origSettings != nil {
			_ = atomicWriteFile(settingsPath, origSettings, origPerm)
		} else if !settingsExisted {
			_ = os.Remove(settingsPath)
		}
	}

	settingsData, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		if initRenamed {
			_ = os.Rename(disc.InitScriptPath+".disabled", disc.InitScriptPath)
		}
		return nil, fmt.Errorf("marshal settings: %w", err)
	}

	if err := atomicWriteFile(settingsPath, settingsData, 0600); err != nil {
		if initRenamed {
			_ = os.Rename(disc.InitScriptPath+".disabled", disc.InitScriptPath)
		}
		return nil, fmt.Errorf("write settings: %w", err)
	}

	// Save marker
	marker := MigrationMarker{
		MigratedAt:           time.Now().UTC().Format(time.RFC3339),
		SourcePath:           disc.ConfigPath,
		SourceChecksum:       disc.Checksums["config.json"],
		Topology:             disc.Topology,
		InitScriptRenamed:    initRenamed,
		OriginPort:           disc.ListenPort,
		AssignedLoopbackPort: newCfg.ListenPort,
	}
	markerData, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		if initRenamed {
			_ = os.Rename(disc.InitScriptPath+".disabled", disc.InitScriptPath)
		}
		rollbackSettings()
		return nil, fmt.Errorf("marshal marker: %w", err)
	}
	if err := atomicWriteFile(markerPath, markerData, 0600); err != nil {
		if initRenamed {
			_ = os.Rename(disc.InitScriptPath+".disabled", disc.InitScriptPath)
		}
		rollbackSettings()
		return nil, fmt.Errorf("write marker: %w", err)
	}

	// Save runtime decision
	decision := RuntimeDecision{
		ActiveGeneration:  "new",
		MigrationStatus:   "migrated",
		DecidedAt:         time.Now().UTC().Format(time.RFC3339),
		GenerationCounter: 1,
	}
	if err := SaveRuntimeDecision(dataDir, decision); err != nil {
		if initRenamed {
			_ = os.Rename(disc.InitScriptPath+".disabled", disc.InitScriptPath)
		}
		rollbackSettings()
		_ = os.Remove(markerPath)
		return nil, fmt.Errorf("save runtime decision: %w", err)
	}

	return newCfg, nil
}

// GetRuntimeDecision reads runtime-decision.json or returns default.
func GetRuntimeDecision(dataDir string) (RuntimeDecision, error) {
	p := filepath.Join(dataDir, "xray", "runtime-decision.json")
	if !fileExists(p) {
		return RuntimeDecision{
			ActiveGeneration: "new",
			MigrationStatus:  "migrated",
		}, nil
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return RuntimeDecision{}, err
	}

	var d RuntimeDecision
	if err := json.Unmarshal(data, &d); err != nil {
		return RuntimeDecision{}, err
	}
	return d, nil
}

// SaveRuntimeDecision atomically writes runtime-decision.json.
func SaveRuntimeDecision(dataDir string, d RuntimeDecision) error {
	d.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	p := filepath.Join(dataDir, "xray", "runtime-decision.json")
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(p, data, 0600)
}
