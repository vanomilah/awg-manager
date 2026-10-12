package openflux

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Prefix starts every openflux share link.
const Prefix = "openflux://v1/"

// ShareTransport describes one transport in an OpenFlux shareable profile.
type ShareTransport struct {
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	URL      string `json:"url,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Dial     string `json:"dial,omitempty"` // host:port for direct
}

// ShareConfig is the JSON payload of an openflux://v1/ link.
type ShareConfig struct {
	Name       string           `json:"name,omitempty"`
	Negotiate  bool             `json:"negotiate,omitempty"`
	Codec      string           `json:"codec,omitempty"`
	Secret     string           `json:"secret,omitempty"`
	Context    string           `json:"context,omitempty"`
	Mode       string           `json:"mode,omitempty"`
	Transports []ShareTransport `json:"transports"`
}

// Encode converts ShareConfig into an openflux://v1/... link.
func Encode(c ShareConfig) (string, error) {
	if len(c.Transports) == 0 {
		return "", fmt.Errorf("share: no transports")
	}
	if len(c.Transports) > 1 && !c.Negotiate {
		return "", fmt.Errorf("share: several transports need a negotiated session")
	}
	if c.Negotiate && SecretChars(c.Secret) < MinSecretChars {
		return "", fmt.Errorf("share: a negotiated session needs a secret of at least %d characters", MinSecretChars)
	}

	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("failed to marshal share config: %w", err)
	}

	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return "", fmt.Errorf("failed to create flate writer: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return "", fmt.Errorf("failed to compress share config: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to close flate writer: %w", err)
	}

	return Prefix + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

// GenerateLink constructs an openflux:// link from the server configuration and given public host/IP.
func GenerateLink(cfg Config, publicHost string) (string, error) {
	isSession := cfg.TransportMode == "session" || cfg.TransportMode == "" || len(cfg.Transports) > 1

	sc := ShareConfig{
		Name:      cfg.Name,
		Negotiate: isSession,
		Secret:    strings.TrimSpace(cfg.Secret),
	}
	if sc.Name == "" {
		sc.Name = "OpenFlux Server"
	}
	if strings.ToLower(strings.TrimSpace(cfg.Codec)) == "legacy" {
		sc.Codec = "legacy"
	}

	// Determine context URL
	if strings.TrimSpace(cfg.MailruURL) != "" {
		sc.Context = strings.TrimSpace(cfg.MailruURL)
	} else if strings.TrimSpace(cfg.YandexURL) != "" {
		sc.Context = strings.TrimSpace(cfg.YandexURL)
	} else {
		sc.Context = "http://#"
	}

	// Dial host for Direct transport
	dialHost := strings.TrimSpace(publicHost)
	if strings.TrimSpace(cfg.DirectHost) != "" {
		dialHost = strings.TrimSpace(cfg.DirectHost)
	}
	directPort := cfg.DirectPort()
	if dialHost != "" {
		// If dialHost doesn't already have port, append directPort
		if _, _, err := net.SplitHostPort(dialHost); err != nil {
			dialHost = net.JoinHostPort(dialHost, strconv.Itoa(directPort))
		}
	}

	if isSession {
		for _, t := range cfg.Transports {
			norm := strings.ToLower(strings.TrimSpace(t))
			switch norm {
			case "direct":
				if dialHost != "" {
					sc.Transports = append(sc.Transports, ShareTransport{
						Type:     "direct",
						Priority: 100,
						Dial:     dialHost,
					})
				}
			case "mailru":
				if strings.TrimSpace(cfg.MailruURL) != "" {
					sc.Transports = append(sc.Transports, ShareTransport{
						Type:     "mailru",
						Priority: 60,
						URL:      strings.TrimSpace(cfg.MailruURL),
					})
				}
			case "yandex":
				if strings.TrimSpace(cfg.YandexURL) != "" {
					sc.Transports = append(sc.Transports, ShareTransport{
						Type:     "yandex",
						Priority: 50,
						URL:      strings.TrimSpace(cfg.YandexURL),
					})
				}
			}
		}
	} else {
		single := "direct"
		if len(cfg.Transports) > 0 {
			single = strings.ToLower(strings.TrimSpace(cfg.Transports[0]))
		}
		switch single {
		case "direct":
			if dialHost != "" {
				sc.Transports = append(sc.Transports, ShareTransport{
					Type: "direct",
					Dial: dialHost,
				})
			}
		case "mailru":
			if strings.TrimSpace(cfg.MailruURL) != "" {
				sc.Transports = append(sc.Transports, ShareTransport{
					Type: "mailru",
					URL:  strings.TrimSpace(cfg.MailruURL),
				})
			}
		case "yandex":
			if strings.TrimSpace(cfg.YandexURL) != "" {
				sc.Transports = append(sc.Transports, ShareTransport{
					Type: "yandex",
					URL:  strings.TrimSpace(cfg.YandexURL),
				})
			}
		}
	}

	if len(sc.Transports) == 0 {
		return "", fmt.Errorf("нет доступных транспортов для ссылки (укажите адрес сервера для direct или URL документа для mailru/yandex)")
	}

	return Encode(sc)
}
