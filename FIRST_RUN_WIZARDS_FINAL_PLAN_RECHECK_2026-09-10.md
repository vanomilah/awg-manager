# Финальная перепроверка плана First-Run Wizards

Дата: 2026-09-10  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

**Архитектура одобрена, но в перечень работ необходимо добавить три обязательных блока. После этого план можно запускать.**

Последняя редакция корректно закрыла замечания о transaction lifecycle, порте Xray 9008, fingerprint callback, package boundaries, direct profile, readiness topology и recovery tests. Новые замечания относятся не к выбранной архитектуре, а к неполному охвату существующего кода.

## Обязательные дополнения

### P0. Добавить переработку runtime-конфигуратора и экспортов Xray

План обещает полноценную матрицу `xhttp_get`/`ws` и egress `direct`/`interface`/`socks`, но в Detailed Implementation Plan отсутствуют изменения `internal/xrayserver`.

Текущее состояние:

- `internal/xrayserver/service.go:51-54` хранит только XHTTP-oriented `Mode`, `UplinkMethod` и числовой `OutboundSocksPort`;
- `service.go:628-695` генерирует все ссылки/экспорты с жёстким `type=xhttp`, `network=xhttp`, `transport.type=xhttp` и `network: xhttp`;
- runtime config в `service.go:851+` также строит только `streamSettings.network = xhttp`;
- interface-bound Xray outbound в текущей `Config` вообще не представлен.

Добавить в план отдельный компонент **Xray transport and outbound materialization**:

1. Ввести типизированные transport/outbound настройки в `xrayserver.Config`, сохранив миграцию старого JSON.
2. Генерировать Xray runtime `streamSettings` раздельно для XHTTP и WebSocket.
3. Генерировать VLESS URL, Happ JSON, Sing-box JSON и Mihomo YAML согласно фактическому transport.
4. Реализовать direct, SOCKS и interface-bound outbound либо явно блокировать неподдерживаемый вариант.
5. Проверять семантическую совместимость каждого экспортируемого формата; «валидный JSON/YAML» недостаточен.
6. Добавить round-trip/golden tests отдельно для XHTTP и WS, а также для каждого поддержанного egress.

Без этого wizard сможет выбрать WS в интерфейсе, но существующий сервис всё равно создаст XHTTP-конфигурацию.

### P0. Очистка hardcoded-доменов должна охватывать `internal/tgwebproxy`

План указывает удалить `ya.ru` из `serverwizard`, но текущее значение находится также в основном Telegram-компоненте:
\n+- `internal/tgwebproxy/types.go:20` — `DefaultTlsDomain = "ya.ru"`;
- комментарии структуры всё ещё содержат персональный direct host и фиксированный TLS default (`types.go:35`, `43`).

Acceptance test `TestNoPersonalDomainsInDefaults`, заявленный в плане, при частичной очистке заведомо провалится.

Добавить изменения `internal/tgwebproxy/types.go`, default initialization, validation, migration и frontend/API contracts. Старые пользовательские конфигурации с явно сохранённым `ya.ru` нельзя молча ломать или переписывать: удалить нужно встроенный default, сохранив загруженное пользовательское значение при миграции.

### P0. `FingerprintEngine` должен получить настоящий fail-closed API

План вводит `StateFingerprintFunc func(...) (string, error)`, но затем предлагает адаптер к существующему `s.fingerprintEngine.Compute(ctx)`. Текущий `Compute` возвращает только `string` и превращает некоторые ошибки probes в часть hash. Такой адаптер не может выполнить заявленный fail-closed contract.

Добавить:

```go
func (e *FingerprintEngine) ComputeStrict(ctx context.Context) (string, error)
```

`ComputeStrict` должен прекращать apply при невозможности надёжно получить критичное состояние. Старый `Compute` можно оставить для некритичного отображения/обратной совместимости, но transaction callback обязан использовать strict-вариант.

Нужны тесты ошибок procfs, state readers и отменённого context; ни одна из них не должна приводить к продолжению transaction.

## P1. Уточнения при реализации

1. `withIngressLock` сейчас не принимает `context.Context`. Не добавлять обращение к несуществующему `ctx` непосредственно в этот метод без изменения сигнатуры. Проще проверить context в callback сразу после реального получения lock и до чтения fingerprint.
2. `PhaseCandidateActive` необходимо добавить в durable recovery switch, journal validation и fault-injection tests, а не только в константы.
3. Для Telegram сценариев требуется full-config snapshot. `ManagedIngressConfig` содержит лишь `Enabled`, `ListenPort`, `PublicHostname` и недостаточен для восстановления direct/raw/admin/carrier/egress/TLS настроек.
4. При rollback использовать обратный порядок фактической активации. В схеме Xray активируется раньше Telegram и dispatcher, поэтому откатывать следует dispatcher, Telegram, Xray.
5. Проверка listener должна учитывать IPv4 и IPv6 procfs (`tcp`, `tcp6`, `udp`, `udp6`) и точный bind address.
6. Secret/UUID не выводить в journal, plan, status, structured error или лог. Best-effort очистка буферов не равна гарантированному стиранию Go string из памяти.

## Дополнить acceptance suite

- `TestXrayRuntimeConfigByTransport`: golden config для XHTTP и WS.
- `TestXrayShareLinksByTransport`: каждый экспорт соответствует выбранному transport.
- `TestXrayOutboundMaterialization`: direct/SOCKS/interface и blocked unsupported cases.
- `TestTelegramLegacyTlsDomainPreservedOnMigration`: пользовательское старое значение сохраняется, встроенного default больше нет.
- `TestFingerprintStrictProbeFailureBlocksTransaction`.
- `TestRecoveryFromCandidateActiveForEveryComponentOrder`.

## Разрешение на реализацию

После включения этих трёх P0-блоков непосредственно в `implementation_plan.md` план можно передавать агенту и выполнять. Повторное архитектурное проектирование после этого не требуется: следует реализовывать по этапам и не переходить к UI до прохождения coordinator/Xray/Telegram transaction tests.
