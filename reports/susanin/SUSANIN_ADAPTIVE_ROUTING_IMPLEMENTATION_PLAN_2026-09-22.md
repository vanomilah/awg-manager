# Susanin Adaptive Routing — готовый план реализации в AWG Manager

**Дата:** 2026-09-22  
**Статус:** READY FOR IMPLEMENTATION  
**Репозиторий:** `E:\AWGM\awg-manager`  
**Цель:** добавить Susanin как третий самостоятельный движок маршрутизации наряду с Mihomo и sing-box, сохранив возможность отправлять выбранный Susanin трафик в обычный сетевой туннель, отдельный proxy-узел, конкретную proxy-группу Mihomo либо существующий составной outbound sing-box (`selector`/`urltest`).

---

## 1. Итоговая продуктовая модель

Susanin — не настройка Mihomo/sing-box и не ещё один набор правил внутри них. Это отдельный адаптивный маршрутизатор, который:

1. наблюдает за TCP/UDP соединениями клиентов;
2. определяет направления, не работающие напрямую;
3. временно проверяет их через выбранный альтернативный выход;
4. запоминает успешные направления отдельно для TCP и UDP;
5. в дальнейшем отправляет только эти назначения через альтернативный выход;
6. при недоступности выхода возвращается к прямому доступу (`fail-open` по умолчанию);
7. поддерживает ручные списки «всегда через альтернативный выход» и «всегда напрямую».

В один момент времени владельцем маршрутизации устройств является только один движок:

- `sing-box`;
- `Mihomo`;
- `Susanin`;
- либо маршрутизация AWG Manager выключена.

При выбранном Susanin процессы Mihomo и sing-box могут продолжать работать как **исполнители прокси**. Mihomo обслуживает созданные туннели, подписки, proxy-порты и полноценные proxy-группы; sing-box — туннели, подписки, proxy-порты и уже существующие составные outbounds `selector`/`urltest`. Однако их обычные режимы маршрутизации устройств — TProxy, FakeIP/Policy TUN и собственные правила маршрутизации LAN — не активируются.

### 1.1. Поддерживаемые выходы Susanin

Susanin получает единый каталог выходов:

| Вид выхода | Как работает |
|---|---|
| Системный туннель | Susanin направляет отмеченный трафик прямо в kernel-интерфейс AWG/WireGuard/WDTT/FreeTurn и т. п. |
| Proxy-туннель Mihomo | Mihomo поднимает специальный TUN-вход Susanin и отправляет весь принятый им трафик в выбранный proxy-узел |
| Подписка Mihomo | специальный TUN Mihomo направляет трафик в группу подписки/её активный узел |
| Proxy-группа Mihomo | специальный TUN Mihomo направляет трафик в выбранную `select`, `url-test`, `fallback` или `load-balance` группу |
| Outbound sing-box | специальный TUN sing-box направляет трафик по правилу `inbound == susanin-tun` в отдельный outbound либо существующий составной outbound `selector`/`urltest` |

Ключевой принцип: Susanin решает **какие назначения** перенаправлять, а выбранное ядро решает только **как выполнить конкретный прокси-выход**.

---

## 2. Где создавать ресурсы в интерфейсе

Редакторы прокси не должны дублироваться в настройках Susanin.

На странице `Туннели` сохранить и развить единый блок proxy-ресурсов:

- вкладка `Прокси-туннели`;
- вкладка `Прокси-подписки`;
- новая вкладка `Прокси-группы`.

### 2.1. Вкладка «Прокси-группы»

На первом этапе полноценно редактируются группы Mihomo, поскольку Mihomo нативно поддерживает требуемые типы групп и runtime-переключение:

- `select`;
- `url-test`;
- `fallback`;
- `load-balance`.

Карточка группы должна показывать:

