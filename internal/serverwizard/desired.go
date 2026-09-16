package serverwizard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/serverwizard/egress"
)

var (
	hostnameRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
)

// DesiredWizardConfig is the validated, normalized desired configuration before planning and execution.
// It MUST NOT store secrets (e.g. Telegram secret, Xray client UUID).
type DesiredWizardConfig struct {
	ServerKind        string `json:"server_kind"`
	Scenario          string `json:"scenario,omitempty"`
	DirectHost        string `json:"direct_host,omitempty"`
	DirectPort        int    `json:"direct_port,omitempty"`
	TlsDomain         string `json:"tls_domain,omitempty"`
	ListenPort        int    `json:"listen_port,omitempty"`
	PublicHostname    string `json:"public_hostname,omitempty"`
	PublicPort        int    `json:"public_port,omitempty"`
	Path              string `json:"path,omitempty"`
	DispatcherPort    int    `json:"dispatcher_port,omitempty"`
	CarrierMode       string `json:"carrier_mode,omitempty"`
	UpstreamDevice    string `json:"upstream_device,omitempty"`
	CDNProfileID      string `json:"cdn_profile_id,omitempty"`
	Transport         string `json:"transport,omitempty"`
	Mode              string `json:"mode,omitempty"`
	UplinkMethod      string `json:"uplink_method,omitempty"`
	OutboundMode      string `json:"outbound_mode,omitempty"`
	OutboundInterface string `json:"outbound_interface,omitempty"`
	OutboundSocksPort int    `json:"outbound_socks_port,omitempty"`
	ClientRemark      string `json:"client_remark,omitempty"`
}

// ValidatePublicHostname validates that a public CDN hostname is a valid FQDN and not an IP address.
func ValidatePublicHostname(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("public hostname cannot be empty")
	}
	if net.ParseIP(host) != nil {
		return errors.New("public hostname must be a valid domain name, not an IP address")
	}
	if !hostnameRegex.MatchString(host) {
		return fmt.Errorf("invalid public hostname format: %s", host)
	}
	return nil
}

// ValidateDirectHost validates a direct host, which may be a valid domain or IP address.
func ValidateDirectHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("direct host cannot be empty")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if !hostnameRegex.MatchString(host) {
		return fmt.Errorf("invalid direct host format: %s", host)
	}
	return nil
}

// ValidateTlsDomain validates a Fake-TLS SNI domain.
func ValidateTlsDomain(domain string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return errors.New("TLS domain cannot be empty")
	}
	if net.ParseIP(domain) != nil {
		return errors.New("TLS domain must be a valid domain name, not an IP address")
	}
	if !hostnameRegex.MatchString(domain) {
		return fmt.Errorf("invalid TLS domain format: %s", domain)
	}
	return nil
}

