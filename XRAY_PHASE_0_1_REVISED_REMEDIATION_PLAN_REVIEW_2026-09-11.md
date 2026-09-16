# Повторное ревью доработанного плана Phase 0/1: Xray Full Management

Дата: 2026-09-11  
Проект: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План заметно улучшен:

- устранено направление зависимостей, создававшее циклический импорт;
- описан многоуровневый overlay;
- добавлены Parser, Balancer и semantic JSON diff;
- конкретизирован базовый transactional apply;
- расширена валидация outbounds.

Тем не менее план пока нельзя считать полностью готовым к реализации. Остались блокирующие пробелы в durable-транзакциях, secret storage, capability detection и границе между legacy `xrayserver.Config` и полной моделью Xray.

Статус: **условно одобряемая архитектура после обязательной доработки пунктов P0 ниже**.

## P0 — обязательные исправления до начала реализации

### 1. `SecretRef` объявлен, но не подключён к модели и хранилищу

В плане добавляется тип `SecretRef`, однако credentials по-прежнему остаются строками:

- `Client.UUID`;
- `Outbound.UUID`;
- `Outbound.Password`;
- `Reality.PrivateKey`;
- subscription credentials и другие raw secrets.

Не описаны `SecretStore`, разрешение ссылок перед compile, права файлов, ротация и garbage collection. Простое наличие структуры `SecretRef` ничего не защищает.

Требуется добавить отдельный компонент:

```text
internal/xrayconfig/secrets.go       интерфейс SecretResolver
internal/xrayserver/secret_store.go  файловая реализация
```

Контракт должен включать:

1. Каталог `0700`, файлы `0600`.
2. Атомарную запись через временный файл на том же носителе, `fsync` файла и каталога.
3. Запрет path traversal и symlink following.
4. Ссылки на секреты в managed model вместо открытых значений.
5. Разрешение секретов только в приватный runtime candidate.
6. Redacted preview/export/history по умолчанию.
7. Commit/rollback ротации секретов вместе с конфигурационной транзакцией.
8. Удаление orphaned secrets только после подтверждённого commit.

### 2. Capability matrix всё ещё заменена фиксированным списком

Раздел называется `Shadowsocks Capability Matrix`, но фактически содержит статический перечень методов. Версия установленного Xray нигде не определяется и не участвует в решении.

Нужно добавить:

- `BinaryInspector` с определением версии и build metadata;
- нормализованную `Capabilities`;
- таблицу protocol/transport/security/method по поддержанным версиям;
- отказ для неизвестной или неподдержанной версии;
- единый источник capabilities для Validator, Compiler, API и UI;
- тесты минимум для текущей встроенной версии, предыдущей поддержанной и неизвестной версии.

Без этого план повторит исходную проблему: UI и validator обещают параметры, которые конкретный бинарник может не поддерживать.

### 3. Нужна durable state machine транзакции

Сканирование только манифестов `state == prepared` недостаточно. Падение возможно в любой точке:

```text
prepared
tested
backup_ready
active_replaced
restarting
verifying
committed
rolling_back
recovery_required
rolled_back
```

Для каждого состояния должно быть определено:

- какие файлы гарантированно существуют;
- можно ли продолжить commit;
- требуется ли rollback;
- что делать при повреждённом manifest;
- что делать, если rollback также завершился ошибкой.

Manifest необходимо атомарно и синхронно сохранять **до и после каждой точки невозврата**. При невозможности подтвердить восстановление система должна перейти в `recovery_required`, сохранить backup и запретить новые apply, а не удалять staging.

### 4. Порядок в Verification Plan противоречит алгоритму

План требует тестировать:

```text
PrepareCandidate -> Validate -> TestConfig -> Diff -> Commit -> Readiness -> Rollback
```

Но `Commit` должен происходить только после успешной readiness. До этого выполняется provisional activation/apply. Корректная последовательность:

```text
Prepare -> Validate -> BinaryTest -> Diff -> Backup -> ActivateCandidate
-> Restart -> Readiness -> Commit
```