- пользовательское имя;
- метку `Mihomo`;
- тип группы;
- число узлов и providers;
- активный узел;
- задержку/состояние членов;
- кнопки `Открыть`, `Тест`, `Изменить`, `Удалить`;
- runtime-переключение активного узла без перезапуска;
- индикатор «используется Susanin», если группа выбрана его выходом.

Источниками членов группы могут быть:

- отдельные proxy-туннели Mihomo;
- подписки и proxy-providers Mihomo;
- другие группы Mihomo без циклов;
- совместимые мосты к ресурсам AWG Manager, уже поддерживаемые текущей архитектурой Mihomo bridge.

Нельзя разрешать прямую ссылку на ресурс другого ядра, если для него не создан и не проверен совместимый bridge. UI показывает причину недоступности, а не молча исключает узел.

### 2.2. Источник истины групп

Для Mihomo источником истины остаётся `internal/mihomonative.Store` и API `/api/mihomo/native/groups`. Группы нельзя повторно хранить в настройках Susanin.

Поле `SingboxRouterSettings.ProxyGroups` считается legacy-представлением маршрутизатора. Перед удалением или изменением его семантики нужна отдельная идемпотентная миграция:

1. составить список реально используемых записей;
2. перенести Mihomo-группы в `mihomonative.Store` со стабильными ID;
3. сохранить ссылки на существующие ресурсы;
4. записать migration marker;
5. повторный запуск миграции не создаёт дублей;
6. legacy-поле не удалять до прохождения миграционных и rollback-тестов.

Для sing-box в первой версии показываются его существующие `selector`/`urltest` подписок и другие уже созданные составные outbounds как выбираемые выходы. Они не считаются полноценными proxy-группами, сопоставимыми с Mihomo. Отдельный универсальный редактор групп sing-box в этот план не входит и не должен блокировать Susanin + Mihomo proxy groups.

---

## 3. Архитектурные границы

### 3.1. Что Susanin владеет

- адаптивным состоянием назначений (`test`, `ok`, `never`) отдельно для TCP/UDP;
- собственными ipset-наборами;
- собственной цепочкой netfilter;
- зарезервированной маской fwmark/connmark;
- отдельной таблицей policy routing;
- правилами `ip rule`, относящимися только к его маске;
- конфигурацией порогов обучения и health-check;
- выбором стабильного `EgressRef`;
- перезапуском соединений после изменения решения;
- журналом решений и статистикой.

### 3.2. Что Susanin не владеет

- конфигурациями proxy-узлов и подписок;
- составом proxy-групп;
- активным членом группы Mihomo;
- основными конфигами и жизненным циклом Mihomo/sing-box;
- чужими TProxy/FakeIP/Policy-TUN цепочками;
- системными туннелями, созданными другими подсистемами;
- общими настройками DNS маршрутизатора.

### 3.3. Контракт исполнителя выхода

Ввести внутренний интерфейс, не привязанный к конкретному ядру:

```go
type AdaptiveEgress interface {
    Resolve(ctx context.Context, ref EgressRef) (ResolvedEgress, error)
    Prepare(ctx context.Context, desired DesiredEgress) (PreparedEgress, error)
    Commit(ctx context.Context, prepared PreparedEgress) error
    Rollback(ctx context.Context, prepared PreparedEgress) error
    Health(ctx context.Context, active ActiveEgress) EgressHealth
    Release(ctx context.Context, active ActiveEgress) error
}
```

`ResolvedEgress` обязан содержать:

- стабильный ID и отображаемое имя;
- тип и engine owner;
- kernel TUN/interface, в который Susanin строит маршрут;
- capabilities: TCP, UDP, ICMP, IPv4, IPv6;
- revision/digest исходного ресурса;
- способ data-plane health-check;
- признак доступности и понятную причину недоступности.

---

## 4. Модель данных

Создать отдельный пакет, например `internal/adaptiverouting`, а не добавлять всю логику в `internal/mihomo` или `internal/singbox/router`.

