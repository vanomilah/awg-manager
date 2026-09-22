# Review плана Mihomo Gate B Revision 7

Дата: 2026-09-22  
Проверен файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Основание: `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV6_REAUDIT_2026-09-21.md` и фактический код рабочей директории.

## Вердикт

План правильно нацелен на оба блокера Revision 6, но **в текущем виде не готов к реализации**. Перед запуском необходимо исправить ownership-классификацию, способ fault injection и структуру production integration tests. Иначе агент может получить зелёные тесты при неверной обработке legacy owner и фиктивном withdraw.

После перечисленных ниже корректировок план можно отдавать в работу.

## Что в плане сделано правильно

- Fail-closed проверка координатора не ослабляется.
- Production `mihomoBridgeRuntime` должен реализовать `ExactBridgeRuntime` и получить compile-time assertion.
- `OwnerUUID` предполагается формировать стабильной функцией `BridgeOwnershipDescription(kind, id)` во всех producer-путях.
- Учтены `Apply`, `regenerateFromDesiredLocked` и rollback RuntimeOff.
- Предусмотрены coordinator-level tests, race test и cross-package regression.
- Сборка IPK и deployment отложены до повторного аудита.

## Обязательные исправления плана

### P0-PLAN-1. Нельзя одновременно присваивать legacy description полям LegacyOwner и OwnerUUID

В плане для `InspectBridge` сказано:

1. если description равен `awg-manager`, `awgm` или `ref.LegacyOwner`, заполнить `LegacyOwner`;
2. если description непустой, заполнить `OwnerUUID` тем же description.

Это сломает legacy migration. Координатор сначала проверяет `ObservedBridge.OwnerUUID`; если он непустой и не равен canonical `ref.OwnerUUID`, операция отклоняется до проверки `LegacyOwner`.

Нужна взаимоисключающая классификация:

```text
description отсутствует             -> Exists=false
description пустой                  -> Exists=true, OwnerUUID="", LegacyOwner="" (unmanaged)
description == canonical owner      -> OwnerUUID=description, LegacyOwner=""
description == допустимый legacy    -> OwnerUUID="", LegacyOwner=description
любое другое непустое description   -> OwnerUUID=description (foreign), LegacyOwner=""
```

Canonical owner должен вычисляться из **точно найденной записи native store**, а не приниматься на доверии из входного `ref`.

Добавить отдельные тесты для canonical, `awg-manager`, `awgm`, resource-specific legacy, empty/unmanaged и foreign description.

### P0-PLAN-2. Запрещены defaults при отсутствии точной записи native store

Формулировка `resolves listen port and canonical owner from native store (or defaults if absent)` является fail-open.

Если bridge нельзя однозначно сопоставить с native resource по `ProxyIndex` и идентичности интерфейсов, runtime не может доказать:

- owner token;
- listen port;
- соответствие ресурсу.

В таком случае `InspectBridge`, `PublishBridge` и `WithdrawBridge` должны завершаться typed error без NDMS mutation. Никаких default owner/port быть не должно.

Нужно определить единый helper, например `resolveOwnedBridge(ref)`, который:

- ищет ровно одну запись native store;
- проверяет `ProxyIndex`, `ProxyInterface`, `KernelInterface`;
- возвращает canonical owner, legacy owners и listen port;
- отклоняет отсутствие, дубликат или несовпадение входного ref.

### P0-PLAN-3. Предложенный stat failpoint не тестирует os.Stat error

План предлагает проверить boolean hook и вернуть ошибку **до вызова `os.Stat`**. Такой тест не доказывает, что production-код корректно классифицирует реальную ошибку stat.

Нужна инъекция результата самого stat, например:

```go
type CoordinatorConfig struct {
    // ...
    Stat func(string) (os.FileInfo, error)
}
```

с production default `os.Stat`, либо небольшой injectable filesystem interface. Тест должен вернуть из stat `fs.ErrPermission`/`EIO` после успешного unlink и пройти через ту же ветку классификации, что и production.

Минимально допустимый test-only hook должен подменять `statErr`, а не short-circuit до проверки.

Проверить три результата:

- `nil` — файл существует, fail closed;
- `os.ErrNotExist` — единственный успешный результат;
- другая ошибка — `recovery_required`, marker, commit запрещён.

### P0-PLAN-4. Fake RemoveProxyIfOwned сейчас делает тест withdraw фиктивным

Существующий `fakeNDMSRegistrarWithOwnership.RemoveProxyIfOwned` в `cmd/awg-manager/mihomo_bridge_runtime_test.go:210-212` всегда возвращает `true, nil`, но:

- не проверяет owner/legacy owner;
- не удаляет запись из `occupied`/`descriptions`;
- не способен доказать отказ удаления foreign bridge.

План должен явно потребовать полноценную реализацию fake:

- разрешить удаление только при совпадении canonical или разрешённого legacy owner;
- удалить состояние при успехе;
- вернуть `false, nil` при foreign/unmanaged owner;
- вести счётчик и журнал mutation calls.

Без этого шаги 5 и 6 integration test не имеют доказательной силы.

### P1-PLAN-5. Сценарии lifecycle нельзя выполнять последовательно одним coordinator

После foreign или unmanaged conflict координатор переходит в `StateRecoveryRequired`. Последующие шаги того же теста уже не являются чистым withdraw/use-case и могут быть заблокированы degraded-state.

Разбить тест на независимые subtests с новым temp dir, store, registrar, runtime и coordinator:

1. publish own + idempotent repeat;
2. publish foreign rejected;
3. publish unmanaged rejected;
4. withdraw own;
5. withdraw foreign rejected;
6. withdraw legacy own и миграция к canonical, если это поддерживаемый сценарий.

### P1-PLAN-6. Учесть gate/readiness двухфазной публикации

`gatedBridgeRegistrar.EnsureProxyIfOwned` не публикует ProxyN, пока gate закрыт. Coordinator выполняет точные операции после runtime/readiness, но integration test должен реально воспроизвести этот порядок, а не искусственно открыть gate без listener proof.

Тест обязан подтвердить:

- до readiness NDMS export отсутствует;
- после запуска соответствующего listener и `activate` gate открыт;
- coordinator exact inspection/publish видит фактическое NDMS состояние;
- при deactivation export удаляется.

Если test тестирует только exact methods изолированно, его нельзя называть production wiring lifecycle test.

### P1-PLAN-7. Не переводить manifest в RecoveryRequired дважды перед handleApplyFailureLocked

В предложенном Apply-псевдокоде сначала вручную выполняются `setState`, marker и `transitionManifestLocked(...StateRecoveryRequired)`, а затем вызывается `handleApplyFailureLocked`, который начинает `rollbackActiveLocked` и переводит manifest в `StateRollbackInProgress`.

Это может конфликтовать с state machine и изменить семантику автоматического rollback.

Перед реализацией выбрать один контракт:

- либо ошибка postcondition считается recoverable apply failure и передаётся в `handleApplyFailureLocked`, который выполняет и проверяет rollback;
- либо неопределённое состояние считается сразу manual recovery и используется отдельный helper без попытки обычного rollback.

Не смешивать оба механизма. Добавить тест состояния manifest и состояния active config/process после ошибки, а не только `IsDegraded()`.

### P2-PLAN-8. Документы уже существуют

В разделе Documentation файлы resolution report и patch помечены `[NEW]`, хотя они уже существуют. Нужно `[MODIFY]/regenerate`. Это не дефект production-кода, но защищает от создания дубликатов или неверной истории.

## Дополненный минимальный acceptance набор

### Compile-time

```go
var _ mihomo.BridgeRuntime = (*mihomoBridgeRuntime)(nil)
var _ mihomo.ExactBridgeRuntime = (*mihomoBridgeRuntime)(nil)
```

### Ownership matrix

Для `InspectBridge`, publish и withdraw проверить:

| NDMS state | Ожидаемый результат |
|---|---|
| отсутствует | `Exists=false`; publish разрешён только при точной store identity |
| canonical owner | own, операция разрешена |
| разрешённый legacy owner | legacy own, строго предусмотренная миграция/удаление |
| empty description | unmanaged, fail closed |
| foreign description | foreign, fail closed |
| LookupProxy error | fail closed, 0 mutations |
| store mapping absent/ambiguous | fail closed, 0 mutations |

### RuntimeOff absence proof

Во всех трёх путях (`Apply`, regenerate, rollback) проверить `exists`, `ENOENT`, permission/I/O error. Только `ENOENT` означает доказанное отсутствие.

### Production integration

- production runtime и fake NDMS с реальной owner semantics;
- отдельный fixture на каждый destructive/failure scenario;
- счётчики mutation calls;
- gate/readiness ordering;
- marker + manifest state + active config + process postcondition;
- идемпотентный повтор.

## Рекомендуемый порядок реализации

1. Ввести строгий `resolveOwnedBridge` и ownership classifier с unit tests.
2. Реализовать production ExactBridgeRuntime и compile-time assertion.
3. Исправить stateful NDMS fake.
4. Добавить независимые production integration subtests.
5. Ввести настоящую stat dependency/fault injection.
6. Исправить три RuntimeOff absence checks и определить единый recovery contract.
7. Запустить targeted, full, race, cmd/wiring и cross-package suites.
8. Обновить отчёт/patch только после независимого реаудита.

## Решение о запуске

**Не запускать текущую редакцию плана без правок.** После включения P0-PLAN-1..4 и уточнения P1-PLAN-5..7 план можно отдавать агенту. Основное направление верное; проблема не в объёме, а в доказательности ownership и негативных тестов.
