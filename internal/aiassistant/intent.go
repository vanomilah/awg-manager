package aiassistant

import (
	"regexp"
	"strings"
)

type Intent struct {
	Kind   string            `json:"kind"`
	Entity map[string]string `json:"entity,omitempty"`
}

var domainPattern = regexp.MustCompile(`(?i)(?:https?://)?([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+)`)

var diagnosticWords = []string{
	"проверь", "диагност", "не работает", "нет интернет", "ошиб", "сломал", "почини",
	"маршрут", "dns", "туннел", "прокси", "подписк", "subscription", "sing-box", "singbox", "mihomo", "wireguard", "порт",
}

func DetectIntent(question string) Intent {
	trimmed := strings.TrimSpace(question)
	match := domainPattern.FindStringSubmatch(trimmed)
	if len(match) > 1 {
		return Intent{Kind: "domain.inspect", Entity: map[string]string{"domain": strings.ToLower(match[1])}}
	}
	lower := strings.ToLower(trimmed)
	for _, word := range diagnosticWords {
		if strings.Contains(lower, word) {
			return Intent{Kind: "diagnostics.general"}
		}
	}
	return Intent{Kind: "chat"}
}
