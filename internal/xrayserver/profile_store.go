package xrayserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/xrayconfig"
)

// ActivePointer records the active generation ID for a profile.
type ActivePointer struct {
	ActiveGenerationID string    `json:"active_generation_id"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// GenerationMetadata contains metadata for a single profile snapshot generation.
type GenerationMetadata struct {
	GenerationID  string                 `json:"generation_id"`
	ProfileID     string                 `json:"profile_id"`
	ProfileName   string                 `json:"profile_name"`
	Role          xrayconfig.ProfileRole `json:"role"`
	Enabled       bool                   `json:"enabled"`
	SchemaVersion int                    `json:"schema_version"`
	CreatedAt     time.Time              `json:"created_at"`
	Checksums     map[string]string      `json:"checksums"`
}

// StoredProfile holds the full configuration of a profile generation.
type StoredProfile struct {
	Metadata   GenerationMetadata        `json:"metadata"`
	Managed    *xrayconfig.ManagedConfig `json:"managed"`
	RawOverlay []byte                    `json:"raw_overlay,omitempty"`
}

// ProfileStore defines the interface for managing crash-consistent profile snapshots.
type ProfileStore interface {
	SaveProfile(ctx context.Context, profileID string, name string, role xrayconfig.ProfileRole, enabled bool, managed *xrayconfig.ManagedConfig, rawOverlay []byte) (string, error)
	GetActiveProfile(ctx context.Context, profileID string) (*StoredProfile, error)
	GetProfileGeneration(ctx context.Context, profileID, generationID string) (*StoredProfile, error)
	ListProfiles(ctx context.Context) ([]GenerationMetadata, error)
	ListGenerations(ctx context.Context, profileID string) ([]GenerationMetadata, error)
	RollbackToGeneration(ctx context.Context, profileID, generationID string) error
	DeleteProfile(ctx context.Context, profileID string) error
	RecoverIndex(ctx context.Context) ([]GenerationMetadata, error)
}

// DiskProfileStore implements ProfileStore using a generation/snapshot directory structure.
type DiskProfileStore struct {
	mu           sync.RWMutex
	rootDir      string
	maxRetention int
}

// NewDiskProfileStore creates and initializes DiskProfileStore under rootDir.
func NewDiskProfileStore(rootDir string) (*DiskProfileStore, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("profile root directory cannot be empty")
	}

	cleanRoot := filepath.Clean(rootDir)
	if err := os.MkdirAll(cleanRoot, 0700); err != nil {
		return nil, fmt.Errorf("create profile store root: %w", err)
	}

	return &DiskProfileStore{
		rootDir:      cleanRoot,
		maxRetention: 10,
	}, nil
}

func (s *DiskProfileStore) profileDir(profileID string) string {
	return filepath.Join(s.rootDir, profileID)
}

func (s *DiskProfileStore) generationsDir(profileID string) string {
	return filepath.Join(s.profileDir(profileID), "generations")
}

func (s *DiskProfileStore) generationPath(profileID, generationID string) string {
	return filepath.Join(s.generationsDir(profileID), generationID)
}

func (s *DiskProfileStore) activePointerPath(profileID string) string {
	return filepath.Join(s.profileDir(profileID), "active.json")
}

func generateGenerationID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("gen-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("gen-%d-%s", time.Now().Unix(), hex.EncodeToString(b))
}

// SaveProfile creates a new generation snapshot and atomically points active.json to it.
func (s *DiskProfileStore) SaveProfile(ctx context.Context, profileID string, name string, role xrayconfig.ProfileRole, enabled bool, managed *xrayconfig.ManagedConfig, rawOverlay []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateID(profileID); err != nil {
		return "", fmt.Errorf("invalid profile ID: %w", err)
	}
	if managed == nil {
		return "", fmt.Errorf("managed config cannot be nil")
	}

	genID := generateGenerationID()
	genDir := s.generationPath(profileID, genID)
	if err := os.MkdirAll(genDir, 0700); err != nil {
		return "", fmt.Errorf("create generation dir: %w", err)
	}

	checksums := make(map[string]string)

	// 1. Write managed.json
	managedBytes, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		_ = os.RemoveAll(genDir)
		return "", fmt.Errorf("marshal managed config: %w", err)
	}
	managedPath := filepath.Join(genDir, "managed.json")
	if err := writeAndSyncFile(managedPath, managedBytes, 0600); err != nil {
		_ = os.RemoveAll(genDir)
		return "", fmt.Errorf("write managed.json: %w", err)
	}
	hM := sha256.Sum256(managedBytes)
	checksums["managed.json"] = hex.EncodeToString(hM[:])

	// 2. Write raw.json (if provided)
	if len(rawOverlay) > 0 {
		rawPath := filepath.Join(genDir, "raw.json")
		if err := writeAndSyncFile(rawPath, rawOverlay, 0600); err != nil {
			_ = os.RemoveAll(genDir)
			return "", fmt.Errorf("write raw.json: %w", err)
		}
		hR := sha256.Sum256(rawOverlay)
		checksums["raw.json"] = hex.EncodeToString(hR[:])
	}

	// 3. Write metadata.json
	meta := GenerationMetadata{
		GenerationID:  genID,
		ProfileID:     profileID,
		ProfileName:   name,
		Role:          role,
		Enabled:       enabled,
		SchemaVersion: xrayconfig.CurrentSchemaVersion,
		CreatedAt:     time.Now().UTC(),
		Checksums:     checksums,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = os.RemoveAll(genDir)
		return "", fmt.Errorf("marshal metadata: %w", err)
	}
	metaPath := filepath.Join(genDir, "metadata.json")
	if err := writeAndSyncFile(metaPath, metaBytes, 0600); err != nil {
		_ = os.RemoveAll(genDir)
		return "", fmt.Errorf("write metadata.json: %w", err)
	}

	// 4. Fsync generation directory
	syncDir(genDir)

	// 5. Atomically replace active.json pointer
	activePtr := ActivePointer{
		ActiveGenerationID: genID,
		UpdatedAt:          time.Now().UTC(),
	}
	ptrBytes, err := json.MarshalIndent(activePtr, "", "  ")
	if err != nil {
		_ = os.RemoveAll(genDir)
		return "", fmt.Errorf("marshal active pointer: %w", err)
	}

	pDir := s.profileDir(profileID)
	ptrPath := s.activePointerPath(profileID)
	if err := atomicWriteSecret(ptrPath, ptrBytes); err != nil {
		_ = os.RemoveAll(genDir)
		return "", fmt.Errorf("atomic update active.json: %w", err)
	}

	// 6. Fsync parent profile directory
	syncDir(pDir)

	// 7. Prune older generations if retention exceeded
	s.pruneGenerationsLocked(profileID, genID)

	return genID, nil
}

// GetActiveProfile retrieves the currently active profile snapshot.
func (s *DiskProfileStore) GetActiveProfile(ctx context.Context, profileID string) (*StoredProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := validateID(profileID); err != nil {
		return nil, fmt.Errorf("invalid profile ID: %w", err)
	}

	ptrPath := s.activePointerPath(profileID)
	if !fileExists(ptrPath) {
		return nil, fmt.Errorf("profile %q not found or has no active generation", profileID)
	}

	ptrBytes, err := os.ReadFile(ptrPath)
	if err != nil {
		return nil, fmt.Errorf("read active pointer: %w", err)
	}

	var ptr ActivePointer
	if err := json.Unmarshal(ptrBytes, &ptr); err != nil {
		return nil, fmt.Errorf("unmarshal active pointer: %w", err)
	}

	return s.getGenerationLocked(profileID, ptr.ActiveGenerationID)
}

// GetProfileGeneration retrieves a specific generation of a profile.
func (s *DiskProfileStore) GetProfileGeneration(ctx context.Context, profileID, generationID string) (*StoredProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := validateID(profileID); err != nil {
		return nil, fmt.Errorf("invalid profile ID: %w", err)
	}
	if err := validateID(generationID); err != nil {
		return nil, fmt.Errorf("invalid generation ID: %w", err)
	}

	return s.getGenerationLocked(profileID, generationID)
}

func (s *DiskProfileStore) getGenerationLocked(profileID, generationID string) (*StoredProfile, error) {
	genDir := s.generationPath(profileID, generationID)
	if !dirExists(genDir) {
		return nil, fmt.Errorf("generation %q for profile %q not found", generationID, profileID)
	}

	// Read metadata
	metaPath := filepath.Join(genDir, "metadata.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read metadata.json: %w", err)
	}
	var meta GenerationMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata.json: %w", err)
	}

	// Read and verify managed.json
	managedPath := filepath.Join(genDir, "managed.json")
	managedBytes, err := os.ReadFile(managedPath)
	if err != nil {
		return nil, fmt.Errorf("read managed.json: %w", err)
	}
	if expHash, ok := meta.Checksums["managed.json"]; ok {
		actHash := sha256.Sum256(managedBytes)
		if hex.EncodeToString(actHash[:]) != expHash {
			return nil, fmt.Errorf("checksum mismatch in managed.json for generation %s", generationID)
		}
	}

	var managed xrayconfig.ManagedConfig
	if err := json.Unmarshal(managedBytes, &managed); err != nil {
		return nil, fmt.Errorf("unmarshal managed.json: %w", err)
	}

	var rawOverlay []byte
	rawPath := filepath.Join(genDir, "raw.json")
	if fileExists(rawPath) {
		rawOverlay, _ = os.ReadFile(rawPath)
	}

	return &StoredProfile{
		Metadata:   meta,
		Managed:    &managed,
		RawOverlay: rawOverlay,
	}, nil
}

// ListProfiles lists active metadata for all existing profiles.
func (s *DiskProfileStore) ListProfiles(ctx context.Context) ([]GenerationMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.rootDir)
	if err != nil {
		return nil, fmt.Errorf("read profile root: %w", err)
	}

	var results []GenerationMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profileID := entry.Name()
		if err := validateID(profileID); err != nil {
			continue
		}

		sp, err := s.GetActiveProfile(ctx, profileID)
		if err != nil {
			continue
		}
		results = append(results, sp.Metadata)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].ProfileID < results[j].ProfileID
	})

	return results, nil
}

// ListGenerations lists all available generation snapshots for a profile.
func (s *DiskProfileStore) ListGenerations(ctx context.Context, profileID string) ([]GenerationMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := validateID(profileID); err != nil {
		return nil, fmt.Errorf("invalid profile ID: %w", err)
	}

	genBase := s.generationsDir(profileID)
	if !dirExists(genBase) {
		return nil, nil
	}

	entries, err := os.ReadDir(genBase)
	if err != nil {
		return nil, fmt.Errorf("read generations dir: %w", err)
	}

	var results []GenerationMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		genID := entry.Name()
		metaPath := filepath.Join(genBase, genID, "metadata.json")
		if !fileExists(metaPath) {
			continue
		}
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta GenerationMetadata
		if err := json.Unmarshal(metaBytes, &meta); err == nil {
			results = append(results, meta)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	return results, nil
}

// RollbackToGeneration switches the active generation pointer to target generation.
func (s *DiskProfileStore) RollbackToGeneration(ctx context.Context, profileID, generationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateID(profileID); err != nil {
		return fmt.Errorf("invalid profile ID: %w", err)
	}
	if err := validateID(generationID); err != nil {
		return fmt.Errorf("invalid generation ID: %w", err)
	}

	targetDir := s.generationPath(profileID, generationID)
	if !dirExists(targetDir) {
		return fmt.Errorf("target generation %s does not exist", generationID)
	}

	// Verify target generation integrity before switching pointer
	if _, err := s.getGenerationLocked(profileID, generationID); err != nil {
		return fmt.Errorf("target generation is corrupt: %w", err)
	}

	activePtr := ActivePointer{
		ActiveGenerationID: generationID,
		UpdatedAt:          time.Now().UTC(),
	}
	ptrBytes, err := json.MarshalIndent(activePtr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pointer: %w", err)
	}

	ptrPath := s.activePointerPath(profileID)
	if err := atomicWriteSecret(ptrPath, ptrBytes); err != nil {
		return fmt.Errorf("write active pointer: %w", err)
	}

	syncDir(s.profileDir(profileID))
	return nil
}

// DeleteProfile removes a profile and all its generations.
func (s *DiskProfileStore) DeleteProfile(ctx context.Context, profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateID(profileID); err != nil {
		return fmt.Errorf("invalid profile ID: %w", err)
	}

	pDir := s.profileDir(profileID)
	if dirExists(pDir) {
		return os.RemoveAll(pDir)
	}
	return nil
}

// RecoverIndex reconstructs the profile catalog by scanning generation metadata.
func (s *DiskProfileStore) RecoverIndex(ctx context.Context) ([]GenerationMetadata, error) {
	return s.ListProfiles(ctx)
}

func (s *DiskProfileStore) pruneGenerationsLocked(profileID, keepActiveID string) {
	genBase := s.generationsDir(profileID)
	entries, err := os.ReadDir(genBase)
	if err != nil || len(entries) <= s.maxRetention {
		return
	}

	type genEntry struct {
		name    string
		modTime time.Time
	}
	var list []genEntry
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keepActiveID {
			continue
		}
		info, err := e.Info()
		if err == nil {
			list = append(list, genEntry{name: e.Name(), modTime: info.ModTime()})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].modTime.Before(list[j].modTime) // oldest first
	})

	toDelete := len(entries) - s.maxRetention
	for i := 0; i < toDelete && i < len(list); i++ {
		_ = os.RemoveAll(filepath.Join(genBase, list[i].name))
	}
}

func writeAndSyncFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func syncDir(dirPath string) {
	if dir, err := os.Open(dirPath); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
}

