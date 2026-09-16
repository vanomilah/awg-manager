# Аудит незавершенной реализации Mihomo Gate 1

Дата: 2026-09-16  
Репозиторий: `E:\AWGM\awg-manager`  
Проверенный план агента: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\task.md`

## Итоговый вердикт

Работа агента **не завершена и не готова ни к сборке, ни к установке на роутер**.

`task.md` прямо оставляет Slice 4–7 невыполненными, но и отметки о завершении Slice 1–3 не подтверждаются кодом и тестами. Реализация содержит полезные заготовки типов, generation store и failpoint-тестов, однако ключевые гарантии Gate 1 — безопасная файловая модель, настоящий CAS, commit boundary, recoverable RuntimeOff и воспроизводимый generation bundle — пока отсутствуют.

Ничего из проверенного в рамках этого аудита не исправлялось, IPK не собирался, установка не выполнялась.

## Что агент фактически сделал

- Добавил новые состояния и поля transaction manifest.
- Добавил `BridgeOperation`, `CleanupJournal`, clone и частичную phase validation.
- Добавил `GenerationStore` и публикацию каталогов поколений.
- Добавил `SecureDir` как обертку над файловыми операциями.
- Добавил большой набор Gate 1 тестов и failpoints.
- Начал отдельную ветку обработки `RuntimeOff`.

Это полезный каркас, но не законченная транзакционная реализация.

## P0 — блокирующие дефекты

### 1. `SecureDir` не является descriptor-relative и не устраняет TOCTOU

Файл: `internal/strictfs/secure_dir.go:9-116`.

`SecureDir` хранит строковый `basePath`, проверяет путь через `EvalSymlinks`, а затем отдельно вызывает обычные path-based операции:

- `os.ReadFile`;
- `StrictWriteAtomic(path, ...)`;
- `os.RemoveAll`;
- `os.Rename`.

Между проверкой и операцией компонент пути можно заменить. Это именно та TOCTOU-модель, от которой Gate 1 должен отказаться. Комментарий о предотвращении TOCTOU не соответствует реализации.

Требуется Linux-реализация от открытого fd корневого каталога: компонентный walk с `openat2` (`RESOLVE_BENEATH`, `RESOLVE_NO_SYMLINKS`, при необходимости `RESOLVE_NO_XDEV`) либо эквивалентная строго проверенная цепочка `openat`/`fstatat`/`renameat`/`unlinkat`. Нужны build-tagged Linux и безопасный тестовый fallback, не выдающий себя за равную гарантию.

### 2. `casManifest` не выполняет compare-and-swap

Файл: `internal/mihomo/coordinator.go:237-275`.

Текущая функция:

1. проверяет переданный объект в памяти;
2. сериализует его;
3. перезаписывает manifest.

Она не читает durable manifest, не сравнивает ожидаемые `TxID`, `Sequence` и `State`, не распознает неоднозначный результат записи и не защищает от stale writer.

Дополнительно `checkpointManifestLocked` и `transitionManifestLocked` сначала мутируют исходный объект (`Sequence`, `State`, `UpdatedAt`), а затем пишут его. При ошибке persist память уже содержит неподтвержденное состояние.

Требуется immutable-next CAS:

- прочитать и валидировать durable current;
- сравнить `(TxID, Sequence, State)` с expected;
- сформировать clone `next` без изменения current;
- записать атомарно и fsync;
- после ambiguous error перечитать durable state и классифицировать исход;
- публиковать `next` в память только после подтвержденной записи.

### 3. `RuntimeOff` пересекает commit boundary без журнала и игнорирует ошибки

Файл: `internal/mihomo/coordinator.go:978-1045`.

Критические операции выполняются без durable intent/outcome:

- игнорируется ошибка digest;
- игнорируются ошибки rename/unlink;
- игнорируется ошибка `StopAndWait`;
- игнорируется ошибка withdrawal bridges;
- `verified-active` пишется до полноценного `StateCommitIntent`;
- ошибка перехода в `StateCommitted` игнорируется;
- snapshot и manifest удаляются с игнорированием ошибок;
- затем состояние принудительно делается `idle`.

После сбоя невозможно надежно определить, что было выполнено, что надо продолжить, а что допустимо откатить. Это нарушает главный recovery-инвариант.

### 4. Generation bundle содержит не то состояние

Файл: `internal/mihomo/generation_store.go:59-160`.

`PublishStagedBundle` принимает `preMutationSnapshotFile` и копирует его как `store.snapshot.json`. В результате bundle связывает новый applied config со **старым** состоянием store. Для восстановления поколения нужен кандидатный post-mutation snapshot, соответствующий digest нового поколения.

Также config пишется безусловно. Это несовместимо с корректным `RuntimeOff`, где поколение допустимо без `config.yaml`. В `GenerationManifest` отсутствует полноценная семантика присутствия конфигурации, а `ReadGenerationBundle` ожидает config всегда.

### 5. Собственные Gate 1 тесты не проходят

Команда:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
```

