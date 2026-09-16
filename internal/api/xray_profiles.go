package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hoaxisr/awg-manager/internal/response"
	"github.com/hoaxisr/awg-manager/internal/xrayconfig"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
	"github.com/hoaxisr/awg-manager/internal/xrayserver/xraybin"
)

// ProfileSummaryDTO describes high-level profile metadata.
type ProfileSummaryDTO struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Role          xrayconfig.ProfileRole `json:"role"`
	Enabled       bool                   `json:"enabled"`
	SchemaVersion int                    `json:"schema_version"`
	CreatedAt     time.Time              `json:"created_at"`
	GenerationID  string                 `json:"generation_id"`
}

// ProfileDetailDTO includes full (safe/redacted) profile details.
type ProfileDetailDTO struct {
	ProfileSummaryDTO
	Config     *xrayconfig.ManagedConfig `json:"config"`
	RawOverlay string                    `json:"raw_overlay,omitempty"`
}

// CreateProfileRequest is the payload for creating a new profile.
type CreateProfileRequest struct {
	ID         string                    `json:"id,omitempty"`
	Name       string                    `json:"name"`
	Role       xrayconfig.ProfileRole    `json:"role,omitempty"`
	Enabled    bool                      `json:"enabled"`
	Config     *xrayconfig.ManagedConfig `json:"config"`
	RawOverlay string                    `json:"raw_overlay,omitempty"`
}

// UpdateProfileRequest is the payload for updating an existing profile.
type UpdateProfileRequest struct {
	Name       string                    `json:"name"`
	Role       xrayconfig.ProfileRole    `json:"role,omitempty"`
	Enabled    bool                      `json:"enabled"`
	Config     *xrayconfig.ManagedConfig `json:"config"`
	RawOverlay string                    `json:"raw_overlay,omitempty"`
}

// ImportProfileRequest is the payload for importing a raw Xray config JSON.
type ImportProfileRequest struct {
	Name    string                 `json:"name,omitempty"`
	Role    xrayconfig.ProfileRole `json:"role,omitempty"`
	Content string                 `json:"content"`
}

// RollbackRequest requests rolling back to a target generation.
type RollbackRequest struct {
	GenerationID string `json:"generation_id"`
}

// RecoveryResolveRequest requests resolving the recovery state.
type RecoveryResolveRequest struct {
	Strategy string `json:"strategy"` // "clear" or "reset"
}

func generateProfileID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("p-%d", time.Now().UnixNano()%0xffff)
	}
	return fmt.Sprintf("prof-%s", hex.EncodeToString(b))
}

// RouteProfiles dispatches requests for /api/servers/xray/profiles and subpaths.
func (h *XrayServerHandler) RouteProfiles(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/servers/xray/profiles")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		if r.Method == http.MethodGet {
			h.ListProfiles(w, r)
		} else if r.Method == http.MethodPost {
			h.CreateProfile(w, r)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED")
		}
		return
	}

	if path == "import" {
		if r.Method == http.MethodPost {
			h.ImportProfile(w, r)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED")
		}
		return
	}

	parts := strings.Split(path, "/")
	profileID := parts[0]

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.GetProfile(w, r, profileID)
		case http.MethodPut:
			h.UpdateProfile(w, r, profileID)
		case http.MethodDelete:
			h.DeleteProfile(w, r, profileID)
		default:
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED")
		}
		return
	}

	action := parts[1]
	switch action {
	case "export-private":
		if r.Method == http.MethodPost {
			h.ExportPrivateProfile(w, r, profileID)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "private export requires POST", "METHOD_NOT_ALLOWED")
		}
	case "export":
		if r.Method == http.MethodGet {
			h.ExportRedactedProfile(w, r, profileID)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "export requires GET", "METHOD_NOT_ALLOWED")
		}
	case "preview":
		if r.Method == http.MethodPost {
			h.PreviewProfileDiff(w, r, profileID)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "preview requires POST", "METHOD_NOT_ALLOWED")
		}
	case "apply":
		if r.Method == http.MethodPost {
			h.ApplyProfile(w, r, profileID)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "apply requires POST", "METHOD_NOT_ALLOWED")
		}
	case "generations":
		if r.Method == http.MethodGet {
			h.ListGenerations(w, r, profileID)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED")
		}
	case "rollback":
		if r.Method == http.MethodPost {
			h.RollbackProfile(w, r, profileID)
		} else {
			response.ErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED")
		}
	default:
		response.ErrorWithStatus(w, http.StatusNotFound, "profile endpoint not found", "NOT_FOUND")
	}
}

