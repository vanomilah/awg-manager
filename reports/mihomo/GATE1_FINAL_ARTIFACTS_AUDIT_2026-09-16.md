# Gate 1 — аудит итогового patch и отчёта

Дата: 2026-09-16  
Проверены:

- `GATE1_FINAL_DIFF_2026-09-16.patch`
- `GATE1_PRO_FINAL_BLOCKER_RESOLUTION_2026-09-16.md`
- фактическое состояние `internal/mihomo` в рабочем дереве

## Вердикт

**Не принимать Gate 1 как завершённый.** Patch действительно применён к рабочему дереву, целевые тесты и race detector проходят, однако утверждение отчёта «Все P0 блокеры устранены» не подтверждается. Остались незакрытые crash-consistency окна и некорректная проверка физического удаления staging-каталога.

## Подтверждённая проверка

В WSL выполнено:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/strictfs
ok github.com/hoaxisr/awg-manager/internal/mihomo

go test -race -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo
```

Также `git apply --check --reverse GATE1_FINAL_DIFF_2026-09-16.patch` проходит: содержимое patch присутствует в текущем рабочем дереве.

## Найденные проблемы

### P0 — crash до `candidate_write_intent` оставляет неучтённый post-mutation snapshot

В `MutateAndApply` после мутации создаётся `CandidatePostMutationStoreSnapshotFile` (`coordinator.go:855-860`), затем вызывается произвольный и потенциально длительный `compileFn` (`coordinator.go:864`), и только после компиляции новое имя snapshot попадает в durable manifest через переход в `StateCandidateWriteIntent` (`coordinator.go:885-893`).

Если процесс или питание пропадут после создания post-snapshot, но до сохранения `candidate_write_intent`, на диске останется manifest в `snapshot_secured`, который не содержит путь post-snapshot. Startup recovery восстановит pre-mutation store, но удалить неизвестный post-snapshot не сможет. Это durable orphan и незакрытое crash-window.

Требуется:

1. Либо durably checkpoint-ить путь post-snapshot сразу после его создания и до `compileFn`.
2. Либо не создавать post-snapshot до завершения компиляции и следующего durable intent.
3. Добавить настоящий crash/restart test без `abortEarlyLocked`: сбой сразу после создания post-snapshot и отдельно во время `compileFn`; после recovery не должно оставаться snapshot/candidate/generation артефактов.

### P0 — ошибка удаления старого staging перед публикацией всё ещё подавляется

В `GenerationStore.PublishStagedBundle` остался код:

```go
_ = secureGens.RemoveAll(stagingName)
```

(`generation_store.go:111-115`). Это ровно тот класс подавленной ошибки удаления, который итоговый отчёт объявляет устранённым. После отказа удаления код продолжает работу и вызывает `MkdirAll` над существующим staging-каталогом, а затем записывает в него новый bundle. В каталоге могут остаться файлы предыдущей незавершённой публикации.

Требуется fail-closed поведение: проверить ошибку удаления и затем доказать `os.IsNotExist`; при любом другом результате остановить публикацию и потребовать recovery. Нужен тест с заранее существующим `.tmp.<genID>` и инъекцией ошибки удаления **до** записи нового bundle.

### P1 — проверка удаления staging/final directory принимает ошибки `Stat` за отсутствие

В `RemoveCandidateGeneration` проверки после удаления имеют вид (`generation_store.go:251-253`, `265-267`):

```go
if _, err := os.Stat(path); err == nil {
    return ...still exists
}
```

Любая ошибка, кроме `nil`, трактуется как успешное отсутствие. `EACCES`, I/O error, stale mount и другие ошибки не доказывают, что каталог удалён. Это противоречит формулировке отчёта «физическое отсутствие каталога проверяется».

Требуется принимать успех только при `os.IsNotExist(err)`. `err == nil` означает «остался», любая прочая ошибка должна возвращаться как failure и сохранять recovery artifacts.

### P1 — новое durable-состояние не включено в общую state machine и phase validation

`StateCandidateWriteIntent` объявлено локально в `coordinator.go:24`, но отсутствует среди состояний в `types.go:20-37`. Оно также не включено в:

- `ManifestState.IsValidNext` (`types.go:513-553`);
- `TransactionManifest.ValidateSchemaForPhase` (`types.go:426-446`);
- unit-тесты таблицы допустимых переходов.

Сейчас `transitionManifestLocked` вообще не вызывает `IsValidNext`, поэтому тесты проходят, но модель состояний стала внутренне противоречивой. При последующем включении проверки переходов новый production flow сломается; повреждённый или неполный `candidate_write_intent` сейчас не будет отклонён phase validation.

Требуется перенести константу в `types.go`, определить переходы как минимум:

- `idle -> candidate_write_intent` для apply без mutation;
- `snapshot_secured -> candidate_write_intent`;
- `candidate_write_intent -> candidate_built` или abort/rollback/recovery;

и валидировать обязательные поля intent (`candidate_generation_id`, а при `config_present=true` — `candidate_config_file` и digest согласно выбранному протоколу).

### P1 — crash-recovery test проверяет неверную runtime-гарантию

Новый тест после recovery утверждает лишь, что тестовый Operator, изначально не запущенный, остаётся не запущенным (`gate1_legacy_test.go:1723-1726`). При этом `rollbackActiveLocked` безусловно вызывает `Operator.StopAndWait` даже для `StateCandidateWriteIntent`, где сам код определяет `activeConfigModified=false` (`coordinator.go:1228-1263`).

Это не проверяет заявленное «runtime не затрагивается». Нужен сценарий с уже работающим runtime/LKG до транзакции: crash до swap не должен останавливать или перезапускать действующий runtime. Если архитектурно startup всегда обязан перезапускать процесс, это должно быть явно оформлено отдельной фазой восстановления и проверено на восстановление исходной generation, а не тестом с изначально остановленным Operator.

## Что действительно исправлено

- Путь candidate config и generation ID теперь сохраняются до atomic write candidate config.
- Добавлен restart-test для падения сразу после candidate config rename.
- Ошибка `RemoveCandidateGeneration` теперь распространяется в cleanup/recovery path.
- Ошибки cleanup journal больше не игнорируются на startup.
- Целевые unit-тесты и race detector проходят.

Эти изменения полезны, но не закрывают Gate 1 полностью.

## Минимальный порядок исправления

1. Закрыть окно post-mutation snapshot до durable intent.
2. Убрать подавление ошибки первичного удаления `.tmp.<genID>` в `PublishStagedBundle`.
3. Исправить `Stat`-проверки на строгое `os.IsNotExist`.
4. Встроить `candidate_write_intent` в единую state machine и phase validation.
5. Добавить тест с работающим runtime до crash-before-swap и зафиксировать ожидаемую гарантию.
6. Повторить unit/race тесты и добавить failpoint matrix для каждого файлового side effect до и после durable checkpoint.

## Критерий повторной приёмки

Gate 1 можно закрывать только если каждый создаваемый файловый артефакт либо уже записан в durable manifest/journal до появления на диске, либо детерминированно обнаруживается и удаляется startup recovery; ошибки `unlink/remove/stat/fsync` не подавляются; а crash до swap доказуемо не меняет работающий runtime.
