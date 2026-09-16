# Аудит реализации Mihomo Stage 2 — Gate 1

**Дата:** 2026-09-15  
**Проверенные документы:**

- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

**Рабочая копия:** `E:\AWGM\awg-manager`  
**Ветка:** `feature/mihomo-ai-proxyrt`  
**Проверенный HEAD:** `87a563eb`

## Вердикт

**Gate 1 не принят. Переходить к Gate 2 пока нельзя.**

Заявленные тестовые команды действительно проходят, но тесты не покрывают несколько аварийных границ. В реализации остаются сценарии, при которых после сбоя новый конфиг может остаться активным, журнал будет ошибочно считать откат завершённым, либо повреждённый LKG будет принят за корректный.

Статус walkthrough `GATE 1 COMPLETED — READY FOR REVIEW` следует заменить на `GATE 1 REVIEW FAILED — REMEDIATION REQUIRED`.

## Что подтверждено независимо

В WSL выполнены команды:

```bash
go test -race -v -count=1 -run 'TestCoordinator_Gate1' ./internal/mihomo
go test -race -count=1 ./internal/strictfs ./internal/mihomonative ./internal/singbox/router ./internal/mihomo
go test -count=1 ./internal/...
git diff --check
```

Результат: все Go-тесты прошли, `git diff --check` не нашёл whitespace-ошибок. При этом `git diff --check` не проверяет новые untracked-файлы, а основные файлы Gate 1 на момент аудита не добавлены в индекс.

## Блокирующие дефекты

### P0-1. `rolled_back` фиксируется до фактического отката

Файл: `internal/mihomo/coordinator.go`, `rollbackActiveLocked`, строки около 1083–1088.

Сейчас сначала вызывается:

```go
_ = c.transitionManifestLocked(m, StateRolledBack)
```

и только затем `rollbackToOldLocked`. Если процесс завершится между этими операциями, startup recovery попадёт в ветку `StateRolledBack`, удалит manifest и snapshots и не выполнит откат. Новый/непроверенный `config.yaml` может остаться активным.

Требуется:

1. Ввести durable-состояние `rollback_in_progress` либо использовать отдельную однозначную recovery-фазу.
2. Сначала зафиксировать намерение отката.
3. Выполнить идемпотентное восстановление config/store/process/bridges.
4. Проверить все postconditions.
5. Только после этого фиксировать `rolled_back`.
6. Startup recovery для `rollback_in_progress` обязан повторять откат.

Нужен crash-тест непосредственно после durable-записи намерения отката и до первого rollback side effect.

### P0-2. Roll-forward из `StatePublished` игнорирует ошибку commit record

Файл: `internal/mihomo/coordinator.go`, строки около 404–436.

При восстановлении `StatePublished` запись `verified-active.json` выполняется внутри `if`, но любая ошибка marshal/write просто пропускается. После этого manifest и snapshots удаляются, функция возвращает успех.

Это позволяет потерять definitive commit record и одновременно уничтожить recovery evidence.

Требуется: ошибка marshal/write/fsync должна сохранять manifest, переводить систему в `recovery_required` и возвращаться вызывающему коду. Удалять артефакты можно только после доказанного durable commit.

### P0-3. Нет заявленной защиты first-install от бесхозного `config.yaml`

Файлы: `internal/mihomo/coordinator.go`, `RecoverOnStartup` и начало `MutateAndApply`.

Если `verified-active.json` отсутствует, но `config.yaml` существует, код не считает состояние неоднозначным. Обычный apply перезапишет файл, а `RuntimeOff` даже переименует его в legacy LKG. Это прямо противоречит plan preflight invariant: unverified active config нельзя автоматически считать предыдущим поколением.

Требуется fail-closed preflight до любой мутации:

- нет verified record + есть active config/LKG pointer/сомнительный generation state → `recovery_required`;
- чистая первая установка допускается только при доказанном отсутствии предыдущего состояния;
- добавить startup и live-apply тесты для orphan `config.yaml`.

### P0-4. Откат может запустить процесс без доказанного восстановления config

Файл: `internal/mihomo/coordinator.go`, `rollbackToOldLocked`, строки около 485–526.

Ошибки чтения pointer/bundle/config и отсутствие legacy LKG молча игнорируются. После этого код всё равно может вызвать `Operator.Start()`. В результате процесс способен стартовать с candidate config либо без корректного config.

Кроме того, `ReadGenerationBundle` не сверяет digest содержимого с manifest/pointer. Сам факт наличия файлов принимается за валидность.

Требуется:

