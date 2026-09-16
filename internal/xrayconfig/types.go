package xrayconfig

import (
	"encoding/json"
	"time"
)

// ProfileRole defines the primary role of an Xray profile.
type ProfileRole string

const (
	RoleServer   ProfileRole = "server"
	RoleClient   ProfileRole = "client"
	RoleRouter   ProfileRole = "router"
	RoleCombined ProfileRole = "combined"
)

// CurrentSchemaVersion is the active schema version for managed Xray profiles.
const CurrentSchemaVersion = 1

// Document represents a lossless top-level Xray configuration JSON.
// Standard sections are represented as raw messages, and any unrecognized
// top-level fields are captured in Extra so that round-trip serialization
// never loses user data.
type Document struct {
	Log              json.RawMessage            `json:"log,omitempty"`
	API              json.RawMessage            `json:"api,omitempty"`
	DNS              json.RawMessage            `json:"dns,omitempty"`
	Routing          json.RawMessage            `json:"routing,omitempty"`
	Policy           json.RawMessage            `json:"policy,omitempty"`
	Inbounds         []json.RawMessage          `json:"inbounds,omitempty"`
	Outbounds        []json.RawMessage          `json:"outbounds,omitempty"`
	Stats            json.RawMessage            `json:"stats,omitempty"`
	Metrics          json.RawMessage            `json:"metrics,omitempty"`
	Observatory      json.RawMessage            `json:"observatory,omitempty"`
	BurstObservatory json.RawMessage            `json:"burstObservatory,omitempty"`
	Reverse          json.RawMessage            `json:"reverse,omitempty"`
	FakeDNS          json.RawMessage            `json:"fakedns,omitempty"`
	Extra            map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling to capture extra top-level fields.
func (d *Document) UnmarshalJSON(data []byte) error {
	type docAlias Document
	var alias docAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*d = Document(alias)

	// Capture all fields into map and delete known keys
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}
	delete(rawMap, "log")
	delete(rawMap, "api")
	delete(rawMap, "dns")
	delete(rawMap, "routing")
	delete(rawMap, "policy")
	delete(rawMap, "inbounds")
	delete(rawMap, "outbounds")
	delete(rawMap, "stats")
	delete(rawMap, "metrics")
	delete(rawMap, "observatory")
	delete(rawMap, "burstObservatory")
	delete(rawMap, "reverse")
	delete(rawMap, "fakedns")

	if len(rawMap) > 0 {
		d.Extra = rawMap
	}
	return nil
}

// MarshalJSON implements custom JSON marshaling to include extra top-level fields.
func (d Document) MarshalJSON() ([]byte, error) {
	type docAlias Document
	rawBytes, err := json.Marshal(docAlias(d))
	if err != nil {
		return nil, err
	}

	if len(d.Extra) == 0 {
		return rawBytes, nil
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(rawBytes, &rawMap); err != nil {
		return nil, err
	}
	for k, v := range d.Extra {
		rawMap[k] = v
	}
	return json.Marshal(rawMap)
}

// SecretRef represents a stable reference to a secret stored in SecretStore.
// Note: per security audit, it does not include checksums to prevent key-leakage or desynchronization.
type SecretRef struct {
	ID      string `json:"id"`
	Type    string `json:"type"`    // e.g. "reality_private_key", "client_uuid", "password", "tls_key"
	Name    string `json:"name"`
	Version int    `json:"version"` // monotonically increasing version
}

// Profile represents an AWG Manager managed Xray configuration profile.
type Profile struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Role          ProfileRole    `json:"role"`
	Enabled       bool           `json:"enabled"`
	SchemaVersion int            `json:"schema_version"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Config        *ManagedConfig `json:"config,omitempty"`
}

// BalancerStrategy defines routing strategy for a balancer.
type BalancerStrategy struct {
	Type     string          `json:"type"` // "random", "roundRobin", "leastPing"
	Settings json.RawMessage `json:"settings,omitempty"`
}

// Balancer represents an Xray outbound balancer.
type Balancer struct {
	Tag      string            `json:"tag"`
	Selector []string          `json:"selector"`
	Strategy *BalancerStrategy `json:"strategy,omitempty"`
}

// ManagedConfig contains structured entities managed by AWG Manager.
type ManagedConfig struct {
	LogLevel     string          `json:"log_level,omitempty"` // debug, info, warning, error, none
	StatsEnabled bool            `json:"stats_enabled"`
	Inbounds     []Inbound       `json:"inbounds,omitempty"`
	Outbounds    []Outbound      `json:"outbounds,omitempty"`
	Balancers    []Balancer      `json:"balancers,omitempty"`
	RoutingRules []RoutingRule   `json:"routing_rules,omitempty"`
	Extra        json.RawMessage `json:"extra,omitempty"`
}

// Client represents an authenticated user for an inbound (e.g. VLESS client).
type Client struct {
	ID        string `json:"id"`     // Stable AWGM client UUID / identifier
	UUID      string `json:"uuid"`   // Xray VLESS/VMess user ID
	Remark    string `json:"remark"` // Human-friendly display name (e.g. "Phone (Happ)")
	Email     string `json:"email"`  // Stable identifier for Xray statistics
	Flow      string `json:"flow,omitempty"`
	Level     int    `json:"level,omitempty"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at,omitempty"`
}

