# Финальная проверка revised remediation plan First-Run Wizards

## Вердикт

Новая редакция устранила основные противоречия предыдущего плана: добавлены union semantics общей ingress-топологии, pre-commit генерация клиентских реквизитов, симметричные component transactions, пустые безопасные defaults, immutable Apply, error-bearing egress catalog, точная readiness и разделение уровней тестирования.

**План почти готов к запуску, но требует четырех обязательных уточнений.** Первое является архитектурным блокером. После их внесения план можно отдавать в реализацию без нового полного проектирования.

## P0 — архитектурный блокер

### 1. Расширить фактическую модель `IngressTopology`, а не только формулу ее вычисления

План правильно вводит правило:

```text
desired topology = previous topology + mutation selected server
```

Но текущий `internal/serveringress.IngressTopology` содержит один общий `PublicHostname`, один `TgPort` и не содержит `TgScenario`, direct/raw endpoints или независимый Telegram CDN hostname. Этого недостаточно для безопасного объединения Xray и Telegram.

При текущей модели применение Telegram может перезаписать hostname Xray, а применение Xray — hostname Telegram. По одному `TgPort` невозможно отличить direct listener, raw backend и web bridge. Условия negative readiness из revised plan невозможно корректно вычислить, потому что `desiredTopo.TgScenario` пока вообще отсутствует.

План должен явно добавить как минимум:

```go
type IngressTopology struct {
    DispatcherEnabled bool
    DispatcherAddress string
    DispatcherPort    int

    XrayEnabled        bool
    XrayAddress        string
    XrayPort           int
    XrayPublicHostname string
    XrayPublicPort     int
    XrayPathPrefix     string

    TgEnabled        bool
    TgScenario       string
    TgDirectAddress  string
    TgDirectPort     int
    TgRawAddress     string
    TgRawPort        int
    TgWebAddress     string
    TgWebPort        int
    TgPublicHostname string
}
```

Названия могут отличаться, но состояния компонентов должны быть независимыми.

Также нужно принять явное решение о разных CDN hostnames:

- либо dispatcher поддерживает Xray и Telegram на разных Host;
- либо мастер запрещает второму серверу другой CDN hostname и объясняет, что оба ingress используют общий домен;
- либо `cdndispatcher.Config` расширяется до host-aware route table.

Текущий `cdndispatcher.Config` имеет единственный `PublicHostname`, поэтому молча поддержать два разных домена он не может.

Acceptance test `TestSharedIngressTopologyUnion` обязан использовать разные исходные настройки компонентов и подтверждать, что изменение одного сервера не меняет поля другого.

## P1 — обязательные уточнения перед реализацией

### 2. Dispatcher transaction должна сохранять и восстанавливать running state

Предложенный `DispatcherComponent` передает в `PrepareCandidate` только `cdndispatcher.Config`. Сам config не говорит, был ли dispatcher запущен до транзакции и должен ли быть запущен после нее.

Добавить transaction candidate/snapshot:

```go
type DispatcherCandidate struct {
    Enabled bool
    Config  cdndispatcher.Config
}
```

Snapshot должен содержать предыдущие `Enabled/Running` и полный config. `CommitPrepared` материализует оба аспекта, `RollbackPrepared` восстанавливает оба. Иначе rollback может восстановить параметры, но оставить неправильное состояние процесса.

### 3. Определить recovery для `PhaseCommitting` и частичного finalize

После point of no return rollback уже не должен выполняться. Если finalize одного component прошел, а следующего завершился ошибкой, recovery должен **продолжить commit/finalize**, а не пытаться восстановить предыдущую topology.

Зафиксировать:

- до `PhaseCommitting` startup recovery выполняет reverse rollback;
- в `PhaseCommitting` recovery выполняет idempotent roll-forward/finalize;
- `FinalizePrepared` для уже finalized tx является безопасным no-op;
- `PhaseCommitted` записывается только после успешного finalize всех компонентов;
- ошибка archive после commit не меняет уже примененную topology и повторно устраняется recovery.

Фразу «zero fallible operations after point of no return» заменить на более точную: **после point of no return нет операций пользовательской материализации, способных лишить пользователя реквизитов; fallible finalize/journal operations восстанавливаются roll-forward**.

### 4. Определить миграцию пустого Telegram `Scenario`

Добавление `Scenario` меняет runtime lifecycle старых конфигураций. Нельзя оставлять пустое значение неявно означающим разные вещи в helper methods.

Зафиксировать миграцию:

- существующий initialized config без `Scenario` мигрируется в сценарий, эквивалентный его прежнему поведению (вероятнее всего `dual`, но это нужно подтвердить по текущим init scripts/config);
- новая конфигурация мастера всегда сохраняет одно из трех явных значений;
- неизвестное значение отклоняется fail-closed;
- `PublicConfig` возвращает эффективный scenario;
- после миграции пустое значение не остается в сохраненном config.

Добавить отдельный migration test для legacy enabled и disabled configs.

## Дополнительные точечные замечания

1. UI placeholder `front.example.com` допустим, но тест `TestNoPersonalDomainsInDefaults` должен отличать placeholder от runtime default. Он не должен запрещать нейтральные примеры в разметке.
2. `BuildShareLinks` и `BuildTgLinks` должны возвращать value, не pointer, если `nil` не является осмысленным результатом.
3. `OnPhaseChange` желательно сделать `func(TransactionPhase) error`: coordinator не должен проходить point of no return, если JobRunner не смог синхронно зафиксировать non-cancellable phase.
4. `Resolve` и fingerprint должны использовать одинаковую семантику snapshot, но не обязательно один объект, живущий между Plan и Apply: на Apply состояние намеренно перечитывается под lock и сравнивается с ожидаемым fingerprint.
5. Проверка executable identity должна использовать точный resolved path/inode и PID start time, чтобы исключить PID reuse и wrapper/substr match.

## Условия разрешения запуска плана

План можно запускать после добавления в него:

- расширенной per-component `IngressTopology`;
- политики разных/общих CDN hostnames;
- `DispatcherCandidate{Enabled, Config}` и восстановления running state;
- roll-forward recovery для `PhaseCommitting`;
- явной миграции legacy Telegram scenario.

После этих правок статус плана: **APPROVED FOR IMPLEMENTATION**.

В текущей редакции статус: **CONDITIONAL — DO NOT START UNTIL P0 MODEL GAP IS CLOSED**.
