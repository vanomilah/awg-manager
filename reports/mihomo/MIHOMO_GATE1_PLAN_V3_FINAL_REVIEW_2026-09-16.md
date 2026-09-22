# Финальное ревью Implementation Plan v3 — Mihomo Gate 1

Дата: 2026-09-16  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Предыдущее ревью: `MIHOMO_GATE1_REVISED_PLAN_REVIEW_2026-09-16.md`

## Вердикт

Версия v3 исправляет оба главных архитектурных P0 из предыдущего ревью:

- существующий контракт `NativeStoreTx` теперь сохраняется;
- digest snapshot записывается в `PreMutationStoreDigest` и сравнивается с `BaseDesiredStoreDigest`.

Однако перед реализацией нужны ещё **три точечные правки плана**. Одна из них является compile-блокером. После их внесения план можно отдавать в работу без нового полного архитектурного пересмотра.

## P0 — compile-блокер

### 1. В `mihomonative.Store` нет метода `marshalLocked()`

План предлагает:

```go
b, err := s.marshalLocked()
```

Такого метода в `internal/mihomonative/store.go` нет. Текущие `CreateSnapshotFile` и `CurrentDigest` сериализуют состояние напрямую:

```go
b, err := json.MarshalIndent(s.data, "", "  ")
```

План должен выбрать один из вариантов:

1. использовать `json.MarshalIndent(s.data, "", "  ")` непосредственно внутри `CreateSnapshotFileAt`; либо
2. явно добавить отдельный метод:

```go
func (s *Store) marshalLocked() ([]byte, error) {
    return json.MarshalIndent(s.data, "", "  ")
}
```

и перевести на него одновременно `CreateSnapshotFileAt` и `CurrentDigest`, чтобы сериализация гарантированно оставалась идентичной.

Рекомендуется второй вариант, но имя должно отражать контракт: вызывающий обязан уже удерживать `s.mu` хотя бы на чтение.

## P1 — recovery и cleanup

### 2. Не удалять snapshot вручную перед `cleanupTxArtifactsLocked`

В `abortPreMutationIntentLocked` и `recoverManifestLocked(StatePreSnapshotWriteIntent)` план сначала вызывает `StrictUnlink(snapshot)`, а затем `cleanupTxArtifactsLocked(m)`.

Общий cleanup уже:

- добавляет basename `PreMutationStoreSnapshotFile` в durable cleanup journal;
- переводит manifest в `StateTerminalCleanup`;
- удаляет snapshot;
- проверяет, что файл действительно исчез;
- удаляет cleanup journal и manifest.

Повторный unlink сейчас технически идемпотентен, но ручное удаление **до записи cleanup journal** ослабляет crash consistency и дублирует ответственность.

Исправить формулировку:

```go
func (c *ApplyCoordinator) abortPreMutationIntentLocked(
    m *TransactionManifest,
    cause error,
) error {
    if err := c.cleanupTxArtifactsLocked(m); err != nil {
        c.setState(StateRecoveryRequired)
        _ = c.writeRecoveryMarkerLocked(...)
        return errors.Join(cause, ErrRecoveryRequired, err)
    }
    c.setState(StateIdle)
    return cause
}
```

Для startup recovery состояния `StatePreSnapshotWriteIntent` также сразу вызывать `cleanupTxArtifactsLocked(&m)`, без предварительного unlink и без восстановления store.

### 3. После cleanup нельзя делать manifest transition в `StateIdle`

В разделе `recoverManifestLocked` всё ещё написано «Transition to StateIdle». Это противоречит правильному инварианту, заявленному выше в плане.

После успешного `cleanupTxArtifactsLocked` manifest уже удалён. Разрешено только:

```go
c.setState(StateIdle)
return nil
```

Нельзя вызывать `transitionManifestLocked(..., StateIdle)` и нельзя заново создавать manifest.

## P1 — дополнительные критерии тестирования

### 4. Crash-тест должен доказать отсутствие второй транзакции поверх orphan manifest

Помимо создания нового coordinator для recovery, добавить assertion:

- старый coordinator после `ErrSimulatedCrash` больше не используется;
- новый coordinator до `RecoverOnStartup` не должен разрешать `MutateAndApply`, если manifest существует;
- после успешного recovery новая транзакция разрешена.

Если текущая архитектура предполагает обязательный вызов `RecoverOnStartup` до обслуживания запросов, это нужно явно зафиксировать тестом startup lifecycle, а не оставлять как предположение.

### 5. Проверить digest физического snapshot-файла

Тест `CreateSnapshotFileAt` должен проверять сразу три значения:

```text
returned digest
== strictfs.ComputeFileDigest(snapshotPath)
== Store.CurrentDigest() в момент snapshot
```

Также нужен отрицательный тест: `targetPath`, не совпадающий с `SnapshotFilePath(txid)`, отклоняется до записи файла.

## Что уже можно оставить без изменений

- Новое состояние `StatePreSnapshotWriteIntent` и его переходы.
- Сохранённый интерфейс `NativeStoreTx` с добавлением `CreateSnapshotFileAt`.
- Сравнение `PreMutationStoreDigest` и `BaseDesiredStoreDigest`.
- Exact-match ownership validation с дополнительным `EvalSymlinks`.
- Новый coordinator/store в crash-recovery тесте.
- Разделение targeted, related integration и full backend tests.
- `mktemp -d` для изолированной проверки.
- Запрет сборки IPK и deployment в рамках Gate 1.

## Разрешение на реализацию

План можно запускать в реализацию **после текстового внесения пунктов 1–3**:

1. определить реальный способ сериализации вместо отсутствующего `marshalLocked()`;
2. удалить предварительный ручной unlink из abort/recovery и доверить удаление durable cleanup journal;
3. заменить «Transition to StateIdle» после cleanup на только `c.setState(StateIdle)`.

Пункты 4–5 добавить в тестовую матрицу. После этого дополнительный раунд согласования плана не требуется: агент может реализовывать, запускать тесты и передать diff/отчёт на code review.

## Короткая команда агенту

Обнови `implementation_plan.md` по пунктам 1–5 этого файла и затем выполняй реализацию. Scope строго ограничен Gate 1 (`internal/mihomo`, `internal/mihomonative`, необходимые `strictfs`-тесты и итоговые Gate 1 artifacts). Не собирай IPK, не выполняй deployment и не затрагивай несвязанные пользовательские изменения.