При ошибке после `ActivateCandidate`:

```text
Rollback -> RestartPrevious -> RollbackReadiness -> RolledBack
```

Если rollback readiness не проходит — `recovery_required`.

### 5. Два mutex и файловый lock требуют строгого порядка владения

Фраза `Coordinator.txMu / Service.mu` не определяет, кто является владельцем транзакции. Захват нескольких блокировок без фиксированного порядка создаёт риск deadlock.

Нужно выбрать один orchestration owner. Рекомендуемый контракт:

1. `serveringress.Coordinator` владеет межсервисной транзакцией и file lock.
2. `xrayserver.Service` выполняет операции только по вызову coordinator.
3. Внутренний `Service.mu` защищает локальное состояние и никогда не удерживается при callback в coordinator.
4. Порядок locks документируется и проверяется concurrent-тестами.

### 6. Backup и atomic replacement описаны недостаточно безопасно

Один файл `config.json.bak` может быть перезаписан следующей транзакцией или остаться несогласованным после сбоя.

Нужно:

- хранить immutable backup внутри `transactions/<txID>/`;
- проверить SHA-256 исходного active config непосредственно перед activation;
- использовать temp + chmod + fsync + rename + fsync(parent dir);
- запретить symlink target;
- сохранять ownership/mode;
- проверять, что staging и active config находятся на одном filesystem;
- не удалять backup до durable commit и завершения history.

### 7. Readiness только по слушающему порту недостаточна

Процесс может занять порт, но работать с неправильным конфигом либо не обслуживать нужный inbound. `ListenerOwnershipProbe` должен быть только первым уровнем.

Минимальная readiness:

1. PID принадлежит ожидаемому бинарнику и instance identity.
2. Процесс не завершился в течение stabilization window.
3. Все ожидаемые listeners принадлежат нужному процессу.
4. При включённом loopback API проходит безопасный status/stats probe.
5. Для server profile выполняется локальная protocol-aware проверка там, где она возможна.
6. Таймауты настраиваемые: фиксированные 5 секунд на роутере могут давать ложный rollback.

### 8. Legacy-адаптер не должен ограничивать полную модель

`FromManagedConfig(m) (xrayserver.Config, error)` опасен: существующий `xrayserver.Config` является узкой моделью одного серверного сценария и не способен выразить полноценные inbounds, outbounds, balancers, routing и opaque-поля.

Следует явно установить:

- `ToManagedConfig` применяется только для одноразовой миграции legacy-конфига;
- обратное преобразование допустимо только для строго распознанного legacy subset либо вообще не используется в новом runtime;
- после миграции источником истины становится `ManagedConfig + opaque overlay`;
- `RenderRuntimeConfig` работает напрямую через `xrayconfig.Compiler`, не через обратное сжатие в legacy `Config`.

Для неподдерживаемого обратного преобразования должна возвращаться явная ошибка без изменения runtime.

## P1 — существенные дополнения плана

### 9. Не описаны schema migrations

Нужно добавить:

- registry миграций `schemaVersion N -> N+1`;
- запрет автоматического открытия неизвестной будущей версии;
- backup до миграции;
- идемпотентность;
- migration marker;
- тест повторного запуска после прерывания миграции.

### 10. Overlay требует правил удаления и конфликтов

Фраза «управляемые поля накладываются поверх» не определяет:

- как удалить существующее поле;
- что означает zero value и что означает «не управляется»;
- кто побеждает при конфликте typed field и opaque field;
- как обрабатывается `null`;
- можно ли opaque-данным заменить `settings`, `streamSettings`, tag или protocol;
- как избежать повторного появления удалённого поля из base document.

Нужны tri-state/field-presence semantics и тесты add/update/delete/no-op для каждого уровня overlay.

### 11. JSON numbers нельзя безоговорочно декодировать в `interface{}`

Обычный `json.Unmarshal` преобразует числа в `float64`, что может потерять точность больших целых. Для `jsonEqual` следует использовать `json.Decoder.UseNumber()` либо канонизатор, сохраняющий числовую семантику.

