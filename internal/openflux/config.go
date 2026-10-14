package openflux

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// MinSecretChars is the minimum character length for shared secrets in OpenFlux.
const MinSecretChars = 16

// DefaultDirectPort is the default listen port for direct TCP transport.
const DefaultDirectPort = 8445

// DefaultClientSocks5Port is the default local SOCKS5 port for OpenFlux client mode.
const DefaultClientSocks5Port = 1085

// DefaultClientHttpPort is the default local HTTP proxy port when enabled.
const DefaultClientHttpPort = 1086

// Default transport priorities for whitelist bypass (cloud first, direct fallback).
const (
	PriorityMailru     = 100
	PriorityVyandex    = 95
	PriorityYandex     = 90
	PriorityCupsonline = 80
	PriorityTelemost   = 75
	PriorityDirect     = 50
)

// DefaultTransportPriority returns the default priority for an OpenFlux transport.
func DefaultTransportPriority(t string) int {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "mailru":
		return PriorityMailru
	case "vyandex":
		return PriorityVyandex
	case "yandex":
		return PriorityYandex
	case "cupsonline":
		return PriorityCupsonline
	case "telemost":
		return PriorityTelemost
	case "direct":
		return PriorityDirect
	default:
		return 50
	}
}

// Config represents OpenFlux exit node server configuration.
type Config struct {
	Enabled             bool           `json:"enabled"`
	Mode                string         `json:"mode"`          // "l4" (default, gVisor stream proxy) | "l3" (packet forward)
	TransportMode       string         `json:"transportMode"` // "session" (multi-transport, recommended) | "single"
	Transports          []string       `json:"transports"`    // list of enabled transports: "mailru", "yandex", "vyandex", "direct", "cupsonline"
	MailruURL           string         `json:"mailruUrl,omitempty"`
	YandexURL           string         `json:"yandexUrl,omitempty"`
	VyandexURL          string         `json:"vyandexUrl,omitempty"`
	CupsonlineURL       string         `json:"cupsonlineUrl,omitempty"`
	TelemostURL         string         `json:"telemostUrl,omitempty"`
	DirectListen        string         `json:"directListen,omitempty"` // "0.0.0.0:8445"
	DirectHost          string         `json:"directHost,omitempty"`   // host/IP used in client links
	TransportPriorities map[string]int `json:"transportPriorities,omitempty"`
	Secret              string         `json:"secret,omitempty"`       // AES-256-GCM shared secret
	Codec               string         `json:"codec,omitempty"`        // "batched" (default) | "legacy"
	OpenFirewall        bool           `json:"openFirewall"`           // open DirectListen port in WAN firewall
	Debug               int            `json:"debug"`                  // 0 = off, 1 = -d, 2 = -dd, 3 = -ddd
	Name                string         `json:"name,omitempty"`         // profile name for generated link

	// Routing, NAT and Egress settings matching WDTT server:
	NatMode          string   `json:"natMode,omitempty"`          // "full" (default) | "internet-only" | "none"
	NatStaticWAN     string   `json:"natStaticWan,omitempty"`     // legacy single WAN
	NatStaticWANs    []string `json:"natStaticWans,omitempty"`    // WAN interface(s) for internet-only NAT
	Policy           string   `json:"policy,omitempty"`           // Keenetic Access Policy name, e.g. "Policy0" or ""
	LanSegments      []string `json:"lanSegments,omitempty"`      // LAN segments allowed for clients
	ExposeToPolicies bool     `json:"exposeToPolicies"`           // whether to expose to access policies
	EgressInterface  string   `json:"egressInterface,omitempty"`  // specific egress WAN / interface (can map to --local-ip)
	NdmsIface        string   `json:"ndmsIface,omitempty"`
	TunIface         string   `json:"tunIface,omitempty"`
	TunMTU           int      `json:"tunMtu,omitempty"`
	PolicyIP         string   `json:"policyIp,omitempty"`         // resolved egress IPv4 from policy or manual setting
}

// DefaultConfig returns recommended default settings for an OpenFlux server.
func DefaultConfig() Config {
	sec, _ := GenerateSecret()
	return Config{
		Enabled:          false,
		Mode:             "l4",
		TransportMode:    "session",
		Transports:       []string{"mailru", "yandex", "vyandex"},
		DirectListen:     fmt.Sprintf("0.0.0.0:%d", DefaultDirectPort),
		Secret:           sec,
		Codec:            "batched",
		OpenFirewall:     true,
		Debug:            0,
		Name:             "Keenetic",
		NatMode:          "full",
		ExposeToPolicies: false,
	}
}