### 4.1. Настройки пользователя

```go
type Settings struct {
    Enabled            bool              `json:"enabled"`
    Source             SourceScope       `json:"source"`
    PrimaryEgress      EgressRef         `json:"primaryEgress"`
    FallbackEgresses   []EgressRef       `json:"fallbackEgresses,omitempty"`
    FailurePolicy      string            `json:"failurePolicy"` // direct | block
    Detection          DetectionSettings `json:"detection"`
    Persistence        PersistenceConfig `json:"persistence"`
    AlwaysFileEnabled  bool              `json:"alwaysFileEnabled"`
    NeverFileEnabled   bool              `json:"neverFileEnabled"`
}

type EgressRef struct {
    Kind       string `json:"kind"`       // kernel-tunnel | mihomo-proxy | mihomo-subscription | mihomo-group | singbox-outbound
    ResourceID string `json:"resourceId"` // стабильный ID, не имя/tag
    Engine     string `json:"engine"`     // system | mihomo | sing-box
}
```

`SourceScope` повторяет понятную пользователю модель маршрутизации AWG Manager:

- устройства из выбранной NDMS policy;
- весь LAN-трафик;
- выбранные LAN-сегменты/интерфейсы;
- входящий трафик выбранного серверного туннеля.

Имена ресурсов разрешаются только для отображения. Все сохранённые ссылки используют ID. Переименование группы или туннеля не ломает Susanin.

### 4.2. Operational state

Пользовательские настройки и runtime-состояние не смешивать. Operational state содержит:

- применённую generation/revision;
- зарезервированные mark/mask, table и rule priorities;
- имя TUN-интерфейса executor;
- digest выбранного ресурса;
- состояние процесса/правил/health;
- активный fallback;
- размеры наборов `test/ok/never`;
- время последнего успешного reconcile;
- recovery marker и причину degraded-state.

Записывать атомарно через temp + fsync + rename. Не помещать volatile counters в основной settings JSON.

---

## 5. Единственный владелец маршрутизации

Ввести явное состояние `RoutingOwner`:

```text
none | sing-box | mihomo | susanin
```

Нельзя вычислять владельца по PID процесса. Процесс Mihomo может работать ради proxy-групп, хотя владельцем маршрутизации является Susanin.

### 5.1. Переход к Susanin

При включении Susanin:

1. проверить выбранный выход и его зависимости;
2. подготовить executor TUN;
3. отключить/park только routing slot текущего Mihomo/sing-box;
4. не останавливать слоты туннелей, подписок, proxy providers, device proxy и bridge;
5. убедиться, что TProxy/FakeIP/Policy-TUN правила прошлого owner сняты;
6. активировать datapath Susanin;
7. после data-plane probe записать `RoutingOwner=susanin`.

UI должен различать:

- `Процесс Mihomo запущен`;
- `Mihomo используется как прокси-исполнитель Susanin`;
- `Маршрутизация устройств выполняется Susanin`.

### 5.2. Уход с Susanin

1. прекратить новое обучение;
2. удалить только принадлежащие Susanin `ip rule`, route и netfilter jump;
3. восстановить исходное состояние источника/NDMS policy;
4. снять executor TUN slot;
5. не удалять proxy-группы и подписки;
6. включить выбранный новый routing owner только после успешной очистки Susanin;
7. при частичном сбое перейти в `recovery_required`, а не продолжать с двумя владельцами.

---

## 6. TUN-исполнитель без маршрутизации ядра

Это обязательная часть реализации, а не факультативная оптимизация.

### 6.1. Общие правила

- отдельное детерминированное имя интерфейса, например `awgsus0`;
- `auto-route=false`;
- никакого автоматического TProxy/REDIRECT;
- никакого захвата LAN ядром;
- TUN получает только пакеты, которые Susanin направил в свою routing table;
- отдельное правило ядра направляет весь трафик этого inbound в выбранный target;
- исходящие сокеты ядра должны обходить Susanin и его TUN;
- смена proxy-группы не должна сбрасывать накопленный Susanin cache;
- удаление target запрещено, пока на него ссылается Susanin, либо требует явной транзакции замены.