// ListProfiles returns all managed profiles metadata.
func (h *XrayServerHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	profiles, err := h.svc.Profiles().ListProfiles(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to list profiles: "+err.Error())
		return
	}

	dtos := make([]ProfileSummaryDTO, 0, len(profiles))
	for _, p := range profiles {
		dtos = append(dtos, ProfileSummaryDTO{
			ID:            p.ProfileID,
			Name:          p.ProfileName,
			Role:          p.Role,
			Enabled:       p.Enabled,
			SchemaVersion: p.SchemaVersion,
			CreatedAt:     p.CreatedAt,
			GenerationID:  p.GenerationID,
		})
	}

	WriteJSON(w, http.StatusOK, dtos)
}

// CreateProfile validates and saves a new managed profile.
func (h *XrayServerHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	var req CreateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Xray Profile"
	}

	if req.Config == nil {
		WriteError(w, http.StatusBadRequest, "config cannot be empty")
		return
	}

	// Validate configuration
	if h.svc.Validator() != nil {
		errs := h.svc.Validator().Validate(req.Config)
		if len(errs) > 0 {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("validation failed: %v", errs[0].Error()))
			return
		}
	}

	role := req.Role
	if role == "" {
		role = xrayconfig.RoleServer
	}

	profileID := strings.TrimSpace(req.ID)
	if profileID == "" {
		profileID = generateProfileID()
	}

	genID, err := h.svc.Profiles().SaveProfile(r.Context(), profileID, name, role, req.Enabled, req.Config, []byte(req.RawOverlay))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to save profile: "+err.Error())
		return
	}

	redactedConfig := req.Config
	if h.svc.Redactor() != nil {
		redactedConfig = h.svc.Redactor().RedactConfig(req.Config)
	}

	WriteJSON(w, http.StatusCreated, ProfileDetailDTO{
		ProfileSummaryDTO: ProfileSummaryDTO{
			ID:            profileID,
			Name:          name,
			Role:          role,
			Enabled:       req.Enabled,
			SchemaVersion: xrayconfig.CurrentSchemaVersion,
			CreatedAt:     time.Now().UTC(),
			GenerationID:  genID,
		},
		Config:     redactedConfig,
		RawOverlay: req.RawOverlay,
	})
}

// GetProfile returns detailed information for a single profile.
func (h *XrayServerHandler) GetProfile(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	stored, err := h.svc.Profiles().GetActiveProfile(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusNotFound, "profile not found: "+err.Error())
		return
	}

	redactedConfig := stored.Managed
	if h.svc.Redactor() != nil {
		redactedConfig = h.svc.Redactor().RedactConfig(stored.Managed)
	}

	WriteJSON(w, http.StatusOK, ProfileDetailDTO{
		ProfileSummaryDTO: ProfileSummaryDTO{
			ID:            stored.Metadata.ProfileID,
			Name:          stored.Metadata.ProfileName,
			Role:          stored.Metadata.Role,
			Enabled:       stored.Metadata.Enabled,
			SchemaVersion: stored.Metadata.SchemaVersion,
			CreatedAt:     stored.Metadata.CreatedAt,
			GenerationID:  stored.Metadata.GenerationID,
		},
		Config:     redactedConfig,
		RawOverlay: string(stored.RawOverlay),
	})
}