// StaticNATList returns list of WANs configured for static NAT.
func (c Config) StaticNATList() []string {
	if len(c.NatStaticWANs) > 0 {
		return c.NatStaticWANs
	}
	if w := strings.TrimSpace(c.NatStaticWAN); w != "" {
		return []string{w}
	}
	return nil
}

// GenerateSecret generates a secure random 24-character hexadecimal secret.
func GenerateSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SecretChars counts UTF-16 code units (matching OpenFlux core requirement).
func SecretChars(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// Validate checks that the configuration is valid for running the exit node.
func (c *Config) Validate() error {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode != "" && mode != "l4" && mode != "l3" {
		return fmt.Errorf("недопустимый режим %q: ожидается l4 или l3", c.Mode)
	}

	if c.TransportMode != "single" && c.TransportMode != "session" && c.TransportMode != "" {
		return fmt.Errorf("недопустимый режим транспорта %q: ожидается session или single", c.TransportMode)
	}

	// In session mode or when secret is set, validate secret length
	isSession := c.TransportMode == "session" || c.TransportMode == "" || len(c.Transports) > 1
	if isSession {
		if strings.TrimSpace(c.Secret) == "" {
			return fmt.Errorf("для сессионного режима требуется общий ключ шифрования (минимум %d символов)", MinSecretChars)
		}
		if SecretChars(strings.TrimSpace(c.Secret)) < MinSecretChars {
			return fmt.Errorf("ключ шифрования слишком короткий (%d символов, требуется минимум %d)", SecretChars(c.Secret), MinSecretChars)
		}
	} else if strings.TrimSpace(c.Secret) != "" && SecretChars(strings.TrimSpace(c.Secret)) < MinSecretChars {
		return fmt.Errorf("ключ шифрования должен содержать не менее %d символов", MinSecretChars)
	}

	// Validate DirectListen if direct transport is in use
	hasDirect := false
	if isSession {
		for _, t := range c.Transports {
			if strings.EqualFold(strings.TrimSpace(t), "direct") {
				hasDirect = true
				break
			}
		}
	} else if len(c.Transports) > 0 && strings.EqualFold(strings.TrimSpace(c.Transports[0]), "direct") {
		hasDirect = true
	}

	if hasDirect {
		dl := strings.TrimSpace(c.DirectListen)
		if dl == "" {
			return fmt.Errorf("для транспорта direct не указан адрес прослушивания (например, 0.0.0.0:%d)", DefaultDirectPort)
		}
		host, portStr, err := net.SplitHostPort(dl)
		if err != nil {
			return fmt.Errorf("некорректный адрес directListen %q: %w", dl, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("некорректный порт в directListen %q", dl)
		}
		switch port {
		case 22, 2222, 1099, 51820:
			return fmt.Errorf("порт %d зарезервирован системой", port)
		}
		_ = host
	}

	switch strings.ToLower(strings.TrimSpace(c.NatMode)) {
	case "full", "none", "":
	case "internet-only":
		if len(c.StaticNATList()) == 0 {
			return fmt.Errorf("natMode internet-only: не выбран WAN (natStaticWANs)")
		}
	default:
		return fmt.Errorf("natMode %q: ожидали full|internet-only|none", c.NatMode)
	}

	return nil
}

// DirectPort returns the TCP port number from DirectListen, or DefaultDirectPort on error.
func (c *Config) DirectPort() int {
	_, portStr, err := net.SplitHostPort(strings.TrimSpace(c.DirectListen))
	if err != nil {
		return DefaultDirectPort
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return DefaultDirectPort
	}
	return port
}

// ResolveInterfaceIPv4 returns the first non-loopback IPv4 address for an interface name or IP string.
func ResolveInterfaceIPv4(target string) string {
	s := strings.TrimSpace(target)
	if s == "" {
		return ""
	}
	if ip := net.ParseIP(s); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String()
		}
		return ""
	}
	ifi, err := net.InterfaceByName(s)
	if err != nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
				return ip4.String()
			}
		}
	}
	return ""
}

