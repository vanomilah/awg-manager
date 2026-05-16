package amneziacp

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DecodeVPNLinkToConf decodes an AmneziaVPN vpn:// payload into a WireGuard / AWG .conf,
// mirroring frontend/src/lib/utils/vpnlink.ts.
func DecodeVPNLinkToConf(input string) (config string, err error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "vpn://") {
		return "", errors.New("ссылка должна начинаться с vpn://")
	}

	encoded := strings.TrimPrefix(trimmed, "vpn://")
	raw, err := base64URLDecode(encoded)
	if err != nil {
		return "", fmt.Errorf("неверный формат vpn:// ссылки: %w", err)
	}

	jsonStr, err := decompressVPNBytes(raw)
	if err != nil {
		return "", err
	}

	return extractConfFromVPNJSON(jsonStr)
}

func base64URLDecode(input string) ([]byte, error) {
	s := strings.TrimSpace(input)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}

func decompressVPNBytes(raw []byte) (string, error) {
	if len(raw) < 5 {
		return "", errors.New("неверный формат vpn:// ссылки")
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		// Legacy (frontend vpnlink.ts): entire blob as UTF-8 JSON.
		return string(raw), nil
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return string(raw), nil
	}
	return string(out), nil
}

func extractConfFromVPNJSON(jsonStr string) (string, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "", errors.New("не удалось распаковать данные")
	}

	containersRaw, ok := data["containers"].([]interface{})
	if !ok || len(containersRaw) == 0 {
		return "", errors.New("конфигурация AWG не найдена в ссылке")
	}

	last := containersRaw[len(containersRaw)-1]
	container, ok := last.(map[string]interface{})
	if !ok {
		return "", errors.New("неверная структура vpn:// данных")
	}

	var proto map[string]interface{}
	if awg, ok := container["awg"].(map[string]interface{}); ok {
		proto = awg
	} else if wg, ok := container["wireguard"].(map[string]interface{}); ok {
		proto = wg
	}
	if proto == nil {
		return "", errors.New("контейнер не содержит AWG или WireGuard конфигурацию")
	}

	config := extractLastConfig(proto["last_config"])
	if config == "" {
		return "", errors.New("ссылка не содержит готовой конфигурации клиента")
	}

	dns1, _ := data["dns1"].(string)
	dns2, _ := data["dns2"].(string)
	if dns1 != "" {
		config = strings.ReplaceAll(config, "$PRIMARY_DNS", dns1)
	}
	if dns2 != "" {
		config = strings.ReplaceAll(config, "$SECONDARY_DNS", dns2)
	}

	return config, nil
}

func extractLastConfig(lastConfig interface{}) string {
	if lastConfig == nil {
		return ""
	}
	switch lc := lastConfig.(type) {
	case string:
		var inner map[string]interface{}
		if err := json.Unmarshal([]byte(lc), &inner); err == nil {
			if s, ok := inner["config"].(string); ok {
				return s
			}
		}
		if strings.Contains(lc, "[Interface]") {
			return lc
		}
	case map[string]interface{}:
		if s, ok := lc["config"].(string); ok {
			return s
		}
	}
	return ""
}