// UpdateProfile updates profile content and records a new generation.
func (h *XrayServerHandler) UpdateProfile(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Config == nil {
		WriteError(w, http.StatusBadRequest, "config cannot be empty")
		return
	}

	if h.svc.Validator() != nil {
		errs := h.svc.Validator().Validate(req.Config)
		if len(errs) > 0 {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("validation failed: %v", errs[0].Error()))
			return
		}
	}

	role := req.Role
	if role == "" {
		role = xrayconfig.RoleServer
	}

	genID, err := h.svc.Profiles().SaveProfile(r.Context(), id, req.Name, role, req.Enabled, req.Config, []byte(req.RawOverlay))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to update profile: "+err.Error())
		return
	}

	redactedConfig := req.Config
	if h.svc.Redactor() != nil {
		redactedConfig = h.svc.Redactor().RedactConfig(req.Config)
	}

	WriteJSON(w, http.StatusOK, ProfileDetailDTO{
		ProfileSummaryDTO: ProfileSummaryDTO{
			ID:            id,
			Name:          req.Name,
			Role:          role,
			Enabled:       req.Enabled,
			SchemaVersion: xrayconfig.CurrentSchemaVersion,
			CreatedAt:     time.Now().UTC(),
			GenerationID:  genID,
		},
		Config:     redactedConfig,
		RawOverlay: req.RawOverlay,
	})
}

// DeleteProfile deletes a profile and its generations.
func (h *XrayServerHandler) DeleteProfile(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	if err := h.svc.Profiles().DeleteProfile(r.Context(), id); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to delete profile: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ImportProfile parses raw Xray JSON and creates a managed profile.
func (h *XrayServerHandler) ImportProfile(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil || h.svc.Profiles() == nil || h.svc.Parser() == nil {
		WriteError(w, http.StatusServiceUnavailable, "services not available")
		return
	}

	var req ImportProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	rawBytes := []byte(strings.TrimSpace(req.Content))
	if len(rawBytes) == 0 {
		WriteError(w, http.StatusBadRequest, "content cannot be empty")
		return
	}

	doc, err := h.svc.Parser().ParseDocument(rawBytes)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "failed to parse Xray document: "+err.Error())
		return
	}

	managed, err := h.svc.Parser().DocumentToManaged(doc)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "failed to convert document: "+err.Error())
		return
	}

	if h.svc.Validator() != nil {
		errs := h.svc.Validator().Validate(managed)
		if len(errs) > 0 {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("imported config validation failed: %v", errs[0].Error()))
			return
		}
	}

	profileID := generateProfileID()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Imported Xray Profile"
	}
	role := req.Role
	if role == "" {
		role = xrayconfig.RoleServer
	}

	genID, err := h.svc.Profiles().SaveProfile(r.Context(), profileID, name, role, true, managed, rawBytes)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to save imported profile: "+err.Error())
		return
	}

	redactedConfig := managed
	if h.svc.Redactor() != nil {
		redactedConfig = h.svc.Redactor().RedactConfig(managed)
	}

	WriteJSON(w, http.StatusCreated, ProfileDetailDTO{
		ProfileSummaryDTO: ProfileSummaryDTO{
			ID:            profileID,
			Name:          name,
			Role:          role,
			Enabled:       true,
			SchemaVersion: xrayconfig.CurrentSchemaVersion,
			CreatedAt:     time.Now().UTC(),
			GenerationID:  genID,
		},
		Config:     redactedConfig,
		RawOverlay: string(rawBytes),
	})
}

// ExportRedactedProfile exports sanitized/masked configuration JSON.
func (h *XrayServerHandler) ExportRedactedProfile(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	stored, err := h.svc.Profiles().GetActiveProfile(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusNotFound, "profile not found: "+err.Error())
		return
	}

	redacted := stored.Managed
	if h.svc.Redactor() != nil {
		redacted = h.svc.Redactor().RedactConfig(stored.Managed)
	}

	compiler := h.svc.Compiler()
	if compiler == nil {
		compiler = xrayconfig.NewCompiler()
	}

	doc, err := compiler.Compile(redacted)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "compile failed: "+err.Error())
		return
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "marshal failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"xray-profile-%s-redacted.json\"", id))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ExportPrivateProfile exports full cleartext configuration via POST with strict no-store caching headers.
func (h *XrayServerHandler) ExportPrivateProfile(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	stored, err := h.svc.Profiles().GetActiveProfile(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusNotFound, "profile not found: "+err.Error())
		return
	}

	compiler := h.svc.Compiler()
	if compiler == nil {
		compiler = xrayconfig.NewCompiler()
	}

	doc, err := compiler.Compile(stored.Managed)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "compile failed: "+err.Error())
		return
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "marshal failed: "+err.Error())
		return
	}

	// Mandatory security headers per audit constraint 2:
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"xray-profile-%s.json\"", id))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// PreviewProfileDiff returns a semantic diff between the active runtime config and target profile.
func (h *XrayServerHandler) PreviewProfileDiff(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	stored, err := h.svc.Profiles().GetActiveProfile(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusNotFound, "profile not found: "+err.Error())
		return
	}

	// Get current active config as ManagedConfig
	activeConfig := xrayserver.LegacyToManagedConfig(h.svc.GetConfig())
	diff := xrayconfig.CalculateDiff(activeConfig, stored.Managed)

	WriteJSON(w, http.StatusOK, diff)
}

