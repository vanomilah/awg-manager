# Аудит реализации First-Run Setup Wizards

Дата: 2026-09-10  
Проверенный документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Репозиторий: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`

## Вердикт

**Реализация не готова к установке на роутер и не принимается.**

Walkthrough утверждает, что требования полностью реализованы, атомарность обеспечена, а мастер проверен тестами. Компиляция и существующие тесты действительно проходят, однако основной production-путь применения конфигурации зависает на повторном захвате нерекурсивной блокировки. Кроме того, мастер не применяет значительную часть выбранных пользователем параметров, содержит персональные значения и фактически не выполняет заявленную проверку готовности.

## Критические замечания

### P0. Production apply зависает на вложенном `WithIngressLock`

`WizardService.executeApplyJob()` получает общую ingress-блокировку в `internal/serverwizard/service.go:342`, а затем внутри неё вызывает `applyTelegram()` или `applyXray()` (`service.go:349-354`). Эти методы вызывают `Coordinator.ApplyTelegramConfig()`/`ApplyXrayConfig()` (`service.go:418`, `service.go:503`).

Оба production-метода координатора повторно вызывают `withIngressLock()` (`internal/serveringress/coordinator.go:702-710`, `714-735`). Сам `withIngressLock()` первым действием захватывает `c.mu.Lock()` и удерживает mutex до возврата callback (`coordinator.go:277-298`). `sync.Mutex` не рекурсивен, поэтому второй вызов блокируется навсегда.

Почему тесты проходят: `mockCoordinatorBridge.WithIngressLock()` в `internal/serverwizard/service_test.go:26-29` просто вызывает callback и не моделирует ни mutex, ни файловый lock. Happy-path тест тем самым проверяет другой контракт, чем production.

Что исправить:

1. Сделать в coordinator один публичный атомарный метод уровня мастера: проверить fingerprint и применить конфигурацию под **одним** захватом ingress-lock.
2. Либо предоставить строго внутренние методы `applyXrayConfigLocked`/`applyTelegramConfigLocked`, вызываемые только при уже удерживаемой блокировке.
3. Добавить integration-тест с настоящим `serveringress.Coordinator`, который должен завершать apply за ограниченное время. Mock блокировки для этой проверки недостаточен.

### P0. Проверка fingerprint не атомарна относительно изменения состояния

`Apply()` проверяет fingerprint под одной блокировкой (`internal/serverwizard/service.go:301-311`), освобождает её, создаёт фоновую job (`service.go:320-324`), а job позже получает уже другую блокировку (`service.go:342`). Между этими операциями другой запрос может изменить ingress/config/runtime state.

Внутри второй блокировки fingerprint повторно не вычисляется. Следовательно, утверждение walkthrough об «atomic validation» неверно даже после устранения deadlock.

Исправление должно объединить последнюю проверку fingerprint и начало транзакционного apply в одну критическую секцию.

### P0. В универсальный мастер встроены личный домен и фиксированный TLS-домен

В backend:

- `internal/serverwizard/service.go:395` — fallback `vinvanvlad.crazedns.ru`;
- `service.go:415` — `TlsDomain: "ya.ru"`;
- `service.go:431` — MTProxy secret строится с доменом `ya.ru`.

Во frontend личный домен также является значением формы по умолчанию:

- `frontend/src/lib/components/servers/telegram-wizard/TelegramProxyWizard.svelte:47`.

Это прямо нарушает требование универсальной настройки без персональных доменов. Пустой обязательный параметр должен блокировать preflight/plan с понятной подсказкой; TLS-домен должен быть отдельным явным полем либо безопасно генерироваться согласно документированному контракту сервиса.

Ошибки `crypto/rand.Read` при создании secret и UUID также игнорируются (`service.go:402`, `service.go:471`); при ошибке генератора применение должно завершаться до мутации состояния.

### P0. Выбор пользователя отображается в плане, но не управляет итоговой конфигурацией

Telegram:

- `Scenario` участвует в UI/DTO, но `applyTelegram()` всегда создаёт одну и ту же конфигурацию;
- `Backend` всегда `127.0.0.1:2398`, `CarrierMode` всегда `get`, `AdminPort` всегда `8086` (`service.go:410-415`);
- варианты `direct_fake_tls`, `cdn_http`, `dual` не включают и не выключают соответствующие компоненты;
- CDN profile валидируется/показывается, но не преобразуется в runtime-конфигурацию.

Xray:

- выбранные `Mode` и `CdnProfileID` игнорируются;
- применяются только `packet-up`, `GET`, dispatcher `9009` и Mihomo SOCKS `1099` (`service.go:496-500`);
- Xray wizard вообще не отправляет egress: в `XrayWizard.svelte:108-119` поля `upstream_device` нет.

Это опасная модель интерфейса: пользователь подтверждает один change plan, а сервер применяет другую, жёстко заданную конфигурацию.

Нужно ввести единую нормализованную server-side desired configuration, из которой одновременно строятся:

- preflight;
- отображаемый change plan;
- fingerprint scope;
- фактический transaction apply.

Нельзя отдельно собирать «описательный план» и затем повторно интерпретировать исходный request другими hardcoded-правилами.

### P0. Фаза `verifying` не выполняет readiness-проверку

После возврата apply мастер меняет phase на `verifying`, выполняет только `time.Sleep(150 * time.Millisecond)` и сразу переходит к `committing` (`internal/serverwizard/service.go:361-368`). Проверок процесса, сокетов, dispatcher route, health endpoint либо создания тестового соединения нет.

Таким образом walkthrough ошибочно заявляет safe application и проверку готовности. Успешная job означает лишь, что вызов apply вернулся без ошибки.

Нужны компонентные readiness probes с deadline и проверкой ожидаемой топологии. Ошибка readiness должна запускать документированный rollback; при неуспешном rollback — переводить coordinator/job в `recovery_required`.

## Высокий приоритет

### P1. Egress-каталог сообщает о готовности Mihomo без runtime-проверки

`internal/serverwizard/egress/adapter.go:72-82` всегда публикует `mihomo:1099` как `Available: true`. Наличие процесса/слушателя проверяется отдельно лишь в Xray preflight, причём предупреждением, а не блокирующей ошибкой. Для Telegram этот ID может быть выбран и сохранён, хотя реального выхода нет.

Кроме того, выбранный egress для Xray не применяется вовсе, а для Telegram просто записывается в `UpstreamDevice`, хотя catalog ID может быть `mihomo:1099`, `direct` или ID туннеля, а поле сервиса ожидает имя сетевого устройства. Это разные типы маршрута и их нельзя передавать одной строкой без resolver/apply contract.

Требуется типизированное разрешение egress:

- direct;
- interface/device;
- SOCKS endpoint;
- router outbound/group.

Capability должен вычислять реальную доступность и поколение, а apply — применять именно выбранный тип.

### P1. Job runner не является устойчивым и не подтверждает заявленную recovery-модель

Jobs, cancellation state и секреты хранятся только в памяти (`internal/serverwizard/jobs.go`). После перезапуска AWG Manager клиент потеряет job, хотя компонентная транзакция может остаться незавершённой. Это допустимо только если API явно сообщает восстановленное состояние через durable coordinator journal и предоставляет пользователю понятный recovery flow; сейчас такого связывания job с transaction journal нет.

Фаза `committing` выставляется уже после завершения component apply (`service.go:349-367`), поэтому заявленный point-of-no-return не соответствует реальной границе мутаций. Отмена во время apply зависит только от контекста и поведения нижнего сервиса, но wizard сам не инициирует rollback и не переводит job в `rolling_back`.

### P1. Preflight неполный и местами проверяет не выбранную конфигурацию

- Xray всегда проверяет Mihomo `:1099`, независимо от пользовательского egress (`internal/serverwizard/preflight.go:259+`).
- Невалидный CDN profile для Telegram — лишь warning, после чего apply всё равно использует hardcoded GET.
- Не проверяются допустимые enum-значения `Scenario`, `Mode`, `DeviceType` и совместимость mode/profile.
- Проверка домена поверхностная: только пробел и `/`; нет нормализации hostname/IDNA/порта.
- Walkthrough перечисляет порт `8081`, но `DefaultTargetPorts` содержит `8086`; документ и код расходятся.

### P1. Нет защиты размера и строгого декодирования request DTO

`internal/api/server_wizard.go:180-184`, `196-200` и `229-233` декодируют неограниченный body обычным `json.Decoder`. Не используются `http.MaxBytesReader`, `DisallowUnknownFields` и проверка единственного JSON-объекта.

Для endpoint'ов, строящих и сохраняющих server-side plan, нужен общий bounded strict decoder и централизованная DTO validation.

## Средний приоритет

### P2. Same-Origin проверяет только host, а не полный origin

`internal/api/server_wizard.go:74-93` сравнивает только `u.Host` и `r.Host`; scheme не проверяется. Комментарий «Strict Same-Origin» поэтому слишком сильный. Следует вычислять ожидаемый origin с учётом доверенной схемы/proxy headers либо явно документировать same-host policy.

### P2. UI остаётся привязанным к одному CDN-провайдеру

Несмотря на требование нейтрального CDN UI, Xray wizard несколько раз называет Cloudflare (`XrayWizard.svelte:245`, `347`, `371`), Telegram wizard — `308`, `355`, а backend profile — `internal/serverwizard/cdn/profile.go:55`.

Названия и подсказки должны описывать capability (`GET-only`, WebSocket, unrestricted/full), а provider-specific инструкции должны загружаться из выбранного профиля и не быть обязательной частью основного мастера.

### P2. Документ завышает качество frontend-проверки

`svelte-check` действительно завершился с 0 errors, но сообщил **118 warnings в 25 файлах**. Формулировка walkthrough «0 errors» технически верна, однако для заявления о полной готовности следует явно фиксировать warning count и отделять существующие предупреждения от добавленных мастером.

## Что подтверждено проверками

Выполнено локально 2026-09-10:

1. `go test -count=1 -race ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/sys/procnet/... ./internal/serverwizard/... ./internal/api/... ./cmd/awg-manager` — **успешно**.
2. `GOOS=linux GOARCH=arm64 go build -o /tmp/awg-manager-wizard-audit ./cmd/awg-manager` — **успешно**.
3. `npm exec -- svelte-check --threshold error` — **0 errors, 118 warnings**.
4. `git diff --check` — новых whitespace errors не показал; присутствуют предупреждения о будущей нормализации CRLF/LF.

Эти результаты подтверждают компиляцию и текущие unit-тесты, но не опровергают production deadlock и семантические ошибки применения, потому что соответствующих интеграционных тестов нет.

## Обязательный план исправления

1. Устранить вложенный ingress-lock и добавить production-coordinator integration test с timeout.
2. Перенести финальную fingerprint-проверку внутрь той же транзакции/блокировки, которая начинает мутацию.
3. Удалить личный домен и все скрытые фиксированные настройки; обработать ошибки CSPRNG.
4. Ввести нормализованный `DesiredWizardConfig`; change plan и apply должны использовать один и тот же объект.
5. Реально реализовать матрицу Telegram scenario и Xray mode/CDN profile.
6. Реализовать типизированный egress resolver и фактическое применение direct/interface/SOCKS/router outbound.
7. Добавить readiness probes, rollback tests, cancellation tests и restart/recovery tests.
8. Усилить DTO validation и bounded JSON decoding.
9. Удалить provider-specific маркетинговые значения из универсального UI.
10. Повторить race tests, ARM64 build, frontend check/build и только затем проводить ручной тест на роутере без `--force-reinstall`.

## Минимальные acceptance tests перед следующим ревью

- `TestWizardApplyRealCoordinatorDoesNotDeadlock`.
- `TestWizardFingerprintRevalidatedUnderMutationLock`.
- Табличные тесты для всех Telegram scenarios: проверяется итоговая конфигурация и набор запущенных компонентов.
- Табличные тесты Xray `xhttp_get`, `ws` и поддерживаемых CDN profiles.
- Табличные тесты всех egress kinds, включая unavailable/degraded и drift после plan.
- Readiness failure -> успешный rollback -> job failed.
- Readiness failure + rollback failure -> `recovery_required`.
- Cancel before mutation, during cancellable apply и отказ cancel после реального point-of-no-return.
- Restart с незавершённой транзакцией и корректное восстановление/отображение состояния.
- Проверка отсутствия персональных доменов, IP и заранее заданных пользовательских credentials в backend/frontend defaults.

## Итог для агента

Не дорабатывать косметику поверх текущего apply-path. Сначала исправить модель блокировки и сделать один атомарный production contract «validate fingerprint + prepare/apply + readiness + commit/rollback». После этого связать UI choices с типизированным desired config. До выполнения P0 установка на роутер для проверки мастера нецелесообразна.
