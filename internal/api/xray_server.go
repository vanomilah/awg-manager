package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/response"
	"github.com/hoaxisr/awg-manager/internal/serveringress"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

type XrayServerHandler struct {
	svc   *xrayserver.Service
	coord *serveringress.Coordinator
}

func NewXrayServerHandler(svc *xrayserver.Service, coord *serveringress.Coordinator) *XrayServerHandler {
	return &XrayServerHandler{svc: svc, coord: coord}
}

func (h *XrayServerHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	cfg := h.svc.GetConfig()
	WriteJSON(w, http.StatusOK, cfg)
}

func (h *XrayServerHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	var cfg xrayserver.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if h.coord == nil {
		WriteError(w, http.StatusServiceUnavailable, "server ingress coordinator not configured")
		return
	}
	if err := h.coord.ApplyXrayConfig(r.Context(), cfg); err != nil {
		if errors.Is(err, serveringress.ErrRecoveryRequired) {
			WriteError(w, http.StatusServiceUnavailable, "system in recovery_required state: manual intervention needed")
			return
		}
		if errors.Is(err, serveringress.ErrLocked) {
			WriteError(w, http.StatusConflict, "operation already in progress by another coordinator")
			return
		}
		WriteError(w, http.StatusInternalServerError, "failed to apply config: "+err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, h.svc.GetConfig())
}

func (h *XrayServerHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	status := h.svc.GetStatus()
	WriteJSON(w, http.StatusOK, status)
}

func (h *XrayServerHandler) Action(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	var req struct {
		Action string `json:"action"` // "start", "stop", "restart"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if h.coord == nil {
		WriteError(w, http.StatusServiceUnavailable, "server ingress coordinator not configured")
		return
	}

	var err error
	switch req.Action {
	case "start":
		err = h.coord.SetXrayEnabled(r.Context(), true)
	case "stop":
		err = h.coord.SetXrayEnabled(r.Context(), false)
	case "restart":
		err = h.coord.RestartXray(r.Context())
	default:
		WriteError(w, http.StatusBadRequest, "unknown action: "+req.Action)
		return
	}

	if err != nil {
		if errors.Is(err, serveringress.ErrRecoveryRequired) {
			WriteError(w, http.StatusServiceUnavailable, "system in recovery_required state: manual intervention needed")
			return
		}
		if errors.Is(err, serveringress.ErrLocked) {
			WriteError(w, http.StatusConflict, "operation already in progress")
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, h.svc.GetStatus())
}

func (h *XrayServerHandler) AddClient(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	var req struct {
		Remark string `json:"remark"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	client, err := h.svc.AddClient(req.Remark)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusCreated, client)
}

func (h *XrayServerHandler) DeleteClient(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/servers/xray/clients/")
	id = strings.TrimSuffix(id, "/")
	if id == "" {
		WriteError(w, http.StatusBadRequest, "missing client id")
		return
	}
	if err := h.svc.DeleteClient(id); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *XrayServerHandler) ToggleClient(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 5 {
			id = parts[4]
		}
	}
	if id == "" {
		WriteError(w, http.StatusBadRequest, "missing client id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.ToggleClient(id, req.Enabled); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *XrayServerHandler) GetLinks(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 5 {
			id = parts[4]
		}
	}
	if id == "" {
		WriteError(w, http.StatusBadRequest, "missing client id")
		return
	}
	links, err := h.svc.GenerateLinks(id)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, links)
}

func (h *XrayServerHandler) GetMigrationStatus(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}

	legacyCfg := "/opt/etc/xray-cdn/config.json"
	legacyInit := "/opt/etc/init.d/S99xray-cdn"
	disc, err := xrayserver.DiscoverLegacy(legacyCfg, legacyInit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	decision, _ := xrayserver.GetRuntimeDecision(h.svc.DataDir())

	WriteJSON(w, http.StatusOK, map[string]any{
		"discovery": disc,
		"decision":  decision,
	})
}

func (h *XrayServerHandler) ResolveConflict(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}

	var req struct {
		Action string `json:"action"` // "keep_new", "keep_legacy", "import_legacy_draft"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	legacyCfg := "/opt/etc/xray-cdn/config.json"
	legacyInit := "/opt/etc/init.d/S99xray-cdn"

	if h.coord == nil {
		WriteError(w, http.StatusServiceUnavailable, "server ingress coordinator not configured")
		return
	}

	var legacyDraft *xrayserver.Config
	if req.Action == "import_legacy_draft" {
		disc, err := xrayserver.DiscoverLegacy(legacyCfg, legacyInit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if disc == nil || !disc.Found {
			WriteError(w, http.StatusNotFound, "legacy configuration not found")
			return
		}
		if disc.Topology == xrayserver.TopologyC {
			WriteError(w, http.StatusConflict, "legacy topology is ambiguous or conflicting: "+disc.ConflictReason)
			return
		}

		curCfg := h.svc.GetConfig()
		if len(disc.Clients) > 0 {
			curCfg.Clients = disc.Clients
		}
		if disc.PublicDomain != "" {
			curCfg.PublicDomain = disc.PublicDomain
		}
		if disc.Path != "" {
			curCfg.Path = disc.Path
		}
		legacyDraft = &curCfg
	}

	if err := h.coord.ResolveMigration(r.Context(), req.Action, legacyDraft); err != nil {
		if errors.Is(err, serveringress.ErrRecoveryRequired) {
			WriteError(w, http.StatusServiceUnavailable, "system in recovery_required state")
			return
		}
		if errors.Is(err, serveringress.ErrLocked) {
			WriteError(w, http.StatusConflict, "operation already in progress")
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	dec, _ := xrayserver.GetRuntimeDecision(h.svc.DataDir())
	WriteJSON(w, http.StatusOK, map[string]any{"success": true, "decision": dec, "config": h.svc.GetConfig()})
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	if status == http.StatusCreated {
		response.Created(w, data)
		return
	}
	response.Success(w, data)
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	response.ErrorWithStatus(w, status, msg, "SERVER_ERROR")
}