### 6.2. Mihomo executor

Добавить в авторитетный компилятор Mihomo отдельный optional-фрагмент `adaptive-egress`, например:

```yaml
tun:
  enable: true
  device: awgsus0
  stack: system
  auto-route: false
  auto-redirect: false
  auto-detect-interface: true
  dns-hijack: []
rules:
  - IN-NAME,awgm-susanin-in,<selected-group>
```

Точная схема TUN/name должна подтверждаться `mihomo -t` для поставляемой версии. Если имя TUN inbound не попадает в `IN-NAME`, компилятор обязан использовать проверенный `IN-TYPE,TUN` только при гарантии, что иных TUN-inbound в этом процессе нет. Нельзя добавлять общий `MATCH,<group>`, который перехватит mixed/SOCKS/API трафик.

В текущей архитектуре фрагмент проходит через существующий `internal/mihomo.ApplyCoordinator`:

- candidate compile;
- validation;
- transactional publish;
- process proof;
- rollback/LKG;
- bridge synchronization.

Не создавать второй независимый писатель `config.yaml`.

Выбранная Mihomo-группа остаётся обычной нативной группой. Runtime-переключение её активного члена через Clash API немедленно влияет на Susanin трафик без изменения правил Susanin.

### 6.3. sing-box executor

Добавить отдельный orchestrator slot, например `adaptive-egress`, с:

- inbound `type=tun`, `tag=awgm-susanin-in`, `interface_name=awgsus0`;
- `auto_route=false` и без `auto_redirect`;
- маршрутом `inbound: [awgm-susanin-in] -> selected outbound`;
- явным `route.auto_detect_interface` либо корректным bind исходящего интерфейса для защиты от loop;
- выключенным DNS hijack, пока он не будет отдельно спроектирован и протестирован.

Slot использует существующий sing-box orchestrator, validation и reload. Он не должен включать `SlotRouter` и не должен менять route final для остальных proxy-портов.

### 6.4. Системный туннель

Для kernel tunnel executor TUN ядра не нужен. Адаптер возвращает существующий kernel interface и проверяет:

- интерфейс существует и `UP`;
- gateway/source определены;
- endpoint самого туннеля не попадёт в Susanin;
- есть реальный data-plane probe;
- удаление туннеля защищено reference checker.

---

## 7. Datapath Susanin и совместимость с Keenetic

Не копировать upstream `datapath.sh` в продукт без адаптации. AWG Manager должен владеть применением и снятием правил, а поставляемый Susanin agent — детекцией/обучением.

### 7.1. Обязательный Gate 0: реестр конфликтов

До написания правил собрать на обоих тестовых роутерах:

- все `ip rule` с priority/mask/table;
- `iptables-save -t mangle` и `-t nat`;
- используемые AWG Manager, NDMS, Mihomo и sing-box marks/masks;
- диапазоны routing table;
- имена OpkgTun/kernel TUN;
- порядок NDMS policy rules.

После этого зарезервировать Susanin:

- маску connmark/fwmark без пересечения с NDMS и существующими движками;
- две величины внутри маски: `test` и `ok`;
- table ID;
- rule priority;
- уникальные имена chain/ipset.

Запрещено без проверки жёстко переносить upstream значения `0x10000000`, `0x20000000`, mask `0x30000000`, table `100`, priority `2000`.

### 7.2. Сохранение чужих mark-битов

Правила используют masked operations:

- `MARK --set-xmark value/mask`;
- `CONNMARK --save-mark` и `--restore-mark` только с Susanin mask;
- никогда не обнуляют полный mark;
- `never`/private/local/engine endpoints проверяются до установки Susanin mark.