- невозможность доказанно восстановить LKG — обязательная ошибка и `recovery_required`;
- до запуска проверить config digest против LKG pointer и generation manifest;
- проверить store snapshot digest и обязательность snapshot для поколения с непустым `AppliedStoreDigest`;
- после запуска подтвердить runtime postconditions;
- не удалять snapshot/manifest при неполном откате.

### P0-5. Для apply без `mutateFn` snapshot предыдущего store необязателен из-за проглоченных ошибок

Файл: `internal/mihomo/coordinator.go`, строки около 711–719.

`CreateSnapshotFile` и `ComputeFileDigest` выполняются с игнорированием ошибок. Затем `PublishStagedBundle` разрешает пустой `preMutationSnapshotFile`, даже если previous generation существует и `AppliedStoreDigest` непустой. Получается LKG bundle без восстановимого store.

Требуется: при `PreviousGenerationPresent == true` snapshot и его digest обязательны независимо от наличия `mutateFn`; любая ошибка является pre-commit failure и не позволяет публиковать bundle/pointer.

### P0-6. `RuntimeOff` обходит безопасный generation/rollback protocol

Файл: `internal/mihomo/coordinator.go`, `applyRuntimeOffLocked`, строки около 932–999.

Проблемы:

- active config переносится в `config.yaml.lkg`, а не публикуется неизменяемый generation bundle;
- ошибки rename/unlink/stop/withdraw/transition commit частично игнорируются;
- при ошибке после удаления active config нет симметричного rollback;
- first-install orphan config может быть принят за LKG;
- cleanup ошибки не попадают в durable `cleanup_pending`.

`RuntimeOff` должен проходить тот же state machine и тот же LKG protocol, что и активные режимы. Отличаться должна только целевая runtime-фаза.

## Высокий приоритет

### P1-1. `AssertPathConfined` не защищает от symlink/hardlink

Файл: `internal/mihomo/types.go`, строки около 315–328.

Функция делает только `filepath.Clean` + `filepath.Rel`. Она не разрешает canonical path, не проверяет каждый компонент через `Lstat`, не запрещает symlink и не проверяет link count/file identity. Более того, равенство target с allowed directory (`rel == "."`) принимается, хотя комментарий обещает строгое вложение.

`ReadLKGPointer` использует `os.Stat`, а `ReadGenerationBundle` затем читает пути, прошедшие только лексическую проверку. Symlink `generations/<valid-id>` способен перенаправить чтение за пределы хранилища.

Нужны Linux-тесты с:

- symlink в одном из промежуточных компонентов;
- symlink generation directory;
- symlink/hardlink вместо config, snapshot, manifest и pointer;
- заменой проверенного компонента между check и use (TOCTOU).

Для чувствительных операций предпочтительны descriptor-relative primitives (`openat2` с confinement flags либо последовательный `openat`/`O_NOFOLLOW`) вместо проверки строкового пути до обычного `os.Open`.

### P1-2. GC не реализует заявленную защиту всех ссылок и скрывает ошибки

Файл: `internal/mihomo/generation_store.go`, `RunRetentionGC`, строки около 284–360.

Функция получает только `activeGenID`, `lkgGenID` и число recent bundles. Она не читает manifest/draft/pending/recovery artifacts и не может гарантировать, что поколение ими не используется.

Также ошибки `RemoveAll` и финального fsync игнорируются, поэтому coordinator практически не сможет зарегистрировать обещанный `cleanup_pending`. Старые `.tmp` удаляются без проверки recovery references.

Требуется:

- собрать единый protected-set из всех durable artifacts;
- fail closed при повреждённом/нечитаемом artifact;
- не удалять recovery evidence автоматически;
- агрегировать delete/fsync errors и возвращать их;
- тестировать ссылки из каждого типа artifact, ошибки удаления и fsync.

Сейчас coordinator вызывает `RunRetentionGC(..., 2)`, хотя согласованная минимальная политика формулировалась как active + LKG + один previous. Семантику `keepRecent` нужно явно зафиксировать и проверить тестом.

### P1-3. State machine не проверяет допустимость переходов

Файл: `internal/mihomo/coordinator.go`, `transitionManifestLocked`, строки около 237–260.

Функция безусловно увеличивает sequence и присваивает любое `nextState`. Нет таблицы allowed transitions, проверки текущей sequence/state и защиты от повторного/обратного перехода. `TransactionManifest.ValidateSchema` проверяет лишь version, txid и непустой state.

Требуется явная таблица переходов и полная schema validation: enum состояния, обязательные поля для каждой фазы, monotonic sequence и cross-field invariants.

