// Package watchdog следит за состоянием клиентских прокси-инстансов (FreeTurn и WDTT)
// и перезапускает их при сбоях сессии (TURN 401 / ошибки авторизации VK) либо по
// истечению заданного интервала автопереподключения.
package watchdog

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/awgmproto"
	"github.com/hoaxisr/awg-manager/internal/proxyrt/instancestore"
)

const (
	defaultCheckInterval = 15 * time.Second
	defaultCooldown      = 60 * time.Second
	defaultStartupGrace  = 20 * time.Second
)

// failureSignatures — характерные строки в журнале клиента, указывающие на
// зависшую или сброшенную сессию VK/TURN.
var failureSignatures = []string{
	"TURN Allocate: Allocate error",
	"error 401: Unauthorized",
	"[VK Auth] Multiple auth errors detected",
	"Credentials cache invalidated",
	"Auth error (cache=",
	"vkCallsTerminalLinkError",
	"[VK Auth] Persona burned",
	"Allocate error response",
	"[VKCalls] FAILED",
	"failed to allocate TURN",
	"all streams failed",
	"all streams down",
	"server did not acknowledge client ID",
	"failed to write client ID",
	"Failed to resolve any endpoint",
	"context deadline exceeded",
	"handshake error",
	"DTLS: failed",
	"channel-bind умер",
	"умер - рецикл",
	"ChannelBind rejected",
	"Fatal provider error",
	"No more solve modes available",
	"Relay: сбой",
	"Relay умер",
}

// Manager — срез *manager.Manager, нужный службе watchdog.
type Manager interface {
	Records() []instancestore.Record
	Restart(ctx context.Context, key string, reason string) error
}

// Journal — логирование событий сторожевого таймера.
type Journal interface {
	Info(action, target, message string)
	Warn(action, target, message string)
}

// Deps — зависимости сторожевой службы.
type Deps struct {
	Manager  Manager
	Snapshot func(key string) (awgmproto.State, bool)
	LogTail  func(key string) string
	Journal  Journal

	Now           func() time.Time
	CheckInterval time.Duration
	Cooldown      time.Duration
	StartupGrace  time.Duration
}

// Watchdog — сервис фонового мониторинга и перезапуска клиентов.
type Watchdog struct {
	deps Deps

	mu          sync.Mutex
	lastRestart map[string]time.Time
}

// New создаёт новый экземпляр Watchdog.
func New(deps Deps) *Watchdog {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.CheckInterval <= 0 {
		deps.CheckInterval = defaultCheckInterval
	}
	if deps.Cooldown <= 0 {
		deps.Cooldown = defaultCooldown
	}
	if deps.StartupGrace <= 0 {
		deps.StartupGrace = defaultStartupGrace
	}
	return &Watchdog{
		deps:        deps,
		lastRestart: make(map[string]time.Time),
	}
}

// ParseInterval разбирает строку интервала (on_failure, 30m, 1h, 2h, 4h, 12h).
// 0 означает «только при сбое».
func ParseInterval(s string) time.Duration {
	switch s {
	case "on_failure":
		return 0
	case "30m":
		return 30 * time.Minute
	case "", "1h":
		return 1 * time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "12h":
		return 12 * time.Hour
	default:
		d, err := time.ParseDuration(s)
		if err == nil && d > 0 {
			return d
		}
		return 1 * time.Hour
	}
}

// DetectFailure проверяет последние строки хвоста журнала на сигнатуры сбоев.
func DetectFailure(logTail string) (string, bool) {
	if logTail == "" {
		return "", false
	}
	lines := strings.Split(logTail, "\n")
	// Проверяем до 50 содержательных непустых строк от новых к старым,
	// отслеживая как фатальные ошибки, так и потерю всех воркеров в телеметрии.
	checked := 0
	hasZeroActive := false
	seenTelemetry := false
	for i := len(lines) - 1; i >= 0 && checked < 50; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// Телеметрия WDTT: [СТАТИСТИКА] Активных: N или [СТАТ] Активных: N
		if strings.Contains(line, "[СТАТИСТИКА]") || strings.Contains(line, "[СТАТ]") {
			if !seenTelemetry {
				seenTelemetry = true
				if strings.Contains(line, "Активных: 0") || strings.Contains(line, "активных: 0") {
					hasZeroActive = true
				} else if strings.Contains(line, "Активных:") || strings.Contains(line, "активных:") {
					// Свежая телеметрия сообщает о наличии активных воркеров (> 0):
					// клиент подключён и здоров — не падаем по старым строкам до подключения.
					return "", false
				}
			}
			continue
		}

		checked++
		for _, sig := range failureSignatures {
			if strings.Contains(line, sig) {
				return sig, true
			}
		}
	}
	if hasZeroActive {
		return "Активных: 0 (потеря всех соединений)", true
	}
	return "", false
}

