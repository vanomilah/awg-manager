# Implementation Plan: Mihomo False Recovery & Susanin Acceptance Remediation

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**References:**
- [`reports/mihomo/MIHOMO_FALSE_RECOVERY_AFTER_UPDATE_REMEDIATION_PLAN_2026-09-22.md`](file:///e:/AWGM/awg-manager/reports/mihomo/MIHOMO_FALSE_RECOVERY_AFTER_UPDATE_REMEDIATION_PLAN_2026-09-22.md)
- [`reports/susanin/SUSANIN_UI_TUNNELS_PERFORMANCE_ACCEPTANCE_REVIEW_2026-09-22.md`](file:///e:/AWGM/awg-manager/reports/susanin/SUSANIN_UI_TUNNELS_PERFORMANCE_ACCEPTANCE_REVIEW_2026-09-22.md)

---

## User Review Required

> [!IMPORTANT]
> **Prohibition Confirmation:** Per user instructions, **NO IPK will be built and NO router deployment will be performed**. All verification will be executed locally in WSL (Ubuntu) and Windows test runners.
> 
> **Mihomo Digest Transition:** We are decoupling configuration digest (`DesiredConfigDigest`) from storage metadata (`StoreSnapshotDigest`). Subscriptions refreshing runtime state (`LastFetched`, `LastError`) will no longer invalidate preflight check or cause false `RecoveryRequired` mode.

---

## Proposed Changes

### 1. Susanin Backend Datapath Isolation & WSL Tests Pass (P0)

#### [MODIFY] [internal/adaptiverouting/service.go](file:///e:/AWGM/awg-manager/internal/adaptiverouting/service.go)
- Ensure `Service` cleanly supports injecting a custom `DatapathController` or mock command runner so unit/handler tests run in non-root environments without `/opt/sbin/ipset`.
- Ensure `ClearCache` and `Forget` handle nil/mock datapath safely.

#### [MODIFY] [internal/api/adaptive_routing_handler_test.go](file:///e:/AWGM/awg-manager/internal/api/adaptive_routing_handler_test.go)
- In `setupTestAdaptiveRoutingHandler`: inject a mock `DatapathController` using `sysexec.Result{ExitCode: 0}` runner.
- Ensure all tests in `adaptive_routing_handler_test.go` pass without executing `/opt/sbin/ipset`.

---

### 2. Mihomo False Recovery Remediation (P0)

#### [MODIFY] [internal/mihomonative/store.go](file:///e:/AWGM/awg-manager/internal/mihomonative/store.go)
- Implement `CurrentDesiredDigest() (string, error)`:
  - Projects canonical desired state: proxies, subscriptions (excluding `LastFetched`, `LastError`, `UpdatedAt`, `CreatedAt`), groups, rules (preserving order), rule providers, bridge allocations.
  - Sorts non-ordered collections (proxies by ID, groups by Name, providers by Name) and computes SHA-256 hex digest.
- Implement `CurrentSnapshotDigest() (string, error)`: computes exact bytes SHA-256 of persisted `state` JSON.
- Retain `CurrentDigest()` returning `CurrentDesiredDigest()` for backward compatibility.
- Ensure `RecordSubscriptionRefresh` updates `LastFetched`, `LastError`, and `UpdatedAt` in the store without altering `CurrentDesiredDigest()`.

#### [MODIFY] [internal/mihomo/coordinator.go](file:///e:/AWGM/awg-manager/internal/mihomo/coordinator.go)
- In preflight check (line 2418): compare `c.appliedRecord.AppliedStoreDigest` against `nativeStore.CurrentDesiredDigest()`.
- Distinguish between desired config mismatch and exact snapshot verification.

#### [MODIFY] [internal/mihomonative/store_test.go](file:///e:/AWGM/awg-manager/internal/mihomonative/store_test.go)
- Add tests:
  - `TestStore_DesiredDigest_SubscriptionRefresh`: verifies that `RecordSubscriptionRefresh` changes `CurrentSnapshotDigest` but preserves `CurrentDesiredDigest`.
  - `TestStore_DesiredDigest_RuleMutation`: verifies that editing a rule or changing rule ordering changes `CurrentDesiredDigest`.

---

### 3. Tunnels Snapshot Hard-Timeout, Singleflight & Copy Protection (P1, P2)

#### [MODIFY] [internal/api/snapshot.go](file:///e:/AWGM/awg-manager/internal/api/snapshot.go)
- **Channel Deadline & Worker Decoupling:** Instead of unbounded `wg.Wait()` on workers that might ignore context cancellation, execute worker goroutines with channel result delivery and hard `select` timeout. If a worker hangs, the handler returns the stale cache within the hard 1500ms budget.
- **Singleflight:** Add a concurrency singleflight coordinator for cache refresh (`external` and `system`). Concurrent requests while a refresh is in-flight will share the result rather than hammering NDMS/RCI simultaneously.
- **Defensive Copying:** Deep-copy or slice-copy cached elements before returning from `Build()` and before writing to cache, preventing race conditions or caller mutations.

#### [MODIFY] [internal/api/snapshot_test.go](file:///e:/AWGM/awg-manager/internal/api/snapshot_test.go)
- Add concurrency tests:
  - 25 concurrent `Build()` requests verify singleflight execution (underlying slow provider called only once).
  - Unresponsive provider test: provider that ignores `ctx.Done()` does not block `Build()` past hard timeout.
  - Race test: concurrent reads and cache invalidation under `go test -race`.

---

### 4. Frontend Mihomo Inventory vs Runtime Stores Separation (P1)

#### [MODIFY] [frontend/src/lib/stores/mihomoNative.ts](file:///e:/AWGM/awg-manager/frontend/src/lib/stores/mihomoNative.ts)
- Separate polling stores:
  - `mihomoInventoryStore`: polls only `fetchMihomoInventory()` (proxies, subscriptions, groups). Default poll interval: 15s.
  - `mihomoRuntimeStore`: polls `fetchMihomoRuntime()` (Clash runtime proxies, providers). Polling interval: 10s.
  - Keep `mihomoNativeResources` as derived or backward-compatible composite store for existing callers.

#### [MODIFY] [frontend/src/routes/+page.svelte](file:///e:/AWGM/awg-manager/frontend/src/routes/+page.svelte)
- Update tab subscriptions:
  - Subscriptions and Groups tabs subscribe only to `mihomoInventoryStore`.
  - Runtime details are loaded lazily when needed.

---

### 5. Susanin UI Accessibility (a11y) Warnings (P2)

#### [MODIFY] [frontend/src/lib/components/routing/SusaninAdaptiveTab.svelte](file:///e:/AWGM/awg-manager/frontend/src/lib/components/routing/SusaninAdaptiveTab.svelte)
- Associate all form `<label>` tags with their respective input/select/textarea controls via `for="id"` and matching `id="..."` attributes on lines 470, 706, 716, 726, 768, 783, 830, 839, 848, 857, 866.
- Run `npm run check` to verify that 0 accessibility warnings remain in this file.

---

## Verification Plan

### Automated Tests
1. **WSL Go Tests (Ubuntu):**
   ```bash
   wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/adaptiverouting ./internal/api"
   wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/adaptiverouting ./internal/api"
   wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/mihomo ./internal/mihomonative"
   ```
2. **Frontend Type Check:**
   ```bash
   cd frontend && npm run check
   ```
3. **Frontend Vitest Component Tests:**
   ```bash
   cd frontend && npx vitest run src/lib/components/tunnels/ProxyGroupsTabSection.test.ts src/lib/components/routing/SusaninAdaptiveTab.test.ts
   ```
4. **Git Formatting:**
   ```bash
   git diff --check
   ```
5. **Frontend Build:**
   ```bash
   cd frontend && npm run build
   ```

---

## 6. Mihomo Interactive Rule Mutation Performance (P0)

### Подтверждённая проблема

Добавление, изменение, удаление и перестановка одного правила Mihomo сейчас ощущаются значительно медленнее аналогичных операций sing-box.

Текущая цепочка объясняет задержку:

1. frontend вызывает `mihomoNativeSaveRule`, `mihomoNativeDeleteRule` или `mihomoNativeReorderRules` с `apply=true`;
2. каждый запрос проходит через `NativeMutationApplier.ApplyNativeMutation`;
3. координатор создаёт полноценную транзакцию, snapshot/manifest, повторно компилирует конфигурацию и выполняет runtime apply с проверками;
4. тяжёлый путь может останавливать и заново запускать Mihomo с проверкой PID, digest и сокетов;
5. после успешного ответа `MihomoPolicyPanel.svelte` для create/update/delete дополнительно вызывает общий `load()` и повторно запрашивает все группы, правила, providers и связанные данные;
6. `moveRule()` сначала ждёт полный server apply и лишь затем присваивает `rules = next`, поэтому даже визуальный отклик интерфейса блокируется сетевой и runtime-задержкой;
7. серия нажатий «вверх/вниз» создаёт отдельную полную транзакцию на каждый клик.

Это не следует исправлять отключением transactional safety, digest-проверок или rollback. Нужен отдельный безопасный fast path для изменений, которые не меняют listeners, bridge-интерфейсы, proxy topology или режим захвата.

### 6.1 Сначала добавить измеримость

#### [MODIFY] `internal/api/mihomo_handler.go`

- Для mutation endpoints возвращать `Server-Timing` с фазами:
  - `lock`;
  - `snapshot`;
  - `mutate`;
  - `compile`;
  - `validate`;
  - `runtime_apply`;
  - `verify`;
  - `commit`;
  - `rollback`, если выполнялся.
- Добавить безопасный correlation/transaction ID в лог и response header.
- Не логировать URI прокси, ключи, UUID, credentials и полное содержимое правил.

#### [MODIFY] `internal/mihomo/coordinator.go`

- Инструментировать фазы транзакции без изменения порядка safety-операций.
- Зафиксировать отдельно время остановки/старта процесса и ожидания readiness.
- Добавить счётчик выбранного пути: `hot_reload`, `full_restart`, `draft_only`, `rollback`.

#### Acceptance

- В браузере и журнале можно однозначно увидеть, какая фаза занимает время.
- Итоговый отчёт содержит сырые результаты минимум 20 операций каждого вида на целевом aarch64-роутере.

### 6.2 Ввести классификацию изменений

#### [ADD] `internal/mihomo/change_kind.go` (или эквивалент внутри coordinator)

Определить строгие категории:

- `RuleOnly`: create/update/delete/reorder обычных rules;
- `RuleProviderContent`: изменение rule-provider без изменения listeners/bridges;
- `ProxyGraph`: proxy/group/subscription topology;
- `ListenerTopology`: ports, TUN/TProxy, mixed/http/socks listeners, bridge exports;
- `EngineMode`: запуск, остановка и смена режима движка.

Только доказанный `RuleOnly` разрешает fast path. Неизвестное изменение всегда использует полный безопасный apply.

Классификация должна вычисляться backend-ом по pre/post desired state, а не приниматься на доверии из frontend.

### 6.3 Безопасный rule-only hot reload

#### [MODIFY] `internal/mihomo/coordinator.go`

Добавить транзакционный `ApplyRuleMutation`/`ApplyMutation(kind, mutateFn)`:

1. получить межпроцессный и внутренний mutation lock;
2. проверить отсутствие recovery marker и незавершённой транзакции;
3. проверить desired/config digests текущего применённого поколения;
4. сохранить pre-mutation snapshot;
5. выполнить mutation;
6. сгенерировать candidate configuration ровно один раз;
7. выполнить штатную проверку candidate конфигурации бинарником Mihomo;
8. убедиться, что listener/bridge/process topology не изменилась;
9. применить candidate через controller reload API Mihomo (`PUT /configs?force=true` либо поддерживаемый текущей версией эквивалент), не убивая процесс;
10. проверить, что PID/daemon epoch не изменились, controller отвечает, обязательные listeners принадлежат тому же процессу, а runtime действительно принял новое поколение;
11. атомарно обновить generation bundle, verified-active/LKG metadata и desired/config digests;
12. при любой ошибке восстановить snapshot и предыдущую конфигурацию через тот же controller; если hot rollback не подтверждён — выполнить существующий полный rollback/restart;
13. только при невозможности безопасного hot reload автоматически перейти на существующий full restart path.

Нельзя просто вызвать controller API в обход coordinator: это снова разведёт persisted state, active config, applied record и LKG.

#### [MODIFY] operator/controller abstraction

- Вынести reload в тестируемый интерфейс, например `ConfigReloader`.
- Различать unsupported/unavailable, validation failure, timeout и runtime rejection.
- Установить конечный timeout; зависший controller не должен навечно удерживать mutation lock.
- Запрещён параллельный hot reload: операции сериализуются или безопасно объединяются до начала транзакции.

### 6.4 Backend endpoint для атомарного порядка и CAS

#### [MODIFY] `internal/api/mihomo_handler.go`

- `PUT /api/mihomo/native/rules/order` должен принимать:
  - полный итоговый массив IDs;
  - `baseRevision` текущего набора rules;
  - необязательный client operation ID для идемпотентного повтора.
- При устаревшей revision возвращать `409 MIHOMO_RULES_STALE` вместе с актуальными rules/revision.
- При успехе возвращать актуальные rules, revision, generation и тип apply path.
- Не заставлять frontend выполнять общий `load()` после успешной операции.

#### [OPTIONAL ADD] batch endpoint

Если пользователь быстро сделал несколько reorder-операций, frontend отправляет одно итоговое расположение. Для массового редактора разрешить один атомарный batch create/update/delete/order вместо серии reload.

### 6.5 Frontend: мгновенный UI без скрытых черновиков

#### [MODIFY] `frontend/src/routes/routing/MihomoPolicyPanel.svelte`

- Reorder:
  - сразу переставлять элементы локально;
  - объединять последовательные клики в последний итоговый порядок с debounce 250–400 мс;
  - одновременно иметь не более одного reorder request;
  - если во время запроса появились новые перемещения, после ответа отправлять только последнее итоговое состояние;
  - на ошибке/409 восстанавливать подтверждённое сервером состояние и показывать понятное уведомление.
- Create/update/delete:
  - обновлять локальную коллекцию из mutation response;
  - не вызывать общий `load()` после каждой успешной операции;
  - точечно обновлять только затронутые данные;
  - показывать неблокирующий статус `Применяется…` и подтверждённое состояние;
  - на rollback вернуть предыдущий список.
- Не превращать поведение в ручной draft: успешное изменение должно быть применено к Mihomo автоматически. Debounce допустим только для короткой серии reorder-команд.
- Блокировать конфликтующие destructive actions на время in-flight mutation, но не замораживать всю страницу.

#### [MODIFY] `frontend/src/lib/api/clientSbRouter.ts`

- Типизировать mutation response: `items`, `revision`, `generation`, `applyPath`, `transactionId`.
- Передавать `baseRevision` при reorder.
- Поддержать `AbortSignal` только до принятия mutation сервером; отмена HTTP-клиента не должна означать откат уже начавшейся серверной транзакции.

### 6.6 Не выполнять bridge reconciliation для RuleOnly

На rule-only пути не должны повторно создаваться, удаляться или проверяться ProxyN/bridge-интерфейсы, если pre/post topology digest совпадает.

При несовпадении topology digest fast path прекращается до runtime mutation и управление передаётся полному apply. Нельзя доверять только названию endpoint-а: проверяется реальный diff состояния.

### 6.7 Тесты

#### Backend unit/integration

- create/update/delete/reorder `RuleOnly` использует hot reload и не вызывает `StopAndWait`/`Start`;
- PID и daemon epoch сохраняются;
- candidate validation выполняется до runtime reload;
- topology change запрещает fast path;
- controller timeout приводит к контролируемому fallback/rollback;
- runtime reject восстанавливает store, active config и applied metadata;
- crash/failure в каждой фазе не оставляет manifest или ложный `RecoveryRequired`;
- два конкурентных reorder сериализуются без lost update;
- stale revision возвращает 409;
- повтор одного operation ID идемпотентен;
- полный restart path остаётся для listener/proxy/bridge изменений.

#### Frontend

- локальная перестановка видима до завершения HTTP;
- 10 быстрых кликов дают один или минимальное число итоговых API-вызовов;
- response не запускает общий `load()`;
- 409 обновляет список с сервера;
- failure возвращает последний подтверждённый порядок;
- create/delete изменяют только затронутую коллекцию.

### 6.8 Performance acceptance на роутере

Замеры делаются отдельно от сборки/локальных тестов и только после явного разрешения на деплой.

Целевые бюджеты после прогрева:

- локальный визуальный отклик reorder: **< 50 мс**;
- один итоговый reorder API: p50 **< 500 мс**, p95 **< 1000 мс**;
- create/update/delete rule API: p50 **< 750 мс**, p95 **< 1500 мс**;
- PID Mihomo не меняется для `RuleOnly` hot path;
- существующие соединения не обрываются;
- серия из 10 перемещений не создаёт 10 рестартов или 10 полных apply;
- после перезагрузки роутера порядок и правила сохраняются;
- при искусственной ошибке controller выполняется подтверждённый rollback без потери интернета.

Если железо не позволяет выдержать бюджеты, отчёт должен показать реальные `Server-Timing` и обосновать новый предел. Нельзя объявлять ускорение только по unit-тестам или субъективному ощущению.

### 6.9 Запреты и границы

- Не ослаблять digest/recovery/LKG invariants ради скорости.
- Не применять rules напрямую только в памяти Mihomo без атомарной фиксации поколения.
- Не выполнять fire-and-forget mutation без результата и rollback.
- Не использовать `apply=false` как постоянное поведение UI.
- Не собирать IPK и не выполнять деплой в рамках реализации этого плана без отдельного указания пользователя.
