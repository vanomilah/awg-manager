# Финальный допуск плана Xray Safety Foundation

Дата: 2026-09-09  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

**Архитектура плана одобрена.** Все существенные замечания предыдущих ревью устранены:

- boot lifecycle больше не меняет сохранённый `Enabled`;
- coordinator, Xray и Telegram recovery объединены в fail-closed gate;
- компенсация касается только компонентов, запущенных helper;
- migration saga использует один journal;
- Xray snapshots проходят явный rollback/finalize lifecycle;
- dispatcher включён в snapshot и восстановление saga;
- full replace dispatcher сохраняет two-phase listener switch;
- порядок блокировок унифицирован.

Перед кодированием нужно внести в план три точечных исправления ниже. Они не требуют нового архитектурного ревью.

## Обязательные implementation notes

### 1. `withIngressLock` должен возвращать ошибку `Unlock()`

Текущий псевдокод вызывает unlock в `defer`, выставляет `recoveryNeeded`, но не изменяет возвращаемую ошибку. Поэтому операция способна вернуть `nil`, хотя межпроцессная блокировка не снята.

Использовать named return и объединять ошибки:

```go
func (c *Coordinator) withIngressLock(txID string, fn func() error) (retErr error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if err := c.lock.Lock(txID); err != nil {
        return err
    }
    defer func() {
        if err := c.lock.Unlock(); err != nil {
            c.recoveryNeeded = true
            c.recoveryReason = fmt.Sprintf("unlock failed for tx %s: %v", txID, err)
            unlockErr := fmt.Errorf("unlock ingress transaction %s: %w", txID, err)
            if retErr != nil {
                retErr = errors.Join(retErr, unlockErr)
            } else {
                retErr = unlockErr
            }
        }
    }()

    retErr = fn()
    return retErr
}
```

Тест должен проверять одновременно non-nil result и `recoveryNeeded=true` при fault-injected unlock failure.

### 2. Enabled-компонент с `nil` implementation должен блокировать boot

Сейчас условие `xrayEnabled && xray != nil` молча пропускает отсутствующий Xray, после чего может запустить dispatcher. Аналогично для Telegram.

Fail-closed проверка перед запуском:

```go
if xrayEnabled && xray == nil {
    return false, errors.New("xray is enabled but lifecycle is unavailable")
}
if tgEnabled && tg == nil {
    return false, errors.New("telegram ingress is enabled but lifecycle is unavailable")
}
if (xrayEnabled || tgEnabled) && disp == nil {
    return false, errors.New("ingress is enabled but dispatcher lifecycle is unavailable")
}
```

Добавить три соответствующих теста.

### 3. Компенсация не должна зависеть от уже отменённого request context

Если входной `ctx` отменён, передача его в `ShutdownRuntime(ctx)` может немедленно сорвать cleanup. Для компенсации нужен отдельный ограниченный context:

```go
cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
```

Все runtime shutdown в compensation выполняются с `cleanupCtx`. Тест: входной context отменён во время start failure, но ранее запущенный runtime всё равно успешно остановлен.

## Уточнения для реализации и тестов

- `StartConfigured()` и `ShutdownRuntime()` обязаны быть идемпотентными.
- `IsRunning()` и recovery getters должны быть потокобезопасными.
- Telegram `StartConfigured()` должен применять workers и выполнить ту же readiness-проверку, что штатный безопасный start, но не менять `s.config`.
- Xray recovery guard следует поставить во всех конфигурационных mutation/start entry points, перечислив их явно; runtime shutdown допускается при recovery, поскольку он нужен для безопасной остановки.
- Saga phase `finalized` записывается только после успешного finalize всех component snapshots.
- При ошибке archive уже finalized saga должна оставаться повторно восстанавливаемой и переводить coordinator в recovery-required.
- В bounded polling тесте dispatcher следует использовать deadline/event synchronization без фиксированного sleep.

## Итог

После добавления трёх implementation notes выше план **можно запускать в реализацию**. Повторное согласование архитектуры не требуется; следующий аудит следует проводить уже по фактическому diff и результатам fault-injection/race тестов.

IPK и деплой до прохождения реализации и тестов не нужны.
