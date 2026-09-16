# Аудит реализации First-Run Wizards от 2026-09-10

Проверены:

- `implementation_plan.md` из Antigravity;
- `walkthrough.md` из Antigravity;
- фактический код рабочей копии `E:\AWGM\awg-manager`;
- профильные Go-тесты с race detector.

## Вердикт

**Реализация пока не готова к приемке и не соответствует заявлению walkthrough о полном выполнении плана.**

Базовые компоненты существуют, проект компилируется на уровне проверенных пакетов, а профильные тесты проходят. Однако несколько ключевых гарантий из плана фактически отсутствуют: сценарии Telegram не материализуются в набор процессов, egress resolver обходится, Xray получает жестко заданные параметры, readiness не проверяет владельцев сокетов и отрицательные условия, а восстановление транзакции после падения не сохраняет полный rollback state.

До исправления пунктов P0 запуск мастера на реальном роутере может применить неверную топологию или оставить систему в состоянии, которое нельзя надежно восстановить после перезапуска.

## Что подтверждено

- Реализованы модели Xray `xhttp`/`ws` и outbound `direct`/`socks`/`interface`.
- Секреты создаются внутри apply job через `crypto/rand`.
- Обычный rollback в `rollbackIngressTx` выполняется в порядке Dispatcher -> Telegram -> Xray.
- `ComputeStrict` существует и вызывается под coordinator lock.
- API применяет ограничение размера JSON, запрещает неизвестные поля и проверяет хвост JSON.
- Профильные тесты проходят с `-race`:
  - `internal/serveringress/...`;
  - `internal/serverwizard/...`;
  - `internal/xrayserver/...`;
  - `internal/tgwebproxy/...`;
  - `internal/api/...`;
  - `cmd/awg-manager`.

Команда проверки:

```sh
go test -count=1 -race ./internal/serveringress/... ./internal/serverwizard/... ./internal/xrayserver/... ./internal/tgwebproxy/... ./internal/api/... ./cmd/awg-manager
```

Прохождение тестов не закрывает найденные ниже семантические дефекты: соответствующих acceptance-тестов в текущем наборе нет либо тестовые doubles обходят реальный runtime.

## P0 — блокирующие дефекты

### 1. Сценарии Telegram не управляют составом процессов

`DesiredWizardConfig.Scenario` доходит до coordinator, но в `tgwebproxy.Config` поля сценария нет. `WorkerSupervisor.ApplyWorkers` при `Enabled=true` перезапускает **все** зарегистрированные workers независимо от `direct_fake_tls`, `cdn_http` или `dual`.

Доказательства:

- `internal/serverwizard/service.go:416-429` — scenario передается кандидату;
- `internal/serveringress/coordinator.go:964-973` — scenario влияет только на необходимость dispatcher;
- `internal/tgwebproxy/types.go:29-44` — runtime Config не содержит scenario;
- `internal/tgwebproxy/workers.go:221-239` — при Enabled запускаются все workers.

Следствие: матрица из плана (`direct only`, `CDN only`, `dual`) реально не обеспечена. Например, direct-сценарий не гарантирует остановку raw/tproxy, а CDN-сценарий — отсутствие публичного listener 8443.

Что исправить:

1. Добавить явную runtime-модель сценария либо вычисляемые флаги `DirectEnabled`, `RawEnabled`, `WebEnabled`.
2. Материализовать их в `ApplyWorkers` и генераторах конфигов.
3. Останавливать лишние workers, а не только запускать требуемые.
4. Добавить настоящий `TestTelegramScenariosAppliedCorrectly` с проверкой start/stop каждого worker и сокетов.

### 2. Egress resolver реализован, но мастер его не использует

`WizardService.Plan` вызывает `NormalizeDesiredConfig(req)` напрямую. Нормализация самостоятельно интерпретирует строку `upstream_device` и не вызывает `egress.Adapter.Resolve`.

Доказательства:

- `internal/serverwizard/service.go:160`;
- `internal/serverwizard/desired.go:140-143` — пустое значение Telegram заменяется жестким `nwg1`;
- `internal/serverwizard/desired.go:179-186` — произвольная строка превращается в interface outbound;
- `internal/serverwizard/egress/adapter.go` — typed resolver существует отдельно от этого пути.

Следствие:

- неизвестный или недоступный интерфейс принимается;
- tunnel ID не обязан быть преобразован в реальный интерфейс;
- проверка доступности SOCKS может быть обойдена;
- значение `direct` для Telegram может попасть в `UpstreamDevice` как имя интерфейса;
- конфигурация привязана к частному имени `nwg1`.

Что исправить: сначала нормализовать общие поля, затем обязательно вызвать typed `Resolve(serverKind, selection)` и сохранить в Desired только результат resolver. Ошибка catalog/probe должна блокировать plan/apply.

### 3. Параметры Xray пользователя теряются при apply

`DesiredWizardConfig` не содержит полноценного контракта public port/path, а apply создает кандидата с константами:

- `PublicPort: 443`;
- `Path: "/cdn-bridge/"`.

