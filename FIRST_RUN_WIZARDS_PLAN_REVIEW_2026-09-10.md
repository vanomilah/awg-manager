# Ревью плана Telegram Proxy и Xray First-Run Wizards

Дата: 2026-09-10  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

**Условно одобрено после доработки плана.** Направление и пользовательский flow верные, но начинать реализацию всего плана сейчас рискованно: несколько критических контрактов оставлены на усмотрение исполнителя. Сначала нужно внести пункты P0/P1 ниже, затем выполнять по этапам.

План покрывает только мастера первого запуска. Он **не закрывает** оставшуюся расширенную задачу со статистикой клиентов и Xray как самостоятельным клиентским ядром. Эти функции следует явно вынести в следующие этапы, чтобы мастер не воспринимался как завершение всего продукта.

## P0 — обязательно исправить до реализации

### 1. Fingerprint не определяет надёжную связь preview с состоянием системы

В плане сказано, что SHA-256 строится из полей плана, портов и timestamps. Этого недостаточно:

- timestamp делает неясным воспроизводимое сравнение;
- обычный hash, возвращённый клиенту вместе с данными, не защищает от изменения payload;
- не определено, хранит ли сервер исходный план;
- не определено, какие revision/state входят в снимок;
- проверка не выполняется под тем же lock, что и apply.

Нужен точный контракт:

1. `plan` создаёт server-side запись с random `PlanID`, `ExpiresAt`, нормализованным request и `StateFingerprint`.
2. `StateFingerprint` включает как минимум revision Xray/TG/dispatcher config, runtime decision, recovery flags, init-script identity/checksum, занятость выбранных портов и identity/status выбранного egress.
3. Клиент получает `PlanID`, а не присылает доверенный `ChangePlan` обратно.
4. `apply` под unified ingress lock загружает план, проверяет session ownership, TTL и повторно вычисляет state fingerprint.
5. При расхождении возвращает `409 PLAN_STALE` до первой мутации.
6. Plan одноразовый: повторный apply возвращает тот же job либо детерминированный conflict, но не запускает вторую операцию.

### 2. Для mutation endpoints отсутствует CSRF-контракт

`h.guarded` означает аутентификацию, но не заменяет CSRF. В существующем Telegram API уже используется отдельная динамическая CSRF-проверка.

Нужно явно записать:

- `preflight` и `plan` остаются read-only, но POST всё равно не должны менять состояние;
- `apply` и `cancel` требуют существующий session-bound CSRF token;
- нельзя создавать второй несовместимый CSRF-механизм;
- frontend использует существующий `requestWithCSRF`/общий механизм проекта;
- тесты: missing, invalid, чужая session и валидный token.

### 3. Секреты нельзя хранить и отдавать через общий `JobStatus.Result any`

Результат может содержать VLESS URL, UUID, Telegram secret и proxy links. Универсальный `Result any` в polling endpoint создаёт риск утечки и случайного логирования.

Нужно разделить:

- job status содержит только безопасное резюме и `ResultRef`;
- job принадлежит конкретной authenticated session/user;
- чужая session получает 404/403;
- секреты выдаются отдельным reveal/share endpoint после успешного job;
- ответы: `Cache-Control: no-store`, без отражения секрета в error/log/audit fields;
- reveal rate-limited и по возможности одноразовый/короткоживущий;
- после cleanup job секретный результат удаляется.

### 4. Не определена транзакционная граница apply и безопасная отмена

Фраза «using transactional coordinators and rollback helpers» недостаточна. Текущий `serveringress.Coordinator` решает ingress/migration задачи, но не является автоматически универсальной wizard-транзакцией.

Для Telegram и Xray нужно отдельно описать:

- порядок prepare → validate/test → commit → readiness → finalize;
- какие snapshot IDs сохраняются;
- компенсацию dispatcher, backend, init scripts и persistent config;
- момент, после которого cancel становится `cancelling`, но не прерывает атомарный commit;
- поведение при падении AWG Manager между фазами;
- recovery journal и восстановление job после рестарта процесса;
- запрет параллельного apply с обычным редактором карточки и migration saga.

In-memory job store допустим для UI polling, но durable mutation state обязан находиться в существующем journal/transaction слое. Нельзя полагаться только на память процесса.

## P1 — добавить в план до начала соответствующих этапов

### 5. CDN profile выбран в UI, но backend реализации в плане нет

`TelegramProxyWizard` и `XrayWizard` предлагают CDN profile, однако нет запланированного пакета capability profiles/adapters.

