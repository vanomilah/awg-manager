package api

import (
	"encoding/json"
	"net/http"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/serveringress"
	"github.com/hoaxisr/awg-manager/internal/vlessserver"
)

// IngressDispatcherHandler manages CDN Ingress Dispatcher routes and XHTTP tuning profiles.
type IngressDispatcherHandler struct {
	coord    *serveringress.Coordinator
	disp     *cdndispatcher.Dispatcher
	vlessSvc *vlessserver.Service
}

// NewIngressDispatcherHandler creates a new IngressDispatcherHandler.
func NewIngressDispatcherHandler(coord *serveringress.Coordinator, disp *cdndispatcher.Dispatcher, vless *vlessserver.Service) *IngressDispatcherHandler {
	return &IngressDispatcherHandler{
		coord:    coord,
		disp:     disp,
		vlessSvc: vless,
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

// GetVlessTuning handles GET /api/servers/vless/tuning (and compatibility alias /api/servers/xray/tuning).
func (h *IngressDispatcherHandler) GetVlessTuning(w http.ResponseWriter, r *http.Request) {
	if h.vlessSvc == nil {
		WriteError(w, http.StatusServiceUnavailable, "vless server service not available")
		return
	}
	tuning := h.vlessSvc.GetTuning()
	WriteJSON(w, http.StatusOK, tuning)
}

// UpdateVlessTuning handles PUT /api/servers/vless/tuning (and compatibility alias /api/servers/xray/tuning).
func (h *IngressDispatcherHandler) UpdateVlessTuning(w http.ResponseWriter, r *http.Request) {
	if h.vlessSvc == nil {
		WriteError(w, http.StatusServiceUnavailable, "vless server service not available")
		return
	}
	var tuning vlessserver.XHTTPTuning
	if err := json.NewDecoder(r.Body).Decode(&tuning); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := h.vlessSvc.UpdateTuning(tuning); err != nil {
		WriteError(w, http.StatusBadRequest, "failed to update xhttp tuning: "+err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, h.vlessSvc.GetTuning())
}
