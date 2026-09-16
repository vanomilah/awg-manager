package xrayconfig

import (
	"encoding/json"
	"strings"
)

// MaskSecret masks a secret string preserving first 4 and last 4 characters if long enough.
func MaskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••••••"
	}
	return s[:4] + "••••••••" + s[len(s)-4:]
}

// Redactor masks sensitive credentials from configurations and logs.
type Redactor struct{}

// NewRedactor creates a new Redactor instance.
func NewRedactor() *Redactor {
	return &Redactor{}
}

// RedactConfig returns a deep clone of ManagedConfig with all secrets securely masked.
func (r *Redactor) RedactConfig(cfg *ManagedConfig) *ManagedConfig {
	if cfg == nil {
		return nil
	}

	clone := &ManagedConfig{
		LogLevel:     cfg.LogLevel,
		StatsEnabled: cfg.StatsEnabled,
	}

	if len(cfg.Extra) > 0 {
		if redactedExtra, err := r.RedactJSON(cfg.Extra); err == nil {
			clone.Extra = redactedExtra
		} else {
			clone.Extra = json.RawMessage(`{}`)
		}
	}

	// Clone inbounds with redacted secrets
	if len(cfg.Inbounds) > 0 {
		clone.Inbounds = make([]Inbound, len(cfg.Inbounds))
		for i, in := range cfg.Inbounds {
			inCopy := in
			if len(in.Clients) > 0 {
				inCopy.Clients = make([]Client, len(in.Clients))
				for ci, client := range in.Clients {
					cCopy := client
					cCopy.UUID = MaskSecret(client.UUID)
					inCopy.Clients[ci] = cCopy
				}
			}
			if in.Reality != nil {
				rCopy := *in.Reality
				if rCopy.PrivateKey != "" {
					rCopy.PrivateKey = "[REDACTED]"
				}
				inCopy.Reality = &rCopy
			}
			if len(in.RawSettings) > 0 {
				if redactedRaw, err := r.RedactJSON(in.RawSettings); err == nil {
					inCopy.RawSettings = redactedRaw
				} else {
					inCopy.RawSettings = json.RawMessage(`{}`)
				}
			}
			clone.Inbounds[i] = inCopy
		}
	}

	// Clone outbounds with redacted secrets
	if len(cfg.Outbounds) > 0 {
		clone.Outbounds = make([]Outbound, len(cfg.Outbounds))
		for i, out := range cfg.Outbounds {
			outCopy := out
			if outCopy.UUID != "" {
				outCopy.UUID = MaskSecret(outCopy.UUID)
			}
			if outCopy.Password != "" {
				outCopy.Password = "[REDACTED]"
			}
			if outCopy.Reality != nil {
				rCopy := *outCopy.Reality
				if rCopy.PrivateKey != "" {
					rCopy.PrivateKey = "[REDACTED]"
				}
				outCopy.Reality = &rCopy
			}
			if len(out.RawSettings) > 0 {
				if redactedRaw, err := r.RedactJSON(out.RawSettings); err == nil {
					outCopy.RawSettings = redactedRaw
				} else {
					outCopy.RawSettings = json.RawMessage(`{}`)
				}
			}
			clone.Outbounds[i] = outCopy
		}
	}

	if len(cfg.Balancers) > 0 {
		clone.Balancers = make([]Balancer, len(cfg.Balancers))
		copy(clone.Balancers, cfg.Balancers)
	}

	if len(cfg.RoutingRules) > 0 {
		clone.RoutingRules = make([]RoutingRule, len(cfg.RoutingRules))
		copy(clone.RoutingRules, cfg.RoutingRules)
	}

	return clone
}

// RedactJSON traverses raw JSON data and masks known sensitive key values.
func (r *Redactor) RedactJSON(data []byte) ([]byte, error) {
	var val interface{}
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, err
	}
	redactedVal := r.redactValue(val)
	return json.Marshal(redactedVal)
}

func isSensitiveKey(kLower string) bool {
	if strings.Contains(kLower, "public") {
		return false
	}
	if kLower == "privatekey" || kLower == "private_key" || kLower == "password" ||
		kLower == "secret" || kLower == "secretkey" || kLower == "secret_key" ||
		kLower == "token" || kLower == "authorization" || kLower == "psk" ||
		kLower == "key" || kLower == "apikey" || kLower == "api_key" ||
		strings.HasSuffix(kLower, "password") || strings.HasSuffix(kLower, "token") ||
		strings.HasSuffix(kLower, "secret") || strings.HasSuffix(kLower, "key") {
		return true
	}
	return false
}

func (r *Redactor) redactValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, item := range val {
			kLower := strings.ToLower(k)
			if isSensitiveKey(kLower) {
				result[k] = "[REDACTED]"
			} else if kLower == "uuid" {
				if strVal, ok := item.(string); ok && len(strVal) > 8 {
					result[k] = MaskSecret(strVal)
				} else {
					result[k] = "[REDACTED]"
				}
			} else if kLower == "id" {
				if strVal, ok := item.(string); ok {
					if len(strVal) > 8 {
						result[k] = MaskSecret(strVal)
					} else {
						result[k] = "[REDACTED]"
					}
				} else {
					result[k] = r.redactValue(item)
				}
			} else {
				result[k] = r.redactValue(item)
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = r.redactValue(item)
		}
		return result
	default:
		return val
	}
}