Для устройств NDMS policy Susanin rule должен иметь приоритет, позволяющий `ok/test` трафику попасть в adaptive table, а немаркированному трафику продолжить обычный DIRECT путь этой policy. Это подтверждается live-тестом, а не предположением о priority.

### 7.3. Source scope

Цепочка Susanin обрабатывает только выбранные источники. Обязательные bypass:

- loopback;
- адреса роутера и локальные подсети;
- multicast/broadcast;
- DNS к локальному роутеру;
- endpoint выбранного туннеля/proxy;
- управляющие порты AWG Manager, Clash API и health probes;
- собственные исходящие соединения proxy engine;
- все записи `never`.

### 7.4. Fail-open и fail-closed

По умолчанию `FailurePolicy=direct`:

- при потере egress удаляется/деактивируется Susanin `ip rule` или default route его таблицы;
- помеченный пакет продолжает поиск по следующим системным правилам и идёт напрямую;
- зависшие conntrack записи выборочно сбрасываются;
- proxy engine не подменяется молча другим target.

Опциональный `block` можно добавить только после рабочего fail-open. Он должен быть явно выбран пользователем и виден в UI.

---

## 8. Health, обучение и DNS

### 8.1. Проверяется data plane, а не PID

`running PID`, существующий TUN и ответ Clash API сами по себе не означают, что выход работает.

Проверка должна пройти через тот же route table/TUN/target, что и пользовательский трафик:

- TCP connect/HTTP probe через executor;
- UDP probe при заявленной UDP capability;
- проверка нескольких адресов;
- debounce до признания выхода недоступным;
- отдельное состояние `engine up / target degraded / data plane down`.

Для Mihomo-группы дополнительно показывать health её членов, но не заменять этим end-to-end probe.

### 8.2. Состояние Susanin

Хранить раздельные наборы:

- `test_tcp`, `test_udp`;
- `ok_tcp`, `ok_udp`;
- `always_net`;
- `never`;
- cooldown/eviction metadata.

Нужны операции:

- забыть IP/сеть;
- очистить только test;
- очистить learned cache с подтверждением;
- экспорт/импорт пользовательских always/never списков;
- показать, почему адрес попал в набор.

### 8.3. DNS

Первая версия не должна принудительно перехватывать DNS. Адаптивное обучение по IP работает и при Private DNS клиента. Ручные доменные списки резолвятся службой AWG Manager через выбранный системный resolver.

UI обязан объяснять ограничение shared CDN IP: маршрутизация изученного IP может затронуть несколько доменов. Не обещать доменную точность там, где Susanin видит только IP.

---

## 9. Backend и API

Предлагаемые пакеты:

```text
internal/adaptiverouting/
  service.go
  types.go
  store.go
  catalog.go
  coordinator.go
  datapath.go
  health.go
  references.go
  migration.go
  diagnostics.go
  executor_system.go
  executor_mihomo.go
  executor_singbox.go
```

Поставляемый upstream/fork Susanin оформляется как управляемый компонент AWG Manager:

- бинарь/исходники фиксированной версии и checksum;
- лицензия и NOTICE в дистрибутиве;
- никаких `wget | sh` во время работы;
- никакого стороннего автообновления;
- lifecycle через AWG Manager;
- конфиг генерируется AWG Manager;
- удаление компонента не удаляет пользовательские списки без отдельного подтверждения.

API:

```text
GET    /api/adaptive-routing/status
GET    /api/adaptive-routing/settings
PUT    /api/adaptive-routing/settings
GET    /api/adaptive-routing/egresses
POST   /api/adaptive-routing/preview
POST   /api/adaptive-routing/apply
POST   /api/adaptive-routing/start
POST   /api/adaptive-routing/stop
POST   /api/adaptive-routing/test-egress
GET    /api/adaptive-routing/learned
POST   /api/adaptive-routing/forget
POST   /api/adaptive-routing/cache/clear
GET    /api/adaptive-routing/events
```

