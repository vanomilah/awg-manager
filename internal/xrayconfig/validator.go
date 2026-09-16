package xrayconfig

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidationError represents a specific configuration inconsistency or invalid field.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// AddressesOverlap determines if two host/bind addresses collide on the same port.
// Handles wildcards (0.0.0.0, [::], empty string), loopback aliases, and specific addresses.
func AddressesOverlap(addr1 string, port1 int, addr2 string, port2 int) bool {
	if port1 != port2 {
		return false
	}

	norm1 := normalizeAddr(addr1)
	norm2 := normalizeAddr(addr2)

	// If either is wildcard, they overlap on the same port
	if isWildcard(norm1) || isWildcard(norm2) {
		return true
	}

	return norm1 == norm2
}

func isWildcard(addr string) bool {
	return addr == "0.0.0.0" || addr == "::"
}

func normalizeAddr(addr string) string {
	a := strings.Trim(strings.ToLower(strings.TrimSpace(addr)), "[]")
	if a == "" || a == "0.0.0.0" {
		return "0.0.0.0"
	}
	if a == "::" || a == "::0" || a == "0:0:0:0:0:0:0:0" {
		return "::"
	}
	if a == "localhost" {
		return "127.0.0.1"
	}
	return a
}

func isValidHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Validator verifies semantic and structural consistency of ManagedConfig.
type Validator struct{}

