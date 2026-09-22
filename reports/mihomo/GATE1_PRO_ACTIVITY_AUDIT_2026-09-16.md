# Gate 1: аудит фактической работы Gemini Pro

Дата: 2026-09-16  
Область: `internal/mihomo`, `internal/strictfs`  
Источник проверки: журнал действий агента и текущее рабочее дерево.

## Короткий вердикт

Агент действительно изменяет реализацию и тесты, а не только готовит отчёты. Направление в целом правильное, но Gate 1 пока нельзя считать завершённым.

На текущем снимке независимо проходят:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
```

Однако зелёный результат не покрывает несколько оставшихся нарушений fail-closed и durability. Деплой и переход к следующему Gate пока преждевременны.

## Что агент реально сделал правильно

1. Убрал гонку `State()`/записи состояния: запись теперь выполняется через `setState()` под `c.mu.Lock()`; добавлен race-тест S14.
2. `handleApplyFailureLocked` больше не переводит координатор в `IDLE`, если terminal cleanup завершился ошибкой.
3. `cleanupTxArtifactsLocked` теперь возвращает ошибку записи cleanup-журнала и ошибку перехода в `TERMINAL_CLEANUP`.
4. Повреждённый или невалидный cleanup-журнал больше не удаляется молча: recovery возвращает `ErrRecoveryRequired`.
5. Rollback прекращается, если не удалось надёжно записать `ROLLBACK_IN_PROGRESS`.
6. При указанном `LKGGenerationID` ошибка чтения bundle теперь является ошибкой rollback; активный конфиг в этом случае не удаляется.
7. Компенсация bridge больше не исключена специально для `RuntimeOff`.
8. Ошибки bridge checkpoint больше не подавляются: координатор устанавливает `RECOVERY_REQUIRED` и возвращает ошибку.
9. S11 теперь проверяет настоящий `cleanupJournalFile`, а не посторонний draft-журнал.

## Что всё ещё сделано неправильно

### P0. Ошибки cleanup по-прежнему массово игнорируются

В `internal/mihomo/coordinator.go` остаётся много вызовов вида:

```go
c.cleanupTxArtifactsLocked(&manifest)
c.setState(StateIdle)
return err
```

Это встречается как минимум в ранних ветках `MutateAndApply` и в recovery-ветках. Если создание/запись cleanup-журнала либо удаление артефакта завершится ошибкой, код всё равно может поставить `IDLE` и потерять сигнал незавершённой очистки.

Требование:

- каждый вызов `cleanupTxArtifactsLocked` обязан проверять ошибку;
- при ошибке состояние не должно становиться `IDLE`;
- должен сохраняться durable recovery marker;
- вызывающий код должен получить ошибку, содержащую исходную ошибку и ошибку cleanup (`errors.Join` либо эквивалент).

Лучше сделать один helper для завершения неуспешной транзакции до commit boundary, чтобы одинаковая логика не копировалась во многих ветках.

### P0. Перезапись частично выполненного cleanup-журнала всё ещё fail-open

В `processCleanupJournalFilesLocked` ветка `remaining != 0 && changed` делает:

```go
if b, err := json.MarshalIndent(cj, "", "  "); err == nil {
    if err := strictfs.StrictWriteAtomic(...); err != nil {
        // только marker
    }
}
```

Ошибка marshal либо rewrite не попадает в возвращаемый `finalErr`. Метод способен вернуть `nil`, хотя durable список оставшихся файлов не записан.

Требование: любая ошибка marshal/rewrite cleanup-журнала должна возвращаться вызывающему коду и оставлять систему вне `IDLE`.

### P0. Bridge-компенсация пропускается при другой ошибке rollback

Сейчас `syncBridgesLocked` в rollback вызывается только внутри:

```go
if rollbackErr == nil { ... }
```

Если восстановление store, остановка процесса или восстановление config завершилось ошибкой, уже применённые bridge side effects вообще не компенсируются. Это оставляет внешнее состояние сети изменённым.

Требование:

- попытка компенсации bridge должна выполняться независимо от ошибок остальных rollback-компонентов;
- ошибки store/config/runtime/bridge следует агрегировать;
- итог всегда `RECOVERY_REQUIRED`, если хотя бы один компонент не восстановлен.

### P1. `handleApplyFailureLocked` скрывает ошибку самого rollback

При ошибке rollback координатор ставит `RECOVERY_REQUIRED`, но в конце возвращает только `origErr`. Пользователь и журнал верхнего уровня не получают фактическую причину провала rollback.

Требование: возвращать составную ошибку `apply failed + rollback failed`, не теряя обе причины.

### P1. Тест S12 всё ещё не доказывает restart/recovery guarantee

Финальная проверка S12 допускает почти любой результат:

```go
if err == nil && coord2.State() == StateRecoveryRequired { ... }
```

Она не утверждает конкретный ожидаемый итог, не считает повторные `ApplyBridges`, не проверяет сохранённый manifest/marker после рестарта и потому может пройти при неверной реализации.

Требование: зафиксировать один ожидаемый сценарий и проверить:

- side effect выполнен ровно один раз;
- после restart он не повторяется;
- coordinator возвращает ожидаемую ошибку/состояние;
- manifest и recovery marker имеют ожидаемое durable состояние.

### P1. Из старого теста удалён `LKGGenerationID`, что сменило проверяемый сценарий

Агент исправил падение `TestCoordinator_ManifestRecovery_RollbackToLKG`, удалив из fixture `LKGGenerationID`. Теперь тест проверяет legacy-файл `lkgConfigFile`, а не восстановление по объявленному generation bundle.

Это допустимо только как отдельный legacy fallback test. Нужны два разных теста:

1. legacy fallback без `LKGGenerationID`;
2. объявленный `LKGGenerationID`, чей bundle отсутствует/повреждён — строгое fail-closed без удаления active config.

### P2. Scope отчёта агента всё ещё загрязнён

В журнале агент выводил огромный `git diff HEAD`, создавал `diff_output.txt` и использовал временный Python-скрипт вне репозитория. Это не дефект продукта, но затрудняет аудит. Итоговый handoff должен содержать только allowlist файлов Gate 1, точный diff-stat и команды проверки; `diff_output.txt` не должен входить в изменения.

## Обязательный следующий порядок работы для агента

1. Исправить все игнорируемые ошибки `cleanupTxArtifactsLocked`.
2. Сделать fail-closed перезапись cleanup-журнала.
3. Выполнять bridge-компенсацию независимо от прочих rollback-ошибок и агрегировать ошибки.
4. Возвращать composite apply/rollback error.
5. Усилить S12 и разделить legacy-LKG/bundle-LKG тесты.
6. Запустить:

```bash
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check -- internal/mihomo internal/strictfs
```

7. Предоставить scoped diff/stat только по Gate 1. Не собирать IPK и не выполнять деплой.

## Критерий готовности

Gate 1 можно принять только после устранения всех P0/P1 выше и независимого повторного ревью. До этого не переходить на Flash и не начинать следующий архитектурный этап.
