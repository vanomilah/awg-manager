# Полноценное управление Xray в AWG Manager — план реализации

Статус: **готов к запуску в работу после подтверждения владельцем**  
Рабочая директория: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`

## 1. Цель

Превратить текущий узкий мастер Xray VLESS/CDN в полноценную, понятную систему управления Xray, сопоставимую по качеству интерфейса с разделами Sing-box и Mihomo.

Пользователь должен однозначно видеть три независимые роли Xray:

1. **Xray-сервер** — принимает подключения внешних клиентов.
2. **Xray-прокси и туннели** — импортирует внешние конфигурации и предоставляет outbounds другим подсистемам AWG Manager.
3. **Xray как ядро маршрутизации** — опционально перехватывает трафик устройств и применяет правила вместо Sing-box/Mihomo.

Текущий мастер первого запуска сохраняется, но становится только кнопкой **«Быстрая настройка»**, а не главным и единственным интерфейсом.

## 2. Жёсткие ограничения

- Не выполнять и не генерировать `opkg install --force-reinstall`.
- Не выполнять `/opt/bin/awg-manager --cleanup`.
- Не собирать IPK и не выполнять деплой без отдельной команды пользователя.
- Не менять `E:\AWGM\awg-manager-mihomo`.
- Не нарушать работу `Wireguard2:51820`, Mihomo mixed inbound `:1099`, Sing-box и существующего Telegram Proxy.
- Все изменения конфигурации проходят validate → plan/diff → atomic apply → readiness → commit/rollback.
- Секреты, private keys, UUID и пароли не попадают в журналы, telemetry, уведомления и диагностические snapshots.
- Старые управляемые Xray-конфигурации мигрируются без потери клиентов и ссылок.
- Не обещать поддержку опции Xray, пока она не подтверждена установленной версией ядра через capability/version matrix.

## 3. Проблемы текущей реализации

### P0: оба мастера падают при открытии

Ошибка:

```text
Cannot read properties of undefined (reading 'egress_options')
```

Причина должна быть установлена по HTTP-ответу capabilities, но UI обязан корректно переживать `undefined`, старую схему ответа и ошибку загрузки.

### Текущая модель слишком узкая

`internal/xrayserver.Config` хранит только один VLESS inbound, два транспорта (`xhttp`, `ws`), один простой egress и список клиентов. Она не способна выразить полноценный Xray config с `log`, `api`, `dns`, `routing`, `policy`, `inbounds`, `outbounds`, `stats`, `metrics`, `observatory` и другими модулями.

### Роли сервера, клиента и маршрутизатора смешаны

Карточка «Xray VLESS · CDN» выглядит как весь Xray, хотя фактически управляет одним серверным шаблоном. Импорт внешнего outbound и выбор Xray как движка маршрутизации отсутствуют.

## 4. Целевая информационная архитектура

### Раздел «Серверы → Xray»

Экран содержит:

- статус бинарника и процесса;
- активный серверный профиль;
- inbounds и клиенты;
- статистику пользователей/inbounds;
- кнопки «Быстрая настройка», «Простой», «Эксперт», «JSON»;
- журнал Xray, проверку конфигурации и историю применений.

### Раздел «Прокси»

При создании прокси/подписки появляется выбор ядра:

- Sing-box;
- Mihomo;
- Xray.

Карточки Xray имеют ту же визуальную систему, тесты, изменение, статистику и метку ядра, что существующие карточки Sing-box/Mihomo.

### Раздел «Маршрутизация»

В drawer выбора движка добавляется Xray. Одновременно активным прозрачным маршрутизатором может быть только одно ядро. Server-only процесс Xray не должен ошибочно считаться активным routing engine.

## 5. Конфигурационная архитектура backend

### 5.1 Разделить desired model и скомпилированный Xray JSON

Создать пакет `internal/xrayconfig`:

- `Document` — lossless JSON document с сохранением неизвестных полей;
- `ManagedModel` — типизированные сущности AWG Manager;
- `Compiler` — ManagedModel → Xray JSON;
- `Parser` — Xray JSON → ManagedModel + opaque fields;
- `Validator` — структурная и семантическая проверка;
- `Capabilities` — поддержка полей с учётом версии Xray;
- `Redactor` — маскирование секретов;
- `Diff` — семантический diff без утечки секретов.

Нельзя сериализовать экспертный конфиг через узкую Go-структуру с потерей неизвестных полей. Для расширяемых блоков применять `json.RawMessage`/lossless map и каноническое представление.

### 5.2 Хранение

Предлагаемая структура:

```text
/opt/etc/awg-manager/xray/
  profiles/index.json
  profiles/<profile-id>/managed.json
  profiles/<profile-id>/expert.json
  profiles/<profile-id>/compiled.json
  profiles/<profile-id>/metadata.json
  history/<transaction-id>/...
  secrets/<secret-id>               # 0600
  runtime/pid.json
  runtime/active-profile.json