// BuildArgs converts Config to OpenFlux CLI invocation arguments.
func (c *Config) BuildArgs(keyFilePath string) []string {
	args := []string{
		"--role=exit",
	}

	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		mode = "l4"
	}
	args = append(args, "--mode="+mode)

	if mode == "l3" {
		tunName := strings.TrimSpace(c.TunIface)
		if tunName == "" && strings.TrimSpace(c.NdmsIface) != "" {
			tunName = strings.ToLower(strings.TrimSpace(c.NdmsIface))
		}
		if tunName != "" {
			args = append(args, "--tun-name="+tunName)
		}
		if c.TunMTU > 0 {
			args = append(args, fmt.Sprintf("--tun-mtu=%d", c.TunMTU))
		}
	}

	egressTarget := strings.TrimSpace(c.EgressInterface)
	if egressTarget == "" && c.NatMode == "internet-only" && len(c.StaticNATList()) > 0 {
		egressTarget = strings.TrimSpace(c.StaticNATList()[0])
	}
	if egressTarget != "" {
		if ip := ResolveInterfaceIPv4(egressTarget); ip != "" {
			args = append(args, "--local-ip="+ip)
		}
	} else if strings.TrimSpace(c.PolicyIP) != "" {
		args = append(args, "--local-ip="+strings.TrimSpace(c.PolicyIP))
	}

	codec := strings.ToLower(strings.TrimSpace(c.Codec))
	if codec == "" {
		codec = "batched"
	}
	args = append(args, "--codec="+codec)

	isSession := c.TransportMode == "session" || c.TransportMode == "" || len(c.Transports) > 1

	if isSession {
		args = append(args, "--negotiate")
		if keyFilePath != "" {
			args = append(args, "--encryption-key-file="+keyFilePath)
		}
		// Context: if mailru or yandex URL is present use it, otherwise default
		contextURL := "http://#"
		if strings.TrimSpace(c.MailruURL) != "" {
			contextURL = strings.TrimSpace(c.MailruURL)
		} else if strings.TrimSpace(c.YandexURL) != "" {
			contextURL = strings.TrimSpace(c.YandexURL)
		} else if strings.TrimSpace(c.VyandexURL) != "" {
			contextURL = strings.TrimSpace(c.VyandexURL)
		} else if strings.TrimSpace(c.CupsonlineURL) != "" {
			contextURL = strings.TrimSpace(c.CupsonlineURL)
		} else if strings.TrimSpace(c.TelemostURL) != "" {
			contextURL = strings.TrimSpace(c.TelemostURL)
		}
		args = append(args, "--session-context="+contextURL)

		// Build transports list with priorities sorted descending
		type transportPrioItem struct {
			part string
			prio int
		}
		var transportItems []transportPrioItem
		hasDirect := false
		hasMailru := false
		hasYandex := false
		hasVyandex := false
		hasCups := false
		hasTelemost := false

		for _, t := range c.Transports {
			norm := strings.ToLower(strings.TrimSpace(t))
			prio := DefaultTransportPriority(norm)
			if c.TransportPriorities != nil {
				if custom, ok := c.TransportPriorities[norm]; ok && custom > 0 {
					prio = custom
				}
			}
			switch norm {
			case "direct":
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("direct:%d", prio), prio: prio})
				hasDirect = true
			case "mailru":
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("mailru:%d", prio), prio: prio})
				hasMailru = true
			case "yandex":
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("yandex:%d", prio), prio: prio})
				hasYandex = true
			case "vyandex":
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("vyandex:%d", prio), prio: prio})
				hasVyandex = true
			case "cupsonline":
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("cupsonline:%d", prio), prio: prio})
				hasCups = true
			case "telemost":
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("telemost:%d", prio), prio: prio})
				hasTelemost = true
			}
		}

		sort.SliceStable(transportItems, func(i, j int) bool {
			return transportItems[i].prio > transportItems[j].prio
		})

		var parts []string
		for _, item := range transportItems {
			parts = append(parts, item.part)
		}
		if len(parts) > 0 {
			args = append(args, "--transports="+strings.Join(parts, ","))
		}

		if hasDirect {
			dl := strings.TrimSpace(c.DirectListen)
			if dl == "" {
				dl = fmt.Sprintf("0.0.0.0:%d", DefaultDirectPort)
			}
			args = append(args, "--direct-listen="+dl)
		}
		if hasMailru && strings.TrimSpace(c.MailruURL) != "" {
			args = append(args, "--mailru-url="+strings.TrimSpace(c.MailruURL))
		}
		if hasYandex && strings.TrimSpace(c.YandexURL) != "" {
			args = append(args, "--yandex-url="+strings.TrimSpace(c.YandexURL))
		}
		if hasVyandex {
			vyURL := strings.TrimSpace(c.VyandexURL)
			if vyURL == "" {
				vyURL = strings.TrimSpace(c.YandexURL)
			}
			if vyURL != "" {
				args = append(args, "--vyandex-url="+vyURL)
			}
		}
		if hasCups && strings.TrimSpace(c.CupsonlineURL) != "" {
			args = append(args, "--cupsonline-url="+strings.TrimSpace(c.CupsonlineURL))
		}
		if hasTelemost && strings.TrimSpace(c.TelemostURL) != "" {
			args = append(args, "--telemost-url="+strings.TrimSpace(c.TelemostURL))
		}
	} else {
		// Single transport mode
		singleType := "mailru"
		if len(c.Transports) > 0 {
			singleType = strings.ToLower(strings.TrimSpace(c.Transports[0]))
		}
		args = append(args, "--transport="+singleType)

		if keyFilePath != "" && strings.TrimSpace(c.Secret) != "" {
			args = append(args, "--encryption-key-file="+keyFilePath)
		}

		switch singleType {
		case "direct":
			dl := strings.TrimSpace(c.DirectListen)
			if dl == "" {
				dl = fmt.Sprintf("0.0.0.0:%d", DefaultDirectPort)
			}
			args = append(args, "--direct-listen="+dl)
		case "mailru":
			if strings.TrimSpace(c.MailruURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.MailruURL))
			}
		case "yandex":
			if strings.TrimSpace(c.YandexURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.YandexURL))
			}
		case "vyandex":
			vyURL := strings.TrimSpace(c.VyandexURL)
			if vyURL == "" {
				vyURL = strings.TrimSpace(c.YandexURL)
			}
			if vyURL != "" {
				args = append(args, "--url="+vyURL)
			}
		case "cupsonline":
			if strings.TrimSpace(c.CupsonlineURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.CupsonlineURL))
			}
		case "telemost":
			if strings.TrimSpace(c.TelemostURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.TelemostURL))
			}
		}
	}

	switch c.Debug {
	case 1:
		args = append(args, "-d")
	case 2:
		args = append(args, "-dd")
	case 3:
		args = append(args, "-ddd")
	}

	return args
}

