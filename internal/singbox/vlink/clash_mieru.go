package vlink

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

// mapClashMieru converts a Clash/mihomo "type: mieru" proxy into a
// sing-box Mieru outbound. Clash allows either port or port-range, not both.
func mapClashMieru(p map[string]any) (*ParsedOutbound, error) {
	host := asString(p["server"])
	if host == "" {
		return nil, errors.New("clash mieru: missing server")
	}
	username := asString(p["username"])
	if username == "" {
		return nil, errors.New("clash mieru: missing username")
	}
	password := asString(p["password"])
	if password == "" {
		return nil, errors.New("clash mieru: missing password")
	}
	transport := asString(p["transport"])
	switch transport {
	case "TCP", "UDP":
	default:
		return nil, fmt.Errorf("clash mieru: invalid transport %q", transport)
	}

	_, hasPort := p["port"]
	_, hasRange := p["port-range"]
	if hasPort == hasRange {
		return nil, errors.New("clash mieru: set exactly one of port or port-range")
	}

	var specs []mieruPortSpec
	var primaryPort uint16
	if hasPort {
		portN, ok := asInt(p["port"])
		if !ok || portN < 1 || portN > 65535 {
			return nil, errors.New("clash mieru: missing or invalid port")
		}
		specs = append(specs, mieruPortSpec{Value: fmt.Sprintf("%d", portN), Numeric: true})
		primaryPort = uint16(portN)
	} else {
		rng, err := normalizeMieruPortRange(asString(p["port-range"]))
		if err != nil {
			return nil, fmt.Errorf("clash mieru: %w", err)
		}
		specs = append(specs, mieruPortSpec{Value: rng})
	}

	out := map[string]any{
		"type":      "mieru",
		"server":    host,
		"transport": transport,
		"username":  username,
		"password":  password,
	}
	if primaryPort != 0 {
		out["server_port"] = int(primaryPort)
	} else {
		out["server_ports"] = []string{specs[0].Value}
	}
	if mux := asString(p["multiplexing"]); mux != "" {
		if _, ok := pb.MultiplexingLevel_value[mux]; !ok {
			return nil, fmt.Errorf("clash mieru: invalid multiplexing %q", mux)
		}
		out["multiplexing"] = mux
	}
	if tp := asString(p["traffic-pattern"]); tp != "" {
		out["traffic_pattern"] = tp
	}

	tag := fmt.Sprintf("mieru-%s-%s-%s", sanitizeTagPart(host), strings.ToLower(transport), mieruPortTagPart(primaryPort, specs))
	out["tag"] = tag
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return &ParsedOutbound{
		Tag:      tag,
		Protocol: "mieru",
		Server:   host,
		Port:     primaryPort,
		Outbound: raw,
		Label:    asString(p["name"]),
	}, nil
}