Добавить backend слой без брендов CDN:

- capability set: direct, GET-only, GET/POST, WebSocket, XHTTP/streaming;
- built-in generic profiles и custom profile;
- совместимость profile ↔ protocol/transport;
- read-only probe с timeout и понятными результатами;
- origin/DNS instructions как вычисляемый результат;
- отсутствие персональных доменов и провайдерских defaults.

### 6. `EgressOption` слишком беден для всех видов туннелей

Полей `{ID, Name, Iface, Type, Status, Available}` недостаточно. Не каждый выход является kernel interface: возможны SOCKS endpoint, router group и provider-backed outbound.

Нужны минимум:

- stable ID и owner/source;
- display name;
- kind (`interface`, `socks`, `router-outbound`, `direct`);
- resolved interface или proxy endpoint только во внутреннем backend DTO;
- TCP/UDP capabilities;
- availability/degraded reason;
- generation/revision для stale-plan проверки.

Использовать существующий `internal/routing.Catalog`, но добавить adapter, а не связывать `serverwizard` с конкретной реализацией каталога.

### 7. Preflight не должен дублировать небезопасный procfs parser

План предлагает новую проверку `/proc/net/tcp*`. Уже проверенный fail-closed parser находится в `internal/serveringress`, но сейчас не экспортирован.

Следует выделить общий read-only listener inspector с tri-state/error contract и использовать его и в migration saga, и в wizard preflight. Копирование parser создаст два разных safety-контракта.

### 8. Не описан путь установки отсутствующих компонентов

Capabilities только сообщает, установлен ли бинарник. Но цель мастера — довести новичка до рабочего подключения.

Нужно выбрать один контракт:

- мастер предлагает безопасно перейти к существующему installer UI; либо
- plan/apply включает отдельный подтверждаемый install step через существующие installer services.

Нельзя скачивать бинарник скрыто во время preflight. Для `xray`, `telemt` и `tproxy-server` должны быть отдельные owner/version/checksum/uninstall правила.

### 9. API DTO должны быть типизированы и ограничены

- заменить `Result any` на tagged union по `Kind`;
- определить request DTO каждого шага и строгую validation;
- ограничить размеры domain/path/client name;
- нормализовать hostname/IDN/path до fingerprint;
- определить стабильные error codes (`PLAN_STALE`, `RECOVERY_REQUIRED`, `PORT_CONFLICT`, `EGRESS_UNAVAILABLE`, `OPERATION_IN_PROGRESS`);
- добавить OpenAPI и сгенерированные frontend schemas в тот же этап.

### 10. Job lifecycle требует дополнительных состояний

Минимально нужны `preparing`, `applying`, `verifying`, `rolling_back`, `cancelling`, `recovery_required`, а не только общий `running`. UI иначе не сможет честно показать, можно ли отменять операцию и завершился ли rollback.

## Что план сознательно не покрывает

Следующие исходные требования вынести в отдельные планы после мастеров:

1. Статистика Xray по клиентам: active connections, last seen, upload/download, ошибки.
2. Раздельная статистика Telegram MTProto и Web Proxy/CDN.
3. Xray client mode: импорт сторонних VLESS/Xray ссылок и подписок.
4. Xray как selectable routing engine/outbound в общей маршрутизации AWG Manager.
5. Автоматическое создание локального туннеля: текущий `XrayAutoTunnelModal` фактически копирует URL и показывает инструкцию.

Их отсутствие не должно блокировать первый wizard milestone, но UI и walkthrough не должны заявлять, что весь Xray integration завершён.

## Рекомендуемый порядок реализации

1. Контракты DTO, PlanID/state snapshot, CSRF, session ownership и error codes.
2. Общий listener inspector и egress catalog adapter.
3. Только Xray wizard backend с синхронным apply в тестах и durable transaction/recovery.
4. Xray API и минимальный UI end-to-end.
5. Telegram wizard на тех же общих примитивах.
6. Async jobs/progress/cancel после доказанной транзакционной семантики.
7. CDN capability profiles и расширенные варианты входа.
8. OpenAPI, frontend tests и router E2E.

Такой порядок уменьшает риск построить большой UI поверх ещё незафиксированного apply-контракта.

## Критерий допуска плана к работе

План можно запускать после внесения P0 пунктов 1–4. P1 можно уточнять по этапам, но CDN profiles и egress adapter должны быть определены до реализации соответствующих экранов.

IPK не собирался. Код и роутеры не изменялись; создан только этот review-файл.
