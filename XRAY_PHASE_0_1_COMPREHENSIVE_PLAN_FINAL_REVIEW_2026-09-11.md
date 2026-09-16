# Финальное ревью комплексного плана Phase 0/1: Xray Full Management

Дата: 2026-09-11  
Проект: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
SHA-256 проверенной редакции: `262B294492643EB87928394A423C30D18E105D6ABE811789AD9AB88CB8FFBB03`

## Вердикт

Эта редакция действительно переработана и закрывает основную часть предыдущих архитектурных замечаний. В ней появились:

- единый orchestration owner;
- направленные зависимости без import cycle;
- SecretStore и границы private/redacted DTO;
- version-aware capabilities;
- многоуровневый lossless overlay;
- durable transaction state machine;
- правильный порядок `Activate -> Readiness -> Commit`;
- rollback readiness и `recovery_required`;
- однонаправленная legacy migration;
- `UseNumber()` в semantic diff;
- API и UI состояния транзакции.

План близок к готовности. Его можно одобрить после внесения перечисленных ниже обязательных уточнений. Они существенно меньше предыдущего набора и не требуют очередной полной переработки архитектуры.

Статус: **условно одобрен после закрытия P0-пунктов 1–6**.

## P0 — исправить до передачи в реализацию

### 1. Удалить публичный `Checksum` из `SecretRef`

Предложенный тип содержит:

```go
Checksum string `json:"checksum,omitempty"`
```

Обычный SHA-хеш пароля, UUID или другого низкоэнтропийного секрета создаёт offline oracle и позволяет проверять варианты перебором. Такой checksum нельзя возвращать в публичной managed model, API, export или UI.

Требуемое решение:

- убрать `Checksum` из публичного `SecretRef`;
- для обнаружения версии использовать случайный opaque version ID;
- если проверка целостности необходима внутри SecretStore — хранить keyed HMAC только в приватном metadata-файле, недоступном API;
- добавить тест, что hash/HMAC секрета отсутствует во всех redacted DTO.

### 2. Исправить каталог SecretStore

В плане указан:

```text
/opt/etc/xray/secrets
```

Это выходит за существующий namespace AWG Manager и усложняет package lifecycle, backup и удаление. Использовать нужно единый управляемый каталог:

```text
/opt/etc/awg-manager/xray/secrets
```

Либо существующий canonical data root, если он уже определён кодом. Путь должен поступать из одного storage layout/config helper, а не быть захардкожен в нескольких пакетах.

### 3. Добавить хранилище профилей и schema migrations

В текущей редакции снова отсутствует конкретный ProfileStore. Есть `ManagedConfig`, но не определено, где и как хранятся:

- индекс профилей;
- desired managed model;
- opaque/base document;
- active profile;
- schema version;
- history metadata.

Также отсутствует registry миграций `schema N -> N+1`, хотя versioned model была обязательной частью Phase 1.

Добавить в план:

```text
internal/xrayserver/profile_store.go
internal/xrayconfig/migrations.go
```

Обязательные свойства:

- атомарная запись и `fsync`;
- файлы `0600`, каталоги `0700`;
- неизвестная будущая schema version открывается только read-only и не перезаписывается;
- миграции последовательны и идемпотентны;
- legacy import имеет durable marker и не повторяется после успешного commit;
- managed model и opaque document сохраняются согласованно в одной транзакции.

### 4. Уточнить recovery для ранних состояний и secret rotation

Для `prepared`, `tested` и `backup_ready` план говорит «удаление транзакции». Этого недостаточно, если в staging уже созданы новые версии секретов или изменены profile metadata.

Для каждого раннего состояния recovery обязан:

1. Подтвердить, что active config не изменён, сверив записанный hash.
2. Отменить staged secret rotation.
3. Удалить только transaction-owned temporary secrets.
4. Не удалять секреты, на которые ссылается active profile.
5. Вернуть profile state в исходное значение.
6. Атомарно отметить транзакцию `aborted`/`rolled_back` перед последующей очисткой.

Нельзя сначала удалить каталог транзакции, не сохранив durable terminal state/history.

### 5. Унифицировать имена состояний state machine

Mermaid использует `Activated`, таблица — `active_replaced`, а pipeline также использует `active_replaced`. Это создаст расхождение между enum, manifest, API и recovery switch.

В плане должен быть один canonical enum, например:

```text
prepared
tested
backup_ready
active_replaced
restarting
verifying
committed
rolling_back
rolled_back
recovery_required
aborted
```

Все диаграммы, таблицы, JSON API и тесты должны использовать именно эти значения. Неизвестное состояние manifest должно безопасно переходить в `recovery_required`, а не удаляться.

### 6. Дополнить API обязательными import/export/profile операциями

Сейчас перечислены validate, preview, apply, status, recovery и history, но отсутствуют операции, необходимые самому Parser/ProfileStore:

- безопасный import/parse существующего Xray JSON;
- получение списка профилей;
- чтение redacted профиля;
- создание/переименование/удаление профиля;
- redacted export по умолчанию;
- полный private export только с отдельным подтверждением и усиленной авторизацией;
- выбор active profile.

Минимально добавить:

```text
POST   /servers/xray/profiles/import
GET    /servers/xray/profiles
POST   /servers/xray/profiles
GET    /servers/xray/profiles/{id}
PATCH  /servers/xray/profiles/{id}
DELETE /servers/xray/profiles/{id}
GET    /servers/xray/profiles/{id}/export
POST   /servers/xray/profiles/{id}/activate
```