// NewValidator creates a new Validator instance.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate checks the configuration and returns all discovered validation errors.
func (v *Validator) Validate(cfg *ManagedConfig) []ValidationError {
	var errs []ValidationError
	if cfg == nil {
		return append(errs, ValidationError{Field: "config", Message: "конфигурация не может быть пустой"})
	}

	inboundTags := make(map[string]bool)
	outboundTags := make(map[string]bool)
	balancerTags := make(map[string]bool)

	seenClientIDs := make(map[string]string)
	seenClientUUIDs := make(map[string]string)
	seenClientEmails := make(map[string]string)

	// Validate Inbounds
	for i, in := range cfg.Inbounds {
		idxStr := fmt.Sprintf("inbounds[%d]", i)

		// Tag
		tag := strings.TrimSpace(in.Tag)
		if tag == "" {
			errs = append(errs, ValidationError{Field: idxStr + ".tag", Message: "тег входящего соединения обязателен"})
		} else if inboundTags[tag] {
			errs = append(errs, ValidationError{Field: idxStr + ".tag", Message: fmt.Sprintf("дубликат тега входящего соединения: %q", tag)})
		} else {
			inboundTags[tag] = true
		}

		// Port & Overlap
		if in.Port <= 0 || in.Port > 65535 {
			errs = append(errs, ValidationError{Field: idxStr + ".port", Message: fmt.Sprintf("недопустимый порт: %d", in.Port)})
		} else {
			for j := 0; j < i; j++ {
				otherIn := cfg.Inbounds[j]
				if AddressesOverlap(in.Listen, in.Port, otherIn.Listen, otherIn.Port) {
					errs = append(errs, ValidationError{
						Field: idxStr + ".port",
						Message: fmt.Sprintf("конфликт портов: адрес %s:%d пересекается со шлюзом %q (%s:%d)",
							in.Listen, in.Port, otherIn.Tag, otherIn.Listen, otherIn.Port),
					})
					break
				}
			}
		}

		// Protocol capability check
		proto := strings.ToLower(in.Protocol)
		if proto == "" {
			errs = append(errs, ValidationError{Field: idxStr + ".protocol", Message: "протокол обязателен"})
		} else if proto != "vless" && proto != "vmess" && proto != "trojan" && proto != "shadowsocks" && proto != "socks" && proto != "http" && proto != "dokodemo-door" {
			errs = append(errs, ValidationError{Field: idxStr + ".protocol", Message: fmt.Sprintf("неподдерживаемый протокол: %q", in.Protocol)})
		}

		// Transport compatibility check
		trans := strings.ToLower(in.Transport)
		if trans != "" && trans != "tcp" && trans != "raw" && trans != "xhttp" && trans != "ws" && trans != "grpc" && trans != "httpupgrade" {
			errs = append(errs, ValidationError{Field: idxStr + ".transport", Message: fmt.Sprintf("неподдерживаемый транспорт: %q", in.Transport)})
		}

		// Path check for HTTP/WS/XHTTP/HTTPUpgrade transports
		if trans == "ws" || trans == "xhttp" || trans == "httpupgrade" {
			if in.Path != "" && !strings.HasPrefix(in.Path, "/") {
				errs = append(errs, ValidationError{Field: idxStr + ".path", Message: "путь должен начинаться со слэша '/'"})
			}
		}

		// Security compatibility check
		sec := strings.ToLower(in.Security)
		if sec != "" && sec != "none" && sec != "tls" && sec != "reality" {
			errs = append(errs, ValidationError{Field: idxStr + ".security", Message: fmt.Sprintf("неподдерживаемый тип безопасности: %q", in.Security)})
		}

		// REALITY constraints
		if sec == "reality" {
			if proto != "vless" && proto != "trojan" {
				errs = append(errs, ValidationError{Field: idxStr + ".security", Message: "REALITY поддерживается только для протоколов vless и trojan"})
			}
			if trans == "ws" {
				errs = append(errs, ValidationError{Field: idxStr + ".transport", Message: "REALITY не совместим с транспортом WebSocket"})
			}
			if in.Reality == nil {
				errs = append(errs, ValidationError{Field: idxStr + ".reality", Message: "отсутствует блок настроек REALITY"})
			} else {
				if strings.TrimSpace(in.Reality.Target) == "" {
					errs = append(errs, ValidationError{Field: idxStr + ".reality.target", Message: "dest/target обязателен для REALITY"})
				}
				if len(in.Reality.ServerNames) == 0 {
					errs = append(errs, ValidationError{Field: idxStr + ".reality.server_names", Message: "не указан ни один serverName для REALITY"})
				}
				if strings.TrimSpace(in.Reality.PrivateKey) == "" {
					errs = append(errs, ValidationError{Field: idxStr + ".reality.private_key", Message: "приватный ключ обязателен для REALITY"})
				}
				for si, sid := range in.Reality.ShortIDs {
					if len(sid) > 16 || !isValidHex(sid) {
						errs = append(errs, ValidationError{
							Field:   fmt.Sprintf("%s.reality.short_ids[%d]", idxStr, si),
							Message: fmt.Sprintf("shortId %q должен быть hex строкой длиной до 16 символов", sid),
						})
					}
				}
			}
		}

		// Server TLS constraints
		if sec == "tls" {
			if in.TLS == nil {
				errs = append(errs, ValidationError{Field: idxStr + ".tls", Message: "отсутствует блок настроек TLS для входящего соединения"})
			} else if len(in.TLS.Certificates) == 0 {
				errs = append(errs, ValidationError{Field: idxStr + ".tls.certificates", Message: "для серверного TLS требуется как минимум один сертификат"})
			} else {
				for ci, crt := range in.TLS.Certificates {
					if strings.TrimSpace(crt.CertFile) == "" || strings.TrimSpace(crt.KeyFile) == "" {
						errs = append(errs, ValidationError{
							Field:   fmt.Sprintf("%s.tls.certificates[%d]", idxStr, ci),
							Message: "для сертификата TLS должны быть указаны certificate_file и key_file",
						})
					}
				}
			}
		}

		// RawSettings check
		if len(in.RawSettings) > 0 {
			var rawMap map[string]interface{}
			if err := json.Unmarshal(in.RawSettings, &rawMap); err != nil {
				errs = append(errs, ValidationError{Field: idxStr + ".raw_settings", Message: fmt.Sprintf("некорректный JSON в raw_settings: %v", err)})
			}
		}

		// VLESS client validation
		if proto == "vless" {
			if len(in.Clients) == 0 && len(in.RawSettings) == 0 {
				errs = append(errs, ValidationError{Field: idxStr + ".clients", Message: "для входящего шлюза VLESS требуется указать хотя бы одного клиента"})
			}
			activeClients := 0
			for ci, client := range in.Clients {
				cIdxStr := fmt.Sprintf("%s.clients[%d]", idxStr, ci)
				uuid := strings.TrimSpace(client.UUID)
				if uuid == "" {
					errs = append(errs, ValidationError{Field: cIdxStr + ".uuid", Message: "UUID клиента обязателен"})
				} else if !uuidRegex.MatchString(uuid) {
					errs = append(errs, ValidationError{Field: cIdxStr + ".uuid", Message: fmt.Sprintf("недопустимый формат UUID (RFC 4122): %q", client.UUID)})
				} else {
					if prevTag, dup := seenClientUUIDs[uuid]; dup {
						errs = append(errs, ValidationError{
							Field:   cIdxStr + ".uuid",
							Message: fmt.Sprintf("дубликат UUID клиента: %q уже используется в шлюзе %q", client.UUID, prevTag),
						})
					} else {
						seenClientUUIDs[uuid] = in.Tag
					}
				}

				if client.ID != "" {
					if prevTag, dup := seenClientIDs[client.ID]; dup {
						errs = append(errs, ValidationError{
							Field:   cIdxStr + ".id",
							Message: fmt.Sprintf("дубликат ID клиента: %q уже используется в шлюзе %q", client.ID, prevTag),
						})
					} else {
						seenClientIDs[client.ID] = in.Tag
					}
				}

				if client.Email != "" {
					emailLower := strings.ToLower(client.Email)
					if prevTag, dup := seenClientEmails[emailLower]; dup {
						errs = append(errs, ValidationError{
							Field:   cIdxStr + ".email",
							Message: fmt.Sprintf("дубликат email клиента: %q уже используется в шлюзе %q", client.Email, prevTag),
						})
					} else {
						seenClientEmails[emailLower] = in.Tag
					}
				}

				if client.Enabled {
					activeClients++
				}
			}
			if len(in.Clients) > 0 && activeClients == 0 {
				errs = append(errs, ValidationError{Field: idxStr + ".clients", Message: "хотя бы один клиент должен быть активен"})
			}
		}
	}

	// Validate Outbounds
	for i, out := range cfg.Outbounds {
		idxStr := fmt.Sprintf("outbounds[%d]", i)
		tag := strings.TrimSpace(out.Tag)
		if tag == "" {
			errs = append(errs, ValidationError{Field: idxStr + ".tag", Message: "тег исходящего соединения обязателен"})
		} else if outboundTags[tag] {
			errs = append(errs, ValidationError{Field: idxStr + ".tag", Message: fmt.Sprintf("дубликат тега исходящего соединения: %q", tag)})
		} else {
			outboundTags[tag] = true
		}

		proto := strings.ToLower(out.Protocol)
		if proto == "" {
			errs = append(errs, ValidationError{Field: idxStr + ".protocol", Message: "протокол исходящего соединения обязателен"})
		} else if proto != "freedom" && proto != "blackhole" && proto != "vless" && proto != "vmess" && proto != "trojan" && proto != "shadowsocks" && proto != "socks" && proto != "http" {
			if len(out.RawSettings) == 0 {
				errs = append(errs, ValidationError{Field: idxStr + ".protocol", Message: fmt.Sprintf("неподдерживаемый протокол исходящего соединения: %q", out.Protocol)})
			}
		}

		// Server and port validation for protocols requiring them
		if proto == "vless" || proto == "vmess" || proto == "trojan" || proto == "shadowsocks" || proto == "socks" || proto == "http" {
			if len(out.RawSettings) == 0 {
				if strings.TrimSpace(out.Server) == "" {
					errs = append(errs, ValidationError{Field: idxStr + ".server", Message: fmt.Sprintf("адрес сервера обязателен для протокола %s", proto)})
				}
				if out.Port <= 0 || out.Port > 65535 {
					errs = append(errs, ValidationError{Field: idxStr + ".port", Message: fmt.Sprintf("недопустимый порт сервера: %d", out.Port)})
				}
			}
		}

		// Transport compatibility check
		trans := strings.ToLower(out.Transport)
		if trans != "" && trans != "tcp" && trans != "raw" && trans != "xhttp" && trans != "ws" && trans != "grpc" && trans != "httpupgrade" {
			errs = append(errs, ValidationError{Field: idxStr + ".transport", Message: fmt.Sprintf("неподдерживаемый транспорт: %q", out.Transport)})
		}

		// Security compatibility check
		sec := strings.ToLower(out.Security)
		if sec != "" && sec != "none" && sec != "tls" && sec != "reality" {
			errs = append(errs, ValidationError{Field: idxStr + ".security", Message: fmt.Sprintf("неподдерживаемый тип безопасности: %q", out.Security)})
		}

		// UUID format check for VLESS/VMess outbounds
		if (proto == "vless" || proto == "vmess") && len(out.RawSettings) == 0 {
			if strings.TrimSpace(out.UUID) == "" {
				errs = append(errs, ValidationError{Field: idxStr + ".uuid", Message: fmt.Sprintf("UUID обязателен для исходящего соединения %s", proto)})
			} else if !uuidRegex.MatchString(out.UUID) {
				errs = append(errs, ValidationError{Field: idxStr + ".uuid", Message: fmt.Sprintf("недопустимый формат UUID (RFC 4122): %q", out.UUID)})
			}
		}

		// Trojan password check
		if proto == "trojan" && len(out.RawSettings) == 0 {
			if strings.TrimSpace(out.Password) == "" {
				errs = append(errs, ValidationError{Field: idxStr + ".password", Message: "пароль обязателен для исходящего соединения trojan"})
			}
		}

		// Shadowsocks method and key validation
		if proto == "shadowsocks" && len(out.RawSettings) == 0 {
			if strings.TrimSpace(out.Password) == "" {
				errs = append(errs, ValidationError{Field: idxStr + ".password", Message: "пароль/ключ обязателен для исходящего соединения shadowsocks"})
			}
			method := strings.ToLower(strings.TrimSpace(out.Method))
			if method == "" {
				method = "2022-blake3-aes-128-gcm"
			}
			switch method {
			case "2022-blake3-aes-128-gcm":
				rawKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out.Password))
				if err != nil || len(rawKey) != 16 {
					errs = append(errs, ValidationError{
						Field:   idxStr + ".password",
						Message: "для метода 2022-blake3-aes-128-gcm требуется ключ base64 длиной ровно 16 байт",
					})
				}
			case "2022-blake3-aes-256-gcm":
				rawKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out.Password))
				if err != nil || len(rawKey) != 32 {
					errs = append(errs, ValidationError{
						Field:   idxStr + ".password",
						Message: "для метода 2022-blake3-aes-256-gcm требуется ключ base64 длиной ровно 32 байта",
					})
				}
			case "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305":
				// Standard AEAD methods
			default:
				errs = append(errs, ValidationError{
					Field:   idxStr + ".method",
					Message: fmt.Sprintf("неподдерживаемый метод шифрования shadowsocks: %q", out.Method),
				})
			}
		}

		// Outbound Reality shortIDs
		if out.Reality != nil {
			for si, sid := range out.Reality.ShortIDs {
				if len(sid) > 16 || !isValidHex(sid) {
					errs = append(errs, ValidationError{
						Field:   fmt.Sprintf("%s.reality.short_ids[%d]", idxStr, si),
						Message: fmt.Sprintf("shortId %q должен быть hex строкой длиной до 16 символов", sid),
					})
				}
			}
		}

		// RawSettings check
		if len(out.RawSettings) > 0 {
			var rawMap map[string]interface{}
			if err := json.Unmarshal(out.RawSettings, &rawMap); err != nil {
				errs = append(errs, ValidationError{Field: idxStr + ".raw_settings", Message: fmt.Sprintf("некорректный JSON в raw_settings: %v", err)})
			}
		}
	}

	// Default "direct" outbound is always present when compiling if outbounds empty
	if len(cfg.Outbounds) == 0 {
		outboundTags["direct"] = true
	}

	// Validate Balancers
	for bi, b := range cfg.Balancers {
		bIdxStr := fmt.Sprintf("balancers[%d]", bi)
		tag := strings.TrimSpace(b.Tag)
		if tag == "" {
			errs = append(errs, ValidationError{Field: bIdxStr + ".tag", Message: "тег балансировщика обязателен"})
		} else if balancerTags[tag] {
			errs = append(errs, ValidationError{Field: bIdxStr + ".tag", Message: fmt.Sprintf("дубликат тега балансировщика: %q", tag)})
		} else {
			balancerTags[tag] = true
		}

		if len(b.Selector) == 0 {
			errs = append(errs, ValidationError{Field: bIdxStr + ".selector", Message: "список селекторов балансировщика не может быть пустым"})
		}

		if b.Strategy != nil && b.Strategy.Type != "" {
			st := strings.ToLower(b.Strategy.Type)
			if st != "random" && st != "roundrobin" && st != "leastping" {
				errs = append(errs, ValidationError{Field: bIdxStr + ".strategy.type", Message: fmt.Sprintf("неподдерживаемая стратегия балансировки: %q", b.Strategy.Type)})
			}
		}
	}

	// Validate Routing Rules
	for i, rule := range cfg.RoutingRules {
		idxStr := fmt.Sprintf("routing_rules[%d]", i)

		if rule.OutboundTag != "" && rule.BalancerTag != "" {
			errs = append(errs, ValidationError{Field: idxStr, Message: "правило маршрутизации не может одновременно указывать outbound_tag и balancer_tag"})
		} else if rule.OutboundTag == "" && rule.BalancerTag == "" {
			errs = append(errs, ValidationError{Field: idxStr, Message: "правило маршрутизации должно указывать outbound_tag или balancer_tag"})
		}

		if rule.OutboundTag != "" && !outboundTags[rule.OutboundTag] {
			errs = append(errs, ValidationError{
				Field:   idxStr + ".outbound_tag",
				Message: fmt.Sprintf("ссылка на несуществующий outbound: %q", rule.OutboundTag),
			})
		}

		if rule.BalancerTag != "" && !balancerTags[rule.BalancerTag] {
			errs = append(errs, ValidationError{
				Field:   idxStr + ".balancer_tag",
				Message: fmt.Sprintf("ссылка на несуществующий balancer: %q", rule.BalancerTag),
			})
		}

		for _, inTag := range rule.InboundTag {
			if !inboundTags[inTag] {
				errs = append(errs, ValidationError{
					Field:   idxStr + ".inbound_tag",
					Message: fmt.Sprintf("ссылка на несуществующий inbound: %q", inTag),
				})
			}
		}
	}

	return errs
}