Также нужно определить поведение для `1`, `1.0` и `1e0`: считать их равными как JSON-числа либо различными как исходное представление.

### 12. Balancer selector нельзя проверять только на точные outbound tags

В Xray selector может использовать правила выбора по префиксу/регулярному выражению в зависимости от поддерживаемой конфигурации. Валидатор должен следовать capability matrix и фактической семантике установленного Xray, а не предполагать только точное совпадение.

### 13. Redaction требует структурированных границ

«Экспорт функций санитизации» недостаточен. Необходимо определить отдельные DTO:

- private runtime DTO;
- redacted API DTO;
- redacted manifest/history DTO;
- diagnostic DTO.

Не следует полагаться на то, что каждый вызывающий код не забудет вручную вызвать redactor. Лучше сделать небезопасную сериализацию закрытой внутри runtime-компонента.

### 14. Не хватает API и frontend integration scope

План добавляет frontend-проверки, но не описывает сами API endpoints и состояния UI. Нужно добавить хотя бы:

- capabilities/version;
- import/parse;
- validate;
- preview/diff;
- apply/status;
- transaction/recovery status;
- history/rollback;
- безопасный export.

UI обязан запрещать повторный Apply во время операции и показывать `recovery_required` как блокирующее состояние.

## Исправленный порядок реализации

1. Зафиксировать orchestration owner и lock hierarchy.
2. Описать durable transaction state machine и crash recovery для каждого состояния.
3. Зафиксировать ownership/overlay/delete semantics.
4. Реализовать capability/version matrix.
5. Реализовать SecretStore/SecretResolver и private/redacted DTO boundaries.
6. Расширить модели и Parser.
7. Реализовать semantic round-trip и JSON comparison через `UseNumber`.
8. Исправить Validator/Compiler для outbounds, Shadowsocks, REALITY/TLS и balancers.
9. Реализовать schema migrations и однонаправленный legacy import.
10. Интегрировать candidate test и transactional activation с coordinator.
11. Реализовать многоуровневую readiness и подтверждённый rollback.
12. Добавить API/UI состояния поверх готового backend pipeline.
13. Выполнить unit, race, failure-injection, crash-recovery и real-binary tests.

## Обязательные тесты

Кроме перечисленных в плане нужны:

- concurrent apply/cancel/restart;
- deadlock test для coordinator/service locks;
- сбой записи/fsync/rename manifest на каждой стадии;
- падение процесса между каждым переходом состояния;
- rollback failure и `recovery_required`;
- повреждённый/частично записанный manifest;
- изменение active config вне AWG Manager между prepare и apply;
- symlink/path traversal проверки;
- сохранение file modes;
- canary secrets во всех API/log/event/history/diagnostic каналах;
- unknown/future schema version;
- неизвестная версия Xray;
- lossless add/update/delete/no-op overlay;
- `jsonEqual` с большими целыми и разными формами чисел;
- обратное преобразование non-legacy профиля должно завершаться ошибкой;
- readiness проверяет не только занятый порт, но и identity процесса.

## Условия одобрения

План можно отдавать в полноценную реализацию после внесения в сам `implementation_plan.md` следующих изменений:

1. Подключённый SecretStore вместо декларативного `SecretRef`.
2. Реальная capability/version matrix вместо фиксированного списка.
3. Durable transaction state machine и recovery всех промежуточных состояний.
4. Исправленный порядок commit/readiness/rollback.
5. Один orchestration owner и строгий lock hierarchy.
6. Transaction-scoped backup с fsync/rename semantics.
7. Многоуровневая readiness.
8. Однонаправленная роль legacy adapter.
9. Schema migrations и overlay delete semantics.
10. Конкретный API/UI integration scope.

После этих правок план можно повторно проверить и затем запускать поэтапно. Начинать реализацию отдельных чистых unit-компонентов допустимо, но подключать их к активному Xray runtime до закрытия P0 нельзя.

