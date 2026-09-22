# 🔧 План реализации: Автономный цикл ИИ-агента ремонта

**Дата:** 2026-09-16  
**Базовый документ:** `AI_ASSISTANT_AUTONOMOUS_REPAIR_IMPLEMENTATION_PLAN_2026-09-16.md`  
**Текущее состояние тестов:** ✅ Все проходят

---

## 📋 Обзор задачи

**Ключевая проблема сегодня:** после неудачного `ApplyAction` + rollback агент завершает работу. Ошибка уходит в никуда. Нет цикла повторного анализа.

**Целевое состояние:** после rollback агенту возвращается ошибка + состояние системы → модель продолжает расследование → готовит новый `ChangeProposal` → пользователь подтверждает → apply → verify → и так до успеха или исчерпания бюджета итераций.

---

## 🏗️ Этапы реализации

### Этап 0 — Рефакторинг: ChangeType и ChangeProposal (Фундамент)
**Файл:** `change_proposal.go` (новый)

**Что делаем:**
1. Определяем `ChangeType` — enum типизированных изменений:
   - `ChangeTunnelRestart` — перезапуск туннеля (target = interface)
   - `ChangeSingboxRestart` — перезапуск sing-box
   - `ChangeMihomoReload` — reload конфигурации mihomo
   - `ChangeEngineSwitch` — переключение движка (target = "mihomo"|"sing-box")
   - `ChangeDNSFlush` — сброс DNS-кэша
   - `ChangeSubscriptionUpdate` — обновление подписки (target = sub-id)
   - `ChangeServiceRestart` — перезапуск произвольного сервиса
   - `ChangeIPTablesRestore` — восстановление iptables из backup
   - `ChangeConfigPatch` — правка конфига (diff/patch в поле Payload)
2. Структура `ChangeProposal`:
   ```go
   type ChangeProposal struct {
       ID          string     `json:"id"`
       RunID       string     `json:"runId"`
       Revision    uint64     `json:"revision"`
       Goal        string     `json:"goal"`
       Changes     []Change   `json:"changes"`
       DiffPreview string     `json:"diffPreview"`
       Risk        string     `json:"risk"`
       Status      string     `json:"status"` // pending|approved|applied|failed|rolled_back
       CreatedAt   time.Time  `json:"createdAt"`
       AppliedAt   *time.Time `json:"appliedAt,omitempty"`
       Error       string     `json:"error,omitempty"`
   }
   
   type Change struct {
       Type    ChangeType `json:"type"`
       Target  string     `json:"target"`
       Payload string     `json:"payload,omitempty"`
       Title   string     `json:"title"`
   }
   ```
3. Функции валидации: `validateChange()` — проверка допустимости target/type
4. Функция `diffPreview()` — текстовое описание того, что произойдёт

**Совместимость:** `RemediationProposal` пока не удаляем — добавляем `ChangeProposal` параллельно. Конвертер `remediationToChange()` для обратной совместимости.

**Тесты (TDD):** `change_proposal_test.go`
- `TestChangeProposalValidation` — допустимые и недопустимые типы/targets
- `TestChangeProposalDiffPreview` — генерация preview
- `TestRemediationToChangeConversion` — конвертация из старого формата

---

### Этап 1 — AgentRun: возобновляемый цикл
**Файл:** `agent_run.go` (новый)

**Что делаем:**
1. Структура `AgentRun`:
   ```go
   type AgentRun struct {
       ID            string           `json:"id"`
       Status        string           `json:"status"` // running|waiting_confirm|done|failed
       Goal          string           `json:"goal"`
       Iteration     int              `json:"iteration"`
       MaxIterations int              `json:"maxIterations"`
       Journal       []JournalEntry   `json:"journal"`
       Proposal      *ChangeProposal  `json:"proposal,omitempty"`
       Snapshot      *SystemSnapshot  `json:"snapshot,omitempty"`
       StartedAt     time.Time        `json:"startedAt"`
       UpdatedAt     time.Time        `json:"updatedAt"`
   }
   
   type JournalEntry struct {
       Time    time.Time `json:"time"`
       Phase   string    `json:"phase"` // diagnose|propose|apply|verify|rollback|retry
       Message string    `json:"message"`
       Error   string    `json:"error,omitempty"`
   }
   ```
