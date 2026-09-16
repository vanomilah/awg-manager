# Аудит финального walkthrough First-Run Setup Wizards

Проверены актуальный `walkthrough.md`, одобренный implementation plan и фактический код `E:\AWGM\awg-manager`.

## Вердикт

**Реализация не соответствует одобренному плану и не готова к приемке или развертыванию.**

Профильные Go-тесты с race detector проходят, но они не проверяют критический порядок транзакции и значительную часть заявленных acceptance semantics. Walkthrough утверждает полное выполнение, однако несколько ключевых пунктов либо отсутствуют, либо реализованы противоположно плану.

## P0 — блокирующие дефекты

### 1. Readiness выполняется до активации candidate

В `ExecuteIngressTransaction` компоненты сначала только проходят `PrepareCandidate`. Затем journal переводится в `PhaseCandidateActive`, вызывается `ReadinessProbe`, и лишь **после** point of no return вызываются `CommitPrepared` Xray, Telegram и dispatcher.

Доказательства: `internal/serveringress/coordinator.go:1251-1319`, `1321-1335`, `1353-1399`.

Следствие: readiness проверяет старую runtime topology либо закрытые candidate endpoints. Новая конфигурация еще не применена. Успех возможен только при совпадении со старым состоянием или при наличии чужого listener; свежая установка должна падать до активации.

Правильная последовательность:

```text
prepare all
activate/CommitPrepared all
PhaseCandidateActive
readiness candidate topology
OnPhaseChange(Committing)
PhaseCommitting
FinalizePrepared all
PhaseCommitted
archive
```

До `PhaseCommitting` активированный candidate должен оставаться откатываемым. Recovery из `PhaseCandidateActive` выполняет reverse rollback. Recovery из `PhaseCommitting` выполняет только idempotent finalize, а не повторную активацию.

### 2. `PhaseCommitted` записывается до finalize

Основной apply path записывает `PhaseCommitted` на строках `1401-1410`, а затем вызывает `FinalizePrepared` на строках `1412-1428`. Это противоречит утверждению плана «PhaseCommitted only after all finalize succeed».

Кроме того, recovery для `PhaseCommitted` игнорирует все ошибки finalize (`coordinator.go:425-445`). Он может архивировать journal как committed после неудачного cleanup/finalize.

Исправление:

- `PhaseCommitting` записывается перед finalize;
- все finalize errors проверяются;
- только после успешного finalize всех компонентов записывается `PhaseCommitted`;
- затем journal архивируется;
- recovery `PhaseCommitting` повторяет только finalize;
- recovery `PhaseCommitted` проверяет/архивирует, не скрывая ошибки.

### 3. Синхронизация cancellation boundary не подключена к job

Одобренный контракт требовал `IngressTransactionParams.OnPhaseChange`. Фактически callback хранится как глобальное поле coordinator (`c.onPhaseChange`) и настраивается через `SetOnPhaseChange`, помеченный как test hook.

Доказательства:

- `internal/serveringress/coordinator.go:76`, `102-106`, `1346-1351`;
- `internal/serveringress/types.go` — `IngressTransactionParams` не содержит callback;
- `internal/serverwizard/service.go:386-397` — WizardService не передает phase callback.

После возврата транзакции WizardService лишь повторно устанавливает `JobPhaseCommitting` (`service.go:551-555`), когда commit уже закончен. Таким образом CancelJob остается рассинхронизированным с настоящим point of no return.

Исправление: callback должен быть per-transaction полем params и синхронно переводить конкретный job в non-cancellable phase до записи `PhaseCommitting`.

### 4. Единый typed Desired pipeline не реализован

Walkthrough заявляет immutable Desired, но `BuildDesiredConfig(ctx, egressResolver, req)` отсутствует. `Plan` по-прежнему вызывает `preflight.Run(ctx, req)`, затем отдельно `NormalizeDesiredConfig(req)`.

Доказательства:

- `internal/serverwizard/service.go:146`, `152-160`;
- `internal/serverwizard/desired.go:86` — существует только `NormalizeDesiredConfig` без resolver/context.

Следствие: Preflight проверяет сырой request, а Plan/Apply используют отдельно интерпретированную конфигурацию. Гарантия «проверено = показано = применено» отсутствует.

### 5. Egress resolver все еще обходится и `nwg1` захардкожен

`NormalizeDesiredConfig` напрямую разбирает `UpstreamDevice`, не вызывая typed egress resolver. Для Telegram пустой выбор снова заменяется на `nwg1`.

Доказательства: `internal/serverwizard/desired.go:140-143` и `179-186` в актуальной структуре функции; точные номера могут смещаться после форматирования.

Также `executeApplyJob` строит `tgCfg` без `UpstreamDevice`, а отдельно передаваемый ingress candidate содержит его. Это создает расхождение между pre-commit link candidate и реально применяемым candidate.

Исправление: typed resolver обязателен внутри `BuildDesiredConfig`; direct материализуется в Telegram runtime как пустая строка; неизвестный или unavailable egress отклоняется до Plan.

### 6. Строгая CDN profile/mode matrix не реализована

Текущая логика выбирает WS, если `CDNProfileID == "cdn_ws" || req.Mode == "ws"`, иначе XHTTP. Она не отклоняет `cdn_get + ws`, неизвестные profile IDs или неизвестные modes.

Доказательство: `internal/serverwizard/desired.go`, блок нормализации Xray после `CDNProfileID`.

Это прежний дефект, который walkthrough ошибочно объявляет исправленным.

### 7. Обязательный TLS domain для direct/dual не проверяется

