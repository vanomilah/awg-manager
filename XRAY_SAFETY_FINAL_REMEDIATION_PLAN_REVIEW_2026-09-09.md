# Ревью плана финальной ремедиации Xray Safety Foundation

Дата: 2026-09-09  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

**План охватывает все шесть замечаний, но в текущем виде условно не одобрен.**

Четыре направления спроектированы правильно. Решения для `TgEnabled`, `keep_legacy` и boot wiring test необходимо уточнить до начала реализации. Самый опасный участок — предложенная compensating transaction: без персистентного журнала она не обеспечивает безопасное переключение поколений и после сбоя может оставить оба варианта неработающими.

## Обязательные изменения плана

### 1. Узкий контракт для управляемых полей Telegram ingress

Метод назван full replacement, но план предлагает менять `PublicHostname` лишь при непустом значении. Тогда hostname невозможно очистить — исходная ошибка останется.

Не следует принимать общий `tgwebproxy.Config` с двусмысленными zero values. Нужен отдельный контракт:

```go
type ManagedIngressConfig struct {
    Enabled        bool
    ListenPort     int
    PublicHostname string
}
```

`ApplyManagedIngress` должен атомарно и безусловно применить эти три поля, включая пустой hostname, но сохранить secret, legacy secret, backend, upstream device, TLS domain и прочие поля, которыми coordinator не владеет.

Обязательные тесты:

- `false -> true` и `true -> false`;
- очистка непустого hostname;
- сохранение всех неуправляемых полей;
- rollback всех управляемых полей при ошибке.

### 2. `keep_legacy` требует crash-recoverable migration saga

Предложенная последовательность «закоммитить отключение managed Xray, запустить legacy, при сбое компенсировать новым apply» недостаточна:

- между первым commit и компенсацией отсутствует рабочий Xray;
- процесс может завершиться до компенсации;
- компенсация также может упасть;
- runtime decision и init script остаются вне исходной транзакции;
- повторное использование `txID` может конфликтовать с terminal journal и snapshot;
- запись `runtime-decision.json` может упасть уже после запуска legacy.

Нужен отдельный персистентный migration journal (либо расширение общего журнала) как минимум с фазами:

```text
prepared
managed_stopped
legacy_script_restored
legacy_started_and_verified
decision_committed
finalized
```

До остановки managed generation должны быть проверены identity/checksum/исполняемость legacy init script и подготовлены snapshots. После старта legacy обязателен process/listener ownership probe с timeout; best-effort проверки недостаточно. При любом сбое необходимо остановить частично запущенный legacy, восстановить init script, managed generation и прежнее решение. Startup recovery должен уметь продолжить или откатить каждую фазу.

Если остается saga/compensation, в плане прямо зафиксировать:

- персистентный журнал;
- уникальный transaction ID для каждого шага;
- идемпотентные операции;
- восстановление после падения на каждой фазе;
- сохранение recovery artifacts до подтвержденного finalize.

### 3. PID record обязателен, включая проверку `pid <= 0`

Commit нельзя продолжать при невалидном PID:

```go
pid := proc.PID()
if pid <= 0 {
    // stop + rollback + error
}
if err := s.writePIDRecord(...); err != nil {
    // stop + rollback + joined error
}
```

`rollbackLocked()` обязан возвращать ошибку повторного `proc.Start()`. Первичная и rollback-ошибка объединяются через `errors.Join`; coordinator переводится в `RecoveryRequired`.

### 4. Committed manifest — обязательная часть commit

Ошибка записи committed manifest должна запускать rollback. Результат rollback игнорировать нельзя: обе ошибки передаются вызывающему коду через `errors.Join`, а snapshot/journal сохраняются для recovery.

Нужны fault-injection тесты для:

- ошибки marshal/write manifest;
- ошибки manifest одновременно с ошибкой rollback;
- последующего `StartupRecovery` по оставшимся артефактам.

### 5. `CanAutoStart()` и реальный boot wiring

Чтение `recoveryNeeded` должно быть защищено тем же mutex/RWMutex либо выполняться через единый потокобезопасный snapshot состояния.

Искусственный mock counter вне production-пути не проверяет wiring. Реальную оркестрацию запуска следует вынести из `wiring_server.go` в тестируемую функцию, которую вызывает production-код, например:

```go
func startIngressIfSafe(gate AutoStartGate, xray, tg, dispatcher Starter) error
```

Тест с corrupt journal должен вызвать именно этот helper и подтвердить, что ни один `Start()` не вызван. Отдельно проверить clean/no journal, успешное recovery и все варианты recovery failure.

### 6. Очистка hostname должна доходить до dispatcher

Безусловное присваивание:

```go
desired.PublicHostname = cfg.PublicDomain
```

для полного PUT корректно. Интеграционный тест должен проверять не только промежуточный `IngressTopology`, но и фактический `dispatcher.GetConfig().PublicHostname` после apply.

## Что в плане уже корректно

- Удаление `pendingXrayConfig` не следует отменять.
- Разделение `Apply`/`applyLocked` — правильная основа.
- Fail-closed dependency coordinator в HTTP handler нужно сохранить.
- Ошибки finalize, rollback и archive нельзя терять.
- Восстановление прежнего migration settings-файла — правильное направление.

## Дополненный verification plan

1. Реальный `tgwebproxy.Service`: enable, disable, clear hostname, сохранение secret и rollback.
2. Crash после каждой фазы `keep_legacy` с последующим startup recovery.
3. Ошибки legacy rename/chmod/start/probe/decision; после каждой managed generation снова работоспособна.
4. Невалидный PID и ошибка PID record в `CommitPrepared` и `rollbackLocked`.
5. Ошибка committed manifest вместе с ошибкой rollback.
6. Production boot helper не вызывает ни одного `Start()` при recovery block.
7. Очистка hostname одновременно в Xray и dispatcher.
8. После исправлений: uncached `go test -race`, `svelte-check`, ARM64 build и `git diff --check`.

## Итог

План можно передавать в реализацию после двух главных исправлений:

1. заменить двусмысленный `ApplyManagedConfig(Config)` узким full-state контрактом для Telegram ingress;
2. заменить простую компенсацию `keep_legacy` на crash-recoverable migration transaction/saga с обязательным listener probe.

Остальные уточнения следует внести одновременно, после чего следующий walkthrough можно рассматривать как кандидат на финальную проверку.
