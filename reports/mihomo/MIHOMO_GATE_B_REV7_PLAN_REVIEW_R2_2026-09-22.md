# Повторное review плана Mihomo Gate B Revision 7

Дата: 2026-09-22  
Проверен обновлённый `implementation_plan.md`.

## Вердикт

План существенно улучшен и учёл основную часть предыдущего review. Однако перед реализацией нужны ещё четыре корректировки, одна из которых закрывает новый production-блокер в самом coordinator lifecycle.

**Статус: почти готов, но текущую редакцию ещё не запускать.**

## Что исправлено в плане

- ownership classification стала взаимоисключающей;
- defaults owner/port запрещены;
- появился `resolveOwnedBridge`;
- предусмотрена настоящая stat dependency;
- NDMS fake должен проверять ownership и изменять состояние;
- failure-сценарии разделены на независимые fixtures;
- учтены gate/readiness и compile-time Exact assertion;
- убрано смешение ручного RecoveryRequired с `handleApplyFailureLocked` в Apply-пути;
- существующие отчёты правильно помечены как MODIFY.

## Обязательные дополнения

### P0-PLAN-R2-1. Retained bridge вообще не проходит ownership inspection

Текущий `syncBridgesLocked` формирует операции только для разницы множеств:

- bridge есть в target, но отсутствует в before → create;
- bridge есть в before, но отсутствует в target → withdraw.

Если bridge присутствует и в before, и в target с тем же `KernelInterface`, код не вызывает ни `verifyBridgePublishAllowedLocked`, ни `InspectBridge`, ни `VerifyBridges`.

Следствие: после успешного применения внешний процесс может удалить ProxyN или заменить его owner. Следующая транзакция с тем же target сочтёт bridge неизменившимся и зафиксирует успех, не обнаружив missing/foreign/unmanaged OS state.

Новый production ExactBridgeRuntime этого автоматически не исправляет: его методы просто не вызываются для retained bridge.

Требуемое изменение плана:

1. В `syncBridgesLocked` выделить множество retained bridges.
2. Для каждого retained bridge выполнить exact inspection до commit.
3. Требовать:
   - `Exists == true`;
   - canonical `OwnerUUID == target.OwnerUUID`, либо явно разрешённый legacy owner с контролируемой миграцией;
   - отсутствие foreign/unmanaged state.
4. Missing retained bridge нельзя молча считать корректным. Нужно либо безопасно republish через тот же intent/checkpoint lifecycle, либо fail closed; выбрать и документировать один контракт.
5. Добавить независимые тесты:
   - retained canonical bridge принимается;
   - retained bridge исчез из NDMS;
   - retained bridge захвачен foreign owner;
   - retained bridge стал unmanaged;
   - inspection error.

Рекомендуется безопасный self-heal для missing own bridge только через durable bridge operation intent; foreign/unmanaged всегда fail closed.

### P0-PLAN-R2-2. resolveOwnedBridge должен выполняться до раннего Exists=false

В описании `InspectBridge` сначала вызывается `LookupProxy`, а при `!exists` немедленно возвращается `Exists=false`. Но далее matrix test требует, чтобы unmapped store bridge завершался typed error.

Эти требования противоречат друг другу: произвольный ref, которого нет ни в store, ни в NDMS, пройдёт как корректный `Exists=false`.

Исправить порядок:

1. Сначала `resolveOwnedBridge(ref)` и полная проверка store identity.
2. Затем `LookupProxy`.
3. Только для доказанно принадлежащего desired/store bridge допускается `Exists=false`.

Так `InspectBridge`, `PublishBridge` и `WithdrawBridge` используют один и тот же строгий identity contract.

### P0-PLAN-R2-3. Глобальные legacy owner должны передаваться и в mutation API

Ownership classifier признаёт `awg-manager` и `awgm` допустимыми legacy owner, но `resolveOwnedBridge` по плану возвращает только `nb.LegacyOwner`.

Получится рассогласование:

1. `InspectBridge` классифицирует `awg-manager` как собственный legacy bridge;
2. coordinator разрешает operation;
3. `EnsureProxyIfOwned`/`RemoveProxyIfOwned` не получают `awg-manager` в `legacyOwners`;
4. registrar возвращает `owned/removed == false`.

`resolveOwnedBridge` должен формировать дедуплицированный набор:

```text
awg-manager
awgm
nb.LegacyOwner (если непустой и не равен canonical)
```

Либо глобальные legacy tokens должны быть централизованы в одной общей функции, используемой classifier и mutation API. Не дублировать списки литералами в разных методах.

### P1-PLAN-R2-4. Не нужны одновременно CoordinatorConfig.Stat и StatHook

План вводит сразу две точки инъекции:

- `CoordinatorConfig.Stat`;
- `ApplyCoordinatorHooks.StatHook`.

При этом helper описан как вызывающий `c.stat`, а точная роль `StatHook` не определена. Две независимые точки создают неоднозначность порядка и риск тестирования не production-пути.

Оставить одну dependency:

```go
Stat func(string) (os.FileInfo, error)
```

с default `os.Stat`. Тесты должны подставлять её через `CoordinatorConfig`. Этого достаточно для `EACCES`, `EIO`, exists и `ENOENT`.

Если hook всё же необходим для существующей crash framework, helper обязан всегда получать конечный результат через одну функцию, а тест должен доказывать тот же classification path. Предпочтительный вариант — удалить лишний hook.

## Дополнительное усиление postcondition create

После `PublishBridge` coordinator сейчас проверяет только `obs.Exists`. Для строгого Exact contract этого недостаточно. Postcondition должна также подтвердить canonical ownership:

```text
Exists == true
OwnerUUID == ref.OwnerUUID
LegacyOwner == "" после canonical publication
```

Иначе ошибочная реализация runtime, создавшая/оставившая чужой bridge, может пройти postcondition только по признаку существования.

Добавить тест: `PublishBridge` возвращает nil, но повторный Inspect показывает foreign owner — transaction обязана завершиться fail closed.

## Уточнение integration tests

Для `publish_own_and_idempotent_repeat` различать:

- отсутствие дополнительных coordinator bridge operations;
- допустимые read-only inspections;
- отсутствие дополнительных NDMS mutations.

Для legacy migration отдельно доказать, что после publish description заменён на canonical owner, а не только принят как допустимый legacy.

## Окончательный набор изменений к плану

Перед запуском добавить:

1. retained bridge ownership verification/self-heal policy в `syncBridgesLocked`;
2. store resolution до `LookupProxy` early return;
3. единый набор global + resource-specific legacy owners;
4. одну, а не две stat dependency;
5. проверку canonical owner в create postcondition;
6. негативные тесты retained/missing/foreign/unmanaged и false-success publish.

После этих дополнений план можно запускать в реализацию. Остальная структура Revision 7 пригодна и соответствует предыдущему аудиту.
