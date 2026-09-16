# Аудит реализации Mihomo Stage 2 по plan/walkthrough

**Дата проверки:** 2026-09-15  
**Рабочая копия:** `E:\AWGM\awg-manager`  
**Ветка:** `feature/mihomo-ai-proxyrt`  
**HEAD:** `87a563eb feat(mihomo): port VMess and SOCKS5 compilers, routing-mark loop protection, and header version fallback`  
**Проверенные документы:**

- `implementation_plan.md` — *Mihomo Stage 2 — Transactional Compile, Apply, and Rollback (Revised v8)*;
- `walkthrough.md` — утверждение о полном выполнении Stage 2.

## Вердикт

**Walkthrough не принимается. Stage 2 нельзя считать завершённым и нельзя рекомендовать к деплою как транзакционно безопасную реализацию.**

В рабочей копии действительно появились полезные заготовки: `internal/strictfs`, типизированные manifest/journal, чистая функция компиляции, coordinator, snapshot API, degraded-флаг и recovery endpoint. Заявленные пакеты компилируются, их текущие тесты проходят, а representative Mihomo binary acceptance проходит.

Однако основные гарантии плана — атомарный commit, доказуемый rollback, crash recovery, convergence внешних входов, process identity и закрытый degraded lifecycle — в реализации отсутствуют либо являются best-effort. В нескольких местах система записывает `committed` до фиксации authoritative applied record, игнорирует ошибки критических операций и удаляет recovery evidence. Это ровно тот класс отказов, который Stage 2 должен был устранить.

## Независимо выполненные проверки

### Успешно

```text
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/AWGM/awg-manager &&
  go test -count=1 ./internal/strictfs ./internal/mihomonative ./internal/mihomo \
    ./internal/api ./internal/singbox/router ./cmd/awg-manager'
```

Все 6 указанных пакетов прошли.

```text
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/AWGM/awg-manager &&
  bash scripts/run-mihomo-acceptance.sh'
```

`TestGenerateMihomoConfig_RepresentativeBinaryValidation` прошёл с реальным Mihomo `v1.19.29`.

```text
cd frontend && npm run check
```

`svelte-check found 0 errors and 118 warnings in 25 files`.

```text
git diff --check
```

Exit code `0`; имеются только предупреждения о будущей нормализации CRLF/LF.

### Не подтверждено

`npm test` был запущен независимо, но не завершился за 60 секунд и был остановлен таймаутом. Поэтому заявление walkthrough о `198 suites / 1924 tests` текущим аудитом не подтверждено и не опровергнуто.

Критически важнее: перечисленные планом 52 сценария плюс delta 53–64 фактически не реализованы. В `internal/mihomo/coordinator_test.go` присутствуют только пять верхнеуровневых тестов coordinator, а не заявленная fault-injection/crash-recovery матрица.

## P0 — блокирующие дефекты

### P0-1. Commit публикуется раньше authoritative applied record, а ошибки durability игнорируются

В `internal/mihomo/coordinator.go:602-632` coordinator:

1. переводит manifest в `StatePublished`;
2. переводит manifest в `StateCommitted`;
3. игнорирует ошибку записи `verified-active.json`;
4. в любом случае обновляет in-memory `appliedRecord`;
5. удаляет manifest, snapshot, draft и pending marker, также игнорируя ошибки.

Следствия:

- сбой записи `verified-active.json` может вернуть вызывающему коду успех;
- после рестарта durable applied generation останется старой или отсутствующей;
- manifest и snapshot могут быть удалены, то есть recovery evidence потеряется;
- заявленный `commit-pending` режим не существует.

Та же ошибка повторена в RuntimeOff (`coordinator.go:667-690`) и roll-forward recovery (`coordinator.go:304-331`).

**Обязательное исправление:** `verified-active.json` с проверенным `TargetInputDigest` должен быть durability boundary до `StateCommitted`. Любая неоднозначность записи переводит систему в `recovery_required`, сохраняя manifest/snapshot. Cleanup разрешён только после доказанного durable commit.

### P0-2. Rollback является best-effort и всегда может сообщить успех

`rollbackToOldLocked` (`coordinator.go:371-405`) игнорирует ошибки:

- withdrawal опубликованных bridges;
- восстановления LKG;
- start/stop процесса;
- повторной публикации прежних bridges;
- восстановления store snapshot;
- удаления артефактов.

Функция в конце безусловно возвращает `nil`. Поэтому вызывающий код не способен узнать, что rollback провалился. Более того, LKG восстанавливается через rename (`coordinator.go:378-380`), то есть эталон **потребляется**, хотя план требует non-consuming restore. При first-install без LKG не удаляется уже опубликованный плохой `config.yaml`.

**Обязательное исправление:** пошаговый проверяемый rollback с aggregation ошибок, detached recovery context, проверкой digest/runtime/bridges/store и переходом в `StateRecoveryRequired` при любом недоказанном результате. LKG восстанавливать copy-to-candidate + atomic rename, не уничтожая LKG.

### P0-3. Durable DraftJournal protocol фактически не реализован

`ApplyDraftOnly` (`cmd/awg-manager/dynamic_engine.go:162-188`) сначала создаёт snapshot, затем мутирует store и только после этого одним действием пишет journal сразу в `DraftPending`.

Отсутствуют durable переходы:

- `snapshot_secured` до мутации;
- `store_mutated` после мутации;
- `consuming` с `AssociatedApplyTxID` перед apply.

Если процесс падает после мутации, но до `SaveDraftJournal`, восстановить store невозможно автоматически. Если `SaveDraftJournal` возвращает ошибку, уже изменённый store не откатывается. `ApplyPendingDraft` (`dynamic_engine.go:191-200`) не переводит journal в `consuming` и не связывает его с apply transaction.

Startup recovery (`coordinator.go:174-245`) вдобавок игнорирует ошибки digest/read/restore/unlink/write и может завершиться `ready`, хотя store не восстановлен.

**Обязательное исправление:** реализовать точный state machine из plan v8 и crash tests для каждой границы записи. Ни одна ошибка восстановления/фиксации journal не должна замалчиваться.

### P0-4. `input.pending.json` существует только как тип и reader; producer и convergence отсутствуют

Поиск по репозиторию показывает, что `PendingInputRecord` нигде не создаётся и `input.pending.json` нигде не записывается. `reconcilePendingInputLocked` (`coordinator.go:407-421`) умеет только удалить marker, если digest уже совпал. Он не:

- проверяет version/state/source;
- переводит `pending -> converging`;
- компилирует и применяет desired input;
- возвращает ошибку;
- переводит corrupt marker в recovery-required (quarantine происходит, но ошибка теряется).

Изменения settings/slots/tunnels поэтому не обладают обещанной eventual convergence после неудачного apply или рестарта.

**Обязательное исправление:** coordinator должен получить compile/convergence callback на startup; все non-native writers должны durable записывать marker до/при неудачном apply; удаление marker — только после commit matching `TargetInputDigest`.

### P0-5. Controlled restart не доказывает identity и readiness

`restartControlledLocked` (`coordinator.go:694-720`) проверяет только digest файла до/после `Operator.Start`. Параметр `listeners` вообще не используется.

Не проверяются:

- новый PID и принадлежность процесса текущему daemon epoch;
- `Operator.CurrentGeneration()`;
- PID start time из `/proc/<pid>/stat`;
- socket inode ownership в `/proc/<pid>/fd`;
- TCP/UDP required listeners;
- ожидаемые redirect `51272`, TProxy UDP `51271`, mixed/custom и bridge listeners;
- identity ответа controller относительно нового процесса.

`daemonEpoch` лишь генерируется и возвращается getter’ом; в manifest/process receipt он не сохраняется и ни с чем не сверяется.

**Обязательное исправление:** ввести process receipt (`pid`, `start_time`, `generation`, `daemon_epoch`), проверять все listener’ы и inode ownership, не доверять controller HTTP без связи с новым PID.

### P0-6. Incremental bridge publication journal отсутствует

`syncBridgesLocked` (`coordinator.go:722-773`) вызывает `ApplyBridges` сразу для всего набора, затем verify, затем withdrawal. `PublishedBridges` присваивается только в памяти в самом конце и manifest после этого изменения отдельно не фиксируется.

При падении между частично успешными OS-операциями startup не знает, какие bridge реально опубликованы. Заявление walkthrough о durable incremental publication неверно.

