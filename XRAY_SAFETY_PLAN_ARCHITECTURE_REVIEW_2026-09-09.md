# Архитектурная проверка предреализационного плана Xray

Дата: 2026-09-09  
Проверен файл:
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Итог

Предыдущие четыре технических блокера учтены: план добавил реальную реконфигурацию
dispatcher, listener ownership, broadcast process exit и package-aware binary resolver.

План пока нельзя запускать без исправления модели портов и владельца общей транзакции.
Legacy Xray мог принимать публичный origin-трафик напрямую. В новой архитектуре этот же
публичный порт должен занять dispatcher, а Xray должен слушать отдельный внутренний порт.
Текущее правило `legacy inbound -> ListenPort Xray` способно изменить публичную точку
входа и нарушить работу существующего подключения.

## P0: миграция должна учитывать топологию входящего трафика

Нельзя безусловно выполнять:

```text
legacy inbounds[0].port -> new Xray ListenPort
```

Необходимо сначала классифицировать обнаруженную схему.

### Топология A: legacy direct

```text
CDN/origin → legacy Xray:P
```

При переходе к общей новой схеме:

```text
CDN/origin → dispatcher:P → new Xray:Q
```

где:

- `P` сохраняется как `DispatcherPort`, чтобы не менять внешний origin endpoint;
- `Q` является отдельным свободным loopback-портом и становится `ListenPort` Xray;
- Xray слушает `127.0.0.1:Q`, если прямой LAN/WAN listener не требуется;
- dispatcher получает `XrayTarget=127.0.0.1:Q`.

Транзакция должна сначала проверить возможность занять `P` после остановки legacy Xray,
а затем запустить внутренний Xray и dispatcher в порядке, исключающем длительный разрыв.

### Топология B: dispatcher уже существовал

```text
CDN/origin → dispatcher:P → legacy/new Xray:Q
```

В этом случае:

- `P` читается из фактической конфигурации/running state dispatcher;
- `Q` читается из его Xray target и сверяется с Xray inbound;
- оба значения сохраняются, если они валидны и не конфликтуют;
- allocator используется только при реальной коллизии, а не меняет рабочий публичный
  порт без необходимости.

### Топология C: состояние неоднозначно

Если невозможно доказать, был ли legacy inbound публичным origin или внутренним target,
автоматическая миграция запрещена. UI показывает preview:

- обнаруженные listeners и их владельцев;
- предполагаемый публичный порт;
- предполагаемый внутренний Xray-порт;
- требуемое изменение origin, если сохранить внешний порт невозможно.

Сервис переходит в `migration_conflict`, не меняя процессы и файлы.

### Обязательное правило

Allocator прежде всего должен сохранять существующий публичный endpoint. Назначение нового
`DispatcherPort` допустимо только как явно подтверждённое изменение, потому что оно может
потребовать изменения origin-настройки вне роутера.

## P0: общую транзакцию не должен выполнять `xrayserver.Service`

План помещает реконфигурацию dispatcher внутрь `xrayserver.applyCandidateLocked`. Это
создаёт неправильное направление зависимостей:

- Xray service начинает управлять shared ingress-компонентом;
- dispatcher используется также Telegram Proxy;
- callbacks `onReload`, mutex Xray и mutex dispatcher могут образовать lock inversion;
- rollback двух компонентов становится скрытым внутри одного сервиса;
- изменение Xray способно неожиданно прервать Telegram Proxy.

Нужен отдельный координатор, например:

```text
internal/serveringress.Coordinator
```

или:

```text
internal/serverwizard.ApplyCoordinator
```

Координатор владеет общей транзакцией:

```text
snapshot dispatcher + Xray + Telegram ingress state
  → validate complete candidate topology
  → reserve ports
  → prepare Xray candidate
  → prepare dispatcher candidate
  → stop only components that must change
  → start internal target
  → verify PID/listener ownership
  → start/reconfigure dispatcher
  → verify routing to target
  → commit both configs/revisions
  → publish events
```