// ClientConfig represents OpenFlux client configuration (OpkgTun L3 interface or SOCKS5 / HTTP proxy outbound on router).
type ClientConfig struct {
	Enabled          bool     `json:"enabled"`
	Name             string   `json:"name,omitempty"`
	InboundMode      string   `json:"inboundMode,omitempty"`      // "tun" (default, L3 OpkgTun interface) | "socks5" (L4 local proxy)
	NdmsIface        string   `json:"ndmsIface,omitempty"`        // NDMS interface name, e.g. "OpkgTun0" (default "OpkgTun0")
	TunIface         string   `json:"tunIface,omitempty"`         // Linux TUN interface name, e.g. "opkgtun0" (default "opkgtun0")
	TunAddr          string   `json:"tunAddr,omitempty"`          // IPv4 CIDR, default "10.10.10.2/24"
	TunMTU           int      `json:"tunMtu,omitempty"`           // MTU, default 1280
	Policy           string   `json:"policy,omitempty"`           // Keenetic Access Policy name, e.g. "Policy0" or ""
	ExposeToPolicies bool     `json:"exposeToPolicies"`           // "ip global auto" in NDMS (default true)
	DefaultRoute     bool     `json:"defaultRoute"`               // Candidate for default route in NDMS (default false)
	TransportMode    string   `json:"transportMode"`              // "session" (default, --negotiate) | "single" | "stream"
	Transports       []string `json:"transports"`                 // list of enabled transports: "direct", "mailru", "yandex", "cupsonline", "telemost"
	DirectDial       string   `json:"directDial,omitempty"`       // remote exit host:port for direct transport
	MailruURL        string   `json:"mailruUrl,omitempty"`        // document URL for mailru
	YandexURL        string   `json:"yandexUrl,omitempty"`        // document URL for yandex
	VyandexURL       string   `json:"vyandexUrl,omitempty"`       // document URL for vyandex
	CupsonlineURL    string   `json:"cupsonlineUrl,omitempty"`    // room URL for cupsonline
	TelemostURL      string   `json:"telemostUrl,omitempty"`      // conference URL for telemost
	Secret           string   `json:"secret,omitempty"`           // AES-256-GCM shared secret
	Context          string   `json:"context,omitempty"`          // KDF context (--session-context)
	Codec            string   `json:"codec,omitempty"`            // "batched" (default) | "legacy"
	Socks5Listen     string   `json:"socks5Listen,omitempty"`     // default "127.0.0.1:1085"
	HttpProxyListen  string   `json:"httpProxyListen,omitempty"`
	AutoReconnect    bool     `json:"autoReconnect"`
	Debug            int      `json:"debug"`
}

