package xrayconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// DiffAction indicates the type of change.
type DiffAction string

const (
	ActionAdd    DiffAction = "add"
	ActionRemove DiffAction = "remove"
	ActionModify DiffAction = "modify"
)

// DiffItem describes a single granular change between configurations.
type DiffItem struct {
	Action      DiffAction `json:"action"`
	Category    string     `json:"category"` // "log", "stats", "inbound", "outbound", "client", "routing"
	Target      string     `json:"target"`
	Description string     `json:"description"`
}

// ConfigDiff contains a structured summary of changes between two configs.
type ConfigDiff struct {
	Summary         string     `json:"summary"`
	Items           []DiffItem `json:"items"`
	RestartRequired bool       `json:"restart_required"`
}

// CalculateDiff produces a safe semantic diff between old and new configurations.
func CalculateDiff(oldCfg, newCfg *ManagedConfig) *ConfigDiff {
	diff := &ConfigDiff{
		Items:           make([]DiffItem, 0),
		RestartRequired: false,
	}

	if oldCfg == nil && newCfg == nil {
		diff.Summary = "Конфигурация не изменилась"
		return diff
	}

	if oldCfg == nil {
		diff.Summary = fmt.Sprintf("Создание новой конфигурации Xray (%d inbounds, %d outbounds)", len(newCfg.Inbounds), len(newCfg.Outbounds))
		diff.RestartRequired = true
		for _, in := range newCfg.Inbounds {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionAdd,
				Category:    "inbound",
				Target:      in.Tag,
				Description: fmt.Sprintf("Добавление входящего шлюза %s (порт %d, %s)", in.Tag, in.Port, in.Protocol),
			})
		}
		for _, out := range newCfg.Outbounds {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionAdd,
				Category:    "outbound",
				Target:      out.Tag,
				Description: fmt.Sprintf("Добавление исходящего маршрута %s (%s)", out.Tag, out.Protocol),
			})
		}
		return diff
	}

	if newCfg == nil {
		diff.Summary = "Удаление конфигурации Xray"
		diff.RestartRequired = true
		diff.Items = append(diff.Items, DiffItem{
			Action:      ActionRemove,
			Category:    "profile",
			Target:      "all",
			Description: "Полная остановка и удаление конфигурации",
		})
		return diff
	}

	// 1. Log level
	if oldCfg.LogLevel != newCfg.LogLevel {
		diff.Items = append(diff.Items, DiffItem{
			Action:      ActionModify,
			Category:    "log",
			Target:      "loglevel",
			Description: fmt.Sprintf("Уровень логирования изменен с %q на %q", oldCfg.LogLevel, newCfg.LogLevel),
		})
	}

	// 2. Stats
	if oldCfg.StatsEnabled != newCfg.StatsEnabled {
		state := "включен"
		if !newCfg.StatsEnabled {
			state = "отключен"
		}
		diff.Items = append(diff.Items, DiffItem{
			Action:      ActionModify,
			Category:    "stats",
			Target:      "stats_enabled",
			Description: fmt.Sprintf("Сбор статистики %s", state),
		})
	}

	// 3. Compare Inbounds
	oldInMap := make(map[string]Inbound)
	for _, in := range oldCfg.Inbounds {
		oldInMap[in.Tag] = in
	}

	newInMap := make(map[string]Inbound)
	for _, in := range newCfg.Inbounds {
		newInMap[in.Tag] = in
		if oldIn, exists := oldInMap[in.Tag]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionAdd,
				Category:    "inbound",
				Target:      in.Tag,
				Description: fmt.Sprintf("Добавление входящего шлюза %s (порт %d, %s)", in.Tag, in.Port, in.Protocol),
			})
		} else {
			// Compare inbound attributes
			if oldIn.Listen != in.Listen {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: адрес прослушивания изменен с %q на %q", in.Tag, oldIn.Listen, in.Listen),
				})
			}
			if oldIn.Port != in.Port {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: порт изменен с %d на %d", in.Tag, oldIn.Port, in.Port),
				})
			}
			if oldIn.Protocol != in.Protocol {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: протокол изменен с %s на %s", in.Tag, oldIn.Protocol, in.Protocol),
				})
			}
			if oldIn.Transport != in.Transport {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: транспорт изменен с %s на %s", in.Tag, oldIn.Transport, in.Transport),
				})
			}
			if oldIn.Security != in.Security {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: безопасность изменена с %s на %s", in.Tag, oldIn.Security, in.Security),
				})
			}
			if oldIn.Path != in.Path {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: путь изменен с %q на %q", in.Tag, oldIn.Path, in.Path),
				})
			}
			if oldIn.Host != in.Host {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: хост изменен с %q на %q", in.Tag, oldIn.Host, in.Host),
				})
			}
			if oldIn.UpstreamDevice != in.UpstreamDevice {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: egress-интерфейс изменен с %q на %q", in.Tag, oldIn.UpstreamDevice, in.UpstreamDevice),
				})
			}
			if !reflect.DeepEqual(oldIn.Sniffing, in.Sniffing) {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: изменены параметры сниффинга трафика", in.Tag),
				})
			}
			if !tlsConfigEqual(oldIn.TLS, in.TLS) {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: изменены параметры TLS", in.Tag),
				})
			}
			if !realityConfigEqual(oldIn.Reality, in.Reality) {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: изменены параметры REALITY", in.Tag),
				})
			}
			if !JSONEqual(oldIn.RawSettings, in.RawSettings) {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "inbound",
					Target:      in.Tag,
					Description: fmt.Sprintf("Шлюз %s: изменены raw_settings", in.Tag),
				})
			}

			// Compare clients within inbound
			diff.Items = append(diff.Items, compareClients(in.Tag, oldIn.Clients, in.Clients)...)
		}
	}

	for _, oldIn := range oldCfg.Inbounds {
		if _, exists := newInMap[oldIn.Tag]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionRemove,
				Category:    "inbound",
				Target:      oldIn.Tag,
				Description: fmt.Sprintf("Удаление входящего шлюза %s", oldIn.Tag),
			})
		}
	}

	// 4. Compare Outbounds
	oldOutMap := make(map[string]Outbound)
	for _, out := range oldCfg.Outbounds {
		oldOutMap[out.Tag] = out
	}

	newOutMap := make(map[string]Outbound)
	for _, out := range newCfg.Outbounds {
		newOutMap[out.Tag] = out
		if oldOut, exists := oldOutMap[out.Tag]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionAdd,
				Category:    "outbound",
				Target:      out.Tag,
				Description: fmt.Sprintf("Добавление исходящего маршрута %s (%s)", out.Tag, out.Protocol),
			})
		} else {
			// Compare outbound fields
			if oldOut.Protocol != out.Protocol {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: протокол изменен с %s на %s", out.Tag, oldOut.Protocol, out.Protocol),
				})
			}
			if oldOut.Server != out.Server {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: сервер изменен с %q на %q", out.Tag, oldOut.Server, out.Server),
				})
			}
			if oldOut.Port != out.Port {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: порт изменен с %d на %d", out.Tag, oldOut.Port, out.Port),
				})
			}
			if oldOut.Transport != out.Transport {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: транспорт изменен с %s на %s", out.Tag, oldOut.Transport, out.Transport),
				})
			}
			if oldOut.Security != out.Security {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: безопасность изменена с %s на %s", out.Tag, oldOut.Security, out.Security),
				})
			}
			if oldOut.UUID != out.UUID {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: изменен UUID (%s -> %s)", out.Tag, MaskSecret(oldOut.UUID), MaskSecret(out.UUID)),
				})
			}
			if oldOut.Password != out.Password {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: изменен пароль подключения", out.Tag),
				})
			}
			if oldOut.Path != out.Path {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: путь изменен с %q на %q", out.Tag, oldOut.Path, out.Path),
				})
			}
			if oldOut.Host != out.Host {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: хост изменен с %q на %q", out.Tag, oldOut.Host, out.Host),
				})
			}
			if oldOut.SendThrough != out.SendThrough {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: sendThrough изменен с %q на %q", out.Tag, oldOut.SendThrough, out.SendThrough),
				})
			}
			if !tlsConfigEqual(oldOut.TLS, out.TLS) {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: изменены параметры TLS", out.Tag),
				})
			}
			if !realityConfigEqual(oldOut.Reality, out.Reality) {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: изменены параметры REALITY", out.Tag),
				})
			}
			if oldOut.Method != out.Method {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: метод шифрования изменен с %q на %q", out.Tag, oldOut.Method, out.Method),
				})
			}
			if !bytes.Equal(oldOut.RawSettings, out.RawSettings) && !JSONEqual(oldOut.RawSettings, out.RawSettings) {
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "outbound",
					Target:      out.Tag,
					Description: fmt.Sprintf("Маршрут %s: изменены raw_settings", out.Tag),
				})
			}
		}
	}

	for _, oldOut := range oldCfg.Outbounds {
		if _, exists := newOutMap[oldOut.Tag]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionRemove,
				Category:    "outbound",
				Target:      oldOut.Tag,
				Description: fmt.Sprintf("Удаление исходящего маршрута %s", oldOut.Tag),
			})
		}
	}

	// 5. Compare Balancers
	oldBalMap := make(map[string]Balancer)
	for _, b := range oldCfg.Balancers {
		oldBalMap[b.Tag] = b
	}

	newBalMap := make(map[string]Balancer)
	for _, b := range newCfg.Balancers {
		newBalMap[b.Tag] = b
		if _, exists := oldBalMap[b.Tag]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionAdd,
				Category:    "routing",
				Target:      b.Tag,
				Description: fmt.Sprintf("Добавлен балансировщик %q (селекторы: %v)", b.Tag, b.Selector),
			})
		} else if !reflect.DeepEqual(oldBalMap[b.Tag], b) {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionModify,
				Category:    "routing",
				Target:      b.Tag,
				Description: fmt.Sprintf("Изменен балансировщик %q", b.Tag),
			})
		}
	}

	for _, oldB := range oldCfg.Balancers {
		if _, exists := newBalMap[oldB.Tag]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action:      ActionRemove,
				Category:    "routing",
				Target:      oldB.Tag,
				Description: fmt.Sprintf("Удален балансировщик %q", oldB.Tag),
			})
		}
	}

	// 6. Compare Routing Rules
	if len(oldCfg.RoutingRules) != len(newCfg.RoutingRules) {
		diff.Items = append(diff.Items, DiffItem{
			Action:      ActionModify,
			Category:    "routing",
			Target:      "rules",
			Description: fmt.Sprintf("Изменено количество правил маршрутизации: %d -> %d", len(oldCfg.RoutingRules), len(newCfg.RoutingRules)),
		})
	} else {
		for ri := range oldCfg.RoutingRules {
			oldR := oldCfg.RoutingRules[ri]
			newR := newCfg.RoutingRules[ri]
			if !reflect.DeepEqual(oldR, newR) {
				target := newR.OutboundTag
				if target == "" {
					target = newR.BalancerTag
				}
				diff.Items = append(diff.Items, DiffItem{
					Action:      ActionModify,
					Category:    "routing",
					Target:      fmt.Sprintf("rule[%d]", ri),
					Description: fmt.Sprintf("Изменено правило маршрутизации #%d (цель: %s)", ri+1, target),
				})
			}
		}
	}

	if len(diff.Items) == 0 {
		diff.Summary = "Конфигурация не изменилась"
		diff.RestartRequired = false
	} else {
		diff.Summary = fmt.Sprintf("Изменений: %d (требуется перезапуск службы)", len(diff.Items))
		diff.RestartRequired = true
	}

	return diff
}

