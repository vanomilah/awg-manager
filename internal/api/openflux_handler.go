package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/hoaxisr/awg-manager/internal/openflux"
	"github.com/hoaxisr/awg-manager/internal/response"
)

type OpenFluxHandler struct {
	service *openflux.Service
}

func NewOpenFluxHandler(service *openflux.Service) *OpenFluxHandler {
	return &OpenFluxHandler{service: service}
}

func (h *OpenFluxHandler) RegisterRoutes(mux *http.ServeMux, guarded func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/openflux/status", guarded(h.handleStatus))
	mux.HandleFunc("POST /api/openflux/config", guarded(h.handleSaveConfig))
	mux.HandleFunc("POST /api/openflux/start", guarded(h.handleStart))
	mux.HandleFunc("POST /api/openflux/stop", guarded(h.handleStop))
	mux.HandleFunc("POST /api/openflux/restart", guarded(h.handleRestart))
	mux.HandleFunc("POST /api/openflux/install", guarded(h.handleInstall))
	mux.HandleFunc("GET /api/openflux/link", guarded(h.handleGetLink))
	mux.HandleFunc("GET /api/openflux/logs", guarded(h.handleGetLogs))

	mux.HandleFunc("GET /api/openflux/client/status", guarded(h.handleClientStatus))
	mux.HandleFunc("POST /api/openflux/client/config", guarded(h.handleSaveClientConfig))
	mux.HandleFunc("POST /api/openflux/client/start", guarded(h.handleClientStart))
	mux.HandleFunc("POST /api/openflux/client/stop", guarded(h.handleClientStop))
	mux.HandleFunc("POST /api/openflux/client/restart", guarded(h.handleClientRestart))
	mux.HandleFunc("POST /api/openflux/client/parse-link", guarded(h.handleParseLink))
	mux.HandleFunc("GET /api/openflux/client/logs", guarded(h.handleGetClientLogs))
}

func (h *OpenFluxHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	var req openflux.Config
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if err := h.service.SaveConfig(r.Context(), req); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, err.Error(), "SAVE_CONFIG_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	if err := h.service.Start(r.Context()); err != nil {
		response.ErrorWithStatus(w, http.StatusInternalServerError, err.Error(), "START_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleStop(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	if err := h.service.Stop(r.Context()); err != nil {
		response.ErrorWithStatus(w, http.StatusInternalServerError, err.Error(), "STOP_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleRestart(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	if err := h.service.Restart(r.Context()); err != nil {
		response.ErrorWithStatus(w, http.StatusInternalServerError, err.Error(), "RESTART_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	if err := h.service.Install(r.Context()); err != nil {
		response.ErrorWithStatus(w, http.StatusInternalServerError, err.Error(), "INSTALL_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleGetLink(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, map[string]string{
		"link": status.Link,
	})
}

func (h *OpenFluxHandler) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	lines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			lines = parsed
		}
	}
	logTail := h.service.GetLogTail(lines)
	response.Success(w, map[string]string{
		"logs": logTail,
	})
}

func (h *OpenFluxHandler) handleClientStatus(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	status := h.service.GetClientStatus()
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleSaveClientConfig(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	var req openflux.ClientConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if err := h.service.SaveClientConfig(r.Context(), req); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, err.Error(), "SAVE_CLIENT_CONFIG_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleClientStart(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	if err := h.service.StartClient(r.Context()); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, err.Error(), "CLIENT_START_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleClientStop(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	if err := h.service.StopClient(r.Context()); err != nil {
		response.ErrorWithStatus(w, http.StatusInternalServerError, err.Error(), "CLIENT_STOP_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleClientRestart(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	if err := h.service.RestartClient(r.Context()); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, err.Error(), "CLIENT_RESTART_FAILED")
		return
	}
	status := h.service.GetStatus(r.Context())
	response.Success(w, status)
}

func (h *OpenFluxHandler) handleParseLink(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	var req struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	share, cfg, err := h.service.ParseClientLink(req.Link)
	if err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, err.Error(), "PARSE_LINK_FAILED")
		return
	}
	response.Success(w, map[string]interface{}{
		"share":  share,
		"config": cfg,
	})
}

func (h *OpenFluxHandler) handleGetClientLogs(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.ErrorWithStatus(w, http.StatusServiceUnavailable, "openflux service not initialized", "UNAVAILABLE")
		return
	}
	lines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			lines = parsed
		}
	}
	logTail := h.service.GetClientLogTail(lines)
	response.Success(w, map[string]string{
		"logs": logTail,
	})
}