Все mutation endpoints проходят общую блокировку routing transition. `apply/start/stop` идемпотентны. API возвращает structured error с phase, rollback status и recovery instructions.

`GET /egresses` отдаёт не сырые конфиги и секреты, а нормализованные DTO:

- stable ID;
- label;
- kind/engine;
- capabilities;
- alive/ready;
- unavailable reason;
- reference revision.

Добавить reference checker Susanin во все delete/update потоки туннелей, подписок и групп.

---

## 10. Транзакционное применение

Применение Susanin — единая транзакция с журналом фаз:

1. `validated` — settings, source и refs валидны;
2. `executor_prepared` — candidate config ядра собран и проверен;
3. `executor_committed` — TUN существует, target route установлен;
4. `executor_verified` — end-to-end probe успешен;
5. `old_owner_parked` — routing slot прошлого owner выключен;
6. `datapath_staged` — новые ipset/chain/table подготовлены без входного jump;
7. `datapath_committed` — атомарно переключён jump/rule;
8. `settings_committed` — generation и owner записаны;
9. `cleanup_complete`.

До `datapath_committed` разрешён полный автоматический rollback. После commit boundary ошибка должна либо завершить roll-forward, либо установить `recovery_required`; нельзя запускать старого owner поверх частично активного Susanin.

Crash recovery при старте читает журнал и доказывает фактическое состояние:

- TUN identity;
- process identity;
- config digest;
- netfilter rule comments/ownership;
- ipset/table/rule ownership;
- выбранный resource revision.

Никакого удаления цепочки/интерфейса только по имени без ownership proof.

---

## 11. UI маршрутизации

Добавить вкладку `Susanin · Адаптивный` на странице `Маршрутизация`.

### 11.1. Простой режим

Три последовательные карточки:

1. **Кого анализировать** — policy/весь LAN/сегменты/серверный туннель;
2. **Как выбирать** — адаптивно + краткие безопасные пороги;
3. **Куда отправлять** — выбранный туннель/proxy/group, состояние и fallback.

Ниже:

- большой статус `Работает / Обучается / Выход недоступен / Требуется восстановление`;
- счётчики learned TCP/UDP;
- текущий executor (`Mihomo TUN → группа …`);
- `Применить`;
- `Остановить`;
- `Проверить выход`;
- `Открыть изученные направления`.

В selector выхода группировать варианты:

```text
Сетевые туннели
Mihomo · прокси
Mihomo · подписки
Mihomo · группы
sing-box · отдельные и составные outbounds (selector/urltest)
```

Недоступные ресурсы видимы disabled с конкретной причиной.

### 11.2. Экспертный режим

- thresholds и таймеры;
- TCP/UDP learning отдельно;
- cache TTL/limit/eviction;
- health targets/debounce;
- fail-open/fail-closed;
- always/never редактор;
- source interfaces/subnets;
- диагностический preview rules/table/marks;
- журнал решений.

Поля mark/table/priority по умолчанию read-only и выдаются allocator. Ручное изменение — только диагностический developer mode.

### 11.3. Соединения и журнал

В `Соединениях` добавить источник решения:

- `Susanin DIRECT`;
- `Susanin TEST → Mihomo: <group>`;
- `Susanin LEARNED → sing-box: <outbound>`;
- `Susanin ALWAYS`;
- `Susanin FAIL-OPEN`.

Лог не должен показывать секреты, полные proxy URL, UUID, пароли или subscription URL.

---

## 12. Порядок реализации по Gate

### Gate 0 — live inventory и утверждение ресурсов

- собрать conflict matrix marks/tables/rules/interfaces на двух роутерах;
- зафиксировать source policy semantics;
- проверить создание `awgsus0` обоими поставляемыми ядрами с `auto-route=false`;
- доказать TCP и UDP через TUN к одному тестовому proxy;
- определить окончательные mark/table ranges;
- подготовить rollback-команды и fixture dumps.