// TLSConfig defines TLS settings for an inbound or outbound.
type TLSConfig struct {
	ServerName   string   `json:"server_name,omitempty"`
	ALPN         []string `json:"alpn,omitempty"`
	Certificates []Cert   `json:"certificates,omitempty"`
}

// Cert defines certificate pair or path.
type Cert struct {
	CertFile string `json:"certificate_file,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
}

// RealityConfig defines VLESS REALITY parameters.
type RealityConfig struct {
	Show        bool     `json:"show,omitempty"`
	Target      string   `json:"target,omitempty"` // Dest server and port (e.g. "ya.ru:443")
	ServerNames []string `json:"server_names,omitempty"`
	PrivateKey  string   `json:"private_key,omitempty"` // Keep secure / redacted
	PublicKey   string   `json:"public_key,omitempty"`
	ShortIDs    []string `json:"short_ids,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"` // chrome, safari, firefox
}

// SniffingConfig defines stream sniffing options.
type SniffingConfig struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"dest_override,omitempty"` // "http", "tls", "quic"
	MetadataOnly bool     `json:"metadata_only,omitempty"`
}

// Inbound represents a managed incoming listener.
type Inbound struct {
	Tag            string          `json:"tag"`
	Listen         string          `json:"listen"` // e.g. "127.0.0.1" or "0.0.0.0"
	Port           int             `json:"port"`
	Protocol       string          `json:"protocol"`  // vless, vmess, trojan, socks, http, dokodemo-door
	Transport      string          `json:"transport"` // raw, xhttp, ws, grpc, httpupgrade
	Security       string          `json:"security"`  // none, tls, reality
	Path           string          `json:"path,omitempty"`
	Host           string          `json:"host,omitempty"`
	UpstreamDevice string          `json:"upstream_device,omitempty"` // AWGM egress binding
	Clients        []Client        `json:"clients,omitempty"`
	TLS            *TLSConfig      `json:"tls,omitempty"`
	Reality        *RealityConfig  `json:"reality,omitempty"`
	Sniffing       *SniffingConfig `json:"sniffing,omitempty"`
	RawSettings    json.RawMessage `json:"raw_settings,omitempty"`
	RawDocument    json.RawMessage `json:"raw_document,omitempty"`
	StreamExtra    json.RawMessage `json:"stream_extra,omitempty"`
}

// Outbound represents a managed outgoing gateway or upstream proxy.
type Outbound struct {
	Tag         string          `json:"tag"`
	Protocol    string          `json:"protocol"` // freedom, blackhole, vless, vmess, trojan, shadowsocks, socks, http
	Server      string          `json:"server,omitempty"`
	Port        int             `json:"port,omitempty"`
	Transport   string          `json:"transport,omitempty"`
	Security    string          `json:"security,omitempty"`
	UUID        string          `json:"uuid,omitempty"`
	Password    string          `json:"password,omitempty"`
	Method      string          `json:"method,omitempty"` // For Shadowsocks
	Path        string          `json:"path,omitempty"`
	Host        string          `json:"host,omitempty"`
	SendThrough string          `json:"send_through,omitempty"` // Interface IP or binding
	TLS         *TLSConfig      `json:"tls,omitempty"`
	Reality     *RealityConfig  `json:"reality,omitempty"`
	RawSettings json.RawMessage `json:"raw_settings,omitempty"`
	RawDocument json.RawMessage `json:"raw_document,omitempty"`
	StreamExtra json.RawMessage `json:"stream_extra,omitempty"`
}

// RoutingRule represents a rule in the Xray routing router.
type RoutingRule struct {
	Type        string   `json:"type"` // "field"
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
	Network     string   `json:"network,omitempty"`
	InboundTag  []string `json:"inbound_tag,omitempty"`
	OutboundTag string   `json:"outbound_tag,omitempty"`
	BalancerTag string   `json:"balancer_tag,omitempty"`
}
