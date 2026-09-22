# Gate 1: ревью восстановительного diff Gemini Pro

Дата: 2026-09-16  
Область: `internal/mihomo/coordinator.go`, `coordinator_legacy_test.go`, `gate1_legacy_test.go`

## Вердикт

Этот diff заметно лучше предыдущей попытки и возвращает почти все изменения, потерянные после `git checkout`. Однако Gate 1 пока нельзя принять: остались два существенных fail-closed дефекта и один тест создаёт более сильное впечатление, чем фактически проверяет.

Независимая проверка текущего дерева:

```text
gofmt -d ...                                           clean
go test -count=1 ./internal/strictfs ./internal/mihomo PASS
go test -race -count=1 ./internal/mihomo               PASS
```

## Что теперь исправлено корректно

1. Возвращён mutex-safe setter состояния и race-тест S14.
2. Ранние apply-ветки сведены к `abortEarlyLocked`, который проверяет cleanup error и не ставит `IDLE` при его провале.
3. `handleApplyFailureLocked` снова учитывает rollback и cleanup errors.
4. Ошибки bridge checkpoint после apply/verify больше не подавляются.
5. Cleanup journal возвращает ошибки marshal/rewrite.
6. Rollback агрегирует ошибки и пытается компенсировать bridges независимо от других ошибок.
7. Missing generation bundle проверяется отдельно от legacy LKG, включая active config, manifest, marker и state.
8. S12 получил счётчик вызовов bridge runtime и детерминированный fail-closed результат.

## Оставшиеся обязательные исправления

### P0. Ошибка `recoverCleanupJournalLocked` игнорируется

В `RecoverOnStartup` остаётся вызов:

```go
c.recoverCleanupJournalLocked()
```

Результат не проверяется. При повреждённом cleanup-журнале метод возвращает `ErrRecoveryRequired`, но startup продолжает draft/manifest recovery и в конце способен выставить `IDLE`.

Нужно:

```go
if err := c.recoverCleanupJournalLocked(); err != nil {
    c.setState(StateRecoveryRequired)
    // сохранить marker, не затирая исходную причину
    return fmt.Errorf("recover cleanup journal: %w", err)
}
```

Добавить restart-тест: невалидный cleanup journal должен оставить файл на месте, создать marker, вернуть `ErrRecoveryRequired` и установить `StateRecoveryRequired`.

### P0. Rollback продолжает bridge sync после ошибки `ListActiveBridges`

Текущий алгоритм:

```go
cb, err := c.cfg.BridgeRuntime.ListActiveBridges(ctx)
if err == nil {
    currentBridges = cb
}
// при err список остаётся пустым
c.syncBridgesLocked(ctx, m, currentBridges, targetBridges)
```

Если получить фактическое состояние bridge не удалось, пустой список нельзя считать достоверным. `syncBridgesLocked` может повторно применить уже существующие side effects или выполнить неверную компенсацию.

Нужно:

- добавить ошибку `ListActiveBridges` в `rollbackErrs`;
- не вызывать `syncBridgesLocked`, когда текущее bridge-состояние неизвестно;
- записать recovery marker;
- завершить rollback в `RECOVERY_REQUIRED`.

Добавить тест с `listFail`, который подтверждает ноль новых apply/withdraw calls после ошибки получения состояния.

### P1. S12 не проверяет restart reconciliation

После первого checkpoint failure уже существует `recovery.marker`. Новый coordinator в `RecoverOnStartup` сразу обнаруживает marker и выходит до чтения manifest и bridge reconciliation. Поэтому утверждение `applyCalls == 1` после restart верно тривиально: recovery-код bridge вообще не запускался.

Текущий S12 полезен как тест fail-closed marker gate, но его нужно так и назвать, например:

```text
TestCoordinator_Gate1_S12_BridgeCheckpointFailure_BlocksStartup
```

Если требуется гарантия идемпотентного resume/reconcile, нужен отдельный тест с явно моделируемым operator action `retry/recover` после подтверждённого снятия marker. Не следует смешивать его с автоматическим startup, который намеренно заблокирован marker-файлом.

### P1. `abortEarlyLocked` может уничтожить snapshot после частичной mutation

При ошибке `mutateFn` вызывается `abortEarlyLocked`, который удаляет snapshot через cleanup, но не восстанавливает store из `PreMutationStoreSnapshotFile`.

Если `mutateFn` успел частично изменить store и затем вернул ошибку, desired store останется частично изменённым, а единственный snapshot будет удалён.

Нужно до cleanup:

1. восстановить `PreMutationStoreSnapshotFile`;
2. проверить digest восстановленного store;
3. при ошибке восстановления перейти в `RECOVERY_REQUIRED` и сохранить snapshot/manifest;
4. cleanup выполнять только после подтверждённого восстановления.

Добавить тест mutate-функции, которая изменяет fake store и затем возвращает ошибку.

## Неблокирующие замечания

1. Имя `setStateLocked` теперь вводит в заблуждение: метод сам берёт mutex через `setState`. Лучше оставить только `setState`, если нет настоящих вызовов под уже удерживаемым `c.mu`.
2. Для составных ошибок предпочтительнее `errors.Join`, чтобы `errors.Is/As` видел и исходную, и cleanup/rollback ошибки. Текущие строки через `%v` часть error chain скрывают.
3. Сообщение в recovery ветке `StateCommitIntent` говорит `rollback succeeded`, хотя там выполнялся roll-forward commit. Следует заменить на `commit roll-forward succeeded but cleanup failed`.

## Точное следующее задание агенту

1. Исправить три пункта P0/P1 выше без `git checkout`, `git restore`, `git reset` и массовой перезаписи файлов.
2. Добавить три узких теста:
   - corrupt cleanup journal blocks startup;
   - bridge list failure causes no bridge mutations;
   - partial mutate error restores pre-mutation store before cleanup.
3. Переименовать S12 в соответствии с реально проверяемой гарантией либо добавить отдельный explicit-recovery тест.
4. Запустить:

```bash
gofmt -w internal/mihomo/coordinator.go \
  internal/mihomo/coordinator_legacy_test.go \
  internal/mihomo/gate1_legacy_test.go
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check -- internal/mihomo internal/strictfs
```

5. Не создавать `diff_output.txt`, не собирать IPK и не выполнять деплой.

## Приёмка

После этих исправлений нужен ещё один короткий независимый аудит. До него Gate 1 и переход на Flash не подтверждены.
