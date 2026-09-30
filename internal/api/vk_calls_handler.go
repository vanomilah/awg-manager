package api

import (
	"encoding/json"
	"net/http"

	"github.com/hoaxisr/awg-manager/internal/proxyapp/vkcalls"
	"github.com/hoaxisr/awg-manager/internal/response"
)

// VKCallsHandler exposes HTTP endpoints for VK Calls management.
type VKCallsHandler struct {
	svc *vkcalls.Service
}

// NewVKCallsHandler creates a new VK Calls HTTP handler.
func NewVKCallsHandler(svc *vkcalls.Service) *VKCallsHandler {
	return &VKCallsHandler{svc: svc}
}

// Generate handles POST /api/proxyrt/vk/calls/generate.
func (h *VKCallsHandler) Generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.MethodNotAllowed(w)
		return
	}

	var req vkcalls.GenerateRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "невалидный JSON: "+err.Error())
			return
		}
	}

	res, err := h.svc.Generate(r.Context(), req)
	if err != nil {
		response.ErrorWithStatus(w, http.StatusUnprocessableEntity, err.Error(), "VK_GENERATE_FAILED")
		return
	}

	response.Success(w, res)
}

// Check handles POST /api/proxyrt/vk/calls/check.
func (h *VKCallsHandler) Check(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.MethodNotAllowed(w)
		return
	}

	var req vkcalls.CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "невалидный JSON: "+err.Error())
		return
	}

	res, err := h.svc.Check(r.Context(), req)
	if err != nil {
		response.ErrorWithStatus(w, http.StatusUnprocessableEntity, err.Error(), "VK_CHECK_FAILED")
		return
	}

	response.Success(w, res)
}

// Config handles GET and POST /api/proxyrt/vk/calls/config.
func (h *VKCallsHandler) Config(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.Success(w, h.svc.GetConfig())
	case http.MethodPost:
		var req struct {
			Token   string `json:"token"`
			GroupID int64  `json:"groupId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "невалидный JSON: "+err.Error())
			return
		}
		if err := h.svc.SaveConfig(req.Token, req.GroupID); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
		response.Success(w, h.svc.GetConfig())
	default:
		response.MethodNotAllowed(w)
	}
}