**Обязательное исправление:** применять/отзывать bridge по одному либо возвращать точный partial result; после каждого шага durable обновлять manifest и проверять фактический набор через `ListActiveBridges`.

### P0-7. RuntimeOff может зафиксировать успех при любой неудаче

`applyRuntimeOffLocked` (`coordinator.go:637-691`) игнорирует ошибки rename active->LKG, unlink active, каждого manifest write, StopAndWait, WithdrawBridges, verified-active write и cleanup. После любого из этих сбоев функция всё равно возвращает `nil` и in-memory record становится RuntimeOff.

Это может оставить одновременно:

- работающий процесс;
- существующий active config;
- активные bridges;
- applied record, утверждающий обратное.

**Обязательное исправление:** RuntimeOff должен быть полноценной транзакцией с file/process/bridge verification и rollback к предыдущему file/process state.

### P0-8. Recovery доверяет непроверенным путям и неполным manifest/journal

Хотя `strictfs.ValidateTxID/ValidateBasename` реализованы, coordinator не применяет их при чтении persisted manifest и draft journal. Поля `StoreSnapshotFile`, `CandidateConfigFile`, `LKGConfigFile`, `DraftSnapshotFile` затем напрямую передаются в restore/rename/unlink.

Также не проверяются:

- обязательные поля и допустимые состояния;
- confinement путей внутри ожидаемых директорий;
- digest snapshot/LKG/active/store;
- соответствие `AssociatedApplyTxID` реальному manifest;
- integrity/version `verified-active.json` (ошибка JSON сейчас просто молча игнорируется в `coordinator.go:131-139`).

**Обязательное исправление:** строгий decoder/schema validator и artifact mismatch matrix до любых файловых действий. Невалидный metadata-файл сохранять как evidence и fail closed.

### P0-9. Старый прямой writer `config.yaml` не удалён и остаётся подключён

Walkthrough утверждает, что runtime mutations идут только через coordinator и что старый генератор удалён. Фактически:

- `internal/singbox/router/service_mihomo.go:18-264` всё ещё содержит `GenerateMihomoConfig()`;
- `service_mihomo.go:259` напрямую вызывает `os.WriteFile(config.yaml, ...)`;
- `cmd/awg-manager/wiring_server.go:486` присваивает его в `dynEngine.OnMihomoReload`;
- legacy fallback в `DynamicEngine.prepareMihomoConfig` остаётся рабочим.

Даже если production wiring сейчас обычно создаёт coordinator, fail-open fallback сохраняет второй источник истины и позволяет обход transactional pipeline при частичном wiring/тестовом composition root.

**Обязательное исправление:** удалить production direct writer и fail-open fallback. При отсутствии coordinator для Mihomo mutation/apply возвращать явную ошибку configuration unavailable.

### P0-10. Startup gating не соответствует плану

В `cmd/awg-manager/wiring_server.go:562-572` `routerSvc.Reconcile()` запускается в goroutine **до** `dynEngine.Startup()` (`:577`). `OnRoutingSlotsChanged` также уже подключён и способен вызвать `SyncMihomoRuntime` до завершения recovery.

`StartupStatus` содержит только `ready/degraded`, хотя plan требует отдельный fatal outcome. Ошибка любого типа превращается в degraded. В degraded mode блокируются только native CRUD через `withNativeMutation`; install/update/uninstall/reload и прочие mutable paths не имеют общего coordinator gate.

**Обязательное исправление:** выполнить recovery синхронно до запуска любых reconcile/scheduler/callback/mutable API; реализовать fatal vs recoverable classification; поставить единый mutation gate на все Mihomo-changing endpoints и callbacks.

### P0-11. Administrative recovery API расходится с безопасным контрактом плана

План разрешает:

- `rollback_to_lkg`;
- `regenerate_from_desired`;
- `export_evidence`.

Фактически реализованы `rollback_to_lkg` и небезопасный `clear_marker` (`coordinator.go:808-834`, `mihomo_handler.go:212-255`). `clear_marker` просто удаляет marker и manifest и объявляет `StateIdle`, не доказывая здоровье config/runtime/store/bridges. `rollback_to_lkg` также игнорирует ошибки rename/unlink, не валидирует LKG и не запускает/проверяет runtime.

