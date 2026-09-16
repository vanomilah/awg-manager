package api

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/hoaxisr/awg-manager/internal/response"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
	"github.com/hoaxisr/awg-manager/internal/xrayserver/xraybin"
)

// XrayStatus represents the runtime and configuration state of Xray CDN Bridge
type XrayStatus struct {
	Installed           bool                `json:"installed"`
	Running             bool                `json:"running"`
	PID                 int                 `json:"pid,omitempty"`
	Version             string              `json:"version,omitempty"`
	Port                int                 `json:"port,omitempty"`
	Path                string              `json:"path,omitempty"`
	UUID                string              `json:"uuid,omitempty"`
	UplinkHTTPMethod    string              `json:"uplinkHTTPMethod,omitempty"`
	CDNHost             string              `json:"cdnHost,omitempty"`
	ServerIP            string              `json:"serverIP,omitempty"`
	Link                string              `json:"link,omitempty"`
	Source              string              `json:"source,omitempty"`
	CanUninstall        bool                `json:"canUninstall"`
	Blockers            []string            `json:"blockers,omitempty"`
	Error               string              `json:"error,omitempty"`
	ClientsCount        int                 `json:"clients_count,omitempty"`
	Clients             []xrayserver.Client `json:"clients,omitempty"`
	RecoveryRequired    bool                `json:"recovery_required,omitempty"`
	RecoveryReason      string              `json:"recovery_reason,omitempty"`
	RecoveryFingerprint string              `json:"recovery_fingerprint,omitempty"`
}

type XrayHandler struct {
	svc      *xrayserver.Service
	resolver *xraybin.Resolver
}

func NewXrayHandler(svc ...*xrayserver.Service) *XrayHandler {
	var s *xrayserver.Service
	if len(svc) > 0 {
		s = svc[0]
	}
	return &XrayHandler{
		svc:      s,
		resolver: xraybin.NewResolver("/opt/etc/awg-manager"),
	}
}

func (h *XrayHandler) SetResolver(r *xraybin.Resolver) {
	h.resolver = r
}

func (h *XrayHandler) SetService(svc *xrayserver.Service) {
	h.svc = svc
}

func (h *XrayHandler) RegisterRoutes(mux *http.ServeMux, guarded func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/xray/status", guarded(h.handleStatus))
	mux.HandleFunc("POST /api/xray/install", guarded(h.handleInstall))
	mux.HandleFunc("POST /api/xray/uninstall", guarded(h.handleUninstall))
}

func (h *XrayHandler) GetStatus(ctx context.Context) XrayStatus {
	st := XrayStatus{}

	var bin xraybin.ResolvedBinary
	if h.resolver != nil {
		bin = h.resolver.Resolve(ctx)
	} else {
		bin = xraybin.Resolve()
	}

	st.Installed = bin.Installed
	st.Version = bin.Version
	st.Source = string(bin.Source)
	st.CanUninstall = bin.CanUninstall
	st.Blockers = bin.Blockers

	if h.svc != nil {
		srvSt := h.svc.GetStatus()
		st.Running = srvSt.Running
		st.PID = srvSt.PID
		st.Port = srvSt.Port
		st.RecoveryRequired = srvSt.RecoveryRequired
		st.RecoveryReason = srvSt.RecoveryReason
		st.RecoveryFingerprint = srvSt.RecoveryFingerprint
		st.ClientsCount = srvSt.ClientsCount
		st.Clients = srvSt.Clients

		cfg := h.svc.GetConfig()
		st.Path = cfg.Path
		st.UplinkHTTPMethod = cfg.UplinkMethod
		st.CDNHost = cfg.PublicDomain
		if len(cfg.Clients) > 0 {
			st.UUID = cfg.Clients[0].ID
			if links, err := h.svc.GenerateLinks(cfg.Clients[0].ID); err == nil && links != nil {
				st.Link = links.VlessURL
			}
		}
	}

	return st
}

func (h *XrayHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	status := h.GetStatus(ctx)
	response.Success(w, status)
}

func (h *XrayHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Only install the xray-core binary package.
	// Process lifecycle and configuration are managed safely via xrayserver.Service and coordinator,
	// NOT through legacy init scripts or ad-hoc config files.
	cmd := exec.CommandContext(ctx, "/opt/bin/opkg", "install", "xray-core")
	if out, err := cmd.CombinedOutput(); err != nil {
		response.InternalError(w, fmt.Sprintf("opkg install failed: %s (%v)", string(out), err))
		return
	}

	response.Success(w, h.GetStatus(ctx))
}

func (h *XrayHandler) handleUninstall(w http.ResponseWriter, r *http.Request) {
	var bin xraybin.ResolvedBinary
	if h.resolver != nil {
		bin = h.resolver.Resolve(r.Context())
	} else {
		bin = xraybin.Resolve()
	}

	if !bin.CanUninstall {
		msg := "Cannot uninstall Xray"
		if len(bin.Blockers) > 0 {
			msg += ": " + strings.Join(bin.Blockers, "; ")
		}
		response.ErrorWithStatus(w, http.StatusConflict, msg, "CONFLICT")
		return
	}

	if err := xraybin.Uninstall(bin); err != nil {
		response.ErrorWithStatus(w, http.StatusInternalServerError, "Failed to uninstall Xray: "+err.Error(), "UNINSTALL_FAILED")
		return
	}

	response.Success(w, map[string]bool{"success": true})
}
