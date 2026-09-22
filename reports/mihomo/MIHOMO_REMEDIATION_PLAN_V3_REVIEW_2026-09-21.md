# Ревью Mihomo Remediation Plan — редакция 3

Проверен файл:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Дата: 2026-09-21.

## Вердикт

Редакция 3 закрывает предыдущие шесть замечаний по существу, но при сверке с фактическим кодом обнаружены ещё **4 конкретные ошибки плана**. Они небольшие по объёму, однако две из них приведут к сохранению исходного дефекта или к ошибкам компиляции/ложной полноте тестов.

Статус: **почти готов, требуется последняя корректировка четырёх пунктов ниже**.

## Что теперь сделано правильно

- Общая durable persistence вынесена из transaction/recovery wrappers.
- Для `regenerate_from_desired` зафиксирован правильный порядок: bundle публикуется до active-config/runtime side effects.
- Добавлен resume algorithm для rollback.
- Target generation передаётся в process receipt.
- Bridge listener port перенесён в отдельный ListenerSpec/procfs proof.
- Добавлены ownership/schema поля parking record.
- Команды PATH сохраняют `/usr/local/go/bin`.
- Race-команда использует `-count=1`.
- IPK/deploy явно исключены без отдельного запроса.
- Происхождение `dev/null` подтверждено Git history.

## Последние обязательные исправления

### 1. Gate A всё ещё не устраняет ambient PATH

План предлагает выбирать verifier по `cfg.Operator.Binary()`, если файл существует.

Но фактический `Operator.Binary()` вызывает `resolveBinary()`, а тот выполняет:

```go
exec.LookPath("mihomo")
```

Следовательно, проверка через `cfg.Operator.Binary()` по-прежнему зависит от ambient PATH — исходный дефект остаётся.

Нужно записать в план одно однозначное решение:

- `NewApplyCoordinator` не выполняет autodetection вообще;
- если `cfg.Verifier != nil`, используется он;
- иначе используется `DefaultProcessVerifier` без проверки наличия binary;
- все unit-тесты с fake operator обязаны передавать Noop/Fake verifier;
- production wiring явно передаёт platform verifier;
- `Operator.Binary()` продолжает использоваться только как ожидаемый canonical executable во время runtime proof, но не для выбора типа verifier.

Альтернатива — добавить `ConfiguredBinaryPath()` без `LookPath`, но она сложнее и не нужна для выбора verifier.

### 2. Смена API `FindListeningProcess` ломает дополнительные пакеты, не перечисленные в плане

Сейчас функция вызывается не только из Mihomo verifier, но также из:

- `internal/mihomo/operator.go`;
- `internal/sys/procnet/listener.go` (внутренний helper);
- `internal/serverwizard/fingerprint.go`;
- `internal/serveringress/migration_saga.go`;
- существующих тестов `internal/sys/procnet/listener_test.go`.

Если просто заменить сигнатуру на четырёхаргументную, эти места перестанут собираться.

Предпочтительное безопасное решение:

```go
FindListeningProcess(procDir, addr string, port int) // совместимый TCP wrapper
FindListeningProcessNetwork(procDir, network, addr string, port int)
```

Mihomo typed listener proof использует новую network-aware функцию. Старые потребители остаются на TCP wrapper до отдельной миграции. Либо план должен явно перечислить и обновить все вызовы и их тесты.

### 3. Mutation matrix не совпадает с зарегистрированными routes

В плане указан несуществующий endpoint:

```text
POST /api/mihomo/native/restart
```

Фактически зарегистрирован:

```text
POST /api/mihomo/reload
```

Также в плане пропущены или неполно представлены:

- `PUT /api/mihomo/native/rule-providers/{id}`;
- `POST /api/mihomo/install`;
- `POST /api/mihomo/uninstall`;
- `POST /api/mihomo/reload`;
- `POST /api/mihomo/recovery/reconcile` — это административное recovery и должно тестироваться отдельно, а не блокироваться общим gate;
- `POST /api/mihomo/router/inspect` — нужно классифицировать, mutation это или диагностический POST;
- `/api/mihomo/clash/*` proxy может пропускать mutating Clash API methods и требует явной политики;
- alias `POST /api/router/mihomo/rules/unsupported/delete`.

План должен требовать автоматически/явно построить inventory из `MihomoHandler.RegisterRoutes`, а затем классифицировать каждый non-GET route:

1. обычная coordinated mutation — блокировать в degraded;
2. recovery command — разрешать только по специальным recovery preconditions;
3. read-only POST/diagnostic — доказать отсутствие side effects;
4. external proxied controller mutation — блокировать либо разрешать по отдельной allowlist.

Нельзя утверждать «полная матрица», пока inventory не совпадает с registration code.

### 4. Controller client и bridge address proof описаны через несуществующие данные

В Gate D написано использовать параметры из `c.cfg.Operator`, но код находится в `MihomoHandler`, где доступны `h.op`, а `newControllerRequest` у `Operator` не экспортирован.

Нужно добавить узкий экспортированный метод Operator, например:

```go
func (o *Operator) RefreshProvider(ctx context.Context, providerName string) error
```

Он внутри использует существующий `newControllerRequest`, configured address и secret. Не следует экспортировать универсальный сырой HTTP client без необходимости.

В Gate C указано `address matching`, но ожидаемого assigned address в `BridgeRef` нет. План должен сформулировать проверку реально доступных данных:

- полное равенство embedded `ObservedBridge.BridgeRef` ожидаемому `BridgeRef`;
- `Exists == true`;
- `Up == true`;
- `AssignedIP != ""` и валиден, если публикация обязана назначать адрес;
- если требуется сравнение с ожидаемым IP, его сначала нужно добавить в контракт bridge runtime и schema.

## Дополнение к тестам

После исправления network-aware API добавить к verification:

```bash
go test -count=1 ./internal/serverwizard ./internal/serveringress ./internal/sys/procnet
```

После inventory mutation routes тест должен падать, если в `RegisterRoutes` появился новый non-GET Mihomo endpoint, который не классифицирован в матрице. Это предотвратит повторное появление обхода degraded gate.

## Решение

После внесения этих четырёх поправок план можно считать окончательно готовым и запускать с Gate A. Повторное архитектурное расширение плана после этого не требуется: дальнейшие замечания следует выявлять уже по diff и тестам каждого завершённого Gate.
