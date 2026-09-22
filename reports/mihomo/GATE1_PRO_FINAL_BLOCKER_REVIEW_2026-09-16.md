# Gate 1: финальное ревью — оставшийся блокер

Дата: 2026-09-16  
Область: `internal/mihomo`, `internal/strictfs`

## Вердикт

Последняя правка корректно закрыла предыдущие три пункта, но выявлен один более широкий P0-дефект восстановления desired store. Gate 1 пока не принимается, однако до приёмки осталась одна локальная архитектурная правка и тесты её ветвей.

Независимая проверка текущего дерева:

```text
gofmt -d coordinator.go coordinator_legacy_test.go gate1_legacy_test.go clean
go test -count=1 ./internal/strictfs ./internal/mihomo              PASS
go test -race -count=1 ./internal/mihomo                            PASS
git diff --check -- internal/mihomo internal/strictfs               PASS
```

## Что подтверждено исправленным

1. Ошибка `recoverCleanupJournalLocked` теперь блокирует startup, сохраняет cleanup journal, создаёт marker и устанавливает `RECOVERY_REQUIRED`.
2. Ошибка `ListActiveBridges` больше не трактуется как пустой список: bridge mutations не выполняются, rollback становится `RECOVERY_REQUIRED`.
3. Частичная mutation, вернувшая ошибку, восстанавливает pre-mutation snapshot и проверяет digest.
4. Ошибка восстановления snapshot оставляет систему в `RECOVERY_REQUIRED`.
5. S12 честно переименован в проверку блокировки startup через recovery marker.
6. `setStateLocked` удалён; все записи состояния используют mutex-safe `setState`.
7. Ошибки apply/rollback/cleanup объединяются через `errors.Join`.

## P0: `abortEarlyLocked` удаляет snapshot без восстановления store

Desired store изменяется внутри `mutateFn`. После успешной mutation существует несколько ошибок, возникающих до runtime commit:

- не удалось создать post-mutation snapshot;
- не удалось записать candidate config;
- static validation candidate завершилась ошибкой;
- не удалось записать/перевести manifest в `CANDIDATE_BUILT`;
- не удалось опубликовать generation bundle;
- не удалось записать `CANDIDATE_PUBLISHED`.

Все эти ветки вызывают:

```go
return c.abortEarlyLocked(&manifest, err)
```

Но `abortEarlyLocked` сейчас выполняет только terminal cleanup. Он удаляет `PreMutationStoreSnapshotFile`, после чего ставит `IDLE`. Desired store остаётся изменённым, хотя apply не состоялся, а rollback snapshot уже уничтожен.

Это нарушает атомарность compile/apply транзакции.

## Требуемое исправление

`abortEarlyLocked` должен отвечать не только за cleanup, но и за восстановление desired store, если manifest содержит `PreMutationStoreSnapshotFile` и mutation могла уже начаться.

Рекомендуемая схема:

```go
func (c *ApplyCoordinator) abortEarlyLocked(m *TransactionManifest, cause error) error {
    var recoveryErrs []error

    if m.PreMutationStoreSnapshotFile != "" {
        if err := c.cfg.StoreTx.RestoreSnapshotFile(m.PreMutationStoreSnapshotFile); err != nil {
            recoveryErrs = append(recoveryErrs, fmt.Errorf("restore pre-mutation store: %w", err))
        } else {
            digest, err := c.cfg.StoreTx.CurrentDigest()
            if err != nil || digest != m.BaseDesiredStoreDigest {
                recoveryErrs = append(recoveryErrs, ...)
            }
        }
    }

    if len(recoveryErrs) != 0 {
        c.setState(StateRecoveryRequired)
        // marker; snapshot и manifest НЕ удалять
        return errors.Join(append([]error{cause}, recoveryErrs...)...)
    }

    if err := c.cleanupTxArtifactsLocked(m); err != nil {
        c.setState(StateRecoveryRequired)
        return errors.Join(cause, err)
    }

    c.setState(StateIdle)
    return cause
}
```

Важные детали:

1. Для проверки использовать digest из manifest (`BaseDesiredStoreDigest`), а не неявно захваченную локальную переменную.
2. При ошибке restore/digest snapshot и manifest должны остаться на диске.
3. После переноса восстановления в helper удалить дублирующую ручную restore-логику из веток `mutErr` и compile error либо гарантировать, что helper не выполняет её повторно.
4. Не выполнять cleanup, если восстановление store не подтверждено.

## Обязательные тесты

Добавить table-driven тест минимум для следующих failpoints после успешной mutation:

1. `CreateSnapshotFile(txid + "-post")` failure;
2. candidate config validation failure;
3. generation bundle publication failure.

Для каждого сценария проверить:

- исходная ошибка возвращена;
- store data и digest равны pre-mutation состоянию;
- при успешном restore coordinator возвращается в `IDLE`;
- manifest/snapshot очищены только после подтверждённого restore.

Отдельно минимум для одного из этих сценариев включить `restoreFail` и проверить:

- `RECOVERY_REQUIRED`;
- recovery marker существует;
- manifest существует;
- pre-mutation snapshot существует;
- частично изменённый store не выдаётся за успешно применённый.

## Следующий порядок работы

1. Исправить только `abortEarlyLocked` и убрать дублирование restore.
2. Добавить указанные tests/failpoints.
3. Не использовать `git checkout`, `git restore`, `git reset` и массовые Python-патчи.
4. Выполнить:

```bash
gofmt -w internal/mihomo/coordinator.go \
  internal/mihomo/coordinator_legacy_test.go \
  internal/mihomo/gate1_legacy_test.go
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check -- internal/mihomo internal/strictfs
```

5. Не собирать IPK и не выполнять деплой.

## Приёмка

После закрытия этого P0 нужен короткий повторный аудит. Если новые регрессии не появятся, Gate 1 можно будет принять и перейти к следующему этапу/Flash для механической работы.
