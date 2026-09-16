package xrayconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Parser provides lossless parsing of raw Xray JSON configurations.
type Parser struct{}

// NewParser creates a new Parser instance.
func NewParser() *Parser {
	return &Parser{}
}

// ParseDocument parses raw JSON into a lossless Document struct.
func (p *Parser) ParseDocument(rawJSON []byte) (*Document, error) {
	if len(bytes.TrimSpace(rawJSON)) == 0 {
		return nil, fmt.Errorf("empty configuration JSON")
	}

	dec := json.NewDecoder(bytes.NewReader(rawJSON))
	dec.UseNumber()

	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid Xray document JSON: %w", err)
	}

	return &doc, nil
}

// DocumentToManaged converts an Xray Document into a structured ManagedConfig,
// preserving underlying raw documents and stream extras.
func (p *Parser) DocumentToManaged(doc *Document) (*ManagedConfig, error) {
	if doc == nil {
		return nil, fmt.Errorf("document cannot be nil")
	}

	cfg := &ManagedConfig{
		Inbounds:     make([]Inbound, 0, len(doc.Inbounds)),
		Outbounds:    make([]Outbound, 0, len(doc.Outbounds)),
		Balancers:    make([]Balancer, 0),
		RoutingRules: make([]RoutingRule, 0),
	}

	// 1. Log
	if len(doc.Log) > 0 && string(doc.Log) != "null" {
		var logSection struct {
			LogLevel string `json:"loglevel"`
		}
		if err := json.Unmarshal(doc.Log, &logSection); err == nil && logSection.LogLevel != "" {
			cfg.LogLevel = logSection.LogLevel
		}
	}

	// 2. Stats
	if len(doc.Stats) > 0 && string(doc.Stats) != "null" {
		cfg.StatsEnabled = true
	}

	// 3. Extra top-level fields
	if len(doc.Extra) > 0 {
		extraBytes, err := json.Marshal(doc.Extra)
		if err == nil {
			cfg.Extra = extraBytes
		}
	}

	// 4. Inbounds
	for i, rawIn := range doc.Inbounds {
		inbound, err := p.parseInbound(rawIn)
		if err != nil {
			return nil, fmt.Errorf("failed to parse inbound[%d]: %w", i, err)
		}
		cfg.Inbounds = append(cfg.Inbounds, inbound)
	}

	// 5. Outbounds
	for i, rawOut := range doc.Outbounds {
		outbound, err := p.parseOutbound(rawOut)
		if err != nil {
			return nil, fmt.Errorf("failed to parse outbound[%d]: %w", i, err)
		}
		cfg.Outbounds = append(cfg.Outbounds, outbound)
	}

	// 6. Routing (rules and balancers)
	if len(doc.Routing) > 0 && string(doc.Routing) != "null" {
		var routingSection struct {
			Rules     []rawRoutingRule `json:"rules"`
			Balancers []Balancer       `json:"balancers"`
		}
		if err := json.Unmarshal(doc.Routing, &routingSection); err == nil {
			if len(routingSection.Balancers) > 0 {
				cfg.Balancers = routingSection.Balancers
			}
			for _, r := range routingSection.Rules {
				cfg.RoutingRules = append(cfg.RoutingRules, RoutingRule{
					Type:        r.Type,
					Domain:      r.Domain,
					IP:          r.IP,
					Port:        r.Port,
					Network:     r.Network,
					InboundTag:  r.InboundTag,
					OutboundTag: r.OutboundTag,
					BalancerTag: r.BalancerTag,
				})
			}
		}
	}

	return cfg, nil
}

type rawRoutingRule struct {
	Type        string   `json:"type"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
	Network     string   `json:"network,omitempty"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	BalancerTag string   `json:"balancerTag,omitempty"`
}