2. Метод `(r *AgentRun) AddJournal(phase, message string)`
3. Метод `(r *AgentRun) CanRetry() bool` — проверка iteration < maxIterations
4. Метод `(r *AgentRun) NextIteration()` — инкремент + reset proposal

**Тесты:** `agent_run_test.go`
- `TestAgentRunCanRetry` — лимит итераций
- `TestAgentRunJournal` — корректное логирование

---

### Этап 2 — Snapshot/Rollback
**Файл:** `snapshot.go` (новый)

**Что делаем:**
1. Структура `SystemSnapshot`:
   ```go
   type SystemSnapshot struct {
       ID        string    `json:"id"`
       RunID     string    `json:"runId"`
       CreatedAt time.Time `json:"createdAt"`
       IPTables  string    `json:"iptables,omitempty"`
       Services  map[string]string `json:"services,omitempty"` // name -> status
       Configs   map[string]string `json:"configs,omitempty"`  // path -> content hash
   }
   ```
2. Интерфейс `SnapshotProvider`:
   ```go
   type SnapshotProvider interface {
       TakeSnapshot(ctx context.Context) (*SystemSnapshot, error)
       Rollback(ctx context.Context, snapshot *SystemSnapshot) error
   }
   ```
3. Минимальная реализация-заглушка `NoopSnapshotProvider` (для тестов)
4. Рабочая реализация `ShellSnapshotProvider` (вызывает iptables-save, читает статус сервисов)

**Тесты:** `snapshot_test.go`
- `TestNoopSnapshotRoundTrip`
- `TestSystemSnapshotSerialization`

---

### Этап 3 — Интеграция в Service: цикл продолжения после ошибки
**Файл:** Модификация `service.go`

**Что делаем:**
1. Добавляем поле `activeRun *AgentRun` в `Service`
2. Новый метод `Service.StartAgentRun(goal string) error` — создание AgentRun, первый вызов диагностики
3. Новый метод `Service.ApplyProposal(runID, proposalID string) error`:
   - Проверка `runID` актуальности (защита от stale confirm)
   - Проверка `proposalID` совпадает с текущим
   - Взятие snapshot
   - Применение каждого Change из proposal
   - При ошибке → rollback → **возврат ошибки в модель** (ключевое изменение!)
   - При успехе → верификация → если failed → rollback → возврат в модель
4. Внутренний метод `continueAfterFailure(run *AgentRun, err error, systemState string)`:
   - Формирует контекст: ошибка + stderr + snapshot + journal
   - Вызывает модель с этим контекстом
   - Модель выдаёт новый ChangeProposal или отказ
   - Если новый proposal — статус → `waiting_confirm`
   - Если отказ — статус → `failed` с итоговым отчётом
5. Модификация `ApplyAction()` — теперь при ошибке вызывает `continueAfterFailure()` вместо завершения

**Совместимость:** Старый `ApplyAction(id)` продолжает работать для legacy proposals. Новый путь через `ApplyProposal(runID, proposalID)`.

**Тесты:** Модификация `remediation_test.go` и новые:
- `TestApplyFailureTriggersRetryLoop` — после ошибки модель получает контекст и выдаёт новый proposal
- `TestRetryLoopRespectsMaxIterations` — не зацикливается бесконечно
- `TestStaleConfirmationRejected` — старый runID/proposalID отклоняется
- `TestRollbackOnVerificationFailure` — автоматический откат при failed verification

---

### Этап 4 — iptables-restore error scenario
**Файл:** Модификация `tools.go` + новый tool

**Что делаем:**
1. Новый tool `iptables.diagnose`:
   - Парсит вывод `iptables-save`
   - Ищет дубликаты правил, конфликтующие chains
   - Специально обрабатывает "line N failed" — парсит номер строки
2. Новый tool `iptables.fix`:
   - Принимает стратегию: `flush_chain`, `remove_duplicates`, `restore_backup`
   - Каждая стратегия — типизированный Change
3. Сценарий в модели:
   - При ошибке "iptables-restore line 68 failed" → автоматический вызов `iptables.diagnose`
   - По результату — `iptables.fix` с конкретной стратегией

**Тесты:** `tools_test.go` расширение
- `TestIPTablesDiagnoseParseError`
- `TestIPTablesFixStrategy`

