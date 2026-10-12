package openflux

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode/utf16"
)

// MinSecretChars is the minimum character length for shared secrets in OpenFlux.
const MinSecretChars = 16

// DefaultDirectPort is the default listen port for direct TCP transport.
const DefaultDirectPort = 8445

// Config represents OpenFlux exit node server configuration.
type Config struct {
	Enabled       bool     `json:"enabled"`
	Mode          string   `json:"mode"`          // "l4" (default, gVisor stream proxy) | "l3" (packet forward)
	TransportMode string   `json:"transportMode"` // "session" (multi-transport, recommended) | "single"
	Transports    []string `json:"transports"`    // list of enabled transports: "direct", "mailru", "yandex"
	MailruURL     string   `json:"mailruUrl,omitempty"`
	YandexURL     string   `json:"yandexUrl,omitempty"`
	DirectListen  string   `json:"directListen,omitempty"` // "0.0.0.0:8445"
	DirectHost    string   `json:"directHost,omitempty"`   // host/IP used in client links
	Secret        string   `json:"secret,omitempty"`       // AES-256-GCM shared secret
	Codec         string   `json:"codec,omitempty"`        // "batched" (default) | "legacy"
	OpenFirewall  bool     `json:"openFirewall"`           // open DirectListen port in WAN firewall
	Debug         int      `json:"debug"`                  // 0 = off, 1 = -d, 2 = -dd, 3 = -ddd
	Name          string   `json:"name,omitempty"`         // profile name for generated link
}

// DefaultConfig returns recommended default settings for an OpenFlux server.
func DefaultConfig() Config {
	sec, _ := GenerateSecret()
	return Config{
		Enabled:       false,
		Mode:          "l4",
		TransportMode: "session",
		Transports:    []string{"direct", "mailru", "yandex"},
		DirectListen:  fmt.Sprintf("0.0.0.0:%d", DefaultDirectPort),
		Secret:        sec,
		Codec:         "batched",
		OpenFirewall:  true,
		Debug:         0,
		Name:          "Keenetic",
	}
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
		}
		args = append(args, "--session-context="+contextURL)

		// Build transports list with priorities
		var parts []string
		hasDirect := false
		hasMailru := false
		hasYandex := false

		for _, t := range c.Transports {
			norm := strings.ToLower(strings.TrimSpace(t))
			switch norm {
			case "direct":
				parts = append(parts, "direct:100")
				hasDirect = true
			case "mailru":
				parts = append(parts, "mailru:60")
				hasMailru = true
			case "yandex":
				parts = append(parts, "yandex:50")
				hasYandex = true
			}
		}

		if len(parts) == 0 {
			parts = append(parts, "direct:100")
			hasDirect = true
		}
		args = append(args, "--transports="+strings.Join(parts, ","))

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
	} else {
		// Single transport mode
		singleType := "direct"
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
