# План реализации полноценного ИИ-агента ремонта в AWG Manager

Дата: 2026-09-16  
Назначение: передача другому агенту для реализации  
Репозиторий: `E:\AWGM\awg-manager`

## 1. Цель

Превратить текущий ИИ-помощник из чата с диагностическими командами и заранее определёнными кнопками в **возобновляемого системного агента**, который способен:

1. самостоятельно собрать факты;
2. локализовать неисправность до конкретного файла, правила, процесса или настройки;
3. подготовить минимальное исправление и показать точный diff/команды;
4. запросить подтверждение пользователя для изменения;
5. безопасно применить изменение со snapshot и rollback;
6. проверить результат;
7. при неудаче автоматически продолжить анализ с новым stderr и состоянием системы;
8. завершить работу только после доказанного исправления либо честного определения внешнего/исходного блокера.

Не давать модели бесконтрольный root-shell. Полноценность достигается не отсутствием ограничений, а богатым набором инструментов, транзакциями, подтверждениями и замкнутым циклом обратной связи.

## 2. Почему текущая реализация выглядит как набор кнопок

### 2.1. Tool-loop и remediation разорваны

`internal/aiassistant/service.go:1059-1156` выполняет только модельный цикл диагностических инструментов. Когда модель создаёт `remediation.propose`, предложение показывается пользователю, но фактическое действие выполняется позднее через отдельный `ApplyAction` (`service.go:511-568`).

После применения:

- stderr/verification не возвращаются в историю tool calls;
- модель не получает новый turn;
- при rollback добавляется шаблонное сообщение «изменение не удалось применить»;
- расследование заканчивается, хотя появился самый важный новый факт.

Именно это произошло на скриншоте с `iptables-restore: line 68 failed`.

### 2.2. Исправление описывается непрозрачной строкой

`command.exec` использует поле `target` как произвольную командную строку. Пользователь не видит структурированный план изменения, затрагиваемые ресурсы, diff, preconditions и проверки.

### 2.3. Нет инструментов подготовки настоящего patch

Есть безопасное чтение и диагностические команды, но нет полного протокола:

- прочитать конкретный диапазон строк;
- получить metadata/digest файла;
- подготовить patch;
- проверить patch без записи;
- создать snapshot;
- атомарно применить;
- проверить и откатить.

### 2.4. Повтор одинакового tool call блокируется глобальным `seen`

В `runNativeToolLoop` одинаковый вызов отклоняется как повторный. После изменения системы повторная проверка той же командой является обязательной, но текущая логика воспринимает её как ошибку. Дедупликация должна учитывать epoch состояния системы.

## 3. Границы первой версии

Первая версия должна уметь ремонтировать:

- AWG Manager и его управляемые конфиги;
- маршрутизацию Sing-box/Mihomo;
- TPROXY/TUN/FakeIP state;
- iptables/ip6tables/nftables/ip rule/ip route;
- init.d-сервисы Entware;
- конфиги управляемых приложений в `/opt/etc`;
- DNS, локальные порты, процессы и пакеты opkg.

Первая версия **не должна**:

- переписывать бинарник AWG Manager на роутере;
- автоматически обновлять firmware;
- менять загрузчик, разделы, SSH/auth/firewall управления без отдельного аварийного протокола;
- выполнять второе mutation-действие после неудачи без нового подтверждения пользователя;
- скрывать от пользователя команды, diff, результат проверки или rollback.

Если причина находится в Go/Svelte-коде установленной версии, агент обязан собрать воспроизводимый incident: версия, симптомы, stderr, проблемный generated artifact, ожидаемое/фактическое состояние. Он не должен притворяться, что способен исправить дефект бинарника локальной командой.

## 4. Целевая архитектура

### 4.1. Durable Agent Run

Добавить пакет `internal/aiassistant/runner` либо логически изолированный модуль в `internal/aiassistant`.

Основная сущность:

```go
type AgentRun struct {
    ID              string
    Revision        uint64
    State           RunState
    Question        string
    Goal            string
    SystemEpoch     uint64
    Exchanges       []ModelToolExchange
    Steps           []AgentStep
    PendingProposal *ChangeProposal
    LastAction      *ActionAttempt
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

Состояния:

```text
investigating
plan_ready
awaiting_approval
applying
verifying
replanning
resolved
blocked
failed
recovery_required
cancelled
```

Run должен durably сохраняться атомарно. После рестарта AWG Manager он либо продолжается с безопасной точки, либо явно переходит в `recovery_required`; нельзя терять pending snapshot или показывать старую кнопку подтверждения.

### 4.2. Единый цикл агента

Заменить разрыв между `runNativeToolLoop` и `ApplyAction` следующим процессом:

```text
user message
  -> model/tool investigation
  -> model produces structured ChangeProposal
  -> server validates and previews proposal
  -> awaiting_approval
  -> user confirms exact proposal revision
  -> snapshot + preflight
  -> apply
  -> verify
  -> result is appended as tool result
  -> model receives next turn automatically
     -> resolved OR proposes another investigation/action
```

После rollback модель получает:

- полный нормализованный stderr;
- verification result;
- rollback result;
- изменившийся system epoch;
- ссылки на сохранённые evidence artifacts.

Она обязана объяснить новую причину или выполнить дополнительные read-only проверки. Следующее изменение требует отдельного подтверждения.

### 4.3. System epoch вместо вечной дедупликации

Tool call key должен включать `SystemEpoch`. Epoch увеличивается после каждой попытки mutation, rollback, service restart и внешнего refresh состояния.

Одинаковая команда запрещается только в пределах одного неизменившегося epoch. После применения/rollback повторный `iptables -S`, `engine.status` или DNS test разрешён и ожидаем.

## 5. Новый контракт инструментов

### 5.1. Read-only инструменты

Сохранить существующие специализированные tools и добавить:

- `system.file.stat_safe(path)` — тип, размер, mode, owner, digest, symlink status;
- `system.file.read_range(path, startLine, endLine)` — ограниченный фрагмент с нумерацией;
- `system.file.search(pattern, roots, maxResults)` — только разрешённые roots;
- `system.command.run_readonly(argv, timeout, outputLimit)` — структурированный argv без shell interpolation;
- `netfilter.snapshot(family, table)`;
- `netfilter.render_managed(mode)` — generated restore input без применения;
- `netfilter.explain_error(artifactID, stderr)` — сопоставление `line N` с нумерованной строкой и контекстом;
- `routing.snapshot_full()`;
- `service.logs(name, since, limit)`;
- `config.owner(path)` — какой компонент управляет файлом и можно ли его редактировать напрямую.

Результат каждого tool должен иметь:

```go
type ToolResult struct {
    Status       string
    Summary      string
    Stdout       string
    Stderr       string
    ExitCode     *int
    Truncated    bool
    ArtifactRefs []string
    StartedAt    time.Time
    FinishedAt   time.Time
    SystemEpoch  uint64
}
```

Не упаковывать существенный stderr в одну короткую строку UI.

### 5.2. Планирование изменений

Вместо непрозрачного `remediation.propose(action,target)` ввести:

```go
type ChangeProposal struct {
    ID             string
    Revision       uint64
    Goal           string
    Reason         string
    Risk           RiskLevel
    Operations     []ChangeOperation
    Preconditions  []CheckSpec
    Verifications  []CheckSpec
    Rollback       RollbackPlan
    Affected       []ResourceRef
    Preview        []DiffPreview
    ExpiresAt      time.Time
}
```

Поддерживаемые операции:

- `managed.config.patch`;
- `managed.config.regenerate`;
- `netfilter.apply_managed`;
- `routing.apply_managed`;
- `service.start|stop|restart`;
- `engine.reload|restart|switch`;
- `tunnel.restart`;
- `opkg.install|remove|upgrade`;
- `system.command.confirmed` — только для команд, не покрытых typed operation.

Для `system.command.confirmed` хранить argv, working directory, timeout и expected effects отдельно. Не передавать shell-строку через `target`.

### 5.3. Работа с файлами

Разрешённые roots задаются сервером, а не моделью:

- `/opt/etc/awg-manager`;
- управляемые конфиги `/opt/etc` по registry;
- runtime/state директории AWG Manager;
- явно подключённые paths интеграций.

Протокол patch:

1. `stat_safe` и digest исходника;
2. модель предлагает unified patch либо typed field changes;
3. сервер применяет patch в memory/temp area;
4. parser/validator компонента проверяет candidate;
5. UI показывает semantic diff и raw diff;
6. при подтверждении сервер повторно сверяет исходный digest;
7. durable snapshot;
8. atomic replace + fsync;
9. reload/restart;
10. verification;
11. commit либо rollback.

Запретить редактирование generated runtime-файла, если его владельцем является генератор. Агент должен изменить source-of-truth и вызвать regenerate.

## 6. Транзакционный Action Engine

Расширить `internal/aiassistant/remediation.go`, не добавляя ещё больше switch-веток в `Service`.

Интерфейс:

```go
type TransactionalOperation interface {
    Validate(context.Context) error
    Preview(context.Context) (*OperationPreview, error)
    Snapshot(context.Context) (*ResourceSnapshot, error)
    Apply(context.Context) (*OperationResult, error)
    Verify(context.Context) (*VerificationResult, error)
    Rollback(context.Context, *ResourceSnapshot) (*RollbackResult, error)
}
```

Обязательные свойства:

- optimistic concurrency по digest/revision;
- один mutation-run одновременно;
- durable action journal до первого side effect;
- timeout/cancellation не уничтожает recovery information;
- rollback проверяется отдельно;
- ошибка rollback переводит run в `recovery_required`;
- все stdout/stderr маскируются от секретов, но сохраняют техническую причину.

## 7. Специализированный ремонт netfilter/TPROXY

Это обязательный вертикальный сценарий первой поставки, потому что он воспроизводит проблему со скриншота.

### 7.1. До применения

Агент должен получить:

- активное routing core/mode;
- managed source-of-truth;
- текущие iptables/ip6tables rules;
- ipset/nft sets и необходимые kernel modules;
- generated `iptables-restore` input как artifact с нумерацией строк;
- capability конкретного `iptables-restore` (`--test` может отсутствовать в BusyBox/Entware).

### 7.2. Валидация

Если `iptables-restore --test` поддерживается — использовать его над candidate.

Если не поддерживается:

- выполнить parser/static validation;
- проверить referenced chains/sets/modules;
- применить через существующий штатный orchestrator с snapshot текущих managed chains;
- установить watchdog/rollback deadline перед изменением;
- не трогать unrelated chains.

### 7.3. Ошибка `line N failed`

Action result обязан сохранить:

- точный restore input;
- строку N и ±5 строк контекста;
- stderr и exit code;
- snapshot до попытки;
- результат rollback;
- состояние chains после rollback.

После этого run автоматически переходит `replanning`, а модель получает evidence. Она может:

- определить отсутствующий ipset/module/несовместимый target;
- предложить изменение source-of-truth;
- обнаружить дефект генератора и сформировать incident для обновления AWG Manager.

Она не должна просто повторять тот же `routing.switch_mode`.

## 8. Approval и безопасность

### 8.1. Уровни риска

- `read`: подтверждение не требуется;
- `low`: reload/flush cache, возможно auto-fix при отдельной настройке;
- `medium`: изменение managed config, restart сервиса — явное подтверждение;
- `high`: firewall/routes/packages/process termination — подтверждение с diff и rollback;
- `critical`: management access, auth, WAN/firewall rescue path, reboot — повторное подтверждение и rescue timer.

### 8.2. Подтверждение привязано к содержимому

Approval token должен включать hash:

- proposal ID/revision;
- operations;
- исходных resource digests;
- preview;
- verification plan;
- expiration.

Если состояние изменилось, старое подтверждение недействительно; агент пересчитывает preview.

### 8.3. Prompt injection

Логи, файлы, имена процессов и ответы внешних сервисов являются недоверенными данными. Их содержимое не может изменять policy tools или просить выполнить команды. Решение о допустимости операции принимает серверный policy engine.

## 9. API

Добавить либо версионировать endpoints:

- `POST /ai/runs` — новый run;
- `GET /ai/runs/{id}` — состояние и timeline;
- `GET /ai/runs/{id}/events` — SSE progress/tool/action events;
- `POST /ai/runs/{id}/messages` — продолжение диалога;
- `POST /ai/runs/{id}/approve` — подтверждение proposal revision/hash;
- `POST /ai/runs/{id}/reject`;
- `POST /ai/runs/{id}/cancel`;
- `GET /ai/runs/{id}/artifacts/{artifactID}` — безопасный просмотр evidence/diff;
- `POST /ai/runs/{id}/recover` — только для явного recovery workflow.

Старые state/analyze/apply endpoints оставить временным compatibility adapter, но UI перевести на run API.

## 10. UI

Переработать `frontend/src/lib/components/system/AIAssistantPanel.svelte` вокруг timeline одного AgentRun.

Показывать:

1. обычные сообщения пользователя и модели;
2. раскрываемые read-only проверки;
3. установленную причину с evidence links;
4. карточку предложения:
   - что изменится;
   - почему;
   - affected resources;
   - semantic/raw diff;
   - риск;
   - preflight/verification/rollback;
5. кнопки «Применить», «Отклонить», «Изменить план»;
6. live-этапы snapshot/apply/verify/rollback;
7. после неудачи — не финальное серое сообщение, а состояние «Продолжаю расследование»;
8. финал только `Исправлено и проверено`, `Нужна ручная работа` или `Требуется восстановление`.

Кнопка отправки должна называться «Отправить», а не всегда «Диагностировать»: обычный разговор не обязан запускать полный diagnostic report.

## 11. Поэтапная реализация

### Этап 0 — зафиксировать baseline и тест проблемы

- Снять текущие API/types/UI snapshots.
- Добавить acceptance test: подтверждённое действие падает с `iptables-restore line 68`; rollback успешен; модель должна получить ошибку и сделать следующий read-only tool call.
- Не менять поведение до появления падающего теста.

### Этап 1 — AgentRun и возобновляемый loop

- Реализовать durable state machine.
- Объединить model tool history и action results.
- После `ApplyAction` автоматически возобновлять run.
- Ввести system epoch и epoch-aware дедупликацию.
- Ограничить число read calls на epoch и mutation attempts на run отдельно.

Критерий: после искусственной ошибки действия модель получает stderr и продолжает расследование без нового сообщения пользователя.

### Этап 2 — структурированные proposals и approval hash

- Ввести `ChangeProposal`, `ChangeOperation`, preconditions/verifications/rollback.
- Compatibility mapping старых action в новые typed operations.
- Убрать `command.exec` из `target`; ввести структурированный confirmed command.
- Добавить preview и stale-proposal rejection.

### Этап 3 — artifacts и безопасная работа с файлами

- Registry владельцев конфигов.
- read range/stat/search;
- candidate patch, validation, diff;
- atomic apply и durable snapshot;
- secret redaction tests.

### Этап 4 — netfilter/TPROXY vertical slice

- render managed rules;
- numbered artifact;
- capability-aware preflight;
- line-N explanation;
- snapshot/apply/verify/rollback;
- automatic replanning.

До завершения этого этапа не заявлять, что агент умеет ремонтировать маршрутизацию.

### Этап 5 — сервисы, движки, туннели, DNS и opkg

- Перевести существующие switch-actions на `TransactionalOperation`.
- Для каждого определить preconditions, expected effects и verification.
- Добавить ownership/conflict checks между Sing-box, Mihomo и NDMS.

### Этап 6 — UI и recovery

- Timeline/SSE;
- полноценные proposal cards;
- diff/artifact viewer;
- restart/resume;
- recovery-required flow.

### Этап 7 — память и playbooks

- Сохранять playbook только после успешной verification.
- Привязывать к версии AWG Manager, firmware, engine и capability fingerprint.
- При несовпадении fingerprint использовать playbook как подсказку, не как автоматическую команду.

## 12. Тестовая стратегия

### Unit

- state transitions/CAS;
- approval hash и expiration;
- command policy и argv parsing;
- path ownership/symlink escape;
- secret redaction;
- epoch-aware duplicate calls;
- action journal recovery;
- rollback failure.

### Provider contract

Для Gemini/OpenAI/DeepSeek/OpenRouter/custom/Ollama:

- tool call round-trip;
- action result как следующий tool result;
- provider error не теряет run;
- malformed arguments rejected server-side;
- модель не может обойти approval.

### Integration

- fake filesystem/process/netfilter adapters;
- apply success;
- apply fail + rollback success + replanning;
- verification fail + rollback;
- process crash в каждой durable фазе;
- restart and resume;
- stale proposal after external state change.

### Router acceptance

На тестовом роутере, без `--force-reinstall`:

1. диагностировать намеренно повреждённое managed TPROXY rule;
2. показать точную строку и diff;
3. дождаться подтверждения;
4. применить;
5. проверить TCP/UDP/DNS и live connections;
6. восстановить исходное состояние;
7. повторить со сбоем применения и доказать rollback + продолжение анализа.

## 13. Главный acceptance scenario по текущему скриншоту

Запрос: «Переключи маршрутизацию на TPROXY».

Ожидаемое поведение:

1. Агент проверяет активное ядро и возможности TPROXY.
2. Рендерит будущие rules без применения.
3. Показывает proposal и просит подтверждение.
4. После подтверждения создаёт snapshot и применяет.
5. `iptables-restore` возвращает `line 68 failed`.
6. Агент автоматически откатывает и доказывает восстановление.
7. Он открывает artifact около строки 68, проверяет referenced set/module/chain и source-of-truth.
8. Если исправима настройка — показывает новый diff и просит новое подтверждение.
9. Если это дефект генератора установленной версии — прямо сообщает об этом и создаёт технический incident, не предлагает повторить заведомо сломанное действие.
10. Диалог остаётся активным; пользователь видит ход расследования.

## 14. Definition of Done

Работа считается завершённой только когда:

- action failure возвращается модели и приводит к следующему осмысленному шагу;
- повторная проверка после mutation разрешена;
- каждое изменение имеет preview, snapshot, verification и rollback;
- подтверждение связано с точной revision изменения;
- модель не может выполнить mutation через read-only tool;
- crash/restart не теряет pending action или rollback data;
- UI различает диагностику, применение, проверку, rollback и продолжение анализа;
- сценарий `iptables-restore line N failed` проходит acceptance test;
- обычное «Привет» получает обычный ответ без обязательного запуска диагностики;
- итог «Исправлено» показывается только после успешной проверки.

## 15. Что не делать

- Не добавлять десятки новых фиксированных кнопок вместо agent loop.
- Не давать модели прямой неаудируемый shell/root.
- Не считать rollback финалом расследования.
- Не редактировать generated файлы вместо source-of-truth.
- Не повторять mutation автоматически после ошибки.
- Не сохранять непроверенный playbook.
- Не собирать IPK и не выполнять deployment в рамках реализации плана, пока пользователь отдельно этого не попросит.