Идентификаторы должны проходить строгую проверку; endpoints должны быть аутентифицированы, защищены от CSRF и не принимать filesystem paths от клиента.

## P1 — включить в ту же реализацию

### 7. В перечне файлов отсутствуют API и frontend изменения

Раздел 1.10 обещает API/UI, но раздел 2 перечисляет только `xrayconfig` и `xrayserver`. Агент может реализовать backend-библиотеку и снова объявить работу завершённой без рабочих endpoint/UI.

Добавить конкретные файлы:

```text
internal/api/xray_profiles.go
internal/api/xray_profiles_test.go
internal/server/server_routes.go
internal/openapi/swagger.yaml
frontend/src/lib/api/clientXray.ts
frontend/src/lib/types/xray.ts
frontend/src/lib/components/servers/... или отдельный profile UI
```

Точные имена можно адаптировать к существующей архитектуре, но deliverables должны быть перечислены явно.

### 8. `recovery/resolve` нельзя оставлять абстрактной командой

`POST /servers/xray/recovery/resolve` потенциально разрушителен. Нужно определить строго типизированные действия:

```text
inspect
retry_rollback
accept_active_after_readiness
restore_selected_snapshot
```

Каждое изменяющее действие требует preview, expected transaction ID/hash, явного подтверждения, audit event и повторной readiness. Нельзя предоставлять endpoint, который просто сбрасывает `recovery_required`.

### 9. Capability matrix должна подтверждаться бинарником, а не только порогом версии

Тезис «SS2022 доступен начиная с Xray v1.6.0+» необходимо подтвердить реальными тестами используемых сборок. У Xray встречаются разные схемы версии и build variants; одного semver comparison недостаточно.

Рекомендуемый порядок:

1. Распознать конкретные известные версии/build IDs.
2. Применить консервативную таблицу возможностей.
3. Обязательно подтвердить candidate через `xray run -test`.
4. Для неизвестной версии скрыть неподтверждённые формы, но разрешить expert raw import с предупреждением и обязательным binary test.

### 10. Таймауты нельзя фиксировать одной константой 5 секунд

На роутере Xray может проверять или запускать конфигурацию дольше из-за нагрузки, DNS и большого числа правил. Таймауты должны быть отдельными ограниченными настройками:

- binary test timeout;
- process start timeout;
- stabilization window;
- readiness timeout;
- rollback readiness timeout.

Нужны разумные defaults и верхние границы. Timeout не должен оставлять дочерний процесс или зависшую транзакцию.

### 11. Lossless overlay требует точного представления field presence

План описывает tri-state, но перечисленные Go-типы всё ещё используют обычные строки, bool и int, которые не различают:

- поле отсутствует;
- поле задано нулевым значением;
- поле должно быть удалено.

Нужно выбрать конкретное представление:

- pointer/optional wrapper для управляемых полей;
- patch DTO с операциями set/delete;
- отдельная presence map.

Не следует использовать `null` как удаление прямо в основной модели, если `null` может иметь допустимую семантику Xray.

### 12. Readiness должна учитывать все ожидаемые listeners

План говорит о единственном `listenAddr:listenPort`, тогда как полная модель поддерживает несколько inbounds. Readiness должна получить набор ожидаемых listeners из compiled candidate и проверить каждый обязательный listener.

Если listener сознательно недоступен локальной проверке, это должно быть явно отражено в capability/readiness plan, а не молча считаться успехом.

## Уточнённый порядок выполнения

1. Удалить checksum из публичного SecretRef и утвердить private integrity metadata.
2. Зафиксировать canonical storage root.
3. Реализовать ProfileStore и schema migration registry.
4. Зафиксировать canonical transaction enum и invariants.
5. Дополнить recovery ранних состояний и staged secrets.
6. Уточнить optional/presence overlay representation.
7. Реализовать автономные Parser/Compiler/Validator/Diff/Redactor.
8. Реализовать SecretStore и BinaryInspector.
9. Реализовать legacy migration только в сторону ManagedConfig.
10. Интегрировать coordinator state machine, apply, multi-listener readiness и rollback.
11. Реализовать profile/import/export/API endpoints и OpenAPI contract.
12. Реализовать UI и блокирующие recovery states.
13. Выполнить unit, race, failure-injection, real-binary и frontend tests.

## Acceptance criteria для окончательного закрытия плана

- публичный SecretRef не содержит checksum или производных секрета;
- все данные находятся под canonical AWG Manager storage root;
- profile storage и schema migrations являются crash-safe;
- canonical transaction enum одинаков в manifest, API, UI и recovery;
- recovery корректно обрабатывает каждое состояние и staged secrets;
- invalid/unknown state не приводит к удалению backup;
- capabilities зависят от подтверждённой версии/build и дополняются binary test;
- Parser/Compiler обеспечивают семантический lossless round-trip;
- field presence/delete semantics покрыты тестами;
- apply проверяет все обязательные listeners;
- import/export/profile endpoints реализованы и документированы;
- private export требует отдельного подтверждения и не попадает в журналы;
- `recovery_required` нельзя снять без подтверждённого восстановления либо принятия проверенного active runtime;
- все перечисленные компоненты реально используются production API/runtime, а не только unit-тестами.

## Итоговое решение

План значительно улучшен и больше не требует полной архитектурной переработки. После внесения P0-пунктов 1–6 его можно запускать в реализацию. P1-пункты должны остаться обязательной частью того же этапа и проверяться по acceptance criteria, чтобы результатом снова не оказался изолированный прототип без рабочего API и интерфейса.

