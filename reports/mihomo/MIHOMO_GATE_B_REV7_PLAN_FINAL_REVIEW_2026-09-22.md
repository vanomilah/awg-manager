# Финальное review плана Mihomo Gate B Revision 7

Дата: 2026-09-22  
Проверена третья редакция `implementation_plan.md`.

## Вердикт

План теперь технически цельный и закрывает почти все замечания двух предыдущих review. До запуска нужно добавить один обязательный блок совместимости с уже существующими generation records и два небольших уточнения контракта.

После внесения раздела **Legacy persisted bridge ownership migration** план можно запускать без дополнительного архитектурного review.

## Подтверждённо учтено

- production `mihomoBridgeRuntime` реализует полный Exact contract;
- compile-time assertion предусмотрен;
- store identity разрешается до NDMS inspection;
- defaults запрещены;
- global и resource-specific legacy owners централизованы;
- retained bridges инспектируются;
- missing retained bridge проходит через durable create intent;
- foreign/unmanaged retained bridge останавливает транзакцию;
- create postcondition требует canonical owner;
- используется одна stat dependency;
- NDMS fake становится stateful и ownership-aware;
- destructive/failure tests разделены на независимые fixtures;
- gate/readiness входит в production integration test;
- deployment и IPK отложены до реаудита.

## Последний обязательный блокер плана

### P0. Совместимость со старыми persisted BridgeRef без OwnerUUID

До Revision 7 следующие producer-пути сохраняли bridge без `OwnerUUID`:

- `StoreTxAdapter.ListBridges()`;
- `AssembleCompileInput()`;
- production `ListActiveBridges()`;
- соответственно, старые `verified-active.json`, `lkg.pointer` generation bundles и `AppliedGenerationRecord.AppliedBridges` могут содержать только `LegacyOwner` либо вообще пустые ownership-поля.

Новые target bridges будут содержать canonical `OwnerUUID`, поэтому обычный forward reconcile чаще всего сможет использовать target-версию ref. Но recovery/rollback читает bridge refs из старого generation manifest. Для такого ref:

```text
ref.OwnerUUID == ""
ObservedBridge.OwnerUUID == "awg-manager:mihomo:<kind>:<id>"
```

Строгое сравнение классифицирует реально собственный bridge как foreign. В результате rollback существующей установки может перейти в `recovery_required` именно тогда, когда должен восстанавливать LKG.

### Требуемое дополнение

Добавить в план отдельную стратегию enrichment/migration:

1. Не доверять пустому owner и не разрешать его wildcard-сравнением.
2. Обогащать legacy `BridgeRef` только через точное уникальное сопоставление с текущим authoritative native store по `ProxyIndex`, `ProxyInterface`, `KernelInterface`.
3. Получать canonical owner через `BridgeOwnershipDescription(kind, id)`.
4. Использовать обогащённую локальную копию refs перед retained/create/withdraw comparison; не менять digest старой immutable generation задним числом.
5. Если store mapping отсутствует или неоднозначен — fail closed, а не принимать пустой owner.
6. Документировать поведение, если ресурс был удалён из текущего store, но требуется rollback старой generation. Нужен отдельный recovery source ownership metadata либо явный manual recovery; молчаливое удаление запрещено.

### Обязательные тесты обновления

- startup с legacy `verified-active.json`, где `OwnerUUID` отсутствует;
- forward reconcile legacy applied record → canonical target;
- rollback к legacy generation при существующем точном store mapping;
- rollback к legacy generation при отсутствующем store mapping;
- legacy custom owner migration;
- foreign NDMS description при legacy persisted ref остаётся rejected.

Это должны быть тесты данных старого формата, а не структуры, уже заполненной новым producer.

## Уточнение 1. Exact contract проверять до любой bridge reconciliation

В retained-разделе план говорит `exactRuntime.InspectBridge`, но должен явно определить получение `exactRuntime`.

В начале `syncBridgesLocked`, если `before` или `target` содержит хотя бы один bridge:

```text
BridgeRuntime != nil
BridgeRuntime implements ExactBridgeRuntime
```

Иначе fail closed до построения/сохранения mutation intent. Пустые before+target могут завершаться без runtime.

Это исключит разное поведение delta и retained ветвей.

## Уточнение 2. Postcondition должна проверять target canonical owner после enrichment

Проверка после publish:

```text
obs.Exists
obs.OwnerUUID == ref.OwnerUUID
obs.LegacyOwner == ""
```

корректна только после гарантированного enrichment `ref.OwnerUUID`. Перед созданием bridge нужно отдельно запрещать пустой target OwnerUUID. Иначе пустое значение может сделать проверку двусмысленной либо гарантированно сломать upgrade.

Добавить invariant:

```text
all bridge refs entering trackOp have non-empty canonical OwnerUUID
```

с typed fail-closed error и тестом.

## Дополнительная рекомендация

Глобальные legacy tokens `awg-manager`/`awgm` исторически недостаточно специфичны. Их разрешение допустимо только вместе с точным native-store mapping и совпадением всех bridge identity fields. План уже движется в эту сторону; тест должен подтвердить, что совпадение одного ProxyIndex недостаточно при конфликтующих interface fields.

## Решение о запуске

Текущая редакция: **условно одобрена**.

Перед передачей в реализацию добавить:

1. раздел legacy persisted ownership enrichment/migration;
2. upgrade/rollback tests старого формата;
3. раннюю обязательную Exact-проверку;
4. invariant непустого canonical OwnerUUID перед `trackOp`.

После этих четырёх дополнений план можно запускать. Повторное согласование остальных разделов не требуется.
