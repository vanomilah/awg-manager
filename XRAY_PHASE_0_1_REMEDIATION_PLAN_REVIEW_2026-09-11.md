# Ревью плана устранения замечаний Phase 0/1: Xray Full Management

Дата: 2026-09-11  
Проект: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План стал существенно лучше и учитывает часть замечаний повторного аудита, однако запускать его в текущем виде рано. В нём присутствует одна блокирующая архитектурная ошибка и не определены несколько обязательных механизмов безопасной интеграции.

До передачи плана в реализацию необходимо устранить замечания ниже.

## Блокирующие замечания

### 1. Предложенная конвертация создаст циклический импорт Go

План предлагает создать в `internal/xrayconfig/convert.go` функции:

```go
ConvertServerConfig(cfg xrayserver.Config) *ManagedConfig
ExportServerConfig(m *ManagedConfig) (xrayserver.Config, error)
```

При этом `internal/xrayserver` должен использовать `internal/xrayconfig`. Получится недопустимый цикл:

```text
xrayconfig -> xrayserver -> xrayconfig
```

Требуемое исправление — выбрать один из вариантов:

1. Разместить legacy-адаптер в `internal/xrayserver`, который импортирует `xrayconfig`.
2. Вынести старую DTO-модель в отдельный нейтральный пакет, не зависящий ни от runtime, ни от compiler.
3. Предпочтительно: оставить `xrayconfig` полностью независимым от `xrayserver`, а migration adapter разместить на уровне orchestration/integration.

### 2. Parser с одним `RawSettings` не обеспечивает lossless round-trip

Неизвестные поля Xray могут находиться не только внутри protocol `settings`, но также:

- в корне inbound/outbound;
- в `streamSettings`;
- в `sockopt`;
- в `mux`;
- в `proxySettings`;
- в `allocate`;
- внутри transport-specific settings;
- внутри TLS/REALITY;
- внутри routing rules и balancers.

Сохранение всех неизвестных данных в существующем `RawSettings` не определяет, в какую исходную секцию они должны вернуться, и может привести к потере либо перемещению полей.

План должен определить lossless-модель на каждом уровне. Допустимые варианты:

- хранить исходный raw JSON объекта и накладывать управляемые изменения поверх него;
- добавить отдельные opaque maps для корня, protocol settings, stream settings и вложенных блоков;
- разделить `Managed` и `Expert` ownership и явно запрещать небезопасное преобразование между ними.

Критерий: `parse -> compile` существующего экспертного конфига без изменений должен давать семантически эквивалентный документ со всеми неизвестными полями.

### 3. Transactional apply упомянут, но не спроектирован

В Proposed Changes отсутствует полный алгоритм применения. Его необходимо описать явно:

1. Заблокировать параллельные операции над профилем/runtime.
2. Сохранить desired model и candidate в staging-каталог той же файловой системы.
3. Проверить внутренним Validator.
4. Запустить установленный Xray в test mode на candidate-файле.
5. Сформировать redacted semantic diff и durable transaction manifest.
6. Создать резервную копию текущего active config.
7. Атомарно заменить runtime config.
8. Перезапустить только затрагиваемый экземпляр Xray.
9. Выполнить readiness probe и проверить ожидаемые listeners/API.
10. При неуспехе атомарно восстановить предыдущий config и перезапустить старую версию.
11. После успеха выполнить commit metadata/history.
12. При запуске AWG Manager обнаруживать незавершённые manifests и выполнять recovery.

План должен использовать существующий coordinator и его границы commit/rollback, а не создавать второй независимый transaction manager.

### 4. Модель Balancer недостаточна

Предлагаемый тип:

```go
type Balancer struct {
    Tag      string
    Selector []string
    Strategy string
}
```

не способен lossless представить конфигурацию Xray: `strategy` является структурированным объектом, а не только строкой. Также нужны:

- проверка уникальности tag;
- проверка непустого selector;
- проверка ссылок selector на допустимые outbounds/prefixes;
- компиляция `balancers` внутрь `routing`;
- parser и diff для balancers;
- opaque-поля для неизвестных параметров strategy;
- capability gating по установленной версии Xray.

### 5. Список методов Shadowsocks нельзя фиксировать без capability matrix

Нельзя безусловно объявлять поддержку всех перечисленных методов, включая `none` и SS2022, для любой установленной версии Xray.

План должен включать:

1. Определение версии установленного Xray.
2. Таблицу поддерживаемых методов по версии/сборке.
3. Отказ при неизвестной версии вместо оптимистичного принятия.
4. Проверку размера декодированного ключа SS2022.
5. Отдельную обработку legacy password и SS2022 key.
6. Golden-тест, который дополнительно проходит реальный Xray test mode.

## Существенные недостающие части

### 6. Versioned schema и миграции

В плане отсутствует механизм перехода между версиями `ManagedConfig`:

- registry миграций `vN -> vN+1`;
- запрет чтения неизвестной будущей версии;
- резервная копия перед миграцией;
- идемпотентность повторного запуска;
- тесты миграции существующего `xrayserver.Config` без изменения активного runtime.

### 7. Хранение секретов

Не определено использование `SecretRef`, хотя оно требуется общим планом. UUID, пароли, private keys и subscription credentials не должны храниться в публичной managed-модели или history.

Необходимо описать:

- каталог секретов с правами `0700`;
- файлы секретов с правами `0600`;
- атомарную запись;
- разрешение `SecretRef` только непосредственно перед compile/apply;
- удаление orphaned secrets после подтверждённого commit;
- redacted export по умолчанию;
- явное подтверждение полного экспорта.

### 8. Redactor должен быть подключён ко всем выходным каналам

Недостаточно реализовать `Redactor` как библиотечную функцию. План должен потребовать его использование для:

- preview и diff API;
- transaction manifest и history;
- application/runtime logs;
- notifications/events;
- diagnostic snapshots;
- API error context;
- export по умолчанию.

Нужны regression-тесты с canary secrets, подтверждающие, что секрет не появляется ни в одном из перечисленных каналов.

### 9. Xray test mode и capability detection

Следует явно добавить отдельные компоненты:

- `BinaryInspector` — версия и build information;
- `Capabilities` — протоколы, transports, security и параметры конкретной версии;
- `CandidateTester` — запуск Xray test mode с timeout и ограниченным окружением;
- нормализация stderr без утечки секретов;
- ошибка при отсутствии бинарника или несовместимой версии.

### 10. Недостаточное frontend/API покрытие

`wizardCapabilities.test.ts` относится в основном к Phase 0. Для интеграции Phase 1 необходимы:

- API contract tests для import/parse/validate/preview/apply/status/history;
- компонентный тест preview/diff;
- отображение validation и Xray test-mode ошибок;
- блокировка Apply при ошибке;
- состояние применения и recovery;
- корректное отображение rollback;
- отсутствие секретов в UI payload.

## Уточнения по предложенной валидации

План проверки outbounds в целом правильный, но его нужно дополнить:

- запрещать `Server`, `Port` и credentials для `freedom`/`blackhole`, если они не находятся в явно поддерживаемом raw-режиме;
- проверять TLS `ServerName` там, где он обязателен;
- проверять наличие блока `Reality` при `security=reality` и запрещать его при другой security;
- проверять отсутствие серверного `PrivateKey` в клиентском REALITY;
- проверять transport/security matrix отдельно для inbound и outbound;
- нормализовать protocol/transport/security перед сравнением;
- проверять уникальность stable client ID глобально в пределах профиля либо документировать область уникальности;
- обеспечить уникальность email, используемого Xray Stats API;
- не считать один `Remark` достаточной стабильной идентичностью клиента.

## Исправленный порядок реализации

1. Исправить направление зависимостей и определить расположение migration adapter.
2. Зафиксировать ownership/lossless merge contract.
3. Расширить типы opaque-полями или raw-object overlay.
4. Реализовать Parser и round-trip golden tests.
5. Реализовать capability/version matrix.
6. Исправить Outbound, Shadowsocks, REALITY/TLS и Balancer validation/compiler.
7. Исправить semantic diff, включая JSON и balancers.
8. Реализовать schema migrations и импорт legacy `xrayserver.Config`.
9. Подключить защищённое secret storage и redaction boundaries.
10. Интегрировать пакет с существующим transaction coordinator.
11. Добавить Xray test mode, atomic apply, readiness, rollback и startup recovery.
12. Добавить API и frontend только поверх готового безопасного backend pipeline.
13. Выполнить unit, integration, failure-injection и real-binary tests.

## Минимальные acceptance criteria

План можно считать выполненным только когда:

- в production-коде присутствуют реальные вызовы `xrayconfig.Parser`, `Validator`, `Compiler`, `Redactor` и diff;
- отсутствует циклический импорт пакетов;
- legacy config мигрируется идемпотентно и не меняет runtime до подтверждённого Apply;
- `parse -> compile` сохраняет неизвестные поля;
- каждый принятый validator outbound проходит Xray test mode;
- invalid candidate никогда не заменяет active config;
- readiness failure автоматически восстанавливает рабочий config;
- незавершённая транзакция восстанавливается после перезапуска AWG Manager;
- semantic diff не реагирует на форматирование JSON;
- ни один canary secret не появляется в API, diff, logs, events, history или diagnostics;
- balancer references и client identities валидируются;
- полный targeted backend/frontend набор тестов проходит в чистой Linux-среде.

## Итоговое решение

Текущий план **не одобрен к запуску без доработки**.

После устранения циклического импорта и добавления конкретного transactional apply/recovery plan его можно повторно представить на ревью. Остальные пункты должны быть включены в тот же план до начала production-интеграции, иначе агент снова реализует только улучшенную библиотеку вместо безопасно работающего управления Xray.