func clientKey(c Client) string {
	if c.ID != "" {
		return "id:" + c.ID
	}
	if c.Email != "" {
		return "email:" + c.Email
	}
	if c.UUID != "" {
		return "uuid:" + c.UUID
	}
	return "remark:" + c.Remark
}

func compareClients(inboundTag string, oldClients, newClients []Client) []DiffItem {
	var items []DiffItem
	oldMap := make(map[string]Client)
	for _, c := range oldClients {
		oldMap[clientKey(c)] = c
	}

	newMap := make(map[string]Client)
	for _, c := range newClients {
		k := clientKey(c)
		newMap[k] = c
		if oldC, exists := oldMap[k]; !exists {
			displayName := c.Remark
			if displayName == "" {
				displayName = MaskSecret(c.UUID)
			}
			items = append(items, DiffItem{
				Action:      ActionAdd,
				Category:    "client",
				Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
				Description: fmt.Sprintf("Добавлен клиент %q (%s)", displayName, MaskSecret(c.UUID)),
			})
		} else {
			displayName := c.Remark
			if displayName == "" {
				displayName = MaskSecret(c.UUID)
			}
			if oldC.Remark != c.Remark {
				items = append(items, DiffItem{
					Action:      ActionModify,
					Category:    "client",
					Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
					Description: fmt.Sprintf("Имя клиента изменено с %q на %q", oldC.Remark, c.Remark),
				})
			}
			if oldC.Enabled != c.Enabled {
				state := "включен"
				if !c.Enabled {
					state = "отключен"
				}
				items = append(items, DiffItem{
					Action:      ActionModify,
					Category:    "client",
					Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
					Description: fmt.Sprintf("Клиент %q %s", displayName, state),
				})
			}
			if oldC.Level != c.Level {
				items = append(items, DiffItem{
					Action:      ActionModify,
					Category:    "client",
					Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
					Description: fmt.Sprintf("Уровень клиента %q изменен с %d на %d", displayName, oldC.Level, c.Level),
				})
			}
			if oldC.Flow != c.Flow {
				items = append(items, DiffItem{
					Action:      ActionModify,
					Category:    "client",
					Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
					Description: fmt.Sprintf("Flow клиента %q изменен с %q на %q", displayName, oldC.Flow, c.Flow),
				})
			}
			if oldC.UUID != c.UUID {
				items = append(items, DiffItem{
					Action:      ActionModify,
					Category:    "client",
					Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
					Description: fmt.Sprintf("UUID клиента %q изменен (%s -> %s)", displayName, MaskSecret(oldC.UUID), MaskSecret(c.UUID)),
				})
			}
			if oldC.Email != c.Email {
				items = append(items, DiffItem{
					Action:      ActionModify,
					Category:    "client",
					Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
					Description: fmt.Sprintf("Email клиента %q изменен с %q на %q", displayName, oldC.Email, c.Email),
				})
			}
		}
	}

	for _, oldC := range oldClients {
		k := clientKey(oldC)
		if _, exists := newMap[k]; !exists {
			displayName := oldC.Remark
			if displayName == "" {
				displayName = MaskSecret(oldC.UUID)
			}
			items = append(items, DiffItem{
				Action:      ActionRemove,
				Category:    "client",
				Target:      fmt.Sprintf("%s:%s", inboundTag, displayName),
				Description: fmt.Sprintf("Удален клиент %q", displayName),
			})
		}
	}

	return items
}