Доказательство: `internal/serverwizard/service.go:442-449`.

Следствие: мастер может показать пользователю один план, но применить другую конфигурацию. Пользовательский path нельзя корректно материализовать; нестандартный внешний порт невозможен.

Что исправить: добавить нормализованные `Path`, `PublicPort` и при необходимости `DispatcherPort` в Desired, Plan summary, fingerprint dimensions и candidate. Запретить повторные hardcode на этапе apply.

### 4. Совместимость CDN profile и Xray mode не валидируется

`NormalizeDesiredConfig` выбирает WS, если `profile == cdn_ws` **или** `mode == ws`. Это допускает несовместимую пару `cdn_get + ws`, неизвестные profile ID и другие неоднозначные комбинации.

Доказательство: `internal/serverwizard/desired.go:165-176`.

Что исправить: profile должен разрешаться через единый каталог, после чего строгая матрица должна разрешать только:

- `xhttp_get` + `cdn_get|cdn_full`;
- `ws` + `cdn_ws|cdn_full`.

Неизвестные profile/mode должны отклоняться.

### 5. Readiness probe не является topology-aware

Текущая реализация делает по одному TCP connect с timeout 500 ms к ожидаемым портам.

Доказательство: `internal/serverwizard/service.go:554-618`.

Отсутствуют заявленные проверки:

- polling до общего deadline 3 s;
- PID и владельца socket;
- bind address (`127.0.0.1`, wildcard, IPv4/IPv6);
- конфигурации маршрута dispatcher;
- отсутствия запрещенных listeners для выбранного сценария;
- принадлежности порта нужному бинарнику.

Следствие: readiness успешно пройдет, если порт занят посторонним процессом; лишний публичный listener останется незамеченным.

Что исправить: использовать `procnet` и runtime status компонентов, проверять положительные и отрицательные ожидания топологии в цикле до deadline.

### 6. Crash recovery не имеет полного rollback state

`PhaseCandidateActive` записывается, но журнал содержит только укрупненные `Previous` topology fields. Полный `prevTg` и `prevDisp` существуют только в памяти вызова.

При startup recovery Telegram восстанавливается через `ManagedIngressConfig`, содержащий лишь `Enabled`, `ListenPort`, `PublicHostname`, а не полный секрет, TLS domain, direct/raw ports, carrier и upstream.

Доказательства:

- `internal/serveringress/coordinator.go:992-1009` — полные snapshots не записываются в durable journal;
- `internal/serveringress/coordinator.go:1026-1033` — phase записывается до последовательной активации;
- `internal/serveringress/coordinator.go:370-420` — startup rollback использует урезанный Telegram state;
- `internal/serveringress/coordinator.go:1146-1179` — полный rollback возможен только пока процесс жив и локальные snapshots доступны.

Дополнительно startup recovery идет по `j.Affected` в прямом порядке, тогда как `calculateAffected` формирует Xray -> Dispatcher -> Telegram. Это нарушает требование обратного порядка.

Что исправить:

1. Хранить rollback snapshot каждого компонента в его собственном защищенном transaction storage, а в общем журнале — только tx IDs/checksums, не секреты.
2. Зафиксировать подфазы активации либо сделать component transactions идемпотентными и однозначно восстанавливаемыми.
3. При recovery откатывать в обратном порядке Dispatcher -> Telegram -> Xray.
4. Добавить crash/restart tests после каждого шага активации.

### 7. Граница отмены job не совпадает с point of no return

Coordinator переходит в `PhaseCommitting` и выполняет finalize внутри `ExecuteIngressTransaction`. JobRunner получает `JobPhaseCommitting` только **после возврата** coordinator.

Доказательства:

- `internal/serveringress/coordinator.go:1095-1121`;
- `internal/serverwizard/service.go:464-484`.

Следствие: UI/API еще считает отмену допустимой в момент, когда coordinator уже прошел необратимую границу. Запрос cancel может быть принят, хотя finalize продолжается.

Что исправить: coordinator должен вызвать phase callback непосредственно перед записью `PhaseCommitting`; callback синхронно переводит JobRunner в non-cancellable state до point of no return.

## P1 — существенные дефекты

### 8. Ошибки rollback теряются на большинстве ветвей

Во многих ветвях `rollbackIngressTx(...)` вызывается без проверки возвращенной ошибки (`internal/serveringress/coordinator.go:1019-1081`). Только readiness/cancellation branches обрабатывают `rbErr`.

Хотя coordinator выставляет внутренний `recoveryNeeded`, наружу часто возвращается исходная ошибка без `ErrRecoveryRequired`. Wizard затем классифицирует ее как `INVALID_REQUEST`.

Исправление: единый helper `failAndRollback`, который всегда объединяет ошибки и возвращает sentinel `ErrRecoveryRequired` при неудачном rollback.

### 9. В fresh config все еще подставляются фиктивные или частные defaults