**Exit:** подтверждены значения ресурсов и отсутствуют конфликты. Без этого Gate код datapath не начинать.

### Gate A — Mihomo proxy groups как самостоятельный ресурс

- вынести UI групп из маршрутизации во вкладку `Туннели → Прокси-группы`;
- использовать существующий `mihomonative.Store`;
- завершить CRUD, validation циклов, runtime select и health;
- мигрировать legacy группы;
- добавить reference API;
- не связывать доступность редактора с активным routing owner.

**Exit:** группу можно создать из прокси/подписок, переключить в runtime и использовать независимо от маршрутизации Mihomo.

### Gate B — adaptive domain model и каталог выходов

- пакет `internal/adaptiverouting`;
- settings/state stores;
- stable refs и catalog adapters;
- capability/unavailable reasons;
- reference checker во всех delete flows;
- API preview без мутаций.

**Exit:** Susanin сохраняет выбранный stable ref и переживает переименование ресурса.

### Gate C — isolated executor TUN

- Mihomo adaptive fragment через единственный coordinator;
- sing-box adaptive orchestrator slot;
- system tunnel adapter;
- target-specific inbound routing;
- loop protection;
- end-to-end TCP/UDP health;
- rollback и crash tests.

**Exit:** пакет, вручную направленный в `awgsus0`, выходит строго через выбранную группу; остальные proxy-порты продолжают работать как раньше.

### Gate D — Susanin engine packaging и observer

- зафиксировать upstream revision/license;
- собрать поддерживаемые архитектуры;
- AWG-managed config/lifecycle;
- перенести/адаптировать detector, cache и state;
- удалить зависимость от самостоятельного install/update script;
- status/events/diagnostics.

**Exit:** observer обучается на fixture conntrack и не модифицирует сеть без coordinator.

### Gate E — owned datapath

- resource allocator;
- masked marks;
- ipset/chain/table reconciler;
- source scope и bypass;
- fail-open;
- NDMS restart reconciliation;
- foreign ownership protection;
- точечный conntrack reset.

**Exit:** live packet tests доказывают DIRECT → TEST → LEARNED → DIRECT transitions без утечки в чужие правила.

### Gate F — routing owner и общая транзакция

- explicit RoutingOwner;
- park/unpark router slots;
- journal/commit boundary/recovery marker;
- cold boot recovery;
- concurrent cancel/update tests;
- обновление/удаление target с активной ссылкой.

**Exit:** ни один failpoint/reboot не оставляет одновременно два активных владельца маршрутизации.

### Gate G — UI

- вкладка Susanin;
- simple/expert;
- selector выхода;
- learned/cache screen;
- connections/log integration;
- proxy-group tab;
- mobile layout и accessibility.

**Exit:** новый пользователь включает адаптивную маршрутизацию без терминала и понимает, какое ядро лишь исполняет выход.

### Gate H — приёмка на роутерах

Сначала staging/router `192.168.50.1`, затем основной `192.168.90.1`; установка только обычным `opkg install`, **без `--force-reinstall`**.

Проверить матрицу:

- kernel AWG/WG tunnel;
- Mihomo proxy;
- Mihomo subscription selector;
- Mihomo `url-test`/`fallback` group;
- sing-box outbound/selector;
- TCP/HTTPS;
- UDP/QUIC;
- Private DNS телефона;
- смена активного члена группы без restart;
- падение proxy target;
- остановка Mihomo/sing-box;
- перезапуск NDM;
- перезапуск AWG Manager;
- reboot роутера;
- удаление/переименование ресурса;
- возврат с Susanin на Mihomo и sing-box.

**Exit:** минимум 24 часа стабильной работы на staging и подтверждённый rollback до установки на основной роутер.

---

## 13. Обязательные тесты

### Unit