func (p *Parser) parseInbound(raw []byte) (Inbound, error) {
	var base struct {
		Tag            string          `json:"tag"`
		Listen         string          `json:"listen"`
		Port           int             `json:"port"`
		Protocol       string          `json:"protocol"`
		Settings       json.RawMessage `json:"settings"`
		StreamSettings json.RawMessage `json:"streamSettings"`
		Sniffing       *SniffingConfig `json:"sniffing"`
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&base); err != nil {
		return Inbound{}, err
	}

	in := Inbound{
		Tag:         base.Tag,
		Listen:      base.Listen,
		Port:        base.Port,
		Protocol:    base.Protocol,
		Sniffing:    base.Sniffing,
		RawDocument: raw,
	}

	// Parse streamSettings
	if len(base.StreamSettings) > 0 {
		var stream struct {
			Network         string          `json:"network"`
			Security        string          `json:"security"`
			RealitySettings json.RawMessage `json:"realitySettings"`
			TLSSettings     json.RawMessage `json:"tlsSettings"`
			XHTTPSettings   struct {
				Path string `json:"path"`
				Host string `json:"host"`
			} `json:"xhttpSettings"`
			WSSettings struct {
				Path    string            `json:"path"`
				Headers map[string]string `json:"headers"`
			} `json:"wsSettings"`
			GRPCSettings struct {
				ServiceName string `json:"serviceName"`
			} `json:"grpcSettings"`
			HTTPUpgradeSettings struct {
				Path string `json:"path"`
				Host string `json:"host"`
			} `json:"httpupgradeSettings"`
		}
		if err := json.Unmarshal(base.StreamSettings, &stream); err == nil {
			in.Transport = stream.Network
			in.Security = stream.Security

			switch strings.ToLower(stream.Network) {
			case "xhttp":
				in.Path = stream.XHTTPSettings.Path
				in.Host = stream.XHTTPSettings.Host
			case "ws":
				in.Path = stream.WSSettings.Path
				if stream.WSSettings.Headers != nil {
					in.Host = stream.WSSettings.Headers["Host"]
				}
			case "grpc":
				in.Path = stream.GRPCSettings.ServiceName
			case "httpupgrade":
				in.Path = stream.HTTPUpgradeSettings.Path
				in.Host = stream.HTTPUpgradeSettings.Host
			}

			if strings.ToLower(stream.Security) == "reality" && len(stream.RealitySettings) > 0 {
				var r struct {
					Show        bool     `json:"show"`
					Dest        string   `json:"dest"`
					ServerNames []string `json:"serverNames"`
					PrivateKey  string   `json:"privateKey"`
					ShortIDs    []string `json:"shortIds"`
				}
				if err := json.Unmarshal(stream.RealitySettings, &r); err == nil {
					in.Reality = &RealityConfig{
						Show:        r.Show,
						Target:      r.Dest,
						ServerNames: r.ServerNames,
						PrivateKey:  r.PrivateKey,
						ShortIDs:    r.ShortIDs,
					}
				}
			}

			if strings.ToLower(stream.Security) == "tls" && len(stream.TLSSettings) > 0 {
				var t struct {
					ServerName   string   `json:"serverName"`
					ALPN         []string `json:"alpn"`
					Certificates []struct {
						CertificateFile string `json:"certificateFile"`
						KeyFile         string `json:"keyFile"`
					} `json:"certificates"`
				}
				if err := json.Unmarshal(stream.TLSSettings, &t); err == nil {
					tlsCfg := &TLSConfig{
						ServerName: t.ServerName,
						ALPN:       t.ALPN,
					}
					for _, c := range t.Certificates {
						tlsCfg.Certificates = append(tlsCfg.Certificates, Cert{
							CertFile: c.CertificateFile,
							KeyFile:  c.KeyFile,
						})
					}
					in.TLS = tlsCfg
				}
			}

			// Capture unmanaged fields in streamSettings into in.StreamExtra
			var streamMap map[string]json.RawMessage
			if err := json.Unmarshal(base.StreamSettings, &streamMap); err == nil {
				delete(streamMap, "network")
				delete(streamMap, "security")
				delete(streamMap, "realitySettings")
				delete(streamMap, "tlsSettings")
				delete(streamMap, "xhttpSettings")
				delete(streamMap, "wsSettings")
				delete(streamMap, "grpcSettings")
				delete(streamMap, "httpupgradeSettings")
				if len(streamMap) > 0 {
					if extraBytes, err := json.Marshal(streamMap); err == nil {
						in.StreamExtra = extraBytes
					}
				}
			}
		}
	}

	// Parse protocol settings
	proto := strings.ToLower(base.Protocol)
	if proto == "vless" && len(base.Settings) > 0 {
		var vlessSettings struct {
			Clients []struct {
				ID    string `json:"id"`
				Flow  string `json:"flow"`
				Email string `json:"email"`
				Level int    `json:"level"`
			} `json:"clients"`
		}
		if err := json.Unmarshal(base.Settings, &vlessSettings); err == nil && len(vlessSettings.Clients) > 0 {
			in.Clients = make([]Client, 0, len(vlessSettings.Clients))
			for _, c := range vlessSettings.Clients {
				in.Clients = append(in.Clients, Client{
					UUID:    c.ID,
					Email:   c.Email,
					Flow:    c.Flow,
					Level:   c.Level,
					Enabled: true,
					Remark:  c.Email,
				})
			}
		} else {
			in.RawSettings = base.Settings
		}
	} else if len(base.Settings) > 0 {
		in.RawSettings = base.Settings
	}

	return in, nil
}

