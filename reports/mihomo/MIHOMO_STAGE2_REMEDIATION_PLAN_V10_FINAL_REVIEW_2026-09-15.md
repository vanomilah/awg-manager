# Финальное ревью Mihomo Stage 2 Remediation Plan v10

**Дата:** 2026-09-15  
**Документ:** `implementation_plan.md` — *Mihomo Stage 2 Remediation Plan (Revised v10 — Final Specification)*  
**Рабочая копия:** `E:\AWGM\awg-manager`  
**Ветка:** `feature/mihomo-ai-proxyrt`

## Вердикт

**План v10 принят как основа реализации. Gate 1 можно запускать после добавления четырёх перечисленных ниже preflight-инвариантов.**

Статус: `APPROVED FOR STAGED IMPLEMENTATION WITH GATE-SPECIFIC CONDITIONS`.

Полная реализация всё ещё не должна выполняться одним большим изменением. Каждый Gate принимается отдельно по собственному walkthrough и фактическим тестам. Одобрение плана не означает заранее одобренную реализацию.

v10 содержательно устраняет замечания предыдущих ревью:

- LKG связан с pre-mutation snapshot;
- описана staged directory publication с fsync и atomic pointer;
- появился writer-side seqlock;
- pending input получил intent-before-mutation;
- commit point отделён от post-commit cleanup;
- process proof включает cmdline и controller socket ownership;
- ListenerSpec учитывает address/family;
- bridge publish/withdraw имеют независимые postconditions;
- evidence DTO больше не содержит raw artifacts;
- `repair_binary` удалён;
- 64 сценария получили имена тестов и Gate mapping;
- verification включает API, wiring, real binary и frontend.

## Preflight Gate 1 — дописать до начала кодинга

Следующие четыре пункта являются обязательными уточнениями Gate 1, но не требуют новой архитектурной редакции плана.

### 1. Доказать соответствие active config прежнему verified generation

Перед архивированием предыдущего поколения недостаточно проверить только store snapshot. Coordinator обязан проверить:

- digest текущего `config.yaml` равен прежнему `AppliedConfigDigest`;
- digest pre-mutation store snapshot равен прежнему `AppliedStoreDigest`;
- generation manifest получает прежний `AppliedInputDigest`, listeners, bridges и runtime mode;
- прежний `verified-active.json` проходит schema/version/path validation.

Если хотя бы один инвариант не доказан, новый LKG bundle и pointer не публикуются, apply прекращается с `recovery_required`.

### 2. Явно определить first-install

Если прежнего `verified-active.json` нет, предыдущего поколения не существует:

- generation bundle не создаётся;
- `lkg.pointer.json` не создаётся и не изменяется;
- manifest содержит `previous_generation_present: false`;
- rollback удаляет candidate/active config, восстанавливает pre-mutation store и доказывает остановку процесса/отсутствие опубликованных bridges;
- наличие старого бесхозного `config.yaml` без verified record считается ambiguity, а не автоматически LKG.

### 3. Добавить безопасную retention policy

Immutable bundles нельзя накапливать без ограничений на роутере. После commit разрешено удалять только поколения, которые одновременно:

- не являются active generation;
- не указаны `lkg.pointer.json`;
- не упомянуты manifest/draft/pending/recovery artifacts;
- старше настроенного retention window.

Минимально сохраняются active + LKG; рекомендуется ещё одно предыдущее поколение при наличии места. Ошибка GC становится `cleanup_pending`, но не отменяет commit. Перед удалением проверяется path confinement. Никакого автоматического удаления recovery evidence.

### 4. Зафиксировать права и ограничения generation storage

- directories: `0700`;
- config/store/manifest/pointer: `0600`;
- запрет symlink/hardlink traversal;
- все staging и final paths находятся на одном filesystem;
- generation ID проходит строгую basename validation и создаётся без коллизий;
- store snapshots рассматриваются как секретные данные и не попадают в evidence/API;
- отдельно тестируются file fsync, staging-dir fsync, rename, parent-dir fsync, pointer write и pointer fsync.

Сценарии S10–S12 следует развернуть в table-driven subtests по каждой из этих границ, а не проверять только один общий `StagingFsyncFail`.

## Условия приёмки Gate 2

Gate 2 можно реализовывать после принятия Gate 1. При реализации требуется сохранить следующие уточнения:

1. `RuntimeProcessIdentity.Generation` является доказательством только внутри текущего `DaemonEpoch`. После рестарта AWG Manager persisted identity проверяется по PID + start ticks + executable + cmdline + sockets.
2. `ExecutablePath` сравнивается с разрешённым фактическим бинарём после canonical path resolution; добавляется тест executable mismatch.
3. Controller listener проверяется по точному address/family/port и принадлежности тому же PID до HTTP `/version`.
4. Набор listeners выводится только из фактической typed compiled config. Не следует безусловно добавлять mixed `1099`, redirect или TProxy только по названию режима.
5. Значение порта `0` сохраняет семантику «listener отключён», если именно так оно задано настройками; compiler не должен самовольно превращать `0` в `1099`, если default уже не был применён слоем настроек.
6. Добавляются отдельные tests для primary TProxy, policy/TUN, sidecar-only и RuntimeOff.
7. Bridge identity с `OwnerUUID + Generation` проверяется как при publish, так и при withdraw; foreign bridge никогда не удаляется.