Endpoint evidence отсутствует. В OpenAPI до сих пор документирован `clear_marker`. UI вызывает только rollback и передаёт `force=true`, хотя force здесь не имеет безопасной семантики.

**Обязательное исправление:** удалить `clear_marker`; реализовать три разрешённых action как single-flight процедуры с postcondition verification, audit log и redacted evidence export.

### P0-12. Manifest transition errors системно игнорируются

В coordinator найдено множество `_ = c.persistManifestLocked(...)` после границ `candidate_valid`, `lkg_secured`, `swap_active`, `reloading`, `runtime_ready`, `published`, `committed`, а также десятки проигнорированных strictfs/runtime/store ошибок.

После crash disk-state может отставать от уже выполненного OS action; recovery выберет неверную ветку. Наличие `WriteAndResolve/RenameAndResolve` в `strictfs` не помогает: coordinator применяет resolve только к candidate->active swap, но не ко всем durable objects, как требует P0-8 плана.

**Обязательное исправление:** единый checked persistence helper для каждого durable transition. `OutcomeAmbiguous` обязан фиксировать recovery marker и закрывать bridge gate; никакого `_ =` для state-changing операций.

## P1 — существенные несоответствия

### P1-1. Compile input не является coherent snapshot под единым generation barrier

`AssembleCompileInput` последовательно читает settings, несколько orchestrator slots, router config, AWG live catalog и native store отдельными вызовами без общего lock/generation token (`service_mihomo.go:298-498`). Между чтениями источники могут измениться.

`json.Marshal` даёт детерминированное представление Go map keys, но это не исправляет generation skew. Кроме того, `ComputeDigest` возвращает пустую строку при marshal error вместо ошибки.

### P1-2. Migration journal объявлен, но не используется

Тип `MigrationJournal` существует только в `internal/mihomo/types.go`. Ни `migration.journal.json`, ни snapshot/rollback для migration pipeline нет. `MigrateLegacyMihomoResources` последовательно импортирует groups и rules; если второй шаг падает, первый уже сохранён.

### P1-3. LKG overwrite guard отсутствует

Перед перезаписью `config.yaml.lkg` coordinator не доказывает, что текущий active соответствует verified applied generation. Более того, чтение active внутри closure игнорирует ошибку (`coordinator.go:557-568`), что может записать пустой LKG.

### P1-4. Required listener derivation расходится с планом

`mihomo_compiler.go:89-107` всегда добавляет mixed-port, подставляя `1099` при `<=0`, но не добавляет redirect TCP `51272` и TProxy UDP `51271`. План требует учитывать фактический режим и отключённый/custom mixed-port. Даже сформированный список сейчас не используется restart verification.

### P1-5. Status/OpenAPI/frontend реализованы лишь частично

Status отдаёт `degraded: bool`, но не reason, coordinator state, pending convergence, active generation/digests или допустимые recovery actions. Frontend показывает только LKG-кнопку. Теста rendering degraded banner в Stage 2 нет, несмотря на заявленный acceptance scenario 63.

### P1-6. Заявленная test matrix не соответствует репозиторию

В частности отсутствуют самостоятельные тесты для:

- commit-pending при ошибке `verified-active.json`;
- всех failpoint границ manifest/LKG/store/draft/pending;
- draft `consuming` roll-forward/roll-back;
- реального startup convergence pending input;
- first-install failed restart cleanup;
- daemon epoch/stale PID rejection;
- socket ownership и TCP/UDP listeners;
- partial bridge publication crash;
- artifact mismatch matrix;
- degraded -> recovery -> scheduler starts exactly once;
- coordinator bypass grep guard;
- safe evidence redaction.

Текущие зелёные тесты подтверждают только узкий happy path и несколько простых recovery fixtures, но не заявленные ACID-like гарантии.

## Сводка соответствия plan v8