// DefaultClientConfig returns recommended default settings for an OpenFlux client.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		Enabled:          false,
		Name:             "OpenFlux Выход",
		InboundMode:      "tun",
		NdmsIface:        "",
		TunIface:         "",
		TunAddr:          "10.10.10.2/24",
		TunMTU:           1280,
		Policy:           "",
		ExposeToPolicies: true,
		DefaultRoute:     false,
		TransportMode:    "session",
		Transports:       []string{"mailru", "yandex", "vyandex"},
		Socks5Listen:     fmt.Sprintf("127.0.0.1:%d", DefaultClientSocks5Port),
		Codec:            "batched",
		AutoReconnect:    true,
		Debug:            0,
	}
}

func validateListenAddr(addr string, defaultPort int, fieldName string) (int, error) {
	a := strings.TrimSpace(addr)
	if a == "" {
		return defaultPort, nil
	}
	if strings.HasPrefix(a, ":") {
		a = "127.0.0.1" + a
	}
	_, portStr, err := net.SplitHostPort(a)
	if err != nil {
		return 0, fmt.Errorf("некорректный адрес %s %q: %w", fieldName, addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("некорректный порт в %s %q", fieldName, addr)
	}
	switch port {
	case 22, 2222, 53, 80, 443, 1099, 8443, 9090, 51820:
		return 0, fmt.Errorf("порт %d зарезервирован системой", port)
	}
	return port, nil
}

// Validate checks structural validity of ClientConfig. If Enabled is true, it also checks readiness for start.
func (c *ClientConfig) Validate() error {
	inbound := strings.ToLower(strings.TrimSpace(c.InboundMode))
	if inbound != "" && inbound != "tun" && inbound != "socks5" {
		return fmt.Errorf("недопустимый режим входящего трафика %q: ожидается tun или socks5", c.InboundMode)
	}

	tm := strings.ToLower(strings.TrimSpace(c.TransportMode))
	if tm != "" && tm != "session" && tm != "single" && tm != "stream" {
		return fmt.Errorf("недопустимый режим транспорта %q: ожидается session, single или stream", c.TransportMode)
	}

	codec := strings.ToLower(strings.TrimSpace(c.Codec))
	if codec != "" && codec != "batched" && codec != "legacy" {
		return fmt.Errorf("недопустимый кодек %q: ожидается batched или legacy", c.Codec)
	}

	if inbound == "tun" || inbound == "" {
		if c.TunMTU < 0 || (c.TunMTU > 0 && (c.TunMTU < 576 || c.TunMTU > 1500)) {
			return fmt.Errorf("некорректный MTU %d (ожидается от 576 до 1500)", c.TunMTU)
		}
		if addr := strings.TrimSpace(c.TunAddr); addr != "" {
			if _, _, err := net.ParseCIDR(addr); err != nil {
				if ip := net.ParseIP(addr); ip == nil {
					return fmt.Errorf("некорректный IP адрес туннеля %q (ожидается CIDR, например 10.10.10.2/24)", addr)
				}
			}
		}
	}

	socksPort, err := validateListenAddr(c.Socks5Listen, DefaultClientSocks5Port, "socks5Listen")
	if err != nil {
		return err
	}

	if strings.TrimSpace(c.HttpProxyListen) != "" {
		httpPort, err := validateListenAddr(c.HttpProxyListen, DefaultClientHttpPort, "httpProxyListen")
		if err != nil {
			return err
		}
		if httpPort == socksPort {
			return fmt.Errorf("порты SOCKS5 и HTTP-прокси не должны совпадать (%d)", socksPort)
		}
	}

	if dd := strings.TrimSpace(c.DirectDial); dd != "" {
		host, portStr, err := net.SplitHostPort(dd)
		if err != nil || strings.TrimSpace(host) == "" {
			return fmt.Errorf("некорректный адрес directDial %q (ожидается host:port)", dd)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("некорректный порт в directDial %q", dd)
		}
	}

	if sec := strings.TrimSpace(c.Secret); sec != "" && SecretChars(sec) < MinSecretChars {
		return fmt.Errorf("ключ шифрования слишком короткий (%d символов, требуется минимум %d)", SecretChars(sec), MinSecretChars)
	}

	if c.Enabled {
		return c.ValidateForStart()
	}
	return nil
}

// ValidateForStart checks that the client configuration has all required fields to establish a connection.
func (c *ClientConfig) ValidateForStart() error {
	tm := strings.ToLower(strings.TrimSpace(c.TransportMode))
	if tm == "" {
		tm = "session"
	}

	if tm == "stream" {
		if strings.TrimSpace(c.MailruURL) == "" && strings.TrimSpace(c.CupsonlineURL) == "" {
			return fmt.Errorf("для stream-режима укажите URL документа Mail.ru или комнаты Cups.online")
		}
		return nil
	}

	isSession := tm == "session" || len(c.Transports) > 1
	if isSession {
		sec := strings.TrimSpace(c.Secret)
		if sec == "" {
			return fmt.Errorf("для сессионного режима требуется общий ключ шифрования (минимум %d символов)", MinSecretChars)
		}
		if SecretChars(sec) < MinSecretChars {
			return fmt.Errorf("ключ шифрования слишком короткий (%d символов, требуется минимум %d)", SecretChars(sec), MinSecretChars)
		}
	}

	hasUsableTransport := false
	for _, t := range c.Transports {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "direct":
			if strings.TrimSpace(c.DirectDial) != "" {
				hasUsableTransport = true
			}
		case "mailru":
			if strings.TrimSpace(c.MailruURL) != "" {
				hasUsableTransport = true
			}
		case "vyandex":
			if strings.TrimSpace(c.VyandexURL) != "" || strings.TrimSpace(c.YandexURL) != "" {
				hasUsableTransport = true
			}
		case "yandex":
			if strings.TrimSpace(c.YandexURL) != "" {
				hasUsableTransport = true
			}
		case "cupsonline":
			if strings.TrimSpace(c.CupsonlineURL) != "" {
				hasUsableTransport = true
			}
		case "telemost":
			if strings.TrimSpace(c.TelemostURL) != "" {
				hasUsableTransport = true
			}
		}
	}

	if !hasUsableTransport {
		return fmt.Errorf("нет настроенных транспортов: импортируйте ссылку openflux:// или укажите адрес Direct-сервера (host:port) либо URL документа (Mail.ru / Яндекс)")
	}

	return nil
}

