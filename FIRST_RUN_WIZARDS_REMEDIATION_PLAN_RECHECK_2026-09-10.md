# Повторная проверка remediation plan First-Run Wizards

Проверен актуальный `implementation_plan.md` из Antigravity после аудита `FIRST_RUN_WIZARDS_IMPLEMENTATION_AUDIT_2026-09-10.md`.

## Вердикт

План стал существенно лучше и адресует все 13 найденных дефектов. **В текущем виде запускать его в реализацию еще не следует.** Перед началом необходимо внести перечисленные ниже обязательные уточнения: два из них предотвращают нарушение уже работающей ingress-топологии и применение сервера без выдаваемых клиенту реквизитов.

После исправления P0-замечаний ниже план можно отдавать агенту на выполнение.

## P0 — обязательные поправки к плану

### 1. Telegram scenario нельзя применять изолированно от общей ingress-топологии

В плане для `direct_fake_tls` требуется отсутствие dispatcher `:9009`, а для `cdn_http` — отсутствие listener `:8443`. Это корректно только когда соответствующий порт не нужен другому действующему серверу.

Coordinator обслуживает общую топологию Xray + Telegram. Если Xray уже использует CDN dispatcher, применение Telegram `direct_fake_tls` не должно останавливать dispatcher. Аналогично readiness не должна требовать отсутствия общего listener только на основании сценария одного компонента.

Исправить архитектурное правило:

```text
desired topology = previous topology + mutation selected server
required shared components = union(requirements of every enabled server)
```

- Dispatcher запускается, если он нужен хотя бы Xray или Telegram CDN.
- Dispatcher останавливается только если после мутации он не нужен ни одному enabled server.
- Negative readiness строится по полной `DesiredTopology`, а не только по `DesiredWizardConfig.Scenario`.
- Проверка отсутствия порта допустима лишь для порта, которым coordinator эксклюзивно управляет и который не требуется итоговой topology.

Добавить тесты применения каждого Telegram scenario при уже работающем Xray и наоборот.

### 2. Client links нельзя генерировать после необратимого commit с возможностью ошибки

План предлагает после успешной транзакции вызвать `GenerateLinks`, а при ошибке завершить job ошибкой. Это оставит реально примененный Xray server, но пользователь не получит UUID/ссылку; повторный apply создаст еще одну конфигурацию.

Исправить:

1. Сразу после генерации UUID/secret и построения candidate вызвать чистый transport-aware builder, не читающий mutable runtime state.
2. Полностью сформировать и проверить reveal payload **до активации либо как минимум до `PhaseCommitting`**.
3. Передать готовый reveal payload дальше по job, но публиковать его только после успешного commit.
4. Если построение payload невозможно, завершить job до мутации или выполнить rollback до point of no return.
5. После commit не должно оставаться ни одной операции, ошибка которой способна лишить пользователя единственных реквизитов.

Лучший контракт:

```go
BuildShareLinks(candidate Config, clientUUID string) (ShareLinks, error)
```

`GenerateLinks` от текущего сохраненного runtime config можно оставить для обычного UI, но wizard должен строить ссылки именно из неизменяемого candidate.

### 3. Durable recovery должен включать dispatcher, а не только Telegram

План вводит private snapshot для Telegram и использует уже существующий Xray transaction, но dispatcher по-прежнему предлагается восстанавливать из укрупненной topology. Этого недостаточно: в конфиге dispatcher есть targets, hostname, path и потенциально дальнейшие поля.

Добавить симметричный transaction contract для dispatcher:

- `PrepareCandidate(txID, candidate)`;
- `CommitPrepared(txID)`;
- `RollbackPrepared(txID)`;
- `FinalizePrepared(txID)`.

Полный предыдущий dispatcher config хранится в component-private каталоге с правами `0700/0600`; общий coordinator journal содержит только component tx ID и checksum.

Recovery должен быть идемпотентным, даже если падение произошло:

- до prepare компонента;
- после prepare, но до activate;
- между активациями компонентов;
- после readiness;
- во время finalize.

### 4. Одного `PhaseCandidateActive` недостаточно без durable component state

Сейчас phase записывается до последовательной активации, поэтому после падения неизвестно, какие компоненты успели активироваться. Private transaction каждого компонента должен сам иметь durable состояния `prepared/active/finalized/rolled_back`, либо coordinator journal должен фиксировать завершение каждого шага.

Предпочтительно первое: recovery вызывает rollback для всех подготовленных component tx в обратном порядке, а операции являются идемпотентными. Отсутствующий или уже rolled-back tx не должен превращать штатное recovery в ошибку.

### 5. Убрать противоречие вокруг встроенного TLS domain

План одновременно требует:

- `DefaultTlsDomain = "gateway.icloud.com"`;
- fresh config без фиксированного domain;
- тест «zero personal/fixed domains in defaults».

Это несовместимо. `gateway.icloud.com` — внешний фиксированный hostname, а не универсальный нейтральный default.

Принять единый контракт:

- `DefaultTlsDomain = ""`;
- для `direct_fake_tls` и `dual` TLS domain является обязательным пользовательским полем;
- UI показывает только нейтральный placeholder вроде `front.example.com`, но placeholder никогда не записывается в config;
- существующее сохраненное значение при миграции сохраняется без переписывания;
- для `cdn_http`, если Fake-TLS listener не используется, TLS domain не требуется.

### 6. `direct` не должен сохраняться как имя интерфейса

