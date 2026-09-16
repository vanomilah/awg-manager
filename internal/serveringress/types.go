package serveringress

import (
	"context"
	"errors"
)

var (
	ErrRecoveryRequired = errors.New("recovery required: coordinator has pending recovery or conflict")
	ErrCorruptJournal   = errors.New("corrupt transaction journal")
	ErrRecoveryConflict = errors.New("recovery conflict: component state diverges from committed transaction")
	ErrLocked           = ErrLockConflict
	ErrPlanStale        = errors.New("plan is stale: system state has changed")
	ErrPointOfNoReturn  = errors.New("point of no return: transaction cannot be cancelled during finalize")
)

type StateFingerprintFunc func(ctx context.Context) (string, error)
type ReadinessProbeFunc func(ctx context.Context, topology IngressTopology) error

type IngressTelegramCandidate struct {
	Enabled        bool
	Scenario       string
	DirectHost     string
	DirectPort     int
	TlsDomain      string
	ListenPort     int
	BackendPort    int
	AdminPort      int
	PublicHostname string
	CarrierMode    string
	UpstreamDevice string
	Secret         string `json:"-"` // MUST NOT be serialized into journals, plans, or logs
}

type IngressXrayCandidate struct {
	Enabled           bool
	ListenAddress     string
	ListenPort        int
	PublicDomain      string
	PublicPort        int
	Path              string
	Transport         string // "xhttp", "ws"
	Mode              string // "packet-up"
	UplinkMethod      string // "GET"
	OutboundMode      string // "direct", "socks", "interface"
	OutboundInterface string
	OutboundSocksPort int
	ClientRemark      string
	ClientUUID        string `json:"-"` // MUST NOT be serialized into journals, plans, or logs
}

type IngressTransactionParams struct {
	TxID                string
	ExpectedFingerprint string
	FingerprintFunc     StateFingerprintFunc
	ServerKind          string // "tgwebproxy" or "xray"
	Telegram            *IngressTelegramCandidate
	Xray                *IngressXrayCandidate
	ReadinessProbe      ReadinessProbeFunc
	OnPhaseChange       func(phase JournalPhase) error
	OnPointOfNoReturn   func(txID string) error
}

// IngressTopology describes the desired or current cross-component ingress configuration.
type IngressTopology struct {
	DispatcherEnabled  bool   `json:"dispatcher_enabled"`
	DispatcherAddress  string `json:"dispatcher_address,omitempty"`
	DispatcherPort     int    `json:"dispatcher_port"` // Origin proxy port (e.g. 9009)

	XrayEnabled        bool   `json:"xray_enabled"`
	XrayAddress        string `json:"xray_address"`     // Loopback or listen address (e.g. "127.0.0.1")
	XrayPort           int    `json:"xray_port"`        // Dedicated port for Xray (e.g. 9008)
	XrayPublicHostname string `json:"xray_public_hostname,omitempty"`
	XrayPublicPort     int    `json:"xray_public_port,omitempty"`
	XrayPathPrefix     string `json:"xray_path_prefix"` // WebSocket prefix (e.g. "/cdn-bridge")

	TgEnabled          bool   `json:"tg_enabled"`
	TgScenario         string `json:"tg_scenario,omitempty"`
	TgDirectAddress    string `json:"tg_direct_address,omitempty"`
	TgDirectPort       int    `json:"tg_direct_port,omitempty"`
	TgRawAddress       string `json:"tg_raw_address,omitempty"`
	TgRawPort          int    `json:"tg_raw_port,omitempty"`
	TgWebAddress       string `json:"tg_web_address,omitempty"`
	TgWebPort          int    `json:"tg_web_port,omitempty"`
	TgPublicHostname   string `json:"tg_public_hostname,omitempty"`

	// Backward compatibility fields
	PublicHostname     string `json:"public_hostname,omitempty"` // Domain for CDN ingress
	TgPort             int    `json:"tg_port,omitempty"`        // Telegram MTProxy/WebProxy port (e.g. 8085)
}
