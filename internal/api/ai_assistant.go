package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/hoaxisr/awg-manager/internal/aiassistant"
	"github.com/hoaxisr/awg-manager/internal/downloader"
	"github.com/hoaxisr/awg-manager/internal/response"
)

type AIAssistantService interface {
	Start(question string) error
	Status() aiassistant.State
}

type AIAssistantHandler struct {
	service  AIAssistantService
	config   *aiassistant.ConfigStore
	embedded *aiassistant.EmbeddedManager
	routes   *downloader.Service
}

func NewAIAssistantHandler(service AIAssistantService) *AIAssistantHandler {
	return &AIAssistantHandler{service: service}
}

func (h *AIAssistantHandler) SetConfigStore(config *aiassistant.ConfigStore) { h.config = config }
func (h *AIAssistantHandler) SetEmbedded(embedded *aiassistant.EmbeddedManager) {
	h.embedded = embedded
}
func (h *AIAssistantHandler) SetRoutes(routes *downloader.Service) { h.routes = routes }

func (h *AIAssistantHandler) RegisterRoutes(mux *http.ServeMux, guarded func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/ai/diagnose", guarded(h.Diagnose))
	mux.HandleFunc("/api/ai/status", guarded(h.Status))
	mux.HandleFunc("/api/ai/action", guarded(h.ApplyAction))
	mux.HandleFunc("/api/ai/config", guarded(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.Config(w, r)
		} else {
			h.SaveConfig(w, r)
		}
	}))
	mux.HandleFunc("/api/ai/embedded", guarded(h.Embedded))
	mux.HandleFunc("/api/ai/embedded/stop", guarded(h.EmbeddedStop))
}

type AIAssistantConfigResponse struct {
	Success bool                          `json:"success" example:"true"`
	Data    aiassistant.PublicModelConfig `json:"data"`
}

type aiDiagnoseRequest struct {
	Question string `json:"question"`
}

type AIAssistantStateResponse struct {
	Success bool              `json:"success" example:"true"`
	Data    aiassistant.State `json:"data"`
}

type AIAssistantEmbeddedResponse struct {
	Success bool                       `json:"success" example:"true"`
	Data    aiassistant.EmbeddedStatus `json:"data"`
}

type aiActionRequest struct {
	ProposalID string `json:"proposalId"`
}

type aiActionApplier interface {
	ApplyAction(string) error
}

// Diagnose starts a read-only, sanitized system diagnostic run.
// POST /api/system/ai/diagnose
//
//	@Summary		Start safe AI diagnostics
//	@Description	Runs sanitized read-only diagnostics without applying changes
//	@Tags			system
//	@Accept			json
//	@Produce		json
//	@Security		CookieAuth
//	@Param			request	body		aiDiagnoseRequest	false	"Diagnostic question"
//	@Success		202		{object}	AIAssistantStateResponse
//	@Failure		400		{object}	APIErrorEnvelope
//	@Failure		409		{object}	APIErrorEnvelope
//	@Router			/system/ai/diagnose [post]
func (h *AIAssistantHandler) Diagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.MethodNotAllowed(w)
		return
	}
	if h.service == nil {
		response.InternalError(w, "AI assistant is unavailable")
		return
	}

	var req aiDiagnoseRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(w, "invalid request")
		return
	}
	if err := h.service.Start(req.Question); err != nil {
		if errors.Is(err, aiassistant.ErrRunning) {
			response.ErrorWithStatus(w, http.StatusConflict, err.Error(), "AI_DIAGNOSTICS_RUNNING")
			return
		}
		response.BadRequest(w, err.Error())
		return
	}
	response.Accepted(w, h.service.Status())
}

// Status returns the current or last assistant diagnostic result.
// GET /api/system/ai/status
//
//	@Summary		AI diagnostics status
//	@Tags			system
//	@Produce		json
//	@Security		CookieAuth
//	@Success		200	{object}	AIAssistantStateResponse
//	@Router			/system/ai/status [get]
func (h *AIAssistantHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	if h.service == nil {
		response.InternalError(w, "AI assistant is unavailable")
		return
	}
	response.Success(w, h.service.Status())
}

func (h *AIAssistantHandler) ApplyAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.MethodNotAllowed(w)
		return
	}
	applier, ok := h.service.(aiActionApplier)
	if !ok {
		response.InternalError(w, "AI remediation is unavailable")
		return
	}
	var req aiActionRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.ProposalID == "" {
		response.BadRequest(w, "invalid remediation request")
		return
	}
	if err := applier.ApplyAction(req.ProposalID); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	response.Success(w, h.service.Status())
}

// Config returns write-only model configuration metadata. The API key is
// represented only by apiKeySet and is never returned.
//
//	@Summary		AI model configuration
//	@Tags			system
//	@Produce		json
//	@Security		CookieAuth
//	@Success		200	{object}	AIAssistantConfigResponse
//	@Router			/system/ai/config [get]
func (h *AIAssistantHandler) Config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	if h.config == nil {
		response.InternalError(w, "AI model configuration is unavailable")
		return
	}
	response.Success(w, h.config.Public())
}

// SaveConfig persists model configuration in a dedicated 0600 file.
//
//	@Summary		Save AI model configuration
//	@Tags			system
//	@Accept			json
//	@Produce		json
//	@Security		CookieAuth
//	@Param			request	body		aiassistant.ConfigUpdate	true	"Model configuration"
//	@Success		200		{object}	AIAssistantConfigResponse
//	@Failure		400		{object}	APIErrorEnvelope
//	@Router			/system/ai/config [put]
func (h *AIAssistantHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		response.MethodNotAllowed(w)
		return
	}
	if h.config == nil {
		response.InternalError(w, "AI model configuration is unavailable")
		return
	}
	var req aiassistant.ConfigUpdate
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.BadRequest(w, "invalid request")
		return
	}
	if h.routes != nil && req.Provider != "local_embedded" {
		if _, err := h.routes.ValidateRoute(r.Context(), &downloader.Route{Tag: req.RouteTag, Kind: req.RouteKind}); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
	}
	public, err := h.config.Save(req)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	response.Success(w, public)
}

// Embedded returns status of the local embedded llama-server engine.
// GET /api/system/ai/embedded
func (h *AIAssistantHandler) Embedded(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	if h.embedded == nil {
		response.InternalError(w, "embedded AI manager is unavailable")
		return
	}
	response.Success(w, h.embedded.Status())
}

// EmbeddedStop stops the running llama-server process to free memory.
// POST /api/system/ai/embedded/stop
func (h *AIAssistantHandler) EmbeddedStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.MethodNotAllowed(w)
		return
	}
	if h.embedded == nil {
		response.InternalError(w, "embedded AI manager is unavailable")
		return
	}
	if err := h.embedded.Stop(); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	response.Success(w, h.embedded.Status())
}