- `DefaultUpstreamDevice = "nwg1"` — `internal/tgwebproxy/types.go:19`;
- пустой TLS domain заменяется на `front.example.com` — `internal/serverwizard/service.go:411-414` и `489-492`;
- генератор также подставляет `front.example.com` — `internal/tgwebproxy/config_gen.go:35`.

`front.example.com` является placeholder, а не рабочим SNI. Его нельзя молча превращать в runtime config. `nwg1` является особенностью конкретной установки.

Исправление: либо требовать валидный TLS domain для direct/dual, либо иметь явно документированный runtime default, который гарантированно поддерживается. Direct egress должен храниться как тип, а не как имя предполагаемого интерфейса.

### 10. Same-Origin проверяет host, но не scheme

Комментарий говорит `Strict Same-Origin`, но код сравнивает только `u.Host` с `r.Host`.

Доказательство: `internal/api/server_wizard.go:92-110` и `151-165`.

Следствие: проверка не реализует заявленное соответствие scheme фактическому TLS state запроса.

Исправление: ожидаемая схема должна быть `https` при `r.TLS != nil`, иначе `http`; сравнивать normalized scheme + host. Учесть доверенный reverse proxy только через уже существующий централизованный механизм, а не безусловный `X-Forwarded-Proto`.

### 11. ComputeStrict не может fail-close на ошибке egress catalog

Контракт `EgressSummaryReader` возвращает только строку, без error. Поэтому `ComputeStrict` не способен отличить корректный summary от сбоя каталога/пробы, несмотря на формулировку плана.

Исправление: изменить контракт на `(string, error)` либо получать типизированный snapshot каталога с ошибкой; включить его в fingerprint только после успешного сбора.

### 12. Ошибочный fallback генерации Xray link

Если `GenerateLinks` завершается ошибкой, fallback всегда добавляет параметры XHTTP (`mode=packet-up`, `uplinkHTTPMethod=GET`) даже при `type=ws`.

Доказательство: `internal/serverwizard/service.go:529-535`.

Исправление: не скрывать ошибку GenerateLinks. Job должен завершаться ошибкой reveal/materialization либо использовать общий transport-aware generator без ручного дублирования URL.

### 13. Preflight, plan и desired используют разные источники истины

`Plan` запускает preflight на сыром request, затем отдельно нормализует Desired. Typed egress resolution в эту последовательность не встроен. Это допускает расхождение между тем, что проверено, показано и применено.

Исправление: единый pipeline:

```text
request -> normalize syntax -> resolve profile/egress -> DesiredWizardConfig
        -> preflight(Desired) -> plan(Desired) -> apply(тот же Desired)
```

## Недостатки тестового покрытия

Заявленный в плане acceptance suite не реализован полностью либо не проверяет реальные гарантии:

- нет полноценной матрицы lifecycle Telegram workers;
- нет topology readiness с реальным procnet/owner/bind/absence;
- нет crash recovery из `candidate_active` после каждого component boundary;
- нет проверки строгой profile/mode matrix;
- нет проверки typed egress от UI selection до runtime config;
- deadlock test использует fake components и dialer, всегда сообщающий успех, поэтому не является end-to-end runtime test;
- stale fingerprint test допускает различное синхронное/асинхронное поведение вместо фиксации одного API-контракта.

## Рекомендуемый порядок исправления

1. Сделать scenario-aware Telegram runtime и тестовую матрицу процессов/портов.
2. Встроить typed egress resolver в единый Desired pipeline.
3. Убрать hardcoded Xray path/port и фиктивные Telegram defaults.
4. Реализовать topology-aware readiness через procnet.
5. Переделать durable rollback/recovery и порядок восстановления.
6. Синхронизировать coordinator phase с JobRunner cancellation boundary.
7. Унифицировать обработку rollback errors.
8. Исправить strict same-origin и transport-aware link generation.
9. Добавить недостающие acceptance/crash tests.
10. После этого повторить full validation, ARM64 build и frontend checks.

## Условия приемки после исправлений

Реализацию можно считать готовой только если:

- каждый Telegram scenario запускает ровно заявленные workers и listeners;
- отключенные listeners явно подтверждаются отсутствующими;
- произвольный/недоступный egress отклоняется до мутации;
- примененный Xray path/port совпадает с plan и export links;
- чужой процесс на ожидаемом порту не проходит readiness;
- падение после любого activation step восстанавливает полный предыдущий state без потери секретов;
- cancel гарантированно невозможен после coordinator point of no return;
- rollback failure всегда переводит систему в видимое `RECOVERY_REQUIRED`;
- все 15 acceptance tests из плана существуют и проверяют реальные runtime semantics;
- проходят race tests, frontend check/build, Linux ARM64 build и `git diff --check`.

## Итоговая оценка

Архитектурное направление плана правильное, но текущая реализация завершена ориентировочно на **55-65% по функциональному объему и существенно меньше по гарантиям атомарности/восстановления**. Walkthrough следует считать отчетом о намерениях и частично выполненных изменениях, а не подтверждением готовности.

**Решение: вернуть на доработку; развертывание мастеров на пользовательских роутерах до закрытия P0 не рекомендовано.**