- normalization/validation settings;
- stable ref resolution;
- group cycle/reference validation;
- mark mask preservation;
- ruleset rendering idempotence;
- cache state transitions TCP/UDP;
- health debounce/failover;
- secret redaction.

### Integration

- candidate executor compile/check;
- TUN inbound routes только в target;
- Mihomo mixed port не попадает под Susanin rule;
- sing-box non-adaptive slots не меняются;
- target delete is blocked;
- rename keeps ref valid;
- coordinator rollback at every phase;
- concurrent apply/cancel;
- restart recovery;
- NDMS rule wipe + reconcile.

### Live packet assertions

Для каждого сценария фиксировать не только «сайт открылся», но:

- counters нужной chain/ipset;
- выбранную `ip rule`/table;
- TUN RX/TX;
- connection в Clash/sing-box API с ожидаемым outbound;
- внешний IP;
- отсутствие пакетов в TProxy chain неактивного owner;
- сохранение прямого доступа при выключенном egress.

---

## 14. Запреты для реализации

- не запускать upstream online installer на роутере;
- не давать Susanin и Mihomo/sing-box одновременно владеть LAN capture;
- не считать запущенный процесс активным routing owner;
- не использовать имя группы как persistent ID;
- не писать второй независимый Mihomo config writer;
- не включать Mihomo `auto-route`/`auto-redirect` для Susanin TUN;
- не включать sing-box `auto_route`/`auto_redirect` для Susanin TUN;
- не использовать без маски полное `MARK`/`CONNMARK` присваивание;
- не удалять чужие rules/chains/TUN по совпадению имени;
- не подменять недоступный target на `DIRECT` молча — это делает только явный fail-open datapath;
- не копить неподтверждённые draft-правила: Apply должен быть транзакционным;
- не хранить secrets в Susanin settings/state/logs;
- не собирать IPK и не деплоить в промежуточных Gate без отдельного запроса пользователя;
- никогда не использовать `--force-reinstall`.

---

## 15. Definition of Done

Задача считается завершённой только если одновременно выполнено следующее:

1. Susanin виден как отдельный routing owner, а не режим Mihomo/sing-box.
2. Mihomo-группы создаются во вкладке `Туннели → Прокси-группы` независимо от активного router engine.
3. Susanin выбирает системный туннель, Mihomo proxy/subscription/proxy-group либо отдельный или существующий составной outbound sing-box (`selector`/`urltest`) по stable ID.
4. Mihomo/sing-box TUN работает с `auto-route=false` и принимает только Susanin traffic.
5. Основная маршрутизация этих ядер при Susanin выключена, но proxy-ресурсы и runtime group switching работают.
6. TCP и UDP фактически проходят через выбранный target, что доказано packet/API counters.
7. Потеря target в режиме fail-open возвращает доступ напрямую.
8. Переходы owner, crash и reboot не оставляют конфликтующих правил.
9. Удаление используемого ресурса защищено reference checker.
10. Все unit/integration/live acceptance tests и `git diff --check` проходят.
11. Есть отдельный resolution report с точным diff, тестами и остаточными рисками.

---

## 16. Источники, которые необходимо сверять при реализации

- Susanin.Keenetic: https://github.com/R17a/Susanin.Keenetic
- Mihomo TUN: https://wiki.metacubex.one/en/config/inbound/tun/
- Mihomo rules (`IN-NAME`, `IN-TYPE`): https://wiki.metacubex.one/en/config/rules/
- Mihomo proxy groups: https://wiki.metacubex.one/en/config/proxy-groups/
- sing-box TUN: https://sing-box.sagernet.org/configuration/inbound/tun/
- sing-box route rules: https://sing-box.sagernet.org/configuration/route/rule/

Версии документации должны сопоставляться именно с бинарниками, поставляемыми AWG Manager. Любое поле конфигурации подтверждать встроенной командой проверки конкретного ядра до публикации candidate config.
