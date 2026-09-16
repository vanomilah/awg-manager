# Финальная дельта-проверка плана Xray Safety Foundation

Дата: 2026-09-09  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

Редакция исправила прежние девять замечаний по существу. Остались **три блокирующих технических дефекта** и несколько небольших уточнений. После их внесения план можно одобрить и запускать в реализацию без нового архитектурного пересмотра.

## Блокеры

### 1. Предложенный `Dispatcher.ApplyConfig` ломает работающий listener

В приведённой реализации full-state метод только заменяет `d.cfg` и snapshot:

```go
d.cfg = cfg
d.snapshot.Store(newSnap)
```

Но существующий `Reconfigure()` при изменении `ListenAddr` сначала открывает новый listener, переключает server и лишь затем дренирует старый. Новый метод обходит эту логику. В результате конфигурация будет сообщать новый адрес, а процесс продолжит слушать старый.

Нужно вынести общую внутреннюю реализацию:

```go
func (d *Dispatcher) applyConfigLocked(cfg Config, mode applyMode) error
```

Она должна для обоих режимов сохранять существующий two-phase listener switch. Разница режимов — только в построении `newCfg`:

- PATCH: накладывает переданные значения на `d.cfg`;
- REPLACE: использует полный `cfg`, включая пустой `PublicHostname`.

`ApplyConfig()` не должен дублировать или обходить lifecycle-логику `Reconfigure()`.

Обязательный тест: запущенный dispatcher меняет `ListenAddr`; старый listener закрывается, новый принимает соединения, `GetConfig()` совпадает с фактическим адресом.

### 2. Порядок recovery двух вложенных журналов не согласован

Saga вызывает `c.applyLocked()` для остановки/восстановления managed Xray. Этот метод создаёт собственный `server-ingress-transaction.json`. Значит, при падении одновременно могут существовать:

- внешний `migration-saga-transaction.json`;
- внутренний `server-ingress-transaction.json`.

План безусловно запускает saga recovery первой. Если падение произошло внутри `applyLocked()`, saga начнёт следующий rollback/forward поверх незавершённой внутренней транзакции. Это может перезаписать active journal или получить конфликт snapshot/txID.

Нужно выбрать и подробно зафиксировать один вариант:

1. **Предпочтительно:** saga владеет одной транзакцией и не вызывает public/general journal workflow внутри своих шагов. Она использует отдельные подготовленные component operations и хранит их transaction IDs в saga journal.
2. Либо вложенная транзакция официально поддерживается: saga journal хранит дочерний ingress txID, а startup сначала завершает/откатывает именно дочерний journal, затем продолжает recovery внешней saga.

Просто «saga first, ingress second» недостаточно. Нужны crash-тесты после создания дочернего journal, после Xray commit и до его archive/finalize.

Также порядок mutex/file lock должен быть единым во всех entry points. Сейчас `Apply()` и `ResolveMigration()` берут `c.mu`, затем file lock; предложенный `StartupRecovery()` — наоборот. Это lock inversion. Следует вынести единый helper захвата или во всех путях использовать одинаковый порядок.

### 3. `StartIngressIfSafe` допускает частично запущенное состояние

Текущий псевдокод продолжает запуск после ошибки:

- Xray может не стартовать, но TG и dispatcher будут запущены;
- TG может не стартовать, но dispatcher всё равно стартует;
- при ошибке dispatcher уже запущенные backend-сервисы остаются работать;
- возвращается `started == false`, хотя часть компонентов фактически запущена.

Нужен явный контракт. Для fail-closed boot рекомендуется transactional/fail-fast запуск:

1. запускать только enabled backend-компоненты;
2. при первой ошибке остановить компоненты, запущенные этим helper, в обратном порядке;
3. запускать dispatcher только после успешного запуска необходимых backend-компонентов;
4. при ошибке dispatcher также выполнить компенсацию;
5. объединить start- и compensation-ошибки через `errors.Join`.

Интерфейс должен включать `Stop()` либо принимать `Lifecycle`:

```go
type Lifecycle interface {
    Start() error
    Stop() error
}
```

Нужны тесты ошибки каждого `Start()` и каждого компенсирующего `Stop()` с проверкой конечного состояния.

## Обязательные небольшие уточнения

### 4. PID ownership legacy daemon нельзя связывать с PID команды init script

`init-script start` обычно завершается, а сервис работает отдельным daemon PID. Формулировка «PID принадлежит процессу, запущенному init script» должна быть конкретизирована: читать доверенный pidfile либо находить владельца listener и проверять executable/cmdline/config path. Проверка только порта или PID shell-процесса неверна.

### 5. Запись фазы должна быть copy-on-write

`advanceSagaPhase` сначала меняет `j.Phase` в памяти, затем пишет файл. При ошибке объект уже отражает незаписанную фазу. Надёжнее копировать journal, записывать новую копию атомарно и лишь после успеха присваивать её текущему состоянию.

### 6. `Coordinator.rollback` не должен удваивать исходную ошибку

Если `rollback(j, txID, origErr)` возвращает только rollback-ошибки, вызывающий код корректно делает `errors.Join(origErr, rbErr)`. Следует явно закрепить этот контракт. Возвращать из `rollback()` уже объединённую ошибку и повторно объединять её с `origErr` нельзя.

### 7. Проверить сохранение snapshot при ошибке его удаления

Ошибка `os.RemoveAll(txPath)` означает, что часть каталога могла быть удалена. Ее нужно вернуть и перевести coordinator в recovery-required, но нельзя обещать, что snapshot гарантированно цел. Recovery должен повторно проверить manifest/checksums и при повреждении завершиться fail-closed.

## Что одобрено

- строгий `ManagedIngressConfig` и отклонение порта `<= 0`;
- сохранение неуправляемых полей Telegram ingress;
- сбор ошибок всех шагов Xray rollback;
- обязательная запись committed manifest;
- возврат rollback-ошибок в coordinator;
- сохранение активного journal при незавершённом rollback;
- отдельный full-state контракт dispatcher как идея;
- persistent migration saga и проверяемые фазы;
- явный rollback при ошибке runtime decision;
- агрегированный coordinator/TG recovery gate;
- production boot helper вместо искусственного теста;
- полный fault-injection/race/ARM64 verification plan.

## Итог агенту

Перед началом реализации исправить блокеры 1–3. Затем внести уточнения 4–7 непосредственно в код и тесты. После этого план можно считать одобренным.

Рекомендуемый порядок реализации:

1. Telegram managed contract.
2. Xray transaction error handling и snapshot lifecycle.
3. Dispatcher full replace поверх общей безопасной lifecycle-реализации.
4. Transactional boot helper.
5. Migration saga без несогласованных вложенных журналов.
6. Fault-injection, crash recovery, race tests и ARM64 build.

IPK и деплой на данном этапе не требуются.