// BuildDesiredConfig validates and normalizes a WizardPlanRequest using the typed egress resolver.
// Egress direct materializes as "" (no nwg1 hardcoding).
// Strict CDN matrix: cdn_get + xhttp_get, cdn_ws + ws, cdn_full + xhttp_get/ws.
// Mandatory TlsDomain for direct/dual without fallback to req.Mode.
func BuildDesiredConfig(ctx context.Context, egressResolver egress.Resolver, req WizardPlanRequest) (DesiredWizardConfig, error) {
	desired := DesiredWizardConfig{
		ServerKind: req.Kind,
	}

	switch req.Kind {
	case "tgwebproxy":
		scenario := req.Scenario
		if scenario == "" {
			scenario = "dual"
		}
		switch scenario {
		case "direct_fake_tls", "cdn_http", "dual":
		default:
			return desired, fmt.Errorf("invalid telegram scenario: %s", scenario)
		}
		desired.Scenario = scenario

		desired.DirectPort = req.DirectPort
		if desired.DirectPort <= 0 {
			desired.DirectPort = 8443
		}

		desired.ListenPort = req.ListenPort
		if desired.ListenPort <= 0 {
			desired.ListenPort = 8085
		}

		desired.DirectHost = strings.TrimSpace(req.DirectHost)
		if scenario == "direct_fake_tls" || scenario == "dual" {
			if desired.DirectHost == "" {
				return desired, errors.New("direct host is required for direct or dual scenario")
			}
			if err := ValidateDirectHost(desired.DirectHost); err != nil {
				return desired, err
			}
		}

		desired.PublicHostname = strings.TrimSpace(req.PublicDomain)
		if scenario == "cdn_http" || scenario == "dual" {
			if desired.PublicHostname == "" {
				return desired, errors.New("public domain is required for CDN or dual scenario")
			}
			if err := ValidatePublicHostname(desired.PublicHostname); err != nil {
				return desired, err
			}
		}

		if scenario == "direct_fake_tls" || scenario == "dual" {
			tlsDomain := strings.TrimSpace(req.TlsDomain)
			if tlsDomain == "" {
				return desired, errors.New("TLS domain is required for direct or dual Telegram scenario")
			}
			if err := ValidateTlsDomain(tlsDomain); err != nil {
				return desired, err
			}
			desired.TlsDomain = tlsDomain
		} else {
			desired.TlsDomain = ""
		}

		desired.PublicPort = req.PublicPort
		if desired.PublicPort <= 0 {
			desired.PublicPort = 443
		}
		desired.DispatcherPort = req.DispatcherPort
		if desired.DispatcherPort <= 0 {
			desired.DispatcherPort = 9009
		}

		// Resolve egress via typed resolver
		desired.CarrierMode = "get"
		if egressResolver != nil {
			res, err := egressResolver.Resolve(ctx, egress.ResolveRequest{
				ServerKind: "tgwebproxy",
				EgressID:   req.UpstreamDevice,
			})
			if err != nil {
				return desired, fmt.Errorf("resolve telegram egress: %w", err)
			}
			switch res.Mode {
			case "direct":
				desired.OutboundMode = "direct"
				desired.OutboundInterface = ""
				desired.UpstreamDevice = ""
			case "interface":
				desired.OutboundMode = "interface"
				desired.OutboundInterface = res.Interface
				desired.UpstreamDevice = res.Interface
			default:
				return desired, fmt.Errorf("unsupported egress mode for telegram: %s", res.Mode)
			}
		} else {
			dev := strings.TrimSpace(req.UpstreamDevice)
			if dev == "" || dev == "direct" {
				desired.OutboundMode = "direct"
				desired.OutboundInterface = ""
				desired.UpstreamDevice = ""
			} else {
				desired.OutboundMode = "interface"
				desired.OutboundInterface = dev
				desired.UpstreamDevice = dev
			}
		}

	case "xray":
		desired.PublicHostname = strings.TrimSpace(req.PublicDomain)
		if desired.PublicHostname == "" {
			return desired, errors.New("public domain is required for Xray CDN server")
		}
		if err := ValidatePublicHostname(desired.PublicHostname); err != nil {
			return desired, err
		}

		desired.ListenPort = req.ListenPort
		if desired.ListenPort <= 0 {
			desired.ListenPort = 9008
		}

		desired.PublicPort = req.PublicPort
		if desired.PublicPort <= 0 {
			desired.PublicPort = 443
		}
		desired.Path = cdndispatcher.NormalizePathPrefix(req.Path)

		desired.DispatcherPort = req.DispatcherPort
		if desired.DispatcherPort <= 0 {
			desired.DispatcherPort = 9009
		}

		desired.ClientRemark = strings.TrimSpace(req.ClientRemark)
		if desired.ClientRemark == "" {
			desired.ClientRemark = "Client"
		}

		profileID := req.CdnProfileID
		if profileID == "" {
			profileID = "cdn_get"
		}
		desired.CDNProfileID = profileID

		// Strict matrix:
		// cdn_get  + xhttp_get
		// cdn_ws   + ws
		// cdn_full + xhttp_get or ws
		switch profileID {
		case "cdn_get":
			if req.Mode != "" && req.Mode != "xhttp_get" {
				return desired, fmt.Errorf("invalid mode %q for CDN profile %s (must be xhttp_get)", req.Mode, profileID)
			}
			desired.Transport = "xhttp"
			desired.Mode = "packet-up"
			desired.UplinkMethod = "GET"
		case "cdn_ws":
			if req.Mode != "" && req.Mode != "ws" {
				return desired, fmt.Errorf("invalid mode %q for CDN profile %s (must be ws)", req.Mode, profileID)
			}
			desired.Transport = "ws"
			desired.Mode = ""
			desired.UplinkMethod = ""
		case "cdn_full":
			mode := req.Mode
			if mode == "" {
				mode = "xhttp_get"
			}
			switch mode {
			case "xhttp_get":
				desired.Transport = "xhttp"
				desired.Mode = "packet-up"
				desired.UplinkMethod = "GET"
			case "ws":
				desired.Transport = "ws"
				desired.Mode = ""
				desired.UplinkMethod = ""
			default:
				return desired, fmt.Errorf("invalid mode %q for CDN profile %s (must be xhttp_get or ws)", mode, profileID)
			}
		default:
			return desired, fmt.Errorf("unsupported CDN profile: %s (allowed: cdn_get, cdn_ws, cdn_full)", profileID)
		}

		// Resolve egress via typed resolver
		if egressResolver != nil {
			res, err := egressResolver.Resolve(ctx, egress.ResolveRequest{
				ServerKind: "xray",
				EgressID:   req.UpstreamDevice,
			})
			if err != nil {
				return desired, fmt.Errorf("resolve xray egress: %w", err)
			}
			switch res.Mode {
			case "direct":
				desired.OutboundMode = "direct"
				desired.OutboundInterface = ""
				desired.OutboundSocksPort = 0
				desired.UpstreamDevice = ""
			case "socks":
				desired.OutboundMode = "socks"
				desired.OutboundSocksPort = res.SocksPort
				desired.OutboundInterface = ""
				desired.UpstreamDevice = ""
			case "interface":
				desired.OutboundMode = "interface"
				desired.OutboundInterface = res.Interface
				desired.OutboundSocksPort = 0
				desired.UpstreamDevice = res.Interface
			default:
				return desired, fmt.Errorf("unsupported egress mode for xray: %s", res.Mode)
			}
		} else {
			dev := strings.TrimSpace(req.UpstreamDevice)
			if dev == "" || dev == "direct" {
				desired.OutboundMode = "direct"
				desired.OutboundInterface = ""
				desired.OutboundSocksPort = 0
				desired.UpstreamDevice = ""
			} else if dev == "mihomo:1099" || dev == "socks" {
				desired.OutboundMode = "socks"
				desired.OutboundSocksPort = 1099
				desired.OutboundInterface = ""
				desired.UpstreamDevice = ""
			} else {
				desired.OutboundMode = "interface"
				desired.OutboundInterface = dev
				desired.OutboundSocksPort = 0
				desired.UpstreamDevice = dev
			}
		}

	default:
		return desired, fmt.Errorf("unsupported server kind: %s", req.Kind)
	}

	return desired, nil
}

// NormalizeDesiredConfig is a convenience wrapper for backwards compatibility without resolver.
func NormalizeDesiredConfig(req WizardPlanRequest) (DesiredWizardConfig, error) {
	return BuildDesiredConfig(context.Background(), nil, req)
}