### P1-4. Чтение generation bundle не доказывает его целостность

Файл: `internal/mihomo/generation_store.go`, `ReadGenerationBundle`, строки около 248–281.

Не проверяются:

- `gm.GenerationID == requested genID`;
- config digest против `gm.AppliedConfigDigest` и LKG pointer;
- store snapshot digest против `gm.AppliedStoreDigest`;
- generation number/digests pointer против manifest;
- типы файлов, права и отсутствие links.

Такой bundle нельзя использовать для rollback.

### P1-5. Failpoint `pointer_fsync` не моделирует заявленную границу

Файл: `internal/mihomo/generation_store.go`, строки около 206–216.

`StrictWriteAtomic` уже fsync-ит временный файл и parent directory. `FailPointerFsync` срабатывает только после завершённой durable atomic write. Тест получает ошибку, хотя pointer уже записан и синхронизирован. Это не проверка отказа до durability boundary и создаёт ложную уверенность.

Нужны failpoints внутри atomic write: до rename, после rename до parent fsync и при parent fsync, с последующей классификацией фактического outcome.

### P1-6. Права существующих директорий не исправляются

`os.MkdirAll(..., 0700)` не меняет режим уже существующего каталога. Нет `Chmod`/проверки владельца и mode для base/generations/final directories. Заявление walkthrough о гарантированных `0700` верно только для вновь созданных каталогов при подходящем umask.

Нужны проверки/исправление mode и тест с заранее созданным каталогом `0755`.

## Дополнительные замечания

1. Ошибки восстановления store и очистки в ряде pre-commit веток игнорируются, после чего state выставляется в `Idle`. Если restore не удался, должен быть `recovery_required` с сохранением evidence.
2. `cleanupPending` хранится только в памяти. После рестарта предупреждения теряются; если по спецификации это recovery evidence, оно должно быть durable.
3. Ошибка fsync base directory после записи LKG pointer игнорируется. Сейчас `StrictWriteAtomic` уже синхронизирует parent, поэтому шаг либо нужно удалить как дублирующий, либо обрабатывать как реальную границу, но не оставлять фиктивную гарантию.
4. `AppliedGenerationRecord.ValidateSchema` не проверяет `GenerationID`, digest-формат, runtime enum, listener schema и cross-field invariants.
5. Тест S17 не выполняет store mutation и потому не подтверждает заявленное восстановление pre-mutation store при first-install rollback.
6. Тест S16 допускает восстановление как generation 1, так и generation 2, хотя корректное ожидаемое поколение должно быть однозначным.

## Обязательный порядок исправления

1. Исправить rollback journal ordering и startup recovery для незавершённого отката.
2. Исправить `StatePublished` roll-forward: commit errors нельзя игнорировать и после них нельзя удалять evidence.
3. Добавить fail-closed first-install/orphan-state preflight.
4. Сделать snapshot предыдущего store обязательным и доказанным во всех apply путях.
5. Сделать rollback зависимым от полной cryptographic validation LKG; запретить старт без доказанного config.
6. Перевести `RuntimeOff` на общий безопасный transaction protocol.
7. Реализовать настоящий symlink/hardlink-safe filesystem access.
8. Переделать GC protected-set и error propagation.
9. Добавить allowed-transition table и фазовые schema invariants.
10. Переставить failpoints на реальные durability boundaries и добавить crash/restart tests.

## Критерии повторной приёмки Gate 1

Gate 1 можно повторно предъявлять только когда:

- все перечисленные P0/P1 закрыты кодом и негативными тестами;
- каждый test name соответствует реальному production path, а не ручной имитации конечного файла;
- crash после каждой durable-фазы приводит к однозначному roll-forward либо rollback;
- ни один путь ошибки восстановления не возвращает `Idle` без доказанных postconditions;
- orphan active config на первой установке приводит к `recovery_required`;
- corrupt/tampered/symlinked generation никогда не используется для rollback;
- GC никогда не удаляет referenced/recovery generation и сообщает реальные ошибки;
- `RuntimeOff` проходит те же failure-injection сценарии;
- повторно проходят `go test -race ./internal/mihomo ./internal/strictfs` и `go test ./internal/...`;
- отдельно выполнен Linux filesystem test suite на ext4/tmpfs, а не только логические tempdir-тесты;
- walkthrough содержит фактический вывод команд и не заявляет гарантий, которых тесты не доказывают.

## Что не выполнялось в рамках аудита

- код не изменялся;
- IPK не собирался;
- установка на роутеры не выполнялась;
- сетевые настройки, WireGuard, Mihomo runtime и пользовательские данные не затрагивались.

