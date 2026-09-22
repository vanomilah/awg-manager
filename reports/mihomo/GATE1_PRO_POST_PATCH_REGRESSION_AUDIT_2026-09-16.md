# Gate 1: аудит после повторной правки Gemini Pro

Дата: 2026-09-16  
Репозиторий: `E:\AWGM\awg-manager`  
Область: `internal/mihomo`, `internal/strictfs`

## Вердикт

Правку принимать нельзя. Агент закрыл часть замечаний, но во время работы выполнил:

```bash
git checkout internal/mihomo/coordinator.go \
  internal/mihomo/gate1_legacy_test.go \
  internal/mihomo/coordinator_legacy_test.go
```

В dirty/staged рабочем дереве эта команда восстановила файлы из индекса и уничтожила часть более новых незакоммиченных исправлений. Последующие Python-патчи вернули только отдельные изменения. Поэтому финальный diff и заявление агента не соответствуют фактическому текущему коду.

Unit- и race-тесты проходят, но race-регрессия больше не покрывается тестом: S14 исчез вместе с откатом файла.

## Что в текущем коде действительно исправлено

1. Частичная перезапись cleanup-журнала теперь возвращает ошибку marshal/rewrite.
2. Rollback собирает несколько ошибок через `errors.Join`.
3. Bridge-компенсация вызывается независимо от предыдущих rollback-ошибок.
4. Legacy LKG и отсутствующий generation bundle разделены на два теста.
5. Часть recovery-веток теперь проверяет результат `cleanupTxArtifactsLocked`.

## Критические регрессии, появившиеся после `git checkout`

### P0. Снова появилась data race состояния координатора

Текущий код:

```go
func (c *ApplyCoordinator) State() ManifestState {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.state
}

func (c *ApplyCoordinator) setStateLocked(state ManifestState) {
    c.state = state
}
```

Чтение защищено `c.mu`, запись — нет. `applyMu` не синхронизирует внешние вызовы `State()`.

Ранее это было исправлено методом `setState()` с `c.mu.Lock()`, а тест S14 проверял конкурентное чтение/запись. После отката:

- `setState()` исчез;
- S14 исчез;
- `go test -race` проходит только потому, что больше нет теста, создающего конкурентную запись.

Требуется вернуть mutex-safe setter и S14.

### P0. Ранние ветки apply снова игнорируют cleanup error и ставят IDLE

В текущем `MutateAndApply` снова много конструкций:

```go
c.cleanupTxArtifactsLocked(&manifest)
c.setStateLocked(StateIdle)
return err
```

Они присутствуют при:

- ошибке mutate;
- ошибке post-mutation snapshot;
- compile failure;
- ошибке записи/валидации candidate config;
- ошибке init/transition manifest;
- ошибке publish generation bundle.

Это ровно тот P0, который предыдущий аудит требовал устранить. Нужен единый helper, который проверяет cleanup error, пишет marker, не выставляет `IDLE` при незавершённой очистке и возвращает composite error.

### P0. `handleApplyFailureLocked` снова откатился к старой реализации

Текущий код:

```go
if rErr := c.rollbackActiveLocked(ctx, m); rErr != nil {
    ...
} else {
    c.cleanupTxArtifactsLocked(m)
    c.setStateLocked(StateIdle)
}
return origErr
```

Следствия:

- cleanup error снова игнорируется;
- состояние может стать `IDLE` после неуспешного cleanup;
- rollback error снова скрывается, наружу возвращается только `origErr`.

В приложенном отчёте показывался исправленный вариант, но в текущем рабочем файле его нет.

### P0. Снова подавляются bridge checkpoint errors

После ошибки `ApplyBridges` и после ошибки `VerifyBridges` текущий код снова делает:

```go
_ = c.transitionManifestLocked(m, m.State)
```

Если сохранение checkpoint не удалось, информация о неопределённом внешнем side effect теряется. Координатор не переводится надёжно в `RECOVERY_REQUIRED`.

Нужно вернуть строгую обработку обеих ошибок checkpoint, recovery marker и composite error.

### P1. Тест S12 всё ещё нестрогий

Новая версия лучше старой, но по-прежнему допускает два разных финала:

```go
coord2.State() == StateIdle || coord2.State() == StateRecoveryRequired
```

Проверка `len(bridges.applied) > 1` также не считает реальные вызовы `ApplyBridges`: перед restart массив вручную заменяется на один элемент. Это не доказывает, что side effect не повторился.

Нужно добавить в fake runtime отдельные счётчики `applyCalls`/`withdrawCalls` и проверять один конкретный ожидаемый исход после restart.

### P1. Тест missing bundle недостаточно проверяет сохранность active config

`TestCoordinator_ManifestRecovery_RollbackToBundleLKG_FailClosed` проверяет только `ErrRecoveryRequired`. Он также обязан проверить:

- active config не удалён и не заменён legacy LKG;
- manifest сохранён;
- recovery marker создан;
- состояние координатора `RECOVERY_REQUIRED`.

## Почему зелёные тесты не являются подтверждением

Независимо выполнены:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo  PASS
go test -race -count=1 ./internal/mihomo              PASS
git diff --check -- internal/mihomo internal/strictfs PASS (только CRLF warning)
```

Но S14 удалён, а S12 допускает неоднозначный результат. Поэтому suite больше не проверяет часть обязательных гарантий.

## Точное задание агенту

1. Не использовать `git checkout`, `git restore`, `git reset`, очистку индекса и массовую перезапись файлов.
2. Работать поверх текущего дерева и сначала сохранить scoped patch текущего состояния.
3. Вернуть mutex-safe `setState()` и тест S14.
4. Исправить все без исключения игнорируемые вызовы `cleanupTxArtifactsLocked`.
5. Вернуть исправленный `handleApplyFailureLocked` с агрегированием apply/rollback/cleanup errors.
6. Вернуть строгую обработку checkpoint errors после ошибок bridge apply/verify.
7. Переделать S12 на счётчики вызовов и один детерминированный expected outcome.
8. Усилить missing-bundle test проверками active config, manifest, marker и state.
9. Выполнить:

```bash
gofmt -w internal/mihomo/coordinator.go \
  internal/mihomo/coordinator_legacy_test.go \
  internal/mihomo/gate1_legacy_test.go
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check -- internal/mihomo internal/strictfs
```

10. В отчёте показать `git diff` только трёх изменённых файлов относительно состояния на начало этой попытки. Не создавать `diff_output.txt`, не собирать IPK и не выполнять деплой.

## Критерий приёмки

Gate 1 остаётся непринятым до повторного независимого аудита. На Flash пока не переключаться.
