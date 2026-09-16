# Ревью плана исправления First-Run Setup Wizards

Дата: 2026-09-10  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Основание: `FIRST_RUN_WIZARDS_WALKTHROUGH_AUDIT_2026-09-10.md`

## Вердикт

**Условно одобрено после обязательной правки архитектуры транзакции. В текущем виде план запускать целиком не следует.**

План правильно признаёт deadlock, неатомарную fingerprint-проверку, hardcoded-параметры, фиктивный readiness и игнорирование выбранного egress. Однако предложенные `Apply*Locked` не обеспечивают заявленную границу `prepare -> apply -> readiness -> commit/rollback`, а некоторые новые решения создают дополнительные проблемы с секретами и API-семантикой.

## Что исправлено в плане правильно

- Прямо зафиксирована причина production deadlock.
- Введена идея одного нормализованного desired config для plan и apply.
- Удаляются персональный домен и фиксированный `ya.ru`.
- Preflight привязывается к выбранным сценарию и egress.
- Убирается ложная `Available: true` для Mihomo `:1099`.
- Добавляются реальные readiness probes и тест с настоящим coordinator.
- Предусмотрены bounded JSON, strict decoding и neutral CDN copy.
- Verification matrix существенно лучше существующих mock-only тестов.

## Обязательные изменения до начала реализации

### P0. Нужен transaction API, а не публичные `Apply*Locked`

Пункты 70-80 и схема 56-64 предлагают вызвать `ApplyTelegramConfigLocked`/`ApplyXrayConfigLocked`, затем выполнить readiness и при ошибке отдельно вызвать `Rollback(journal, ...)`.

Проблема: существующий `applyLocked()` сам проходит подготовку/фиксацию/очистку journal. Когда он вернулся успешно, транзакция уже может быть завершена, а snapshot удалён. Readiness, выполняемый после такого возврата, находится **за пределами** настоящей transaction boundary. Публичная передача внутреннего `TransactionJournal` наружу также раскрывает детали coordinator и позволяет ошибочно использовать locked API без lock.

Нужно выбрать один из двух безопасных вариантов:

1. Предпочтительно: один публичный метод coordinator уровня операции:
   `ExecuteWizardTransaction(ctx, txID, expectedFingerprint, desired, readinessFn)`.
   Coordinator сам удерживает lock, повторно вычисляет fingerprint, создаёт snapshots/journal, применяет candidate, вызывает readiness callback и только затем commit/cleanup. При ошибке он сам делает rollback/recovery transition.
2. Либо явный объект транзакции с непубличными internals:
   `Begin -> ApplyCandidate -> Verify -> Commit`, `Rollback`, причём объект невозможно создать без удерживаемой coordinator-блокировки.

Не экспортировать наружу `TransactionJournal` и не делать общедоступные методы с суффиксом `Locked`, контракт которых нельзя проверить системой типов.

### P0. Секреты и UUID нельзя хранить в `DesiredWizardConfig`/PlanStore

Пункт 88 включает `Secret`, пункт 89 — `ClientUUID`, а пункт 121 сохраняет весь `DesiredWizardConfig` в PlanStore на 10 минут. Это противоречит изоляции credential material.

Нормализованный plan должен хранить только несекретные намерения. Secret и UUID необходимо создавать с проверкой ошибки CSPRNG **внутри apply transaction**, непосредственно перед подготовкой candidate config. После успешного commit они помещаются только в transient reveal store. При rollback временные значения удаляются. В API plan/status/log они не попадают.

Если UUID уже является частью существующей конфигурации, наружу всё равно не должен возвращаться полный stored desired record без redaction.

### P0. Семантика stale plan и HTTP 409 сейчас противоречива

Согласно схеме, fingerprint перепроверяется внутри асинхронной job. Но HTTP `POST /apply` к этому моменту уже вернул `202 Accepted` с `job_id`, поэтому вернуть из job `409 Conflict` невозможно.

Нужно явно выбрать контракт:

- рекомендуемый: `/apply` возвращает 202, а job завершается `failed` с `error_code=PLAN_STALE`; UI автоматически возвращает пользователя к пересозданию plan;
- либо синхронное резервирование transaction slot с fingerprint validation до 202. Простая синхронная проверка с последующим отпусканием lock снова создаст TOCTOU и не подходит.

Acceptance test должен проверять выбранную реальную семантику, а не формулировку «409 внутри job».

### P0. `Applying` не должен автоматически означать point of no return

Пункты 59 и 130 запрещают отмену сразу при переходе в `Applying`. Тогда пользователь фактически сможет отменить только задачу, ожидающую lock, а заявленная cancellation/rollback модель исчезает.

Корректнее:

- `pending/preparing`: отмена без rollback;
- `applying/verifying`: отмена запрашивается через context и приводит к rollback candidate;
- `committing`: отмена запрещена;
- `rolling_back`: повторный cancel идемпотентен.

Настоящая точка невозврата определяется coordinator непосредственно перед необратимым commit, а не UI-фазой до начала мутации.

### P0. Egress должен иметь исполнимый контракт

`ResolvedEgress{ID, Kind, Target}` недостаточно. Telegram и Xray используют разные механизмы выхода: имя интерфейса нельзя подставить вместо SOCKS endpoint, а `direct` нельзя записывать как device. План должен описать materialization для каждого сочетания server/egress:

| Kind | Telegram | Xray |
|---|---|---|
| direct | пустой/default dialer | direct outbound |
| interface | bind/routing через реальный iface | freedom outbound с корректным bind/interface strategy |
| socks | поддерживаемый SOCKS dialer либо blocked | SOCKS outbound host+port |
| router_outbound/group | явный bridge/export endpoint | явный bridge/export endpoint |