// ApplyProfile activates a managed profile through the coordinator transaction.
func (h *XrayServerHandler) ApplyProfile(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	stored, err := h.svc.Profiles().GetActiveProfile(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusNotFound, "profile not found: "+err.Error())
		return
	}

	// Compile candidate document
	compiler := h.svc.Compiler()
	if compiler == nil {
		compiler = xrayconfig.NewCompiler()
	}

	doc, err := compiler.Compile(stored.Managed)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "failed to compile profile: "+err.Error())
		return
	}

	candidateJSON, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to serialize candidate: "+err.Error())
		return
	}

	// Apply through transaction
	txID := fmt.Sprintf("apply-%s-%d", id, time.Now().UnixNano())
	cfg := h.svc.GetConfig()
	cfg.Enabled = stored.Metadata.Enabled

	// Update legacy listeners if inbound exists
	if len(stored.Managed.Inbounds) > 0 {
		cfg.ListenAddress = stored.Managed.Inbounds[0].Listen
		cfg.ListenPort = stored.Managed.Inbounds[0].Port
	}

	if h.coord != nil {
		if err := h.coord.ApplyXrayConfig(r.Context(), cfg); err != nil {
			WriteError(w, http.StatusInternalServerError, "failed to apply profile: "+err.Error())
			return
		}
	} else {
		// Prepare & commit directly if coordinator not attached
		preparedTxID, err := h.svc.PrepareCandidate(txID, cfg)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "failed to prepare candidate: "+err.Error())
			return
		}
		if err := h.svc.CommitPrepared(preparedTxID); err != nil {
			WriteError(w, http.StatusInternalServerError, "failed to commit candidate: "+err.Error())
			return
		}
		_ = h.svc.FinalizePrepared(preparedTxID)
	}

	_ = candidateJSON
	WriteJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"profile_id":    id,
		"generation_id": stored.Metadata.GenerationID,
	})
}

// ListGenerations lists all historical generations for a profile.
func (h *XrayServerHandler) ListGenerations(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	generations, err := h.svc.Profiles().ListGenerations(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to list generations: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, generations)
}

// RollbackProfile restores an earlier generation snapshot.
func (h *XrayServerHandler) RollbackProfile(w http.ResponseWriter, r *http.Request, id string) {
	if h.svc == nil || h.svc.Profiles() == nil {
		WriteError(w, http.StatusServiceUnavailable, "profile store not available")
		return
	}

	var req RollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if strings.TrimSpace(req.GenerationID) == "" {
		WriteError(w, http.StatusBadRequest, "generation_id is required")
		return
	}

	if err := h.svc.Profiles().RollbackToGeneration(r.Context(), id, req.GenerationID); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to rollback: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"generation_id": req.GenerationID,
	})
}

// GetCapabilities returns Xray binary capabilities.
func (h *XrayServerHandler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	binPath := "/opt/sbin/xray"
	if h.svc != nil && h.svc.BinPath() != "" {
		binPath = h.svc.BinPath()
	}

	caps, err := xraybin.InspectBinary(r.Context(), binPath)
	if err != nil {
		// Return safe default capabilities if binary not found/not executable
		caps = &xraybin.Capabilities{
			RawVersion:      "unknown",
			SupportsXHTTP:   true,
			SupportsReality: true,
			Protocols:       []string{"vless", "vmess", "trojan", "shadowsocks", "socks", "http", "freedom", "blackhole"},
			Transports:      []string{"tcp", "xhttp", "ws", "grpc", "httpupgrade"},
		}
	}

	WriteJSON(w, http.StatusOK, caps)
}

// ResolveRecovery allows administrator to clear or resolve recovery_required state.
func (h *XrayServerHandler) ResolveRecovery(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}

	var req RecoveryResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.svc.ClearRecoveryRequired()
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
