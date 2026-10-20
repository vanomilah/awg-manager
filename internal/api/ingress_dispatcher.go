package api

import (
	"encoding/json"
	"net/http"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/serveringress"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

// IngressDispatcherHandler manages CDN Ingress Dispatcher routes and XHTTP tuning profiles.
type IngressDispatcherHandler struct {
	coord   *serveringress.Coordinator
	disp    *cdndispatcher.Dispatcher
	xraySvc *xrayserver.Service
}

// NewIngressDispatcherHandler creates a new IngressDispatcherHandler.
func NewIngressDispatcherHandler(coord *serveringress.Coordinator, disp *cdndispatcher.Dispatcher, xray *xrayserver.Service) *IngressDispatcherHandler {
	return &IngressDispatcherHandler{
		coord:   coord,
		disp:    disp,
		xraySvc: xray,
	}
}

// GetDispatcherConfig handles GET /api/servers/ingress/dispatcher.
func (h *IngressDispatcherHandler) GetDispatcherConfig(w http.ResponseWriter, r *http.Request) {
	if h.coord == nil {
		WriteError(w, http.StatusServiceUnavailable, "ingress coordinator not configured")
		return
	}
	cfg := h.coord.GetDispatcherConfig()
	WriteJSON(w, http.StatusOK, cfg)
}

// UpdateDispatcherConfig handles PUT /api/servers/ingress/dispatcher.
func (h *IngressDispatcherHandler) UpdateDispatcherConfig(w http.ResponseWriter, r *http.Request) {
	if h.coord == nil {
		WriteError(w, http.StatusServiceUnavailable, "ingress coordinator not configured")
		return
	}
	var cfg cdndispatcher.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := h.coord.ApplyDispatcherConfig(r.Context(), cfg); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to apply dispatcher config: "+err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, h.coord.GetDispatcherConfig())
}

// GetXrayTuning handles GET /api/servers/xray/tuning.
func (h *IngressDispatcherHandler) GetXrayTuning(w http.ResponseWriter, r *http.Request) {
	if h.xraySvc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	tuning := h.xraySvc.GetTuning()
	WriteJSON(w, http.StatusOK, tuning)
}

// UpdateXrayTuning handles PUT /api/servers/xray/tuning.
func (h *IngressDispatcherHandler) UpdateXrayTuning(w http.ResponseWriter, r *http.Request) {
	if h.xraySvc == nil {
		WriteError(w, http.StatusServiceUnavailable, "xray service not available")
		return
	}
	var tuning xrayserver.XHTTPTuning
	if err := json.NewDecoder(r.Body).Decode(&tuning); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := h.xraySvc.UpdateTuning(tuning); err != nil {
		WriteError(w, http.StatusBadRequest, "failed to update xray tuning: "+err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, h.xraySvc.GetTuning())
}