В `DesiredWizardConfig` может храниться typed egress mode `direct`, но в `tgwebproxy.Config.UpstreamDevice` direct должен материализоваться как пустая строка. Иначе старый или сторонний генератор может создать `bindtodevice = "direct"`.

Убрать `DefaultUpstreamDevice = "direct"`; безопасный runtime default — `""`.

## P1 — необходимые уточнения

### 7. Единый pipeline не должен повторно нормализовать request при Apply

В разделе API написано «Connect single BuildDesiredConfig pipeline in Preflight, Plan, Apply». Apply не должен заново интерпретировать исходный request.

Правильный контракт:

```text
Preflight request -> BuildDesiredConfig -> checks/preview
Plan request      -> BuildDesiredConfig -> checks -> store immutable Desired
Apply plan_id     -> load stored Desired -> fingerprint check -> materialize exactly it
```

Apply принимает plan ID/session/CSRF, но не новый набор wizard fields. Иначе снова появится расхождение plan/apply.

### 8. Fingerprint не должен включать желаемые поля как наблюдаемое состояние

Фраза «Include Path, PublicPort, DispatcherPort in fingerprint dimensions» неоднозначна. Fingerprint описывает текущее наблюдаемое состояние системы, а Desired уже защищен неизменяемой plan record.

В fingerprint следует включить текущие runtime/config values соответствующих компонентов. Desired values не нужно примешивать в state fingerprint; вместо этого plan record может иметь отдельный canonical desired hash.

### 9. Egress catalog должен иметь настоящий error-bearing контракт

Недостаточно изменить только `EgressSummary`. Если нижележащий catalog возвращает slice без error, fail-closed все равно будет фиктивным.

План должен указать один из вариантов:

- изменить catalog snapshot API на `(Snapshot, error)`;
- либо адаптер самостоятельно выполняет все error-bearing probes и возвращает ошибку при неполном snapshot.

`Resolve` и `ComputeStrict` должны использовать один и тот же snapshot, чтобы доступность не расходилась.

### 10. Worker start/stop order нужно определить явно

В плане stop order указан как `tproxy-server`, `telemt-direct`, `telemt-raw`, что не является обратным порядком логичной активации `telemt-direct`, `telemt-raw`, `tproxy-server`.

Зафиксировать dependency order:

```text
start: telemt-direct (если нужен), telemt-raw, tproxy-server
stop:  tproxy-server, telemt-raw, telemt-direct
```

Независимый direct worker можно запускать до или после raw, но teardown web chain обязательно идет frontend -> backend.

### 11. Readiness должна проверять endpoint identity, а не только PID/name

Плану нужны точные ожидания:

- protocol TCP;
- local address и wildcard semantics;
- IPv4/IPv6;
- port;
- socket inode -> PID;
- executable identity через точный разрешенный path, а не substring cmdline;
- dispatcher route/path/target из фактически активного config.

Для negative checks любая LISTEN-запись на управляемом endpoint является ошибкой, независимо от PID.

### 12. Same-Origin должен учитывать реальную схему развертывания

Сравнение с `r.TLS` корректно при прямом TLS, но может сломать работу за доверенным reverse proxy. Следует использовать общий helper определения external origin, если такой механизм уже есть. `Forwarded`/`X-Forwarded-Proto` нельзя доверять от произвольного клиента.

Добавить тесты direct HTTP, direct HTTPS, forged forwarded headers и доверенный proxy — если он поддерживается приложением.

### 13. Acceptance suite разделить на unit/integration/router smoke

Фраза «all 15 tests checking real runtime semantics» чрезмерна для обычных Go unit tests. Тест с fake dialer не подтверждает реальные сокеты, а запуск router-specific процессов в CI может быть невозможен.

Разделить:

- unit: normalization, matrices, generators, rollback ordering;
- Linux integration: procnet, real listeners, ownership, crash fixtures;
- router smoke (ручной/опциональный): реальные init scripts и процессы Entware.

Каждый acceptance criterion должен ссылаться на конкретный уровень теста.

### 14. Verification plan неполон

Добавить:

```text
pnpm run build
git diff --check
GOOS=linux GOARCH=arm64 go build ./cmd/awg-manager
```

После PowerShell cross-build обязательно восстанавливать прежние `GOOS`, `GOARCH`, `GOMIPS`, чтобы не загрязнять окружение последующих команд. MIPSLE не является обязательной платформой текущего пользователя и может остаться дополнительной проверкой, но не заменяет ARM64.

## Скорректированный порядок реализации

1. Зафиксировать shared `DesiredTopology` и component transaction interfaces.
2. Реализовать scenario-aware Telegram lifecycle с пустыми безопасными defaults.
3. Реализовать typed Desired/egress/profile pipeline.
4. Перенести Xray path/port и pure share-link materialization в candidate stage.
5. Реализовать private tx storage для Telegram и dispatcher.
6. Переделать coordinator activation/recovery на idempotent component transactions.
7. Синхронизировать cancellation boundary и унифицировать rollback errors.
8. Реализовать readiness полной итоговой topology через procnet.
9. Исправить API origin contract.
10. Добавить unit, Linux integration и router smoke coverage.
11. Выполнить полную проверку без сборки IPK и без deployment.

## Решение о запуске

**Сейчас: не запускать.**

**Можно запускать после внесения в `implementation_plan.md` как минимум пунктов 1-7 из этого ревью.** Остальные пункты должны остаться обязательными условиями приемки, а не необязательными пожеланиями.