---

### Этап 5 — Живой журнал (Action Journal) в чат
**Файл:** Модификация `service.go`

**Что делаем:**
1. Каждая фаза AgentRun (`diagnose`, `propose`, `apply`, `verify`, `rollback`, `retry`) создаёт `ChatMessage` с role=`system`
2. Формат сообщений:
   - 🔍 `[Диагностика]` Обнаружено: ...
   - 📋 `[Предложение]` Изменение #N: ...
   - ⚡ `[Применение]` Выполняется: ...
   - ✅ `[Верификация]` Проверка пройдена / ❌ Проверка не пройдена
   - ↩️ `[Откат]` Восстановлено из snapshot ...
   - 🔄 `[Повтор]` Итерация N/M, ошибка: ...
3. Сообщения добавляются в `state.Messages` через `appendChatMessage()`

**Тесты:**
- `TestJournalAppearsInChatMessages`

---

### Этап 6 — Защита от повторного применения и устаревших подтверждений
**Файл:** Модификация `service.go`

**Что делаем:**
1. Каждый `ChangeProposal` имеет уникальный `ID` + `Revision`
2. `ApplyProposal()` проверяет:
   - `proposal.ID` совпадает с переданным
   - `proposal.Status == "pending"` (не applied, не expired)
   - `proposal.CreatedAt` не старше TTL (10 минут, настраивается)
   - `run.ID` совпадает с активным (предотвращает подтверждение от устаревшей сессии)
3. Дедупликация: хеш applied changes → reject если тот же change уже failed в этом run

**Тесты:**
- `TestDuplicateApplyRejected`
- `TestExpiredProposalInRunRejected`
- `TestWrongRunIDRejected`

---

### Этап 7 — Восстановление после перезапуска (persistence)
**Файл:** `agent_run.go` + `service.go`

**Что делаем:**
1. Сериализация `AgentRun` в JSON файл: `/tmp/awgm-agent-run.json`
2. При старте Service — проверка наличия файла:
   - Если есть и `status == "waiting_confirm"` → восстановление
   - Если есть и `status == "running"` → пометка `interrupted`, запись в журнал
3. Очистка файла при `status == "done"` или `status == "failed"`

**Тесты:**
- `TestAgentRunPersistAndRestore`
- `TestInterruptedRunDetection`

---

## 📊 Порядок файлов для создания/изменения

| # | Файл | Действие | Зависит от |
|---|------|----------|------------|
| 1 | `change_proposal.go` | Новый | — |
| 2 | `change_proposal_test.go` | Новый | #1 |
| 3 | `agent_run.go` | Новый | #1 |
| 4 | `agent_run_test.go` | Новый | #3 |
| 5 | `snapshot.go` | Новый | — |
| 6 | `snapshot_test.go` | Новый | #5 |
| 7 | `service.go` | Изменение | #1, #3, #5 |
| 8 | `remediation_test.go` | Расширение | #7 |
| 9 | `tools.go` | Расширение (iptables) | #1 |
| 10 | `tools_test.go` | Расширение | #9 |

---

## ⚠️ Инварианты безопасности

1. **Никаких shell-команд от модели** — только типизированные `ChangeType`
2. **Snapshot обязателен** перед любым apply
3. **Rollback автоматический** при ошибке apply или failed verification
4. **Max 5 итераций** по умолчанию (настраивается)
5. **TTL 10 минут** на подтверждение proposal
6. **Все существующие тесты должны проходить** после каждого этапа
7. **Нет deployment** — только локальная разработка и тесты

---

## ✅ Критерии готовности каждого этапа

- `go test ./internal/aiassistant/ -count=1` проходит
- `go vet ./internal/aiassistant/` без ошибок
- Новые типы/методы задокументированы
- Обратная совместимость со старым `ApplyAction()` сохранена

---

## 📝 Текущее состояние

- [x] Анализ существующего кода завершён
- [x] План составлен
- [ ] Этап 0 — ChangeProposal
- [ ] Этап 1 — AgentRun
- [ ] Этап 2 — Snapshot
- [ ] Этап 3 — Интеграция в Service
- [ ] Этап 4 — iptables scenario
- [ ] Этап 5 — Action Journal
- [ ] Этап 6 — Stale protection
- [ ] Этап 7 — Persistence
