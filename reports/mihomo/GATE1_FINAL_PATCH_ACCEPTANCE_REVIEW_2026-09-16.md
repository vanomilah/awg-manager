# Gate 1: приёмочное ревью финального patch

Дата: 2026-09-16  
Проверены:

- `GATE1_PRO_FINAL_BLOCKER_RESOLUTION_2026-09-16.md`
- `GATE1_FINAL_DIFF_2026-09-16.patch`
- фактическое рабочее дерево `internal/mihomo`, `internal/strictfs`

## Вердикт

Patch полностью применён к текущему рабочему дереву (`git apply --check --reverse` проходит), основной P0 с восстановлением desired store исправлен корректно. Однако окончательно принимать Gate 1 пока нельзя: остаются два сценария потери учёта файлов после успешного atomic rename и последующей ошибки durability.

Независимые проверки:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo PASS
go test -race -count=1 ./internal/mihomo               PASS
git diff --check -- internal/mihomo internal/strictfs  PASS
```

## Что подтверждено исправленным

1. `abortEarlyLocked` централизованно восстанавливает pre-mutation store.
2. Восстановленный digest сравнивается с `m.BaseDesiredStoreDigest`.
3. При restore/digest failure cleanup не запускается; manifest и snapshot сохраняются.
4. После подтверждённого восстановления выполняется terminal cleanup и выставляется `IDLE`.
5. Убрано дублирование restore из mutation/compile error веток.
6. Добавлены post-snapshot, validation и generation publication тесты.
7. Добавлен тест restore failure с сохранением manifest/snapshot.

## P0. Candidate config теряется из учёта при ошибке после rename

Текущий код:

```go
candidatePath := ...
if err := strictfs.StrictWriteAtomic(candidatePath, data, 0600); err != nil {
    return c.abortEarlyLocked(...)
}
manifest.CandidateConfigFile = candidatePath
```

Но `StrictWriteAtomic` выполняет:

1. запись и fsync temp;
2. `os.Rename(tmpName, dest)`;
3. failpoint `FPAfterRenamePreSync` / `SyncDir(dir)`.

Следовательно, функция может вернуть ошибку после того, как `candidatePath` уже существует. В этот момент `manifest.CandidateConfigFile` ещё пуст, поэтому `abortEarlyLocked` не добавит файл в cleanup journal, затем может удалить manifest и выставить `IDLE`. На диске останется неучтённый candidate config.

Исправление:

```go
candidatePath := ...
manifest.CandidateConfigFile = candidatePath // до StrictWriteAtomic
if err := strictfs.StrictWriteAtomic(...); err != nil {
    return c.abortEarlyLocked(...)
}
```

Добавить тест с `strictfs.FPAfterRenamePreSync`, проверяющий, что после clean abort candidate-файл отсутствует.

## P0. Candidate generation не зафиксирован durable до публикации

`manifest.CandidateGenerationID` присваивается только в памяти перед `PublishStagedBundle`. После присваивания manifest не checkpoint-ится до завершения публикации:

```go
manifest.CandidateGenerationID = newGenID
err := c.genStore.PublishStagedBundle(...)
```

В `PublishStagedBundle` staging directory переименовывается в финальный `generations/<genID>`, а затем выполняются fsync `generations/` и base directory. При ошибке `FailParentDirFsync` либо реальном fsync failure финальная generation directory уже может существовать, но durable manifest всё ещё не содержит `CandidateGenerationID`.

После `abortEarlyLocked` manifest удаляется, а generation directory не входит в cleanup journal. Получается неучтённый orphan bundle при состоянии `IDLE`.

Требование:

1. До публикации durable записать intent/checkpoint с `CandidateGenerationID` (отдельное состояние либо CAS того же состояния с увеличением sequence).
2. Terminal cleanup должен уметь удалять непринятую candidate generation либо явно заносить её в durable cleanup journal.
3. Нельзя удалять active/LKG generation; удаляется только generation текущей незавершённой транзакции после проверки ID/digest.
4. При неоднозначном результате rename/fsync предпочтителен fail-closed recovery, если безопасное удаление доказать нельзя.

Добавить тест минимум для `GenerationStoreHooks{FailParentDirFsync: true}`:

- публикация возвращает ошибку после rename;
- coordinator не оставляет неучтённый bundle в `IDLE`;
- либо bundle гарантированно удалён через durable cleanup;
- либо manifest/marker сохранены и состояние `RECOVERY_REQUIRED`.

## Почему текущий generation test недостаточен

Тест использует `FailFileFsync`, который срабатывает до финального rename staging directory. `defer RemoveAll(stagingName)` успешно убирает staging, поэтому он не покрывает неоднозначную ошибку после rename.

Нужен именно `FailParentDirFsync` или эквивалентный failpoint после rename.

## Последнее задание агенту

1. Вернуть присваивание `manifest.CandidateConfigFile` перед `StrictWriteAtomic` и добавить after-rename failpoint test.
2. Сделать durable учёт `CandidateGenerationID` до публикации.
3. Добавить безопасную terminal cleanup/recovery обработку candidate generation.
4. Добавить post-rename generation fsync failure test.
5. Не использовать `git checkout`, `git restore`, `git reset` и массовые Python-патчи.
6. Запустить:

```bash
gofmt -w internal/mihomo/coordinator.go \
  internal/mihomo/coordinator_legacy_test.go \
  internal/mihomo/gate1_legacy_test.go \
  internal/mihomo/generation_store.go
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check -- internal/mihomo internal/strictfs
```

7. Не собирать IPK и не выполнять деплой.

## Итог

Основной P0 из прошлого ревью закрыт. Остались два узких, но настоящих durability-блокера на границе atomic rename/fsync. После их исправления и одного короткого аудита Gate 1 можно принять.