Эти условия не блокируют начало Gate 1.

## Условия приёмки Gate 3

Перед началом Gate 3 план/реализация должны явно закрыть два оставшихся вопроса.

### 1. Конкурентные PendingInput producers

Один `input.pending.json` нельзя перезаписывать параллельными settings/tunnel/slot/native событиями. Нужен один из вариантов:

- глобальная сериализация desired mutations через coordinator;
- durable очередь operation intents;
- coalescing journal с monotonic desired generation и объединённым source set.

Обязательные свойства:

- второй producer не теряет первый intent;
- после любого crash actual desired state перечитывается целиком;
- startup convergence применяет последнюю durable desired generation;
- устаревший apply не может удалить marker более нового изменения;
- unlink выполняется с compare-and-delete по operation/generation ID.

Добавить tests: concurrent producers, new intent during apply, stale apply completion, crash during coalescing.

### 2. Полный список compile inputs и revision owners

Текущий `MihomoCompileInput` включает больше источников, чем перечислено в execution Gate 3. В stable snapshot/version vector должны попасть все реально используемые значения, включая:

- settings;
- router config, rules, DNS и final outbound;
- subscription/tunnel/AWG orchestrator slots;
- native proxies/providers/groups/rules/listeners/bridges;
- tunnel/AWG fallback catalogs, если fallback остаётся;
- `TunIface` и источник его определения;
- dynamic cloud CIDRs;
- sidecar/mode decision.

Для каждого поля таблица должна указывать owner, snapshot method, revision/digest и writer barrier. Если значение нельзя снабдить стабильной revision, оно не должно читаться скрытым fallback-путём во время compile.

`SourceVersionVector` следует расширить недостающими revisions либо заменить generic sorted map с типизированными source IDs.

## Неблокирующие редакционные замечания

1. В state diagram переход `StatePublished -> StateRecoveryRequired` корректен только если `verified-active.json` не стал durable. После commit point требуется roll-forward/cleanup-pending.
2. `SanitizedSummary` должен создаваться по таблице `FailureCode -> constant message`; нельзя помещать туда исходный `error.Error()`.
3. Команда `go test -race ...` по выбранным пакетам не заменяет финальный `go test ./...`; план это уже упоминает, но walkthrough должен показать результаты обеих команд отдельно.
4. Скрипт zero-writer обязан иметь allowlist конкретных функций/путей и ненулевой exit code, а не просто печатать результаты `grep`.
5. Если реальный полный frontend test снова не укладывается в короткий timeout, walkthrough должен честно сообщить timeout, а не считать suite пройденным по частичному выводу.
6. Термин `ACID-grade` лучше не использовать в итоговом walkthrough: файловая и OS-транзакция обеспечивает доказуемое восстановление по описанным инвариантам, но не является классической транзакцией базы данных.

## Порядок работы, разрешённый этим ревью

1. Внести четыре Gate 1 preflight-инварианта в рабочую спецификацию или непосредственно в red tests.
2. Реализовать только Gate 1.
3. Подготовить отдельный Gate 1 walkthrough и остановиться.
4. Провести независимое ревью Gate 1.
5. После принятия перейти к Gate 2 с его условиями.
6. До Gate 3 формализовать concurrency/coalescing и полную input ownership table.
7. Gate 4 начинать только после принятия первых трёх ворот.

## Минимальный Gate 1 acceptance

Gate 1 считается завершённым только если тестами доказаны:

- валидный immutable bundle содержит согласованные config/store/input digests предыдущего verified generation;
- first install не создаёт фиктивный LKG;
- partial staging никогда не становится видимым через pointer;
- любой fsync/rename/pointer failure сохраняет прежний LKG;
- rollback не потребляет bundle;
- path/symlink/hardlink escape отклоняется;
- post-commit cleanup failure не откатывает runtime и повторяется при startup;
- GC никогда не удаляет active, LKG или referenced recovery generation;
- все новые tests проходят с `-race`;
- существующие targeted Go tests и `git diff --check` остаются зелёными.

## Итог для следующего агента

План v10 можно запускать **только по Gate 1**, после фиксации четырёх preflight-инвариантов этого документа. Не переходить автоматически к Gate 2–4 и не заявлять полное завершение Stage 2 по факту прохождения Gate 1.

Первое изменение должно быть набором красных тестов для generation bundle/first-install/fsync/retention. После этого реализуется storage/state machine до их прохождения. Production wiring, frontend и деплой в Gate 1 не требуются.
