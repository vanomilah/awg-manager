# Финальное ревью Mihomo Remediation Plan — редакция 5

Проверен документ:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Дата: 2026-09-21.

## Вердикт

Редакция 5 исправила три обязательных замечания предыдущего ревью:

1. В Gate C включена production-реализация `ExactBridgeRuntime`.
2. В Gate D указаны реальные action identifiers: `mihomo.restart`, `mihomo.reload`, `subscription.update`.
3. `verified-active.json` и LKG pointer больше не объявлены побайтово равными: зафиксировано структурное сравнение pointer и проверка digest bundle.

**Gate A и Gate B можно начинать.** Новую редакцию плана делать не требуется.

Однако Gate C нельзя реализовывать буквально по текущему тексту: обнаружен один архитектурный блокер в контракте наблюдения bridge и одна обязательная конкретизация recovery. Ниже приведены binding amendments — исполнитель обязан применить их непосредственно при реализации.

## Binding amendment 1: наблюдаемое состояние bridge

### Почему текущая формулировка невыполнима

План предлагает в `cmd/awg-manager/mihomo_bridge_runtime.go` обращаться к:

```text
pm.queries.Interfaces.Get / GetProxy
```

Но `queries` — приватное поле `internal/singbox.ProxyManager`. Код пакета `cmd/awg-manager` не может к нему обратиться.

Текущий публичный контракт `bridgeProxyRegistrar` возвращает через `LookupProxy` только:

- description;
- exists.

Этого недостаточно, чтобы независимо доказать:

- `State` / `Link` / `Up`;
- `SystemName`;
- address;
- полный список существующих `ProxyN`.

Кроме того:

- `ndms.ProxyInfo` не содержит `SystemName` и address;
- `StoreTxAdapter.ListBridges()` сейчас не заполняет `OwnerUUID` и `Generation`;
- canonical NDMS description имеет вид `awg-manager:mihomo:<kind>:<id>` и не содержит generation;
- следовательно, `OwnerUUID` и `Generation` нельзя честно восстановить из наблюдаемого состояния NDMS;
- подстановка этих полей из desired store не является независимым runtime proof.

### Как реализовать правильно

1. В `internal/singbox` добавить публичный read-only typed API, например:

   ```go
   type ProxyObservation struct {
       Name        string
       Exists      bool
       Description string
       State       string
       Link        string
       Up          bool
       SystemName  string
       Address     string
   }

   func (pm *ProxyManager) InspectProxy(ctx context.Context, index int) (ProxyObservation, error)
   func (pm *ProxyManager) ListProxyObservations(ctx context.Context) ([]ProxyObservation, error)
   ```

   Либо внедрить эквивалентный узкий observer-interface в `mihomoBridgeRuntime`. Нельзя экспортировать внутреннее поле `queries` и нельзя читать desired store вместо NDMS.

2. Расширить `bridgeProxyRegistrar` этим read-only контрактом и fake-реализации в тестах.

3. Разделить две сущности:

   - **наблюдаемые OS/NDMS-факты:** Proxy index/name, description, state/link/up, system name, address;
   - **transaction metadata:** generation и иные поля, существующие только в manifest/store.

4. Не требовать полного равенства `ObservedBridge.BridgeRef == ref`, если часть полей не кодируется в NDMS. Сравнивать только доказуемый observable identity:

   - `ProxyIndex` / `ProxyInterface`;
   - canonical owner description `awg-manager:mihomo:<kind>:<id>`;
   - фактический `KernelInterface`;
   - `Exists` и `Up`;
   - listener/process proof отдельно через procfs.

5. `Generation` проверять по durable transaction manifest/bridge-operation receipt, а не выдавать его за наблюдаемое поле ОС. Если проект действительно требует OS-observable generation, сначала необходимо изменить устойчивый ownership token/NDMS description и описать миграцию старых записей.

6. Требование `AssignedIP != ""` применять только после проверки реального поведения Keenetic ProxyN. Если ProxyN законно не имеет собственного address, отсутствие IP не должно ложно переводить рабочую систему в recovery. Для такого интерфейса доказательством должны быть owner, Up/SystemName и listener/process proof.

7. `PublishBridge` и `WithdrawBridge` должны работать точечно. Поиск соответствующего native resource и listen port по `ProxyIndex` допустим, но обязан:

   - дать ровно одно совпадение;
   - проверить canonical owner;
   - завершиться fail-closed при нуле или нескольких совпадениях;
   - не вызывать глобальный `manager.Reconcile(ctx, nil)`.

### Обязательные тесты

- production adapter компилируется как `mihomo.ExactBridgeRuntime`;
- observer читает fake NDMS, а не native store;
- desired says Up, NDMS says Down — результат Down;
- foreign/empty description блокирует publish/withdraw;
- отсутствие и неоднозначность mapping `ProxyIndex -> native resource` блокируются;
- `AssignedIP` проверяется согласно фактическому контракту ProxyN, а не искусственному ожиданию;
- generation проверяется transaction receipt, а не копируется в observed proof;
- publish одного bridge не изменяет соседние bridges.

## Binding amendment 2: recovery после RuntimeVerified

Строка плана о startup recovery из `RuntimeVerified` недостаточно точна. Старый LKG pointer и проверенный candidate record могут относиться к разным поколениям. Их нельзя просто сравнить между собой.

Для выбранной безопасной семантики `rollback_to_lkg` последовательность должна быть явной:

1. прочитать и проверить существующий LKG pointer;
2. проверить manifest/config/store snapshot его bundle по digest;
3. восстановить store snapshot LKG;
4. атомарно восстановить active config LKG;
5. остановить прежний процесс и дождаться reap;
6. запустить и доказать runtime именно поколения LKG;
7. восстановить/проверить bridges поколения LKG;
8. записать и перечитать `verified-active.json` для LKG record;
9. проверить существующий LKG pointer без его продвижения (`advanceLKG=false`);
10. только после этого зафиксировать terminal recovery state и удалить marker/journal.

Это применяется и к crash после `ConfigPromoted`, и к crash после `RuntimeVerified`, если commit LKG ещё не был доказан. Альтернативный roll-forward candidate допустим только как отдельный явно описанный протокол с доказанным candidate bundle; смешивать оба поведения нельзя.

## Порядок передачи в работу

Не нужно возвращать план на редакцию 6. Передать исполнителю:

1. исходный `implementation_plan.md` редакции 5;
2. этот файл как обязательные поправки;
3. выполнять строго по одному Gate;
4. после каждого Gate предоставить фактический diff и полный вывод заявленных тестов;
5. не переходить дальше при падении тестов;
6. не собирать IPK и не выполнять deploy без отдельной команды пользователя;
7. не изменять `wdtt` и `qwdtt`.

## Решение по запуску

- **Gate A: разрешён.**
- **Gate B: разрешён с recovery-последовательностью из этого файла.**
- **Gate C: разрешён только с typed observer API и разделением observed state / transaction metadata.**
- **Gate D и Gate E: после приёмки предыдущих Gate.**

Следующее ревью должно проверять уже реализацию и тестовые доказательства, а не очередной пересказ плана.