Если компонент технически не поддерживает kind, capability обязан пометить сочетание unsupported и preflight должен блокировать apply. Нельзя обещать все egress kinds до реализации соответствующего runtime adapter и его cleanup/rollback.

### P0. Матрица mode/profile должна преобразовываться в конкретную конфигурацию

План называет `xhttp_get`, `ws`, `cdn_get`, `cdn_ws`, но не задаёт таблицу преобразования в поля Xray, dispatcher и Telegram proxy. До кодинга нужна явная compatibility matrix:

- допустимые пары mode/profile;
- требуемые компоненты и порты;
- HTTP methods и transport settings;
- dispatcher routes;
- direct/CDN prerequisites;
- генерируемые клиентские ссылки;
- unsupported комбинации.

Без этого новый `DesiredWizardConfig` снова рискует стать описательной структурой, а apply — набором hardcoded значений.

## Важные дополнения

### P1. Добавить restart/recovery contract и тест

План пропустил обязательный тест из аудита: перезапуск AWG Manager во время `applying`, `verifying` и `committing`. JobRunner остаётся in-memory. Не обязательно делать все jobs durable, но после старта сервис должен:

- восстановить/откатить coordinator transaction из durable journal;
- не показывать старую job как успешно завершённую;
- сообщить recovery state и понятное дальнейшее действие;
- гарантированно удалить transient secret после неуспешной операции.

Добавить `TestWizardRestartDuringTransactionRecoversConsistently`.

### P1. Strict JSON trailing-data check указан неверно

Пункт 146 предлагает проверять trailing JSON через `!dec.More()`. `Decoder.More()` предназначен для массивов/объектов и не является корректной проверкой второго top-level JSON value.

После первого `Decode(&dst)` нужно выполнить второй `Decode(&struct{}{})` и потребовать `io.EOF`, либо использовать общий проверенный helper. Одновременно ограничить body через `MaxBytesReader`, вызвать `DisallowUnknownFields` и валидировать Content-Type при принятом проектом контракте.

### P1. Не доверять `X-Forwarded-*` без trusted proxy policy

Пункт 147 разрешает использовать `X-Forwarded-Proto`/`X-Forwarded-Host`. На роутере клиент может отправить эти заголовки напрямую. Их можно учитывать только если запрос пришёл от настроенного доверенного reverse proxy. В обычной схеме ожидаемую схему брать из `r.TLS`/локальной конфигурации, host — из нормализованного `r.Host`.

### P1. Live availability не должна выполнять дорогой procfs scan при каждом render без кэша

Пункт 98 должен определить probe dependency, timeout, caching/generation и поведение при ошибке. Ошибка проверки — `Available=false`/degraded, а не optimistic true. Для SOCKS полезна проверка listener ownership, а не только занятости порта произвольным процессом.

### P1. Domain validation следует разделить по назначению

Один RFC 1123 validator недостаточен для:

- публичного CDN hostname;
- прямого адреса, где может быть IPv4/IPv6/DDNS;
- Fake-TLS SNI, где IP обычно не подходит;
- IDNA-домена.

Необязательно запрещать корректную завершающую точку — её можно нормализовать. Placeholder `google.com`/`cloudflare.com` снова создаёт vendor-specific default; лучше `front.example.com` с пояснением, что пользователь должен указать допустимый SNI согласно своей конфигурации.

### P1. Telegram scenario `cdn_http` не должен неявно требовать raw telemt без пояснения

Пункт 122 утверждает, что CDN-only сценарий включает `tproxy-server + raw telemt`. Это допустимо лишь если raw telemt является обязательным внутренним backend web proxy. Тогда UI/plan должен называть его внутренним компонентом, не выдавать direct credentials и не открывать direct listener наружу. Нужен тест сетевой экспозиции, а не только проверка config fields.

### P2. Не включать установку на роутер в критерий завершения кодового плана

Пункты 210-213 правильно запрещают `--force-reinstall`, но `--force-downgrade --force-overwrite` тоже являются принудительными режимами и не должны быть универсальной командой проверки. Сначала собрать IPK и сообщить путь; установку выполнять только по отдельному запросу пользователя и после проверки фактической версии/архитектуры.

## Исправленный порядок реализации

1. Спроектировать coordinator-owned transaction API и тест реального lock без deadlock.
2. Зафиксировать mode/profile/scenario/egress compatibility matrix.
3. Ввести secret-free normalized desired model и строгую validation.
4. Реализовать typed egress resolution и materialization только для подтверждённых сочетаний.
5. Строить plan и candidate configs из одного desired model; добавить equivalence tests.
6. Реализовать transaction phases, readiness до commit, rollback/recovery и cancel semantics.
7. Исправить API decoding, origin policy и async stale error contract.
8. Подключить frontend, убрать персональные/provider-specific defaults.
9. Добавить integration, failure-injection, restart/recovery и UI tests.
10. Выполнить race suite, ARM64 build, frontend check/build; IPK не собирать без отдельного запроса.

## Критерий разрешения на запуск плана

План можно отдавать агенту после внесения в него четырёх явных решений:

1. coordinator-owned transaction остаётся открытой до окончания readiness;
2. secret/UUID не входят в сохранённый desired plan;
3. stale после `202 Accepted` выражается через job error code либо применяется настоящее атомарное reservation;
4. опубликована исполнимая compatibility/materialization matrix для scenario/mode/profile/egress.

Без этих изменений агент может устранить текущий deadlock, но оставить ложную атомарность и получить конфигурацию, которую невозможно корректно откатить.