Результат: `internal/strictfs` проходит на Windows, `internal/mihomo` падает.

Подтвержденные падения:

- S10: staging-каталоги `.tmp.gen-*` остаются после шести вариантов ошибок fsync/rename/pointer;
- S15: при ошибке manifest write после swap ожидался `recovery_required`, получен `idle`;
- S16: rollback/non-consuming LKG ломается на `eval parent symlinks: Access is denied`;
- S17: rollback первой установки оставляет LKG pointer;
- S23: verified-active failure блокируется той же ошибкой доступа;
- retention GC не удаляет obsolete generation;
- в конце Windows не может удалить тестовый exe — это отдельная проблема окружения, но она не отменяет перечисленные функциональные падения.

## P1 — архитектурные недоделки

### 6. Transaction schema неполная

Файл: `internal/mihomo/types.go:186-268`.

Не хватает или не разведены явно:

- previous generation ID и candidate generation ID;
- rollback target generation ID;
- candidate post-mutation store snapshot path/digest;
- previous config digest;
- полноценные abort/rollback/candidate-published состояния;
- sequence и generation IDs в cleanup journal;
- независимое `ConfigPresent` в immutable generation manifest.

Сейчас новые поля смешаны с legacy `LKGGenerationID`, `StoreSnapshotFile`, `LKGConfigFile` и `PublishedBridges`, из-за чего источник истины остается неоднозначным.

### 7. Phase validation слишком слабая

Файл: `internal/mihomo/types.go:404-424`.

`ValidateSchemaForPhase` проверяет только три частных случая. Она не проверяет допустимые переходы, согласованность runtime mode/config presence, bridge journal, cleanup journal, digests предыдущего/кандидатного поколения и commit boundary.

### 8. Staging cleanup ненадежен

Файл: `internal/mihomo/generation_store.go:107-116`.

Обе попытки `RemoveAll` игнорируют ошибки. Это уже проявилось утечками `.tmp.gen-*` в S10. Cleanup должен быть durable, повторяемым и наблюдаемым, а не best effort с `_ =`.

### 9. GC не защищает все живые ссылки

Retention GC не должен удалять поколения, на которые ссылаются:

- verified-active;
- LKG pointer;
- активный transaction manifest;
- draft/pending input;
- recovery/rollback target;
- cleanup journal.

Текущий тест retention уже падает, а path-based удаление и игнорирование ошибок делают поведение еще менее надежным.

## Оценка `task.md`

| Отметка агента | Фактическая оценка |
|---|---|
| Slice 1 Types & CAS Logic — сделано | Типы частично добавлены; CAS отсутствует |
| Slice 2 Snapshots & Durable FS — сделано | Generation store частичный; SecureDir небезопасен; тесты падают |
| Slice 3 RuntimeOff & Active Swap — сделано | Commit/recovery semantics некорректны |
| Slice 4–7 — не сделано | Подтверждается самим task.md |

Итого: это ранняя незавершенная реализация, а не завершенные первые три slice.

## Обязательный порядок исправления

1. **Не продолжать UI и новые возможности**, пока не зеленый backend Gate 1.
2. Зафиксировать точную state machine и допустимые переходы на бумаге/в коде.
3. Реализовать настоящий descriptor-relative `SecureDir` для Linux.
4. Реализовать immutable true CAS с durable reread и ambiguous-write resolution.
5. Развести previous/candidate/verified поколения и post-mutation snapshot.
6. Сделать generation bundle полным и самодостаточным, включая `RuntimeOff` без config.
7. Публиковать candidate bundle **до** destructive swap.
8. Ввести durable intent/applied/verified для swap, process lifecycle и bridge operations.
9. Сделать recovery и rollback идемпотентными; после commit boundary rollback запрещен.
10. Сделать cleanup journal и GC повторяемыми и не теряющими ошибки.
11. Добить Slice 4–7 только после исправления Slice 1–3.
12. Повторить unit/failpoint/race/crash tests на Linux; затем только интеграционный тест на роутере.

## Критерий повторной приемки

Работу нельзя считать завершенной, пока одновременно не выполнено следующее:

- `go test -count=1 ./internal/strictfs ./internal/mihomo` проходит;
- Linux tests подтверждают защиту от symlink/rename races;
- stale writer не может перезаписать manifest;
- crash в каждой точке до/после swap и commit восстанавливается детерминированно;
- `RuntimeOff` восстанавливается без обязательного `config.yaml`;
- generation bundle содержит согласованные config/store/bridge/runtime данные;
- GC не удаляет ни одно поколение с живой ссылкой;
- нет `_ =` на durability-critical операциях;
- end-to-end: prepare -> publish candidate -> swap -> verify -> commit -> restart -> rollback/recovery.

## Инструкция следующему агенту

Не принимать галочки из `task.md` как доказательство. Начать с восстановления красных Gate 1 тестов и исправлять архитектуру снизу вверх. Не собирать IPK и не выполнять деплой до прохождения повторной приемки. Не использовать `--force-reinstall`.