| Требование plan v8 | Статус |
|---|---|
| Typed manifest/journal structs | Частично реализовано |
| Draft crash protocol | Не реализовано |
| Base/target input digest fields | Поля есть, postconditions не доказаны |
| `input.pending.json` convergence | Не реализовано |
| RuntimeOff durable transaction | Не реализовано безопасно |
| Pure compiler function | Реализована |
| Coherent immutable input snapshot | Не реализовано |
| Atomic legacy migration journal | Не реализовано |
| Uniform `InspectOutcome` | Не реализовано |
| Controlled restart + process identity | Не реализовано |
| Required listener/socket ownership verification | Не реализовано |
| Incremental bridge journal | Не реализовано |
| Detached rollback context | Есть, но rollback ошибки игнорируются |
| Safe admin recovery actions | Не реализовано; присутствует unsafe `clear_marker` |
| Redacted evidence export | Не реализовано |
| Synchronous startup gating | Частично и с обходами |
| Degraded mutation gate | Только часть native CRUD |
| OpenAPI/frontend degraded UX | Частично реализовано |
| Exhaustive fault-injection matrix | Не реализовано |

## Что уже можно сохранить

Не нужно выбрасывать всю работу. Полезная база:

- `internal/strictfs` и его базовые тесты;
- structs в `internal/mihomo/types.go` после усиления validation;
- pure `CompileMihomoConfigFromInput`;
- snapshot API native store после path confinement;
- общий каркас `ApplyCoordinator`;
- wiring `NativeMutationApplier`;
- degraded banner как начальный UX;
- real-binary compiler acceptance harness.

Эти части нельзя считать production-ready транзакционной системой, но они подходят как foundation для исправления.

## Рекомендуемый порядок исправления

1. **Остановить расширение UI и не деплоить Stage 2.** Зафиксировать failing acceptance tests для всех P0 ниже.
2. **Сформализовать durable state machine:** schema validation, allowed transitions, exact artifact names/path confinement, checked writes, commit boundary.
3. **Исправить commit/rollback/LKG/RuntimeOff:** ни одной проигнорированной ошибки; non-consuming LKG; first-install rollback; recovery-required на ambiguity.
4. **Полностью реализовать DraftJournal:** четыре durable состояния, associated tx, apply pending draft, crash table.
5. **Реализовать PendingInputRecord producer + startup convergence** для settings/slots/tunnels.
6. **Добавить process receipt/readiness:** epoch, PID, `/proc` start time, generation, controller, TCP/UDP listener inode ownership.
7. **Сделать incremental bridge journal** с фактической сверкой OS state.
8. **Убрать direct writer/fallback** и поставить coordinator gate на все mutation paths/callbacks.
9. **Исправить startup ordering:** recovery до goroutine/callback/scheduler/API mutation availability.
10. **Заменить recovery API:** удалить `clear_marker`, реализовать verified rollback/regenerate/redacted evidence.
11. **Ввести migration journal и coherent input barrier.**
12. **Только затем** синхронизировать OpenAPI/types/frontend и выполнить полный regression + fault injection + restart acceptance на изолированном Linux стенде.

## Минимальные re-acceptance критерии

Stage 2 можно повторно предъявлять на аудит только когда одновременно выполнено следующее:

1. Все заявленные 64 сценария существуют как реальные тесты либо план честно сокращён с явным обоснованием.
2. Нет unchecked (`_ =`) ошибок на manifest/store/LKG/active/verified/draft/pending/runtime/bridge transitions.
3. `grep` не находит production writer `config.yaml` вне coordinator.
4. Fault injection после каждой durable и OS boundary либо доказывает commit, либо восстанавливает прежнее состояние, либо оставляет полные evidence и `recovery_required`.
5. Apply/restart/rollback проверяют exact config digest, store/input digest, process identity, listeners и bridge set.
6. Pending draft и pending external input переживают kill/restart и сходятся идемпотентно.
7. В degraded mode ни один mutable endpoint, scheduler или callback не обходит gate.
8. Recovery actions не просто удаляют marker, а доказывают postconditions.
9. Полностью проходят Go tests, real-binary acceptance, frontend tests/check и `git diff --check`.
10. Отдельный restart/E2E тест сравнивает runtime state с `verified-active.json` после normal apply, failed apply, RuntimeOff, crash и administrative recovery.

## Итог для следующего агента

Не исправлять дефекты точечно вокруг существующих зелёных тестов. Сначала добавить отсутствующие негативные/fault-injection тесты и сделать их красными. Затем последовательно закрывать commit protocol, rollback и recovery. Текущий walkthrough следует переписать после реализации: формулировки «fully implemented», «enterprise-grade ACID-like» и «all lifecycle checks» сейчас не соответствуют фактическому коду.