`ValidateTlsDomain("")` возвращает nil с комментарием «optional». Нормализация также использует `req.Mode` как fallback для TLS domain, смешивая два несвязанных поля.

Доказательство: `internal/serverwizard/desired.go`, `ValidateTlsDomain` и Telegram branch.

Исправление:

- `tls_domain` обязателен для `direct_fake_tls` и `dual`;
- для `cdn_http` он остается пустым;
- `req.Mode` никогда не используется как TLS domain;
- placeholder не материализуется.

### 8. Expanded topology добавлена, но основной transaction path продолжает использовать legacy поля

В актуальном `ExecuteIngressTransaction` присутствуют обращения к устаревшим `desiredTopo.PublicHostname` и `desiredTopo.TgPort`, а новые независимые поля заполняются непоследовательно.

Доказательства: `internal/serveringress/coordinator.go:1147-1217`, `1286-1303`.

Например:

- Xray candidate обновляет общий `PublicHostname`, а не только `XrayPublicHostname`;
- Telegram candidate обновляет общий `PublicHostname` и `TgPort`;
- dispatcher candidate одновременно получает legacy `PublicHostname` и новые per-component hosts;
- `TgScenario`, direct/raw/web addresses и ports в этом path не материализуются полностью.

Следствие: обещанная изоляция состояний Xray и Telegram не гарантирована; изменение одного мастера может повлиять на другой.

### 9. Readiness не является заявленной topology-aware проверкой

`probeTopologyReadiness` по-прежнему выполняет одиночные TCP connect с timeout 500 ms.

Доказательство: `internal/serverwizard/service.go:558-647`.

Отсутствуют:

- polling до общего deadline 3 s;
- procnet PID/socket ownership;
- executable path и PID start time;
- bind address и IPv4/IPv6 identity;
- HTTP Host+path dispatcher probes;
- проверка полной combined topology;
- отрицательная проверка dispatcher;
- negative checks при injected test dialer (они намеренно пропускаются при `s.dialTimeout != nil`).

Walkthrough утверждает наличие этих проверок, но код их не содержит.

## P1 — существенные дефекты

### 10. Plan preview по-прежнему содержит hardcoded path и dispatcher port

`buildXrayPlan` описывает `/cdn-bridge/` и `0.0.0.0:9009`, вместо `desired.Path` и `desired.DispatcherPort`.

Доказательство: `internal/serverwizard/service.go:274-285`.

Это нарушает immutable plan contract: пользователь может увидеть не те значения, которые сохранены в Desired.

### 11. Apply выполняет лишнюю синхронную проверку и расходует plan до запуска job

`Apply` сначала вызывает `MarkUsed`, затем отдельно берет coordinator lock и проверяет fingerprint, после чего запускает async job, который повторно проверяет fingerprint внутри transaction.

Доказательство: `internal/serverwizard/service.go:329-366`.

Если первая проверка падает, plan уже использован, job не создан, а API contract становится частично синхронным. Следует создавать job и выполнять единственную авторитетную проверку внутри coordinator transaction либо менять порядок consume semantics.

### 12. Recovery скрывает ошибки durable operations

В `PhaseCommitting` recovery игнорирует ошибки `FinalizePrepared` и `writeJournal` (`coordinator.go:494-508`). В `PhaseCommitted` также игнорируются finalize errors. Это может привести к ложному successful archive при незавершенной транзакции.

### 13. Walkthrough завышает тестовое покрытие

Из ключевых заявленных acceptance tests найдены лишь отдельные тесты, например `TestTelegramScenariosAppliedCorrectly` и `TestNoPersonalDomainsInDefaults`. Поиск не обнаружил точных тестов:

- `TestSharedIngressTopologyUnion`;
- `TestRecoveryFromPhaseCommittingRollForward`;
- `TestStrictCDNProfileModeMatrix`;
- `TestEgressResolutionAndApplication`;
- `TestCancellationBoundarySynchronized`.

Существующие mocks позволяют readiness пройти без реальной активации candidate, поэтому текущая P0-ошибка порядка транзакции тестами не обнаруживается.

## Проверка тестов

Выполнено:

```sh
go test -count=1 -race ./internal/tgwebproxy/... ./internal/cdndispatcher/... ./internal/xrayserver/... ./internal/serveringress/... ./internal/serverwizard/... ./internal/api/... ./cmd/awg-manager
```

Результат: все перечисленные пакеты прошли.

Это подтверждает отсутствие выявляемых race/unit regressions в существующем наборе, но **не подтверждает правильность runtime transaction semantics**.

## Обязательный порядок исправлений

1. Исправить transaction order: prepare -> activate -> readiness -> committing -> finalize.
2. Исправить recovery semantics и положение `PhaseCommitted`; перестать игнорировать durable errors.
3. Подключить per-job `OnPhaseChange` через transaction params.
4. Реализовать единый `BuildDesiredConfig` с typed egress resolver.
5. Убрать `nwg1`, TLS/mode fallback и добавить строгую profile matrix.
6. Полностью материализовать новые per-component topology fields; удалить legacy shared fields после миграции journal schema.
7. Реализовать реальную procnet/HTTP topology readiness.
8. Исправить preview и plan consume semantics.
9. Добавить недостающие integration/crash tests, включая тест, который падает, если readiness вызывается до activation.

## Итог

Walkthrough нельзя принимать как достоверное подтверждение готовности. Реализация содержит фундаментальную ошибку жизненного цикла candidate и несколько незакрытых дефектов предыдущего аудита.

**Статус: REJECTED — вернуть на доработку. IPK не собирать и на роутеры не устанавливать.**
