# Повторное ревью плана исправления Mihomo Gate B — Revision 2

**Дата:** 2026-09-21  
**Проверен:** `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
**Вердикт:** **REVISE — прогресс существенный, но запускать реализацию пока нельзя.**

Revision 2 закрывает большую часть замечаний предыдущего ревью на уровне дизайна: введены `OperationKind`, granular rollback states, таблица startup recovery, разделены immutable generation facts и live activation receipt, запрещён автоматический legacy fallback. Тем не менее остаются критические противоречия в recovery semantics и тестовой методике.

## P0 — обязательные исправления плана

### 1. Roll-forward после `RuntimeVerified` противоречит утверждённой Binding Amendment 2

В ранее утверждённом `MIHOMO_REMEDIATION_PLAN_V5_FINAL_REVIEW_2026-09-21.md:112-129` выбрана однозначная семантика:

> После crash в `ConfigPromoted` или `RuntimeVerified`, если commit LKG ещё не доказан, выполнять `rollback_to_lkg`. Roll-forward candidate допустим только как отдельно описанный и отдельно утверждённый протокол; смешивать поведения нельзя.

Revision 2 вместо этого предписывает:

- `StateRuntimeVerified`: закончить bridge sync и продвинуть candidate в LKG;
- `StateBridgesReconciling`: повторить sync и продвинуть candidate в LKG;
- тесты `TC-CR-05/06` ожидают именно roll-forward.

Это меняет уже принятое safety decision и делает результат зависимым от точки crash.

**Требование:** для `regenerate` после `ConfigPromoted`, `RuntimeVerified` и `BridgesReconciling`, пока не записан и не проверен commit proof, всегда выполнять полный rollback к существующему LKG по 10-шаговому протоколу. Если нужен roll-forward — оформить отдельную Binding Amendment 3 с доказательством candidate bundle/runtime/bridges и получить отдельное одобрение до реализации.

### 2. «Atomic Archive Protocol» имеет собственное crash window

Предложено сначала переименовать active manifest в archive, а затем выполнить CAS старого manifest в новый. Между этими действиями возможен crash: active manifest отсутствует, новый не записан, а startup recovery не ищет orphan archive.

После ужесточения CAS это также логически конфликтует: CAS ожидает старый manifest по исходному пути, но он уже переименован.

**Безопасный вариант:**

1. атомарно **скопировать**, а не переместить, старый manifest в immutable archive;
2. fsync archive и directory;
3. CAS-заменой active manifest записать administrative manifest со ссылкой и digest архива;
4. при crash до шага 3 старый active manifest остаётся авторитетным;
5. при crash после шага 3 новый manifest содержит доказанную ссылку на архив.

Либо использовать отдельный administrative journal, записанный до любых изменений исходного manifest. В плане должен быть только один из этих вариантов.

### 3. Предложенный `CrashHook` не моделирует жёсткий crash

Возврат sentinel error раскручивает стек Go и выполняет все `defer` внутри `Reconcile`. Фраза «test abandons coordinator immediately» этого не меняет: к моменту возврата cleanup уже мог выполниться.

**Требование:** crash tests должны запускать сценарий в отдельном subprocess и завершать его через `os.Exit` в crash hook либо удерживать процесс на boundary и убивать его родительским процессом. Затем новый subprocess/coordinator открывает ту же директорию. Panic не подходит — при panic `defer` также выполняются.

Тест должен доказывать, что после crash на диске остались именно артефакты выбранной boundary.

### 4. Матрица не покрывает crash между side effect и следующим checkpoint

Текущие `TC-CR-*` в основном падают после записи состояния. Главные crash windows находятся в противоположном месте:

- snapshot создан, но `StateSnapshotSecured` ещё не записан;
- bundle опубликован, но `StateCandidatePublished` ещё не записан;
- active config заменён, но `StateConfigPromoted` ещё не записан;
- процесс перезапущен, но `StateRuntimeVerified` ещё не записан;
- один или несколько bridges изменены, но bridge checkpoint ещё не записан;
- verified-active/LKG pointer записан, но commit state ещё не записан;
- файл cleanup удалён, но cleanup journal ещё не обновлён.

**Требование:** для каждого side effect нужны две границы: `before effect` и `after effect / before checkpoint`. Только crash после уже записанного checkpoint недостаточен.

## P1 — необходимые уточнения

### 5. Abort ранней `regenerate` не может безусловно переводить degraded систему в `Idle`

Administrative regenerate начинается из уже неисправного состояния. Если crash произошёл до изменения active config (`PreSnapshotWriteIntent`, `SnapshotSecured`, `RecoveryIntent`, `CandidatePublished`), cleanup новой попытки не устраняет исходную неисправность.

План сейчас удаляет manifest и возвращает `StateIdle`. Это может снять блокировку mutations, хотя первоначальная причина recovery не устранена.

**Требование:** после раннего abort восстановить исходный archived manifest/marker либо оставить `StateRecoveryRequired`. В `Idle` переходить только при доказанном здоровом pre-state, зафиксированном до начала administrative operation.

### 6. `StateRollbackIntent` должен безопасно возобновляться

В этом состоянии mutations ещё не начались, а целевой LKG уже проверен. План и `TC-CR-08` оставляют систему навсегда в `RecoveryRequired`, хотя все данные для детерминированного продолжения доступны.

**Требование:** startup должен повторно проверить LKG pointer/bundle и продолжить rollback со store restore. Fail-closed нужен только при несовпадении/повреждении доказательств.

### 7. RuntimeOff противоречит общей таблице recovery

Общая строка `StateConfigPromoted` требует rollback к LKG, а `TC-CR-13` ожидает roll-forward к `RuntimeOff`. Нужно устранить противоречие. По принятой Binding Amendment до commit proof требуется rollback к предыдущему LKG также и для candidate RuntimeOff.

Успешный RuntimeOff proof должен включать: процесс отсутствует, принадлежащие Mihomo listeners отсутствуют, bridges соответствуют target, verified-active записан. Но это относится к нормальному непрерванному commit, не к roll-forward после crash.

### 8. Manifest должен хранить реальные bridge sets, а не только digests

`TargetBridgeDigests []string` позволяет проверить известные записи, но не позволяет после холодного старта восстановить, какие bridges надо создать/удалить, и не доказывает отсутствие лишних bridges.

**Требование:** хранить canonical `PreviousBridges []BridgeRef` и `TargetBridges []BridgeRef` плюс общий digest канонической коллекции. Проверять exact set и ownership. Существующий `BridgeOperations` использовать как durable per-operation journal, а не заменять списком отдельных digest.

### 9. Filesystem checks не должны быть частью чистой schema validation

Таблица относит «bundle существует», «active config совпадает», «operator running» к `ValidateSchemaForPhase`. Этот метод проверяет структуру manifest и не имеет filesystem/runtime dependencies.

**Требование:** разделить:

- `ValidateSchemaForPhase()` — обязательность и формат полей;
- `validateManifestOwnershipLocked()` — path confinement/ownership;
- `verifyDurableStateForPhase(ctx, m)` — файлы, digest, process, listeners, bridges.

Startup recovery сначала валидирует schema/ownership, затем observed durable state.

### 10. Legacy migration лучше исключить из Gate B

Новый explicit migration flow сам является отдельным полноценным apply protocol и заметно расширяет Gate B. Кроме того, «compile config.yaml.lkg» сформулировано неверно: legacy YAML можно валидировать, но compiler строит config из desired store.

**Требование:** в Gate B оставить строгий отказ `rollback_to_lkg`, если verified LKG bundle отсутствует. `reconcile_legacy_migration` вынести в отдельный последующий Gate/план. Это уменьшит риск и объём текущего исправления.

### 11. Нужны дополнительные corruption/cleanup cases

Добавить минимум:

- отсутствующий store snapshot LKG;
- digest mismatch snapshot;
- отсутствующий config RuntimeOn;
- ошибку marker write;
- ошибку manifest/archive fsync;
- crash во время terminal cleanup;
- foreign bridge и ambiguous bridge ownership;
- LKG pointer с неверным daemon epoch сразу после advance;
- повторный recovery после частично выполненного rollback.

## Что в Revision 2 уже можно сохранить

- `OperationKind` и granular rollback states;
- create-only semantics для CAS после исправления archive protocol;
- разделение immutable facts и live receipt;
- canonical comparison с нормализацией slices;
- запрет автоматического legacy fallback;
- WSL unit/race/cross-package проверки;
- строгий запрет Gate C, IPK build, deploy и изменений submodules.

## Условие разрешения реализации

Нужна Revision 3, которая:

1. возвращает утверждённую rollback-to-LKG семантику после crash до commit proof;
2. заменяет rename-before-CAS безопасным archive/journal protocol;
3. использует реальный subprocess crash без выполнения `defer`;
4. покрывает crash после каждого side effect до checkpoint;
5. не переводит ранний abort degraded recovery в `Idle` без health proof;
6. возобновляет `StateRollbackIntent`;
7. хранит previous/target bridge sets и per-operation journal;
8. выносит legacy migration за пределы Gate B.

После этих правок план можно передавать исполнителю без дополнительного архитектурного изобретательства в процессе кодирования.

