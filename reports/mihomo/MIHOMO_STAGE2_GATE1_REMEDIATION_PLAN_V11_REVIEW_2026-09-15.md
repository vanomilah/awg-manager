# Ревью Mihomo Stage 2 Gate 1 Remediation Plan v11

**Дата:** 2026-09-15  
**План:** `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
**Walkthrough:** `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
**Рабочая копия:** `E:\AWGM\awg-manager`, ветка `feature/mihomo-ai-proxyrt`, HEAD `87a563eb`

## Итоговый вердикт

**План v11 заметно улучшен и правильно переносит 6 P0 + 6 P1 из предыдущего аудита, но запускать его в реализацию в текущем виде ещё рано. Требуется revision v12.**

Причина: план закрывает локальные симптомы старой реализации, но не определяет корректный жизненный цикл immutable generation, не устраняет TOCTOU в filesystem-защите и оставляет неоднозначные crash-состояния в rollback, pointer publication и bridge synchronization.

Текущий `walkthrough.md` не является отчётом о реализации v11. Это старый walkthrough с изменённым статусом в заголовке. Его содержимое противоречиво: сверху указано `GATE 1 REVIEW FAILED`, ниже утверждается, что Gate 1 завершён и строго проверен, а в конце снова написано `Gate 1 is COMPLETE`. Передавать его как доказательство выполнения нельзя.

## Что в v11 сделано правильно

План корректно включает:

- durable `rollback_in_progress` вместо преждевременного `rolled_back`;
- запрет удаления evidence при ошибке roll-forward commit;
- fail-closed обработку orphan config при первой установке;
- обязательную проверку config/store digest перед rollback;
- обязательный store snapshot и для apply без `mutateFn`;
- перевод `RuntimeOff` на общий transaction protocol;
- state transition table и фазовые schema invariants;
- durable `cleanup.pending.json`;
- расширенные negative/crash tests;
- запрет сборки IPK и деплоя в рамках Gate 1.

Эти пункты нужно сохранить в следующей редакции.

## Обязательные исправления плана перед реализацией

### P0-A. Определить единый жизненный цикл generation и стабильный GenerationID

Текущая реализация создаёт активному поколению ID при commit, но bundle с этим ID не существует. При следующем apply старый config архивируется уже под другим ID, построенным из нового txid:

- commit: `gen-<N>-<current-txid>` записывается в `verified-active.json`;
- следующий apply: та же generation N архивируется как `gen-<N>-<next-txid>`.

Из-за этого `AppliedGenerationRecord.GenerationID` не является идентичностью реального immutable bundle, а `activeGenID` в GC часто указывает на несуществующий каталог. v11 это не исправляет.

В v12 требуется зафиксировать следующий протокол:

1. GenerationID создаётся один раз для candidate generation и больше никогда не меняется.
2. До swap новый generation bundle полностью публикуется и fsync-ится, но ещё не считается active.
3. `verified-active.json` после успешной runtime verification указывает именно на существующий и проверенный bundle.
4. На следующем apply LKG pointer переключается на уже существующий bundle текущего active generation; повторного архивирования под новым ID нет.
5. First-install commit также создаёт первый полноценный bundle.
6. Orphan candidate bundle после crash не считается active и удаляется только безопасным GC после recovery.

Нужен invariant: любой non-off `verified-active.GenerationID` всегда разрешается в существующий bundle, manifest и digests которого совпадают с verified record.

Для `RuntimeOff` следует явно определить: создаётся ли отдельное off-generation без `config.yaml` либо record указывает на последний active bundle и хранит отдельный target runtime mode. Нельзя оставлять `GenerationID`, который не имеет однозначного bundle.

### P0-B. `EvalSymlinks` + `Lstat` не устраняют TOCTOU

Раздел 2.7 v11 сначала проверяет строковый путь, затем предлагает обычные filesystem-операции. Между check и use компонент каталога можно заменить symlink. Это не соответствует заявлению о строгой защите от symlink traversal.

В v12 основной Linux-путь должен использовать descriptor-relative операции:

- выделенный доверенный root directory fd;
- `openat2` с `RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS | RESOLVE_NO_MAGICLINKS`, где доступно;
- либо последовательный `openat` с `O_NOFOLLOW`, проверкой каждого fd через `fstat` и операциями rename/unlink относительно проверенных dirfd;
- regular file + owner/mode + `Nlink == 1` проверяются на уже открытом fd, а не до `os.Open`;
- platform-specific реализация через build tags.

`filepath.EvalSymlinks` допустим только как дополнительная диагностика, но не как security boundary.

### P0-C. State graph не соответствует обязательному snapshot при pure apply

В диаграмме `Idle -> Preparing` при `mutateFn == nil`, однако раздел 2.5 требует snapshot предыдущего store для любого apply с previous generation. Durable state должен отражать наличие этого snapshot.

Нужно определить:

- `Idle -> SnapshotSecured -> Preparing` для previous generation даже без store mutation;
- чистый first-install без store mutation может идти `Idle -> Preparing`;
- фазовые validators должны различать эти два случая.

Иначе startup recovery не сможет доказать, был ли обязательный snapshot создан до crash.

### P0-D. Нужна отдельная семантика pre-swap abort и post-swap rollback

Диаграмма отправляет compile/validation/publish failures в `RollbackInProgress`, хотя до swap runtime/config могли вообще не изменяться. Одновременно rollback routine в плане ориентирован на обязательный LKG.

В v12 нужно выбрать один из вариантов:

- отдельный `abort_in_progress` для восстановления только desired store и transient artifacts до swap;
- либо единый rollback routine с явно описанными phase-dependent postconditions для first-install, pre-swap и post-swap.

При невозможности durable-записать rollback/abort intent никакие новые side effects выполнять нельзя; система должна сохранять manifest и переходить в fail-closed recovery.

### P0-E. Не определена обработка неоднозначного результата bundle/pointer publication

План переносит failpoints внутрь atomic write, но не описывает решение после ошибки `after rename, before parent fsync`. В этот момент новый pointer/config может уже находиться на диске, хотя операция вернула ошибку.

Для каждой atomic boundary нужно определить outcome resolver:

- `old retained`;
- `new applied and digest proven`;
- `ambiguous -> recovery_required`.

После ошибки pointer publication coordinator обязан перечитать pointer через безопасный fd, проверить digest/manifest и только затем выбрать roll-forward, retry или recovery. Простого возврата ошибки недостаточно.

### P0-F. Rollback order и bridge compensation должны быть точными

План говорит «restore config, store, process, bridges», но не задаёт безопасный порядок. Candidate process нельзя оставлять работающим во время замены config/store.

Зафиксировать:

1. Остановить и доказать остановку candidate process.
2. Отозвать только те новые bridges, которые реально успели опубликоваться.
3. Восстановить и проверить store.
4. Восстановить и проверить config.
5. Восстановить старые bridges.
6. Запустить предыдущий process только после всех доказанных disk/network postconditions.
7. Проверить итоговый process/listeners в пределах доступного Gate 1 proof.

Bridge progress должен журналироваться инкрементально. Нельзя присваивать `PublishedBridges = target` лишь после завершения всей синхронизации: при успехе Apply и сбое Verify новые bridges уже существуют, но rollback о них не знает.

### P1-A. GC protected-set нуждается в типизированной модели ссылок

`VerifiedActiveGeneration` сейчас является числом, а каталоги имеют строковые IDs с txid; по числу нельзя однозначно защитить каталог. Draft/pending records в текущей schema вообще не содержат generation IDs, а snapshot path не является ссылкой на generation bundle.

В v12 нужно:

- добавить явные `ActiveGenerationID`, `PreviousGenerationID`, `CandidateGenerationID` в соответствующие durable records;
- перечислить для каждого artifact конкретные поля, формирующие protected-set;
- при unreadable/corrupt artifact полностью запрещать GC;
- recovery marker должен запрещать GC целиком;
- GC работает только после успешного startup reconciliation;
- delete и fsync errors агрегируются и сохраняются durable.

### P1-B. Durable cleanup-pending тоже имеет failure boundary

После commit откат запрещён, но запись `cleanup.pending.json` сама может завершиться ошибкой. План должен определить fallback:

- не удалять committed transaction manifest, пока cleanup intent не записан либо cleanup не завершён;
- при restart восстанавливать cleanup intent из `StateCommitted` manifest;
- in-memory warning не считается durable доказательством;
- повреждённый cleanup journal должен fail closed только для cleanup/GC, но не откатывать уже committed runtime.

### P1-C. Runtime proof не должен обещать больше Gate 1

`verifyRollbackPostconditionsLocked` должен чётко разделять:

- доказательство disk config/store/bridge state в Gate 1;
- доказательство identity конкретного процесса, executable, cmdline и socket ownership, которое ранее было отнесено к Gate 2.

Либо минимально необходимый process proof переносится в Gate 1 вместе с тестами, либо walkthrough не заявляет, что именно нужное поколение процесса криптографически доказано.

### P1-D. `os.Chmod(baseDir, 0700)` нельзя применять без определения ownership boundary

Нужно подтвердить, что `ConfigDir` является выделенным приватным каталогом только Mihomo. Если он разделяется с другими компонентами AWG Manager, изменение mode может сломать их. В плане следует назвать точный storage root, ожидаемого владельца, допустимые modes и поведение при невозможности исправить права.

### P1-E. Monotonic sequence нельзя доказать одним перезаписываемым manifest без правила state/sequence

После restart доступна только последняя версия manifest. Поэтому термин «monotonic sequence validation» должен быть уточнён:

- задать ожидаемый sequence/range для каждой state либо хранить previous sequence/state;
- transition проверяет in-memory current state и sequence до записи;
- startup schema проверяет согласованность state, sequence и обязательных полей;
- если нужна история переходов, требуется append-only journal, а не один atomic snapshot.

## Требуемые дополнения к тест-плану

Кроме уже перечисленных в v11 тестов, добавить:

1. `verified-active.GenerationID` всегда соответствует реально существующему bundle после first и subsequent commits.
2. Следующий apply не меняет ID предыдущего generation.
3. Crash после публикации candidate bundle, но до swap оставляет active record прежним.
4. Pointer write: before rename, after rename/pre-fsync, fsync failure — с проверкой outcome resolver.
5. Crash после durable `rollback_in_progress` на каждом отдельном rollback side effect.
6. Bridge Apply succeeded + Verify failed — новый bridge обязательно отзывается.
7. Pure apply с previous generation проходит через durable snapshot state.
8. Невозможность записать rollback intent не запускает rollback side effects и сохраняет evidence.
9. Ошибка записи `cleanup.pending.json` восстанавливается из committed manifest после restart.
10. Symlink component swap race проверяется на Linux; простой заранее созданный symlink-test недостаточен.
11. RuntimeOff: first install, previous active generation, crash перед stop, после stop, после withdraw и перед commit.
12. GC защищает active/previous/candidate IDs из каждого реального durable artifact, а не искусственно переданные строки.

## Требования к новому walkthrough

Текущий walkthrough следует не дополнять, а заменить после реализации v12. Новый документ должен содержать:

- commit/hash или точный dirty-tree snapshot проверенных файлов;
- список реально реализованных пунктов v12;
- фактический вывод новых negative/crash tests;
- отдельное описание generation lifecycle и recovery outcomes;
- честное указание непроверенных frontend/router сценариев;
- отсутствие утверждения `COMPLETE`, пока независимый аудит не принят.

## Разрешение на дальнейшую работу

**Рекомендация: вернуть план агенту на revision v12; реализацию v11 пока не запускать.**

После внесения перечисленных изменений можно начинать Gate 1 remediation. Gate 2, сборка IPK и деплой по-прежнему запрещены до отдельной приёмки Gate 1.

## Что проверялось и что не выполнялось

- Прочитаны актуальные plan и walkthrough.
- Сопоставлены предложения v11 с текущими production paths и предыдущим аудитом.
- Подтверждено, что текущий код всё ещё содержит старую Gate 1 реализацию; признаков реализации v11 (`StateRollbackInProgress`, transition table, durable cleanup journal, secure descriptor-relative FS) нет.
- Код не изменялся.
- Тесты повторно не запускались, поскольку после предыдущего аудита релевантные source-файлы не были переработаны; v11 на данный момент является только планом.
- IPK не собирался и роутеры не затрагивались.