При ошибке координатор откатывает оба компонента в обратном порядке. `xrayserver.Service`
предоставляет узкие операции prepare/start/stop/restore и не вызывает dispatcher напрямую.

Определить единый порядок locks либо не удерживать component mutex во время внешних
вызовов и ожидания readiness. Добавить тест отсутствия deadlock при одновременном Xray
apply и Telegram Proxy reload.

## P1: readiness должна проверять полный маршрут dispatcher → Xray

Привязка socket inode к PID подтверждает listener Xray, но не подтверждает правильную
конфигурацию dispatcher. После запуска обоих компонентов необходим probe через входной
порт dispatcher и Xray path, который подтверждает:

- dispatcher слушает ожидаемый публичный порт;
- запрос с Xray path направляется в Xray target;
- Telegram path, если включён Telegram Proxy, остаётся направлен в Telegram worker;
- неизвестный path обрабатывается согласно заданной default policy;
- probe не требует и не журналирует реальные UUID/secrets.

Если безопасный protocol-level health probe Xray невозможен, минимум должен проверять
ownership обоих listener и routing decision dispatcher через внутренний диагностический
API/route matcher без клиентских секретов.

## P1: `keep_legacy` не должен уничтожать состояние нового config

План предлагает установить новому серверу `Enabled=false`. Если новый config уже был
настроен, это является мутацией пользовательского состояния и мешает позднее вернуться к
нему.

Правильнее хранить отдельное runtime decision:

```text
active_generation = legacy | new | none
migration_status = conflict | deferred | completed
```

При `keep_legacy`:

- новый settings-файл не изменяется;
- новый runtime подавляется координатором;
- legacy config/init/process остаются без изменений;
- UI показывает отложенную миграцию;
- пользователь позже может выбрать `activate_new` без восстановления потерянного флага.

## P1: `SourceManaged` также требует корректного удаления

План блокирует `opkg remove` для `SourceManaged`, но карточка обещает кнопку `Удалить` для
любого установленного ядра. Нужно определить поведение по источнику:

- `SourcePackage` — удаление через opkg после dependency gate;
- `SourceManaged` — удаление только через тот installer, который создал managed binary,
  с проверкой manifest/fingerprint и тем же dependency gate;
- `SourceExternal` — AWG Manager не удаляет файл, показывает `Управляется вне AWG Manager`;
- неизвестный источник — fail-closed.

UI меняет подпись/доступность действия согласно source и не обещает удалить внешний
бинарник.

## Дополнительные тесты

Добавить:

1. legacy direct `Xray:9009` мигрирует в `dispatcher:9009 → Xray:free-loopback-port`;
2. существующий внешний endpoint сохраняется без изменения;
3. уже работающая схема `dispatcher:P → Xray:Q` сохраняет оба порта;
4. неоднозначная топология переходит в conflict без мутаций;
5. allocator не меняет публичный порт автоматически;
6. coordinator rollback восстанавливает Xray и dispatcher;
7. ошибка dispatcher после успешного старта Xray откатывает Xray;
8. Telegram route остаётся рабочим после Xray migration;
9. конкурентный Telegram reload и Xray apply не приводят к deadlock;
10. `keep_legacy` не изменяет persisted new `Enabled`;
11. `SourceManaged` удаляется только owning installer;
12. `SourceExternal` никогда не удаляется AWG Manager.

## Критерий допуска

План можно запускать после:

1. замены прямого mapping legacy inbound на topology-aware migration;
2. сохранения публичного origin/dispatcher port по умолчанию;
3. выделения отдельного cross-component transaction coordinator;
4. добавления проверки маршрута dispatcher → Xray без раскрытия credentials;
5. сохранения нового settings при `keep_legacy`;
6. определения uninstall semantics для package/managed/external binary;
7. добавления cross-component rollback и concurrency tests.

После этих изменений архитектура будет готова к безопасной реализации.
