# Ревью плана исправления Mihomo Stage 2

**Дата:** 2026-09-15  
**Проверенный документ:** `implementation_plan.md` — *Mihomo Stage 2 Remediation Plan: Transactional Durability, Verifiable Rollback & Process Identity*  
**Рабочая копия:** `E:\AWGM\awg-manager`  
**Ветка:** `feature/mihomo-ai-proxyrt`  
**Основание:** `MIHOMO_STAGE2_WALKTHROUGH_AUDIT_2026-09-15.md`

## Вердикт

**План существенно улучшен и правильно нацелен на обнаруженные дефекты, но в текущем виде ещё не должен запускаться в реализацию. Требуется одна обязательная редакция спецификации.**

Он содержательно охватывает все основные темы аудита: durable commit, rollback, draft journal, pending input, process identity, bridge publication, RuntimeOff, path confinement, удаление direct writer, startup gate, recovery API, LKG guard, статус и frontend. Это уже пригодная основа, а не попытка закрыть замечания формально.

Однако несколько обещаний плана невозможно корректно выполнить через существующие интерфейсы. Если агент начнёт кодировать текст буквально, он либо упрётся в архитектурные противоречия, либо создаст реализацию, которая выглядит строгой, но всё ещё не доказывает postconditions.

**Решение:** `CONDITIONALLY APPROVED` — отдавать в работу только после включения поправок P0-A–P0-I ниже непосредственно в `implementation_plan.md`.

## Что план исправляет правильно

1. `verified-active.json` должен становиться durable до перехода manifest в `committed`.
2. Ошибка записи applied record должна оставлять manifest, snapshot и recovery evidence.
3. Rollback должен быть не потребляющим LKG и проверять результат каждого шага.
4. Для первой установки явно предусмотрено удаление невалидного active config при отсутствии LKG.
5. DraftJournal получает четыре состояния и связь с apply transaction.
6. Pending external input предполагается восстанавливать после перезапуска.
7. RuntimeOff превращается в проверяемую транзакцию.
8. Persisted metadata проходит schema/path validation.
9. Direct writer и fallback предполагается удалить.
10. Startup recovery переносится перед фоновым reconcile/scheduler.
11. Небезопасный `clear_marker` удаляется из API.
12. Добавляются recovery evidence, расширенный status и degraded UI.

## P0 — обязательные поправки до начала реализации

### P0-A. ProcessReceipt разделяет ответственность Operator и Coordinator неправильно

План предлагает:

```go
ProcessReceipt() (ProcessReceipt, error)
```

в `Operator`, но одновременно включает в receipt `DaemonEpoch`. Текущий `daemonEpoch` принадлежит `ApplyCoordinator`, а `Operator` о нём не знает. Простое копирование epoch в Operator создаст два источника истины.

Кроме того, `StartedAt time.Time` не является надёжной process identity: достоверным идентификатором повторно использованного PID на Linux является сочетание PID и `/proc/<pid>/stat` field 22.

**Нужно изменить план:**

- Operator возвращает `RuntimeProcessIdentity{PID, ProcStartTicks, Generation, ExecutableIdentity, ConfigDirIdentity}`;
- Coordinator добавляет собственный `DaemonEpoch` и номер applied generation при формировании persisted receipt;
- `ExecutableIdentity` проверяет ожидаемый executable, а не только существование PID;
- парсер `/proc/<pid>/stat` обязан корректно обрабатывать имя процесса в скобках с пробелами/скобками;
- в production Linux невозможность прочитать process identity означает fail-closed;
- тестовая/Windows реализация должна быть отдельным внедряемым probe, а не fallback, который объявляет runtime готовым без доказательства.

### P0-B. RequiredListeners нельзя задавать фиксированным списком

План безусловно требует `51272`, `51271`, mixed `1099/custom` и bridge ports. Это неверно для всех режимов:

- `RuntimeOff` не должен иметь listeners;
- sidecar-конфигурация не обязана содержать transparent-routing ports;
- HTTP/SOCKS/mixed могут быть отключены значением `0`;
- TUN/TPROXY режимы имеют разный фактический набор сокетов;
- bridge listener должен проверяться согласно реальному типу listener, а не автоматически как TCP+UDP;
- порт в `BridgeRef` сейчас отсутствует.

**Нужно изменить план:** `RequiredListeners` выводятся из уже скомпилированной typed-конфигурации (или из результата её разбора), то есть описывают ровно те сокеты, которые кандидат действительно должен открыть. Никаких безусловных портов. Каждый `ListenerSpec` должен включать address/family/protocol/port/purpose/required owner. Отдельно проверяется controller endpoint.

### P0-C. Существующий BridgeRuntime не поддерживает обещанную пошаговую транзакцию

Текущий интерфейс принимает slices, а production `ApplyBridges` в `cmd/awg-manager/mihomo_bridge_runtime.go` игнорирует переданный набор и вызывает общий `manager.Reconcile(ctx, nil)`. Поэтому цикл coordinator «один bridge → persist manifest» сам по себе ничего не гарантирует.

**Нужно изменить план:** сначала переработать контракт на точные операции, например:

```go
PublishBridge(ctx, BridgeRef) (ObservedBridge, error)
WithdrawBridge(ctx, BridgeRef) error
InspectBridge(ctx, BridgeRef) (ObservedBridge, error)
```

или сделать `ReconcileExact` с возвращаемым списком реально выполненных операций. Manifest обновляется только по observed OS/NDMS result. Нужны тесты, доказывающие, что переданный bridge действительно является единственным изменённым объектом.

### P0-D. `s.mu.RLock()` не создаёт coherent snapshot внешних хранилищ

`AssembleCompileInput` читает `SettingsStore`, orchestrator slots, router config, AWG catalog и `mihomonative.Store`. Лок `ServiceImpl` не блокирует независимых владельцев этих данных. Предложенный планом `s.mu.RLock()` создаст лишь ощущение атомарности.

**Нужно выбрать и зафиксировать один реальный протокол:**

1. предпочтительно — optimistic stable read: получить version/digest каждого источника до чтения, собрать deep copy, повторно получить versions/digests и повторять при изменении;
2. либо единый immutable aggregate snapshot, публикуемый владельцем входов;
3. либо общий transaction barrier, который обязаны использовать все writers.

`MihomoCompileInput` должен содержать source version vector, а `InputDigest` вычисляться по canonical deep-copied payload вместе с версиями. Требуется ограниченное число retry и явная ошибка `input changed during snapshot`.

### P0-E. PendingInputRecord не имеет конкретных producers

Фраза «On external input changes» недостаточна. В текущем коде marker никто не пишет. План должен перечислить все mutation seams:

- settings API, влияющий на Mihomo;
- router rules/outbounds/final/DNS;
- orchestrator subscription/tunnel/AWG slots;
- native resources и bridge allocation;
- tunnel lifecycle/catalog changes;
- dynamic cloud CIDR changes, если они входят в compile input.

Для каждого seam требуется порядок: durable marker **до** публикации нового desired state либо единая транзакция с ним; затем convergence; marker удаляется только после совпадения target input digest с verified-active. Простого callback после mutation недостаточно: crash между mutation и marker снова потеряет обновление.

### P0-F. Глобальный degraded gate и recovery allowlist не определены

План перечисляет несколько Mihomo endpoints, но обещает блокировать «all mutable operations». Mihomo input изменяется также через общие settings/router/tunnel handlers и callbacks. Gate только в `MihomoHandler` этого не обеспечивает.

Также безусловная блокировка install/update может сделать recovery невозможным, если причина — отсутствующий или несовместимый binary.

**Нужно изменить план:**