```

Каталоги `0700`, файлы с конфигурацией/секретами `0600`. Экспорт по умолчанию redacted; полный экспорт требует явного подтверждения.

### 5.3 Сущности

- `XrayProfile`: id, name, role (`server`, `client`, `router`, `combined`), enabled, schema version.
- `Inbound`: tag, listen, port, protocol, settings, stream settings, sniffing, allocate.
- `Outbound`: tag, protocol, server/account, stream settings, mux, proxy settings, send-through.
- `RoutingRule`: domain/IP/port/network/protocol/inbound/user/process/attrs и target outbound/balancer.
- `Balancer`, `Observatory`, `DNS`, `Policy`, `Log`, `API`, `Stats`, `Metrics`.
- `Client`: stable AWGM ID, protocol credentials, email/statistics key, level, enabled, share metadata.
- `SecretRef`: ссылка на защищённое хранилище вместо секрета в публичной модели.

## 6. Фазы реализации

## Phase 0 — срочное восстановление текущего UI

1. Исправить capabilities contract для Xray и Telegram wizard.
2. В TypeScript задать безопасные defaults:
   - `egress_options ?? []`;
   - `profiles ?? []`;
   - `modes ?? []`;
   - `scenarios ?? []`.
3. До успешной загрузки capabilities показывать skeleton/loading, а при ошибке — содержательное сообщение с HTTP status/request ID и кнопкой повторения.
4. Запретить переход дальше, если обязательные capabilities не получены.
5. Исправить контраст/disabled-состояния мастеров, видимые на скриншотах.
6. Добавить schema/API tests на старый, новый, пустой и ошибочный ответ.

Критерий: оба мастера открываются без JS exception и либо работают, либо показывают диагностируемую ошибку.

## Phase 1 — безопасное ядро конфигурации

1. Реализовать `internal/xrayconfig` и versioned schemas.
2. Импортировать текущий `xrayserver.Config` как управляемый server profile без изменения runtime-конфига.
3. Реализовать validation pipeline:
   - schema validation;
   - уникальность tags/ports;
   - ссылки routing → существующие outbound/balancer;
   - совместимость protocol/transport/security;
   - обязательные поля TLS/REALITY;
   - loop/port collision detection;
   - запуск установленного Xray в test mode на candidate-файле.
4. Реализовать redacted semantic diff.
5. Расширить существующий coordinator transaction для профилей и compiled config, не создавать второй конкурирующий transaction manager.
6. Сохранять неизвестные JSON-поля round-trip.

Критерий: импорт → preview → export не теряет поля; неверный конфиг никогда не заменяет активный.

## Phase 2 — сервер Xray, режим «Простой»

### Основные поля

- название профиля;
- bind address и port;
- protocol: сначала VLESS, затем VMess/Trojan/Shadowsocks отдельными capability-gated итерациями;
- transport: `raw`, `xhttp`, `grpc`, `websocket`, `httpupgrade`, `mkcp`, `hysteria` согласно версии ядра;
- security: `none`, `tls`, `reality` только в совместимых сочетаниях;
- внешний host/port, CDN on/off, dispatcher path;
- egress: direct, выбранный AWGM tunnel/proxy, Xray outbound;
- sniffing и базовые socket options;
- logging level.

### Динамические формы

- XHTTP: mode, path, host, xmux и upload/download параметры, доступные установленной версии;
- WS: path, host, headers, early data;
- gRPC: service name, authority, multi mode;
- RAW: header/type и socket options;
- TLS: certificates, SNI/ALPN, min/max version;
- REALITY: target, server names, private/public key workflow, short IDs, fingerprint; предупреждения о небезопасном target;
- Hysteria transport показывается только при совместимом protocol/security.

Формы строятся из внутреннего schema descriptor, а не из россыпи несогласованных `if` в Svelte.

## Phase 3 — клиенты, ссылки и статистика

1. CRUD клиентов с редактированием remark/email/level/flow/лимитов и ротацией credentials.
2. Генерация URI, QR и конфигураций для совместимых клиентов; показывать несовместимость формата явно.
3. Импорт клиента из URI/JSON.
4. Включить Xray `stats` + `policy` и loopback-only API с `StatsService`.
5. Статистика:
   - online/offline;
   - last seen;
   - uplink/downlink/total;
   - текущая скорость;
   - inbound/outbound totals;
   - графики 5 минут/час/сутки;
   - reset counters с подтверждением.
6. Для пользовательской статистики каждому клиенту назначать стабильный уникальный `email`, не содержащий секретов.
7. API слушает только loopback; не публиковать gRPC API в LAN/WAN.

Критерий: трафик двух клиентов отражается раздельно, после рестарта identity клиентов сохраняется.

## Phase 4 — Xray как клиент: прокси, подписки и туннели

1. Импорт:
   - одиночные URI;
   - JSON fragments/full config;
   - subscription URL с ручным обновлением и расписанием;
   - clipboard/file upload без записи секретов в logs.
2. Поддерживаемые outbounds вводить поэтапно и capability-gated: VLESS, VMess, Trojan, Shadowsocks, SOCKS, HTTP, WireGuard и другие реально поддержанные установленным ядром.
3. Нормализовать отображаемое имя из remark/fragment/server name; не показывать технические `sub-<hash>` при наличии имени.
4. Создавать Xray proxy group abstraction для selector/url-test/fallback/load-balance через routing/balancer/observatory, не копируя Clash-семантику там, где Xray её не имеет.
5. Каждый outbound публиковать в общий AWGM Egress Catalog со stable ID `xray:<profile>:<tag>`.
6. Предоставлять локальный SOCKS/HTTP inbound или управляемый интерфейс для использования NDMS, HR Neo, WDTT, Sing-box и Mihomo.
7. Карточки должны поддерживать «Изменить», «Тест», «Удалить», delay, traffic и метку `Xray`.

Критерий: импортированный внешний VLESS outbound можно выбрать в политике AWGM и подтвердить его использование в соединениях/статистике.

## Phase 5 — экспертный конструктор

Создать интерфейс, аналогичный простому/экспертному режиму маршрутизации:

- список inbounds;
- список outbounds;
- routing rules с first-match визуализацией;
- balancers и observatory;
- DNS servers/hosts/rules;
- policy levels;
- stats/API/metrics;
- logs;
- advanced stream/sockopt;
- drag-and-drop порядка правил;
- поиск ссылок на tag перед удалением;
- предупреждения о недостижимых правилах, циклах и неиспользуемых объектах.

Любое изменение создаёт draft. Кнопки: «Проверить», «Показать diff», «Применить», «Отменить».

## Phase 6 — JSON-режим

1. Monaco/лёгкий редактор с JSON schema, completion и диагностикой.
2. Вкладки:
   - `Managed JSON`;
   - `Compiled JSON` read-only;
   - `Raw Xray JSON`.
3. Preview redacted diff.
4. `xray run -test -config <candidate>` перед применением.
5. Явное предупреждение при переходе из raw в managed, если часть полей не представима формами.
6. Никакого автоматического destructive normalization raw-конфига.

## Phase 7 — Xray как ядро маршрутизации

1. Добавить `Xray` в engine selector с раздельными статусами:
   - server process running;
   - proxy component running;
   - transparent routing active.
2. Реализовать TProxy/TUN capture через общий proxy runtime orchestration AWG Manager, а не отдельные несогласованные init scripts.
3. Поддержать policy source, whole-router mode, bypass private/LAN, DNS interception и IPv4/IPv6.
4. Правила AWG Manager компилировать в Xray routing, сохраняя first-match semantics.
5. Соединения отображать с client, destination, inbound, outbound, rule и traffic.
6. Переключение Sing-box/Mihomo/Xray — только транзакционно с readiness и автоматическим rollback до точки невозврата.
7. Server-only Xray продолжает работать, когда routing engine другой, если порты и ресурсы не конфликтуют.

Критерий: устройство в NDMS policy проходит через выбранный Xray outbound; UI Connections показывает фактический маршрут.

## Phase 8 — Telegram Proxy завершение продуктовой части

Чтобы Telegram не остался вторым недоделанным мастером:

- исправить общий capabilities bug в Phase 0;
- добавить отдельную статистику MTProto и Web Proxy;
- online clients, traffic, active connections, last activity;
- понятные режимы Direct / CDN / Dual;
- редактирование egress, ports, host/path и key rotation вне мастера;
- мастер оставить как «Быстрая настройка»;
- убрать компрометирующие/провайдер-специфичные формулировки, использовать нейтральное «совместимый CDN»;
- логи и readiness обоих каналов.

## 7. API-контракты

Добавить versioned endpoints, сохранив текущие до миграции frontend:

```text
GET    /api/xray/v2/capabilities
GET    /api/xray/v2/profiles
POST   /api/xray/v2/profiles
GET    /api/xray/v2/profiles/{id}
PATCH  /api/xray/v2/profiles/{id}
DELETE /api/xray/v2/profiles/{id}
POST   /api/xray/v2/profiles/{id}/validate
POST   /api/xray/v2/profiles/{id}/plan
POST   /api/xray/v2/plans/{id}/apply
GET    /api/xray/v2/jobs/{id}
POST   /api/xray/v2/jobs/{id}/cancel
GET    /api/xray/v2/profiles/{id}/compiled
GET    /api/xray/v2/profiles/{id}/stats
GET    /api/xray/v2/profiles/{id}/connections
POST   /api/xray/v2/import
POST   /api/xray/v2/subscriptions
POST   /api/xray/v2/subscriptions/{id}/refresh
GET    /api/xray/v2/history
POST   /api/xray/v2/history/{id}/restore
```

Требования ко всем mutation endpoints:

- CSRF/auth guard;
- request ID;
- optimistic revision/fingerprint;
- typed error codes;
- redacted audit event;
- запрет параллельного apply;
- ограничения размера JSON/subscription и timeout;
- SSRF-защита subscription fetcher с явной политикой маршрута.

## 8. UI/UX требования

- Единый дизайн с Sing-box/Mihomo, без отдельного «приложения внутри приложения».
- На первом экране показывать полезное состояние, а не все поля сразу.
- Basic/Expert/JSON переключаются без потери draft.
- Зависимые поля появляются динамически и имеют пояснения.
- Unsupported опции скрыты либо disabled с указанием требуемой версии Xray.
- Статусы процесса, server role и routing role не смешиваются.
- Ошибка API никогда не превращается в бесконечный spinner или JS exception.
- Мобильная ширина и клавиатурная навигация обязательны.
- Перед опасным применением показывать affected services, downtime и rollback policy.

## 9. Тестовая стратегия

### Backend

- unit tests parser/compiler/validator/redactor/diff;
- golden tests compiled Xray JSON по версиям;
- round-trip тесты неизвестных полей;
- protocol/transport/security compatibility matrix;
- transaction fault injection на каждой durable boundary;
- concurrency/race/cancel/recovery;
- stats parser/API unavailable/restart cases;
- SSRF и limits для subscriptions/import;
- process ownership PID/start-time/binary fingerprint;
- миграция текущего Xray server без изменения результата.

### Frontend

- schema parsing с отсутствующими полями;
- component tests всех динамических форм;
- draft preservation между режимами;
- API error/loading/retry;
- semantic diff redaction;
- playwright flows: quick setup, add client, import outbound, apply rule, rollback;
- responsive/accessibility checks.

### Проверки каждой фазы

```bash
go test -count=1 -race ./internal/xrayconfig/... ./internal/xrayserver/... ./internal/serveringress/... ./internal/serverwizard/... ./internal/api/...
cd frontend && npm run check
cd frontend && npm run test
git diff --check
```

ARM64 compile выполнять отдельно без создания IPK. Full repository tests могут запускаться после целевых тестов.

## 10. Порядок поставки

Каждая фаза должна завершаться самостоятельным review и рабочим UI:

1. Phase 0 — немедленный hotfix мастеров.
2. Phase 1 — конфигурационное ядро и миграция.
3. Phase 2–3 — полноценный server UX и статистика.
4. Phase 4 — клиентские outbounds/подписки и общий каталог.
5. Phase 5–6 — Expert/JSON.
6. Phase 7 — routing engine, только после стабилизации server/client ролей.
7. Phase 8 — продуктовая полировка Telegram Proxy.

Не объединять все фазы одним гигантским непроверяемым commit. После каждой фазы обновлять handoff/walkthrough с фактическими файлами, тестами, известными ограничениями и следующим шагом.

## 11. Definition of Done

Работа считается завершённой только когда:

- текущие мастера не падают и являются необязательным быстрым путём;
- Xray server настраивается в Basic, Expert и JSON режимах;
- сторонние Xray URI/JSON/subscriptions импортируются как outbounds;
- Xray outbounds доступны другим маршрутизаторам AWG Manager;
- Xray можно осознанно выбрать как transparent routing engine;
- клиенты, inbounds и outbounds имеют подтверждаемую статистику;
- Connections показывает фактический маршрут;
- unknown raw JSON fields не теряются;
- любое изменение проходит validation, preview/diff и безопасную транзакцию;
- секреты не утекли в API/logs/history;
- migration, rollback и recovery проверены fault-injection тестами;
- UI не содержит зависших spinner, необработанных Promise или JS exceptions;
- ARM64 daemon и API tests компилируются;
- целевые backend/frontend тесты проходят.

## 12. Первый рабочий пакет задач

Агенту следует начать только с:

1. воспроизведения `egress_options` ошибки через Network/API test;
2. Phase 0 hotfix и frontend regression tests;
3. ADR для `internal/xrayconfig`, ролей и lossless JSON;
4. инвентаризации текущих Xray endpoints/config migrations;
5. каркаса v2 capabilities/profile API без переключения runtime;
6. walkthrough Phase 0/1 foundation.

После этого можно переходить к compiler/validator. Нельзя начинать с рисования большого UI поверх старой узкой `xrayserver.Config`.