// Socks5Port returns the TCP port number from Socks5Listen, or DefaultClientSocks5Port.
func (c *ClientConfig) Socks5Port() int {
	a := strings.TrimSpace(c.Socks5Listen)
	if a == "" {
		return DefaultClientSocks5Port
	}
	if strings.HasPrefix(a, ":") {
		a = "127.0.0.1" + a
	}
	_, portStr, err := net.SplitHostPort(a)
	if err != nil {
		return DefaultClientSocks5Port
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return DefaultClientSocks5Port
	}
	return port
}

// Socks5URI returns a local socks5:// share URI usable by Mihomo or other router services.
func (c *ClientConfig) Socks5URI() string {
	port := c.Socks5Port()
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = "OpenFlux"
	}
	return fmt.Sprintf("socks5://127.0.0.1:%d#%s", port, url.PathEscape(name))
}

// HttpURI returns the local HTTP proxy URL if HttpProxyListen is configured.
func (c *ClientConfig) HttpURI() string {
	a := strings.TrimSpace(c.HttpProxyListen)
	if a == "" {
		return ""
	}
	if strings.HasPrefix(a, ":") {
		a = "127.0.0.1" + a
	}
	_, portStr, err := net.SplitHostPort(a)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%s", portStr)
}

// BuildArgs converts ClientConfig to OpenFlux CLI arguments for --role=client.
func (c *ClientConfig) BuildArgs(keyFilePath string) []string {
	inbound := strings.ToLower(strings.TrimSpace(c.InboundMode))
	if inbound == "" {
		inbound = "tun"
	}

	args := []string{
		"--role=client",
	}

	if inbound == "tun" {
		args = append(args, "--inbound=tun")
		if tunIface := strings.TrimSpace(c.TunIface); tunIface != "" {
			args = append(args, "--tun-name="+tunIface)
		}
		if tunAddr := strings.TrimSpace(c.TunAddr); tunAddr != "" {
			args = append(args, "--tun-addr="+tunAddr)
		}
		if c.TunMTU > 0 {
			args = append(args, fmt.Sprintf("--tun-mtu=%d", c.TunMTU))
		}
	} else {
		args = append(args, "--inbound=socks5")
		socks := strings.TrimSpace(c.Socks5Listen)
		if socks == "" {
			socks = fmt.Sprintf("127.0.0.1:%d", DefaultClientSocks5Port)
		}
		args = append(args, "--socks5="+socks)

		if hp := strings.TrimSpace(c.HttpProxyListen); hp != "" {
			args = append(args, "--http-proxy="+hp)
		}
	}

	codec := strings.ToLower(strings.TrimSpace(c.Codec))
	if codec == "" {
		codec = "batched"
	}
	args = append(args, "--codec="+codec)

	tm := strings.ToLower(strings.TrimSpace(c.TransportMode))
	if tm == "" {
		tm = "session"
	}

	type transportPrioItem struct {
		part string
		prio int
	}
	var transportItems []transportPrioItem
	hasDirect := false
	hasMailru := false
	hasYandex := false
	hasVyandex := false
	hasCups := false
	hasTelemost := false

	for _, t := range c.Transports {
		norm := strings.ToLower(strings.TrimSpace(t))
		prio := DefaultTransportPriority(norm)
		switch norm {
		case "direct":
			if strings.TrimSpace(c.DirectDial) != "" {
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("direct:%d", prio), prio: prio})
				hasDirect = true
			}
		case "mailru":
			if strings.TrimSpace(c.MailruURL) != "" {
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("mailru:%d", prio), prio: prio})
				hasMailru = true
			}
		case "yandex":
			if strings.TrimSpace(c.YandexURL) != "" {
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("yandex:%d", prio), prio: prio})
				hasYandex = true
			}
		case "vyandex":
			vyURL := strings.TrimSpace(c.VyandexURL)
			if vyURL == "" {
				vyURL = strings.TrimSpace(c.YandexURL)
			}
			if vyURL != "" {
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("vyandex:%d", prio), prio: prio})
				hasVyandex = true
			}
		case "cupsonline":
			if strings.TrimSpace(c.CupsonlineURL) != "" {
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("cupsonline:%d", prio), prio: prio})
				hasCups = true
			}
		case "telemost":
			if strings.TrimSpace(c.TelemostURL) != "" {
				transportItems = append(transportItems, transportPrioItem{part: fmt.Sprintf("telemost:%d", prio), prio: prio})
				hasTelemost = true
			}
		}
	}

	sort.SliceStable(transportItems, func(i, j int) bool {
		return transportItems[i].prio > transportItems[j].prio
	})

	var sessionParts []string
	for _, item := range transportItems {
		sessionParts = append(sessionParts, item.part)
	}

	if tm == "stream" {
		args = append(args, "--mode=stream")
		singleType := "mailru"
		if len(c.Transports) > 0 && strings.EqualFold(strings.TrimSpace(c.Transports[0]), "cupsonline") {
			singleType = "cupsonline"
		} else if strings.TrimSpace(c.MailruURL) == "" && strings.TrimSpace(c.CupsonlineURL) != "" {
			singleType = "cupsonline"
		}
		args = append(args, "--transport="+singleType)
		if singleType == "cupsonline" && strings.TrimSpace(c.CupsonlineURL) != "" {
			args = append(args, "--url="+strings.TrimSpace(c.CupsonlineURL))
		} else if strings.TrimSpace(c.MailruURL) != "" {
			args = append(args, "--url="+strings.TrimSpace(c.MailruURL))
		}
	} else if len(c.Transports) == 1 && strings.EqualFold(strings.TrimSpace(c.Transports[0]), "telemost") {
		// Telemost-only: OpenFlux CLI uses single-transport invocation
		singleType := "telemost"
		args = append(args, "--transport="+singleType)
		if keyFilePath != "" && strings.TrimSpace(c.Secret) != "" {
			args = append(args, "--encryption-key-file="+keyFilePath)
			if strings.TrimSpace(c.Context) != "" {
				args = append(args, "--session-context="+strings.TrimSpace(c.Context))
			}
		}
		if strings.TrimSpace(c.TelemostURL) != "" {
			args = append(args, "--url="+strings.TrimSpace(c.TelemostURL))
		}
	} else if (tm == "session" || len(c.Transports) > 1) && len(sessionParts) > 0 {
		args = append(args, "--negotiate")
		if keyFilePath != "" {
			args = append(args, "--encryption-key-file="+keyFilePath)
		}

		contextURL := strings.TrimSpace(c.Context)
		if contextURL == "" {
			if strings.TrimSpace(c.MailruURL) != "" {
				contextURL = strings.TrimSpace(c.MailruURL)
			} else if strings.TrimSpace(c.YandexURL) != "" {
				contextURL = strings.TrimSpace(c.YandexURL)
			} else if strings.TrimSpace(c.VyandexURL) != "" {
				contextURL = strings.TrimSpace(c.VyandexURL)
			} else if strings.TrimSpace(c.CupsonlineURL) != "" {
				contextURL = strings.TrimSpace(c.CupsonlineURL)
			} else if strings.TrimSpace(c.TelemostURL) != "" {
				contextURL = strings.TrimSpace(c.TelemostURL)
			} else {
				contextURL = "http://#"
			}
		}
		args = append(args, "--session-context="+contextURL)

		if len(sessionParts) > 0 {
			args = append(args, "--transports="+strings.Join(sessionParts, ","))
		}

		if hasDirect {
			args = append(args, "--direct-dial="+strings.TrimSpace(c.DirectDial))
		}
		if hasMailru {
			args = append(args, "--mailru-url="+strings.TrimSpace(c.MailruURL))
		}
		if hasYandex {
			args = append(args, "--yandex-url="+strings.TrimSpace(c.YandexURL))
		}
		if hasVyandex {
			vyURL := strings.TrimSpace(c.VyandexURL)
			if vyURL == "" {
				vyURL = strings.TrimSpace(c.YandexURL)
			}
			if vyURL != "" {
				args = append(args, "--vyandex-url="+vyURL)
			}
		}
		if hasCups {
			args = append(args, "--cupsonline-url="+strings.TrimSpace(c.CupsonlineURL))
		}
		if hasTelemost && strings.TrimSpace(c.TelemostURL) != "" {
			args = append(args, "--telemost-url="+strings.TrimSpace(c.TelemostURL))
		}
	} else {
		// Single transport mode
		singleType := "mailru"
		if len(c.Transports) > 0 {
			singleType = strings.ToLower(strings.TrimSpace(c.Transports[0]))
		}
		args = append(args, "--transport="+singleType)

		if keyFilePath != "" && strings.TrimSpace(c.Secret) != "" {
			args = append(args, "--encryption-key-file="+keyFilePath)
			if strings.TrimSpace(c.Context) != "" {
				args = append(args, "--session-context="+strings.TrimSpace(c.Context))
			}
		}

		switch singleType {
		case "direct":
			if strings.TrimSpace(c.DirectDial) != "" {
				args = append(args, "--direct-dial="+strings.TrimSpace(c.DirectDial))
			}
		case "mailru":
			if strings.TrimSpace(c.MailruURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.MailruURL))
			}
		case "yandex":
			if strings.TrimSpace(c.YandexURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.YandexURL))
			}
		case "vyandex":
			vyURL := strings.TrimSpace(c.VyandexURL)
			if vyURL == "" {
				vyURL = strings.TrimSpace(c.YandexURL)
			}
			if vyURL != "" {
				args = append(args, "--url="+vyURL)
			}
		case "cupsonline":
			if strings.TrimSpace(c.CupsonlineURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.CupsonlineURL))
			}
		case "telemost":
			if strings.TrimSpace(c.TelemostURL) != "" {
				args = append(args, "--url="+strings.TrimSpace(c.TelemostURL))
			}
		}
	}

	switch c.Debug {
	case 1:
		args = append(args, "-d")
	case 2:
		args = append(args, "-dd")
	case 3:
		args = append(args, "-ddd")
	}

	return args
}

