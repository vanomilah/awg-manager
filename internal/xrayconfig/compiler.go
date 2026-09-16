package xrayconfig

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Compiler transforms a ManagedConfig into a canonical Xray Document.
type Compiler struct{}

// NewCompiler creates a new Compiler instance.
func NewCompiler() *Compiler {
	return &Compiler{}
}

// Compile generates a full Xray Document ready to be serialized to config.json.
func (c *Compiler) Compile(cfg *ManagedConfig) (*Document, error) {
	return c.CompileWithBase(nil, cfg)
}

// CompileWithBase merges the managed configuration into an optional base document,
// preserving custom top-level fields (e.g. DNS, Observatory, Reverse, FakeDNS)
// while replacing managed sections (log, inbounds, outbounds, routing, policy, stats).
func (c *Compiler) CompileWithBase(base *Document, cfg *ManagedConfig) (*Document, error) {
	if cfg == nil {
		return nil, fmt.Errorf("managed config cannot be nil")
	}

	doc := &Document{}
	if base != nil {
		doc.API = base.API
		doc.DNS = base.DNS
		doc.Observatory = base.Observatory
		doc.BurstObservatory = base.BurstObservatory
		doc.Reverse = base.Reverse
		doc.FakeDNS = base.FakeDNS
		doc.Metrics = base.Metrics

		if len(base.Extra) > 0 {
			doc.Extra = make(map[string]json.RawMessage, len(base.Extra))
			for k, v := range base.Extra {
				doc.Extra[k] = v
			}
		}
	}

	// Merge cfg.Extra into doc.Extra if present
	if len(cfg.Extra) > 0 {
		if doc.Extra == nil {
			doc.Extra = make(map[string]json.RawMessage)
		}
		var extraMap map[string]json.RawMessage
		if err := json.Unmarshal(cfg.Extra, &extraMap); err != nil {
			return nil, fmt.Errorf("invalid extra JSON in managed config: %w", err)
		}
		for k, v := range extraMap {
			doc.Extra[k] = v
		}
	}

	// 1. Log section
	logLevel := strings.ToLower(cfg.LogLevel)
	if logLevel == "" {
		logLevel = "warning"
	}
	logJSON, err := json.Marshal(map[string]string{
		"loglevel": logLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("compile log section: %w", err)
	}
	doc.Log = logJSON

	// 2. Inbounds
	doc.Inbounds = make([]json.RawMessage, 0, len(cfg.Inbounds))
	for _, in := range cfg.Inbounds {
		inJSON, err := c.compileInbound(in, cfg.StatsEnabled)
		if err != nil {
			return nil, fmt.Errorf("compile inbound %q: %w", in.Tag, err)
		}
		doc.Inbounds = append(doc.Inbounds, inJSON)
	}

	// 3. Outbounds
	doc.Outbounds = make([]json.RawMessage, 0, len(cfg.Outbounds))
	if len(cfg.Outbounds) == 0 {
		// Default direct outbound if none specified
		directOut := map[string]interface{}{
			"tag":      "direct",
			"protocol": "freedom",
			"settings": map[string]interface{}{},
		}
		rawDirect, _ := json.Marshal(directOut)
		doc.Outbounds = append(doc.Outbounds, rawDirect)
	} else {
		for _, out := range cfg.Outbounds {
			outJSON, err := c.compileOutbound(out)
			if err != nil {
				return nil, fmt.Errorf("compile outbound %q: %w", out.Tag, err)
			}
			doc.Outbounds = append(doc.Outbounds, outJSON)
		}
	}

	// 4. Routing
	if len(cfg.RoutingRules) > 0 || len(cfg.Balancers) > 0 {
		routingMap := map[string]interface{}{
			"domainStrategy": "AsIs",
		}
		if len(cfg.RoutingRules) > 0 {
			rulesList := make([]map[string]interface{}, 0, len(cfg.RoutingRules))
			for _, r := range cfg.RoutingRules {
				rMap := map[string]interface{}{
					"type": "field",
				}
				if len(r.Domain) > 0 {
					rMap["domain"] = r.Domain
				}
				if len(r.IP) > 0 {
					rMap["ip"] = r.IP
				}
				if r.Port != "" {
					rMap["port"] = r.Port
				}
				if r.Network != "" {
					rMap["network"] = r.Network
				}
				if len(r.InboundTag) > 0 {
					rMap["inboundTag"] = r.InboundTag
				}
				if r.OutboundTag != "" {
					rMap["outboundTag"] = r.OutboundTag
				}
				if r.BalancerTag != "" {
					rMap["balancerTag"] = r.BalancerTag
				}
				rulesList = append(rulesList, rMap)
			}
			routingMap["rules"] = rulesList
		}
		if len(cfg.Balancers) > 0 {
			balancersList := make([]map[string]interface{}, 0, len(cfg.Balancers))
			for _, b := range cfg.Balancers {
				bMap := map[string]interface{}{
					"tag":      b.Tag,
					"selector": b.Selector,
				}
				if b.Strategy != nil {
					sMap := map[string]interface{}{
						"type": b.Strategy.Type,
					}
					if len(b.Strategy.Settings) > 0 {
						var rawSet map[string]interface{}
						if err := json.Unmarshal(b.Strategy.Settings, &rawSet); err == nil {
							sMap["settings"] = rawSet
						}
					}
					bMap["strategy"] = sMap
				}
				balancersList = append(balancersList, bMap)
			}
			routingMap["balancers"] = balancersList
		}
		routingJSON, err := json.Marshal(routingMap)
		if err != nil {
			return nil, fmt.Errorf("compile routing: %w", err)
		}
		doc.Routing = routingJSON
	}

	// 5. Stats and Policy
	if cfg.StatsEnabled {
		doc.Stats = json.RawMessage(`{}`)
		policyMap := map[string]interface{}{
			"system": map[string]bool{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		}
		policyJSON, _ := json.Marshal(policyMap)
		doc.Policy = policyJSON
	}

	return doc, nil
}

func (c *Compiler) compileInbound(in Inbound, statsEnabled bool) (json.RawMessage, error) {
	inMap := map[string]interface{}{
		"tag":      in.Tag,
		"listen":   in.Listen,
		"port":     in.Port,
		"protocol": in.Protocol,
	}

	if in.Listen == "" {
		inMap["listen"] = "127.0.0.1"
	}

	// Validate and parse raw_settings strictly if present
	var rawSettingsMap map[string]interface{}
	if len(in.RawSettings) > 0 {
		if err := json.Unmarshal(in.RawSettings, &rawSettingsMap); err != nil {
			return nil, fmt.Errorf("invalid raw_settings for inbound %q: %w", in.Tag, err)
		}
	}

	// Protocol settings
	proto := strings.ToLower(in.Protocol)
	switch proto {
	case "vless":
		if len(in.Clients) > 0 {
			clients := make([]map[string]interface{}, 0, len(in.Clients))
			for _, client := range in.Clients {
				if !client.Enabled {
					continue
				}
				cMap := map[string]interface{}{
					"id":    client.UUID,
					"level": client.Level,
				}
				if client.Email != "" {
					cMap["email"] = client.Email
				}
				if client.Flow != "" {
					cMap["flow"] = client.Flow
				}
				clients = append(clients, cMap)
			}
			inMap["settings"] = map[string]interface{}{
				"clients":    clients,
				"decryption": "none",
			}
		} else if rawSettingsMap != nil {
			inMap["settings"] = rawSettingsMap
		} else {
			inMap["settings"] = map[string]interface{}{
				"clients":    []interface{}{},
				"decryption": "none",
			}
		}
	default:
		if rawSettingsMap != nil {
			inMap["settings"] = rawSettingsMap
		} else {
			inMap["settings"] = map[string]interface{}{}
		}
	}

	// Stream settings (transport & security)
	streamMap := map[string]interface{}{}
	network := in.Transport
	if network == "" || network == "raw" {
		network = "tcp"
	}
	streamMap["network"] = network

	security := in.Security
	if security == "" {
		security = "none"
	}
	streamMap["security"] = security

	// Transport settings
	switch network {
	case "xhttp":
		xhttpMap := map[string]interface{}{
			"mode": "packet-up",
		}
		if in.Path != "" {
			xhttpMap["path"] = in.Path
		}
		if in.Host != "" {
			xhttpMap["host"] = in.Host
		}
		streamMap["xhttpSettings"] = xhttpMap
	case "ws":
		wsMap := map[string]interface{}{}
		if in.Path != "" {
			wsMap["path"] = in.Path
		}
		if in.Host != "" {
			wsMap["headers"] = map[string]string{"Host": in.Host}
		}
		streamMap["wsSettings"] = wsMap
	case "grpc":
		grpcMap := map[string]interface{}{}
		if in.Path != "" {
			grpcMap["serviceName"] = in.Path
		}
		streamMap["grpcSettings"] = grpcMap
	case "httpupgrade":
		huMap := map[string]interface{}{}
		if in.Path != "" {
			huMap["path"] = in.Path
		}
		if in.Host != "" {
			huMap["host"] = in.Host
		}
		streamMap["httpupgradeSettings"] = huMap
	}

	// Security settings
	switch security {
	case "reality":
		if in.Reality != nil {
			rMap := map[string]interface{}{
				"show":        in.Reality.Show,
				"dest":        in.Reality.Target,
				"xver":        0,
				"serverNames": in.Reality.ServerNames,
				"privateKey":  in.Reality.PrivateKey,
				"shortIds":    in.Reality.ShortIDs,
			}
			streamMap["realitySettings"] = rMap
		}
	case "tls":
		if in.TLS != nil {
			tlsMap := map[string]interface{}{
				"serverName": in.TLS.ServerName,
			}
			if len(in.TLS.ALPN) > 0 {
				tlsMap["alpn"] = in.TLS.ALPN
			}
			if len(in.TLS.Certificates) > 0 {
				certs := make([]map[string]string, 0, len(in.TLS.Certificates))
				for _, crt := range in.TLS.Certificates {
					certs = append(certs, map[string]string{
						"certificateFile": crt.CertFile,
						"keyFile":         crt.KeyFile,
					})
				}
				tlsMap["certificates"] = certs
			}
			streamMap["tlsSettings"] = tlsMap
		}
	}

	// Merge StreamExtra into streamMap if present
	if len(in.StreamExtra) > 0 {
		var streamExtraMap map[string]interface{}
		if err := json.Unmarshal(in.StreamExtra, &streamExtraMap); err == nil {
			for k, v := range streamExtraMap {
				streamMap[k] = v
			}
		}
	}

	inMap["streamSettings"] = streamMap

	// Sniffing
	if in.Sniffing != nil && in.Sniffing.Enabled {
		inMap["sniffing"] = map[string]interface{}{
			"enabled":      true,
			"destOverride": in.Sniffing.DestOverride,
			"metadataOnly": in.Sniffing.MetadataOnly,
		}
	}

	// If RawDocument exists, overlay inMap onto it to preserve unmanaged fields
	if len(in.RawDocument) > 0 {
		var rawDocMap map[string]interface{}
		if err := json.Unmarshal(in.RawDocument, &rawDocMap); err == nil {
			for k, v := range inMap {
				rawDocMap[k] = v
			}
			return json.Marshal(rawDocMap)
		}
	}

	return json.Marshal(inMap)
}

func (c *Compiler) compileOutbound(out Outbound) (json.RawMessage, error) {
	outMap := map[string]interface{}{
		"tag":      out.Tag,
		"protocol": out.Protocol,
	}

	if out.SendThrough != "" {
		outMap["sendThrough"] = out.SendThrough
	}

	// Strictly validate raw_settings if provided
	var rawSettingsMap map[string]interface{}
	if len(out.RawSettings) > 0 {
		if err := json.Unmarshal(out.RawSettings, &rawSettingsMap); err != nil {
			return nil, fmt.Errorf("invalid raw_settings for outbound %q: %w", out.Tag, err)
		}
	}

	proto := strings.ToLower(out.Protocol)
	switch proto {
	case "freedom":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			outMap["settings"] = map[string]interface{}{}
		}

	case "blackhole":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			outMap["settings"] = map[string]interface{}{}
		}

	case "vless":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			userMap := map[string]interface{}{
				"id":         out.UUID,
				"encryption": "none",
			}
			vnextItem := map[string]interface{}{
				"address": out.Server,
				"port":    out.Port,
				"users":   []map[string]interface{}{userMap},
			}
			outMap["settings"] = map[string]interface{}{
				"vnext": []map[string]interface{}{vnextItem},
			}
		}

	case "vmess":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			userMap := map[string]interface{}{
				"id":       out.UUID,
				"security": "auto",
			}
			vnextItem := map[string]interface{}{
				"address": out.Server,
				"port":    out.Port,
				"users":   []map[string]interface{}{userMap},
			}
			outMap["settings"] = map[string]interface{}{
				"vnext": []map[string]interface{}{vnextItem},
			}
		}

	case "trojan":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			serverItem := map[string]interface{}{
				"address":  out.Server,
				"port":     out.Port,
				"password": out.Password,
			}
			outMap["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{serverItem},
			}
		}

	case "shadowsocks":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			method := out.Method
			if method == "" {
				method = "2022-blake3-aes-128-gcm"
			}
			serverItem := map[string]interface{}{
				"address":  out.Server,
				"port":     out.Port,
				"password": out.Password,
				"method":   method,
			}
			outMap["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{serverItem},
			}
		}

	case "socks":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			serverItem := map[string]interface{}{
				"address": out.Server,
				"port":    out.Port,
			}
			outMap["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{serverItem},
			}
		}

	case "http":
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			serverItem := map[string]interface{}{
				"address": out.Server,
				"port":    out.Port,
			}
			outMap["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{serverItem},
			}
		}

	default:
		if rawSettingsMap != nil {
			outMap["settings"] = rawSettingsMap
		} else {
			return nil, fmt.Errorf("unsupported outbound protocol %q", out.Protocol)
		}
	}

	// Stream settings (transport & security) for outbound
	needsStream := out.Transport != "" || (out.Security != "" && out.Security != "none") || out.Reality != nil || out.TLS != nil || len(out.StreamExtra) > 0
	if needsStream {
		streamMap := map[string]interface{}{}
		network := out.Transport
		if network == "" || network == "raw" {
			network = "tcp"
		}
		streamMap["network"] = network

		security := strings.ToLower(out.Security)
		if security == "" {
			security = "none"
		}
		streamMap["security"] = security

		switch network {
		case "xhttp":
			xhttpMap := map[string]interface{}{
				"mode": "packet-up",
			}
			if out.Path != "" {
				xhttpMap["path"] = out.Path
			}
			if out.Host != "" {
				xhttpMap["host"] = out.Host
			}
			streamMap["xhttpSettings"] = xhttpMap
		case "ws":
			wsMap := map[string]interface{}{}
			if out.Path != "" {
				wsMap["path"] = out.Path
			}
			if out.Host != "" {
				wsMap["headers"] = map[string]string{"Host": out.Host}
			}
			streamMap["wsSettings"] = wsMap
		case "grpc":
			grpcMap := map[string]interface{}{}
			if out.Path != "" {
				grpcMap["serviceName"] = out.Path
			}
			streamMap["grpcSettings"] = grpcMap
		case "httpupgrade":
			huMap := map[string]interface{}{}
			if out.Path != "" {
				huMap["path"] = out.Path
			}
			if out.Host != "" {
				huMap["host"] = out.Host
			}
			streamMap["httpupgradeSettings"] = huMap
		}

		switch security {
		case "reality":
			if out.Reality != nil {
				rMap := map[string]interface{}{
					"show":        out.Reality.Show,
					"fingerprint": out.Reality.Fingerprint,
					"serverName":  "",
					"publicKey":   out.Reality.PublicKey,
					"shortId":     "",
				}
				if len(out.Reality.ServerNames) > 0 {
					rMap["serverName"] = out.Reality.ServerNames[0]
				} else if out.TLS != nil && out.TLS.ServerName != "" {
					rMap["serverName"] = out.TLS.ServerName
				}
				if len(out.Reality.ShortIDs) > 0 {
					rMap["shortId"] = out.Reality.ShortIDs[0]
				}
				if rMap["fingerprint"] == "" {
					rMap["fingerprint"] = "chrome"
				}
				streamMap["realitySettings"] = rMap
			}
		case "tls":
			if out.TLS != nil {
				tlsMap := map[string]interface{}{}
				if out.TLS.ServerName != "" {
					tlsMap["serverName"] = out.TLS.ServerName
				}
				if len(out.TLS.ALPN) > 0 {
					tlsMap["alpn"] = out.TLS.ALPN
				}
				streamMap["tlsSettings"] = tlsMap
			}
		}

		// Merge StreamExtra into streamMap if present
		if len(out.StreamExtra) > 0 {
			var streamExtraMap map[string]interface{}
			if err := json.Unmarshal(out.StreamExtra, &streamExtraMap); err == nil {
				for k, v := range streamExtraMap {
					streamMap[k] = v
				}
			}
		}

		outMap["streamSettings"] = streamMap
	}

	// If RawDocument exists, overlay outMap onto it to preserve unmanaged fields
	if len(out.RawDocument) > 0 {
		var rawDocMap map[string]interface{}
		if err := json.Unmarshal(out.RawDocument, &rawDocMap); err == nil {
			for k, v := range outMap {
				rawDocMap[k] = v
			}
			return json.Marshal(rawDocMap)
		}
	}

	return json.Marshal(outMap)
}