// Check выполняет один цикл проверки всех активных инстансов.
func (w *Watchdog) Check(ctx context.Context) {
	if w.deps.Manager == nil {
		return
	}
	records := w.deps.Manager.Records()
	now := w.deps.Now()

	w.mu.Lock()
	// Очищаем старые ключи, которых больше нет
	activeKeys := make(map[string]bool, len(records))
	for _, rec := range records {
		activeKeys[rec.Key()] = true
	}
	for k := range w.lastRestart {
		if !activeKeys[k] {
			delete(w.lastRestart, k)
		}
	}
	w.mu.Unlock()

	for _, rec := range records {
		if !rec.Enabled {
			continue
		}
		var autoReconnect bool
		var intervalStr string

		switch rec.Kind {
		case instancestore.KindFreeTurnClient:
			if rec.FreeTurnClient == nil || !rec.FreeTurnClient.AutoReconnect {
				continue
			}
			autoReconnect = true
			intervalStr = rec.FreeTurnClient.AutoReconnectInterval
		case instancestore.KindWdttClient:
			if rec.WdttClient == nil || !rec.WdttClient.AutoReconnect {
				continue
			}
			autoReconnect = true
			intervalStr = rec.WdttClient.AutoReconnectInterval
		default:
			continue
		}

		if !autoReconnect {
			continue
		}

		key := rec.Key()

		// 1. Cooldown: избегаем циклического перезапуска при постоянной недоступности
		w.mu.Lock()
		last, hasLast := w.lastRestart[key]
		w.mu.Unlock()
		if hasLast && now.Sub(last) < w.deps.Cooldown {
			continue
		}

		// 2. Проверяем снимок процесса
		if w.deps.Snapshot == nil {
			continue
		}
		snap, ok := w.deps.Snapshot(key)
		if !ok || snap.PID <= 0 {
			// Процесс аварийно завершился или не запущен, а инстанс включён
			reason := "автопереподключение: процесс не запущен или аварийно завершился"
			w.triggerRestart(ctx, key, reason, now)
			continue
		}

		// 3. Окно старта (grace period): даём процессу завершить handshake
		if time.Duration(snap.UptimeS)*time.Second < w.deps.StartupGrace {
			continue
		}

		// 4. Проверка по интервалу времени
		interval := ParseInterval(intervalStr)
		if interval > 0 && time.Duration(snap.UptimeS)*time.Second >= interval {
			reason := fmt.Sprintf("автопереподключение: истёк интервал (%s)", intervalStr)
			if intervalStr == "" {
				reason = "автопереподключение: истёк интервал (1h)"
			}
			w.triggerRestart(ctx, key, reason, now)
			continue
		}

		// 5. Проверка по снимку ошибки процесса
		if snap.LastError != "" {
			if sig, found := DetectFailure(snap.LastError); found {
				reason := fmt.Sprintf("автопереподключение: ошибка в снимке процесса (%s)", sig)
				w.triggerRestart(ctx, key, reason, now)
				continue
			}
		}

		// 6. Проверка по сигнатурам ошибок в журнале
		if w.deps.LogTail != nil {
			tail := w.deps.LogTail(key)
			if sig, found := DetectFailure(tail); found {
				reason := fmt.Sprintf("автопереподключение: обнаружен сбой в журнале (%s)", sig)
				w.triggerRestart(ctx, key, reason, now)
				continue
			}
		}
	}
}

func (w *Watchdog) triggerRestart(ctx context.Context, key, reason string, now time.Time) {
	w.mu.Lock()
	w.lastRestart[key] = now
	w.mu.Unlock()

	if w.deps.Journal != nil {
		w.deps.Journal.Warn("watchdog", key, reason)
	}

	if err := w.deps.Manager.Restart(ctx, key, reason); err != nil {
		if w.deps.Journal != nil {
			w.deps.Journal.Warn("watchdog", key, fmt.Sprintf("ошибка перезапуска: %v", err))
		}
	}
}

// Run запускает фоновый цикл сторожа до отмены контекста.
func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(w.deps.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Check(ctx)
		}
	}
}