func tlsConfigEqual(a, b *TLSConfig) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	if a.ServerName != b.ServerName {
		return false
	}
	if !reflect.DeepEqual(a.ALPN, b.ALPN) {
		return false
	}
	return reflect.DeepEqual(a.Certificates, b.Certificates)
}

func realityConfigEqual(a, b *RealityConfig) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	if a.Show != b.Show || a.Target != b.Target || a.Fingerprint != b.Fingerprint {
		return false
	}
	if a.PrivateKey != b.PrivateKey || a.PublicKey != b.PublicKey {
		return false
	}
	if !reflect.DeepEqual(a.ServerNames, b.ServerNames) {
		return false
	}
	return reflect.DeepEqual(a.ShortIDs, b.ShortIDs)
}

// JSONEqual performs a lossless semantic JSON comparison using json.Number
// to prevent float64 precision loss on 64-bit integers and handle unordered map keys.
func JSONEqual(a, b []byte) bool {
	trimmedA := bytes.TrimSpace(a)
	trimmedB := bytes.TrimSpace(b)
	if len(trimmedA) == 0 && len(trimmedB) == 0 {
		return true
	}
	if len(trimmedA) == 0 || len(trimmedB) == 0 {
		return false
	}

	var objA, objB interface{}
	decA := json.NewDecoder(bytes.NewReader(trimmedA))
	decA.UseNumber()
	if err := decA.Decode(&objA); err != nil {
		return bytes.Equal(trimmedA, trimmedB)
	}

	decB := json.NewDecoder(bytes.NewReader(trimmedB))
	decB.UseNumber()
	if err := decB.Decode(&objB); err != nil {
		return bytes.Equal(trimmedA, trimmedB)
	}

	return reflect.DeepEqual(objA, objB)
}