func (p *Parser) parseOutbound(raw []byte) (Outbound, error) {
	var base struct {
		Tag            string          `json:"tag"`
		Protocol       string          `json:"protocol"`
		SendThrough    string          `json:"sendThrough"`
		Settings       json.RawMessage `json:"settings"`
		StreamSettings json.RawMessage `json:"streamSettings"`
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&base); err != nil {
		return Outbound{}, err
	}

	out := Outbound{
		Tag:         base.Tag,
		Protocol:    base.Protocol,
		SendThrough: base.SendThrough,
		RawDocument: raw,
	}

	// Parse streamSettings
	if len(base.StreamSettings) > 0 {
		var stream struct {
			Network         string          `json:"network"`
			Security        string          `json:"security"`
			RealitySettings json.RawMessage `json:"realitySettings"`
			TLSSettings     json.RawMessage `json:"tlsSettings"`
			XHTTPSettings   struct {
				Path string `json:"path"`
				Host string `json:"host"`
			} `json:"xhttpSettings"`
			WSSettings struct {
				Path    string            `json:"path"`
				Headers map[string]string `json:"headers"`
			} `json:"wsSettings"`
			GRPCSettings struct {
				ServiceName string `json:"serviceName"`
			} `json:"grpcSettings"`
			HTTPUpgradeSettings struct {
				Path string `json:"path"`
				Host string `json:"host"`
			} `json:"httpupgradeSettings"`
		}
		if err := json.Unmarshal(base.StreamSettings, &stream); err == nil {
			out.Transport = stream.Network
			out.Security = stream.Security

			switch strings.ToLower(stream.Network) {
			case "xhttp":
				out.Path = stream.XHTTPSettings.Path
				out.Host = stream.XHTTPSettings.Host
			case "ws":
				out.Path = stream.WSSettings.Path
				if stream.WSSettings.Headers != nil {
					out.Host = stream.WSSettings.Headers["Host"]
				}
			case "grpc":
				out.Path = stream.GRPCSettings.ServiceName
			case "httpupgrade":
				out.Path = stream.HTTPUpgradeSettings.Path
				out.Host = stream.HTTPUpgradeSettings.Host
			}

			if strings.ToLower(stream.Security) == "reality" && len(stream.RealitySettings) > 0 {
				var r struct {
					Show        bool   `json:"show"`
					ServerName  string `json:"serverName"`
					PublicKey   string `json:"publicKey"`
					ShortID     string `json:"shortId"`
					Fingerprint string `json:"fingerprint"`
				}
				if err := json.Unmarshal(stream.RealitySettings, &r); err == nil {
					out.Reality = &RealityConfig{
						Show:        r.Show,
						PublicKey:   r.PublicKey,
						Fingerprint: r.Fingerprint,
					}
					if r.ServerName != "" {
						out.Reality.ServerNames = []string{r.ServerName}
					}
					if r.ShortID != "" {
						out.Reality.ShortIDs = []string{r.ShortID}
					}
				}
			}

			if strings.ToLower(stream.Security) == "tls" && len(stream.TLSSettings) > 0 {
				var t struct {
					ServerName   string   `json:"serverName"`
					ALPN         []string `json:"alpn"`
					Certificates []struct {
						CertificateFile string `json:"certificateFile"`
						KeyFile         string `json:"keyFile"`
					} `json:"certificates"`
				}
				if err := json.Unmarshal(stream.TLSSettings, &t); err == nil {
					tlsCfg := &TLSConfig{
						ServerName: t.ServerName,
						ALPN:       t.ALPN,
					}
					for _, c := range t.Certificates {
						tlsCfg.Certificates = append(tlsCfg.Certificates, Cert{
							CertFile: c.CertificateFile,
							KeyFile:  c.KeyFile,
						})
					}
					out.TLS = tlsCfg
				}
			}

			// Capture unmanaged fields in streamSettings into out.StreamExtra
			var streamMap map[string]json.RawMessage
			if err := json.Unmarshal(base.StreamSettings, &streamMap); err == nil {
				delete(streamMap, "network")
				delete(streamMap, "security")
				delete(streamMap, "realitySettings")
				delete(streamMap, "tlsSettings")
				delete(streamMap, "xhttpSettings")
				delete(streamMap, "wsSettings")
				delete(streamMap, "grpcSettings")
				delete(streamMap, "httpupgradeSettings")
				if len(streamMap) > 0 {
					if extraBytes, err := json.Marshal(streamMap); err == nil {
						out.StreamExtra = extraBytes
					}
				}
			}
		}
	}

	// Parse protocol settings
	proto := strings.ToLower(base.Protocol)
	switch proto {
	case "vless", "vmess":
		var vnextSettings struct {
			Vnext []struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
				Users   []struct {
					ID string `json:"id"`
				} `json:"users"`
			} `json:"vnext"`
		}
		if err := json.Unmarshal(base.Settings, &vnextSettings); err == nil && len(vnextSettings.Vnext) > 0 {
			out.Server = vnextSettings.Vnext[0].Address
			out.Port = vnextSettings.Vnext[0].Port
			if len(vnextSettings.Vnext[0].Users) > 0 {
				out.UUID = vnextSettings.Vnext[0].Users[0].ID
			}
		} else {
			out.RawSettings = base.Settings
		}

	case "trojan":
		var trojanSettings struct {
			Servers []struct {
				Address  string `json:"address"`
				Port     int    `json:"port"`
				Password string `json:"password"`
			} `json:"servers"`
		}
		if err := json.Unmarshal(base.Settings, &trojanSettings); err == nil && len(trojanSettings.Servers) > 0 {
			out.Server = trojanSettings.Servers[0].Address
			out.Port = trojanSettings.Servers[0].Port
			out.Password = trojanSettings.Servers[0].Password
		} else {
			out.RawSettings = base.Settings
		}

	case "shadowsocks":
		var ssSettings struct {
			Servers []struct {
				Address  string `json:"address"`
				Port     int    `json:"port"`
				Password string `json:"password"`
				Method   string `json:"method"`
			} `json:"servers"`
		}
		if err := json.Unmarshal(base.Settings, &ssSettings); err == nil && len(ssSettings.Servers) > 0 {
			out.Server = ssSettings.Servers[0].Address
			out.Port = ssSettings.Servers[0].Port
			out.Password = ssSettings.Servers[0].Password
			out.Method = ssSettings.Servers[0].Method
		} else {
			out.RawSettings = base.Settings
		}

	case "socks", "http":
		var sSettings struct {
			Servers []struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
			} `json:"servers"`
		}
		if err := json.Unmarshal(base.Settings, &sSettings); err == nil && len(sSettings.Servers) > 0 {
			out.Server = sSettings.Servers[0].Address
			out.Port = sSettings.Servers[0].Port
		} else {
			out.RawSettings = base.Settings
		}

	default:
		out.RawSettings = base.Settings
	}

	return out, nil
}