- ввести один общий `MihomoMutationGate`, используемый всеми перечисленными producers и background callbacks;
- описать полный endpoint/callback inventory;
- сделать явный allowlist в recovery mode: read-only status, evidence export и строго проверяемые recovery operations;
- binary repair/update разрешать только как отдельную administrative recovery action с последующей validation, а не как обычный install/update endpoint;
- HTTP 503 должен иметь единый machine-readable envelope и `Retry-After` только если повтор действительно имеет смысл.

### P0-G. Recovery semantics остаются неоднозначными

`rollback_to_lkg` должен откатывать не только файл и процесс. Он обязан привести к одному согласованному поколению:

- active config digest;
- desired/applied store digest;
- applied input digest;
- process receipt;
- exact listeners;
- exact bridge set;
- runtime mode.

Если LKG относится к старому store snapshot/input, одного config-файла недостаточно. План должен хранить рядом с LKG полный `AppliedGenerationRecord` и ссылку на непотребляемый store snapshot либо честно объявлять такой rollback невозможным и оставаться в `recovery_required`.

`regenerate_from_desired` должен использовать тот же стабильный compile snapshot protocol, а не обходить его.

`force` следует удалить либо строго определить. Он не может разрешать пропуск schema, digest, process, listener или bridge checks.

### P0-H. Evidence export может сам раскрыть секреты

Редактирование только полей с названиями password/key недостаточно. Секреты могут находиться в URL, raw JSON/YAML, headers, query strings, subscription payload и logs. `RecentLogs []string` особенно опасен.

**Нужно изменить план:**

- экспортировать typed allowlisted DTO, а не сырые persisted документы;
- вместо raw config давать digest, размер, schema/version и безопасные structural facts;
- пути сокращать до basename;
- логи либо исключить, либо пропускать через централизованный redactor с тестовыми canary secrets;
- response пометить `Cache-Control: no-store` и `Content-Disposition: attachment`;
- добавить тест, который размещает один секрет во всех поддерживаемых формах и проверяет отсутствие plaintext и encoded variants.

### P0-I. Тестовый план не соответствует заявленной полноте

В тексте заявлено закрытие 12 P0, 6 P1 и ранее заявленной матрицы из 64 сценариев, но перечислено только 18 тестов. Это тематические happy/failure tests, а не exhaustive fault-injection matrix.

**Нужно изменить план:**

- создать таблицу всех durable/OS boundaries и инъекций `before`, `during/ambiguous`, `after`;
- покрыть каждый manifest state crash/restart;
- покрыть повторный recovery, idempotency и concurrent mutation/cancel;
- покрыть PID reuse, stale exit callback и listener, принадлежащий чужому процессу;
- покрыть TCP/UDP, IPv4/IPv6 и sidecar/primary/TUN/TPROXY/RuntimeOff;
- покрыть частично опубликованные bridges;
- покрыть settings/slot/native mutation crash между desired write и pending marker;
- добавить `go test -race` для coordinator/dynamic engine/bridge runtime;
- превратить zero-writer grep в исполняемый тест/script с ненулевым exit code, а не ручной просмотр вывода;
- не заявлять число 64, пока в документе нет трассируемой таблицы из 64 реально реализуемых сценариев.

## P1 — уточнения реализации

### P1-A. State machine должна быть формально определена

Недостаточно метода `transitionManifestLocked`. Нужна таблица допустимых переходов, terminal states и commit point. Каждая запись manifest включает monotonic revision/sequence. Recovery определяет действие по persisted state + observed facts, а не только по enum.

Нельзя вызывать переход в `recovery_required`, если запись самого manifest сломалась, как будто этот переход сохранён. В таком случае отдельно durable пишется recovery marker с минимальными безопасными данными, а API сообщает persistence ambiguity.

### P1-B. Cleanup после commit тоже требует политики

Ошибка удаления snapshot/manifest после доказанного commit не должна откатывать успешно работающий runtime, но не должна игнорироваться. Она должна:

- сохранить committed truth;
- зарегистрировать `cleanup_pending`/maintenance warning;
- повторяться идемпотентно при старте;
- не удалять единственную recovery копию до подтверждения retention policy.

