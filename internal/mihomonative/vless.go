package mihomonative

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CompileVLESS parses a VLESS share URI without converting through sing-box,
// preserving Mihomo-specific transports such as XHTTP.
func CompileVLESS(raw string, preference, routingEngine EnginePreference) (*ProxyNode, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "vless") {
		return nil, fmt.Errorf("mihomo vless: invalid URI")
	}
	if u.User == nil || u.User.Username() == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("mihomo vless: uuid and server are required")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("mihomo vless: valid port is required")
	}
	q := u.Query()
	transport := strings.ToLower(q.Get("type"))
	if transport == "" {
		transport = "tcp"
	}
	compatibility := CheckVLESS(transport)
	selected, reason := SelectEngine(preference, compatibility, routingEngine)
	if selected == "" {
		return nil, fmt.Errorf("mihomo vless: %s", reason)
	}
	name, _ := url.PathUnescape(u.Fragment)
	if strings.TrimSpace(name) == "" {
		name = u.Hostname() + ":" + strconv.Itoa(port)
	}
	proxy := map[string]interface{}{
		"name": name, "type": "vless", "server": u.Hostname(), "port": port,
		"uuid": u.User.Username(), "udp": true, "network": transport,
		"encryption": q.Get("encryption"),
	}
	if flow := q.Get("flow"); flow != "" && flow != "none" {
		proxy["flow"] = flow
	}
	security := strings.ToLower(q.Get("security"))
	if security == "tls" || security == "reality" {
		proxy["tls"] = true
		if sni := q.Get("sni"); sni != "" {
			proxy["servername"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			proxy["client-fingerprint"] = fp
		}
	}
	if security == "reality" {
		proxy["reality-opts"] = map[string]interface{}{
			"public-key": q.Get("pbk"),
			"short-id":   q.Get("sid"),
		}
	}
	if alpn := splitCSV(q.Get("alpn")); len(alpn) > 0 {
		proxy["alpn"] = alpn
	}
	addTransportOptions(proxy, transport, q)
	return &ProxyNode{
		Name: name, Protocol: "vless", Transport: transport,
		EnginePreference: preference, SelectedEngine: selected,
		RawURI: raw, NativeConfig: proxy, Compatibility: compatibility, Enabled: true,
	}, nil
}

func addTransportOptions(proxy map[string]interface{}, transport string, q url.Values) {
	path, host := q.Get("path"), q.Get("host")
	switch transport {
	case "ws":
		opts := map[string]interface{}{}
		if path != "" {
			opts["path"] = path
		}
		if host != "" {
			opts["headers"] = map[string]string{"Host": host}
		}
		proxy["ws-opts"] = opts
	case "grpc":
		service := q.Get("serviceName")
		if service == "" {
			service = q.Get("service-name")
		}
		proxy["grpc-opts"] = map[string]interface{}{"grpc-service-name": service}
	case "h2":
		opts := map[string]interface{}{}
		if path != "" {
			opts["path"] = path
		}
		if host != "" {
			opts["host"] = splitCSV(host)
		}
		proxy["h2-opts"] = opts
	case "http":
		opts := map[string]interface{}{}
		if path != "" {
			opts["path"] = []string{path}
		}
		if host != "" {
			opts["headers"] = map[string][]string{"Host": []string{host}}
		}
		proxy["http-opts"] = opts
	case "xhttp":
		opts := map[string]interface{}{}
		if path != "" {
			opts["path"] = path
		}
		if host != "" {
			opts["host"] = host
		}
		if mode := q.Get("mode"); mode != "" {
			opts["mode"] = mode
		}
		proxy["xhttp-opts"] = opts
	}
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
