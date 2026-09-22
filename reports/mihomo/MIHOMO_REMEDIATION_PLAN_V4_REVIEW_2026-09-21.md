# Ревью Mihomo Remediation Plan — редакция 4

Проверен документ:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Дата: 2026-09-21.

## Вердикт

Четыре замечания предыдущего ревью исправлены. При проверке production wiring обнаружен один новый существенный блокер: запланированный строгий Bridge Proof не может работать на роутере, потому что реальная реализация bridge runtime не поддерживает `ExactBridgeRuntime`.

Дополнительно нужны две небольшие текстовые корректировки mutation inventory и generation persistence.

Статус: **одобрить после добавления production ExactBridgeRuntime и двух уточнений ниже**.

## Исправления предыдущего ревью подтверждены

- Выбор verifier больше не зависит от `Operator.Binary()`/`LookPath`.
- Сохранён совместимый TCP wrapper и добавляется отдельный network-aware procnet API.
- Inventory HTTP routes значительно расширен и разделён по категориям.
- Refresh вынесен в узкий `Operator.RefreshProvider`.
- Bridge port проверяется через ListenerSpec/procfs.

## Блокирующее замечание

### Production `mihomoBridgeRuntime` не реализует `ExactBridgeRuntime`

Файл:

`cmd/awg-manager/mihomo_bridge_runtime.go`

Сейчас production-тип имеет только compile-time assertion:

```go
var _ mihomo.BridgeRuntime = (*mihomoBridgeRuntime)(nil)
```

Он реализует:

- `ApplyBridges`;
- `WithdrawBridges`;
- `VerifyBridges`;
- `ListActiveBridges`.

Но не реализует:

- `PublishBridge`;
- `WithdrawBridge`;
- `InspectBridge`;
- `ListObservedBridges`.

Поэтому в `ApplyCoordinator.syncBridgesLocked` проверка:

```go
exactRuntime, isExact := c.cfg.BridgeRuntime.(ExactBridgeRuntime)
```

на реальном роутере всегда даёт `isExact == false`. Строгие проверки owner, exact BridgeRef, `Up` и `AssignedIP` работают только в `gate2_test.go` на mock-реализации.

Более того:

- production `ApplyBridges` игнорирует переданный список и вызывает общий `manager.Reconcile(ctx, nil)`;
- `VerifyBridges` доказывает только наличие listener ports;
- `ListActiveBridges` строит результат из desired native store, а не из наблюдаемого состояния ОС;
- `ListActiveBridges` сейчас не заполняет `OwnerUUID` и `Generation`.

Таким образом, без изменения production adapter Gate C создаст иллюзию строгого proof, но фактически coordinator продолжит выполнять broad reconciliation без exact ownership postconditions.

## Что добавить в Gate C

### Production Exact Bridge Runtime

Добавить изменение файла:

`cmd/awg-manager/mihomo_bridge_runtime.go`

Требования:

1. Реализовать `mihomo.ExactBridgeRuntime` на `mihomoBridgeRuntime`:

```go
var _ mihomo.ExactBridgeRuntime = (*mihomoBridgeRuntime)(nil)
```

2. `PublishBridge(ctx, ref)` должен применять только указанный bridge и не выполнять неограниченный reconcile всех записей.
3. `WithdrawBridge(ctx, ref)` должен удалять только bridge с совпадающим canonical owner/OwnerUUID.
4. `InspectBridge(ctx, ref)` должен читать фактическое состояние NDMS/ядра, а не просто native store:
   - существует ли proxy/kernel interface;
   - поднят ли интерфейс;
   - назначенный IP;
   - фактический owner/description;
   - фактические индексы/имена интерфейсов.
5. `ListObservedBridges` должен возвращать наблюдаемое состояние системы.
6. `ListActiveBridges` должен сохранять `OwnerUUID` и `Generation`, если остаётся частью compatibility API.
7. Ошибка или невозможность доказать owner должна быть fail-closed, а не трактоваться как отсутствие конфликта.

Если текущий NDMS adapter не может вернуть все необходимые факты, нужно сначала расширить его read-only API. Нельзя заполнять `ObservedBridge` значениями из того же desired store, который coordinator пытается проверить — это не независимое доказательство.

### Тесты production adapter

Добавить тесты непосредственно для `cmd/awg-manager/mihomo_bridge_runtime.go` с fake NDMS/backend:

- publish одного bridge не меняет соседние bridges;
- withdraw не удаляет foreign owner;
- empty/unknown owner блокируется;
- InspectBridge различает desired и observed state;
- интерфейс отсутствует;
- интерфейс существует, но down;
- AssignedIP отсутствует/невалиден;
- exact BridgeRef mismatch;
- restart/reconcile сохраняет OwnerUUID и Generation.

## Два обязательных уточнения

### 1. Имена AI remediation actions в плане не соответствуют коду

В коде нет функций:

```text
remediateMihomoProviderRefresh
remediateMihomoRestart
```

Фактические action identifiers:

- `mihomo.restart`;
- `mihomo.reload`;
- `subscription.update`.

`subscription.update` через server wiring может вызвать `MihomoHandler.RefreshNativeSubscription`.

В Gate D следует перечислить реальные action identifiers и проверить весь путь proposal confirmation → remediation executor → server callback → mutation gate. Иначе тест может покрыть выдуманные имена, а реальный обход останется.

### 2. Формулировка byte equality record и pointer некорректна

`verified-active.json` и `lkg.pointer.json` имеют разные схемы и не могут быть побайтово равны друг другу.

Нужно записать точный контракт:

- bytes `verified-active.json` после чтения и canonical serialization соответствуют `rec`;
- parsed LKG pointer совпадает с подмножеством полей `rec`: generation ID/number, config digest, store digest и epoch;
- bundle manifest/config/store snapshot отдельно сверяются по digest;
- при `advanceLKG=false` pointer не перезаписывается, а только проверяется против target LKG record.

## Итоговое решение

После включения production `ExactBridgeRuntime` в Gate C и двух уточнений выше план можно запускать без очередного полного архитектурного ревью.

Дальнейшая приёмка должна выполняться уже по результатам каждого Gate:

1. фактический diff;
2. targeted tests;
3. race tests;
4. подтверждение отсутствия изменений `wdtt`/`qwdtt`;
5. отсутствие IPK/deploy без отдельного запроса.