### P1-C. LKG следует хранить как immutable generation bundle

Один перезаписываемый `config.yaml.lkg` не позволяет надёжно связать конфиг с store/input/process/bridges. Предпочтительна директория поколения с manifest и immutable artifacts; current LKG — только атомарный pointer/index на проверенное поколение.

### P1-D. Migration journal расположен не в том слое

`MigrateLegacyMihomoResources` в compiler package не владеет файловой транзакцией и сейчас имеет интерфейс только с отдельными `ImportLegacyGroups/Rules`. Для rollback требуется snapshot/restore или единый `ImportLegacyAtomic`.

Журнал должен находиться у migration coordinator/store, иметь source digest, target digest, imported identities, состояние rollback/recovery_required и crash recovery. `context.Context` должен реально проверяться.

### P1-E. Удаление direct writer должно быть полным

Лучше удалить production `GenerateMihomoConfig` и `OnMihomoReload`, чем оставить метод, который всегда возвращает ошибку. Необходимо обновить все callbacks и тесты. Единственный production путь записи active config — coordinator через `strictfs`.

### P1-F. API/interface изменения должны быть запланированы явно

Текущий `NativeMutationApplier` не содержит `CheckMutationAllowed` и `ExportEvidence`; `CoordinatorConfig.Operator` — concrete `*Operator`. План должен заранее определить новые интерфейсы, тестовые fakes и error mapping, иначе изменения расползутся по слоям.

`MihomoStatusSnapshot` должен получать атомарный coordinator snapshot одним методом, а не несколькими независимыми getter-вызовами, иначе status может смешать разные поколения.

## Исправленный порядок выполнения

1. Добавить red tests и формальную таблицу state/boundary/failure outcomes.
2. Определить интерфейсы `ProcessProbe`, exact `BridgeRuntime`, coordinator status snapshot и global mutation gate.
3. Определить immutable generation bundle, LKG semantics и recovery postconditions.
4. Реализовать schema/path validation и strict transition persistence.
5. Исправить commit, RuntimeOff и cleanup-pending semantics.
6. Реализовать проверяемый rollback и administrative recovery.
7. Реализовать mode-aware listener derivation и Linux socket ownership.
8. Реализовать stable compile-input snapshot и durable pending-input producers.
9. Реализовать four-state DraftJournal и связать его с реальным apply TxID.
10. Переработать bridge runtime на exact incremental operations.
11. Удалить direct writer/fallback и исправить synchronous startup ordering.
12. Подключить global degraded gate ко всем mutation paths и background workers.
13. Реализовать safe evidence DTO, OpenAPI, frontend status/recovery UX.
14. Выполнить полную fault-injection/race/restart/real-binary проверку.

## Критерии принятия исправленного плана

План можно запускать в работу, когда в нём явно присутствуют:

- разделение operator process identity и coordinator daemon epoch;
- вывод required listeners из фактической compiled config по режиму;
- exact bridge mutation API;
- настоящий multi-source stable snapshot protocol;
- полный список pending-input producers с write ordering;
- глобальный mutation gate и recovery allowlist;
- generation-bundle semantics для LKG/rollback;
- allowlisted redacted evidence format;
- трассируемая fault-injection matrix вместо декларации о 18/64 тестах;
- однозначное определение `force`, cleanup-pending и persistence ambiguity.

После этих правок план можно отдавать агенту поэтапно. Реализацию следует принимать не одним большим walkthrough, а по четырём независимым воротам:

1. durable filesystem/state machine;
2. process/listener/bridge proof;
3. input/draft convergence and global gating;
4. API/frontend плюс полный restart/fault-injection acceptance.

## Итог для другого агента

Не начинать с массового редактирования `coordinator.go`. Сначала обновить сам `implementation_plan.md` согласно P0-A–P0-I и добавить тестовую матрицу. Иначе текущие интерфейсы заставят либо ослабить заявленные гарантии по ходу реализации, либо повторно переделывать Operator, BridgeRuntime, compile input и recovery API.
