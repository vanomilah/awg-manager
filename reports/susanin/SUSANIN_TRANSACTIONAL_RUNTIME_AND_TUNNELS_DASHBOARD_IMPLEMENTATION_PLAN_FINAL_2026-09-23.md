# Susanin: точный план исправления runtime и dashboard

Дата: 2026-09-23  
Репозиторий: `E:\AWGM\awg-manager`  
Режим: **выполнить код, не создавать новый план и не начинать очередной цикл ревью**.

## Цель и критерии готовности

Исправить небезопасный lifecycle Susanin и потерю Mihomo-ресурсов в общем dashboard. Готово только когда:

1. неуспешный `Apply` не оставляет `Enabled=true`, owner Susanin, PID/TUN или сетевые артефакты;
2. boot восстанавливает только последнюю успешно примененную конфигурацию;
3. policy scope никогда молча не превращается в all-LAN;
4. `running` подтвержден process + executor + interface + datapath inventory;
5. dashboard показывает AWG/AWG3/sing-box и все Mihomo proxy/subscription/provider/group;
6. тесты зелены без изменения ожидаемого поведения лишь ради теста.

## Жесткие ограничения

- Не собирать IPK, не деплоить и не подключаться к роутерам.
- Не использовать force-флаги установки или автоматический cleanup.
- Не делать reset/checkout/cleanup dirty worktree.
- Не менять Xray, Telegram proxy, AI assistant, WDTT/QWDTT и server-wizard.
- Не считать Susanin рабочим по одному TUN или PID.
- Не менять тест `system -> mixed` без отдельного доказанного решения.

## Почему предыдущий план нужно заменить

- Упомянуты несуществующие методы Store без миграции.
- `RestoreRoutingSlot()` ошибочно считается полным rollback: он не восстанавливает executor/datapath/applied generation.
- Не учтено, что текущий `UpdateState()` игнорирует ошибку записи.
- Нет атомарного commit applied/state.
- Предложено просто поменять тест Mihomo на `mixed` с неподтвержденным объяснением.
- Backend safety, dashboard и UX смешаны без stop-gates.
- Не выделена отдельная регрессия VOX subscription.

## Gate 0 — baseline

Сохранить в итоговый отчет `git status --short`, `git diff --stat` и результаты:

```bash
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/adaptiverouting ./internal/mihomo ./internal/api'
cd E:\AWGM\awg-manager\frontend
npm run check
npm test -- --run frontend/src/lib/utils/tunnelDashboardFlat.test.ts frontend/src/lib/components/routing/SusaninAdaptiveTab.test.ts
```

Известное падение `TestGenerateConfig_AdaptiveEgress` из-за `system/mixed` не маскировать до Gate 4.

## Gate 1 — Desired / Applied / Runtime

Файлы: `internal/adaptiverouting/types.go`, `store.go`, `store_test.go`.

Разделить desired (черновик), applied (последняя успешно примененная конфигурация) и operational state (наблюдаемый runtime).

Добавить `AppliedConfig{Generation string, Settings Settings}`.

Store API: `GetDesiredSettings`, `SaveDesiredSettings`, `GetApplied`, `CommitApplied`, `ClearApplied`, `SaveState`. Старые двусмысленные методы удалить либо оставить только временными wrappers; новая логика их не использует.

Записи: temp в том же каталоге, fsync, rename, 0600; ошибка записи не меняет in-memory state. Предпочтительно хранить applied+state в одном атомарном runtime-документе. Если два файла — обеспечить recovery предыдущей согласованной пары.

Миграция: `susanin.json` считать desired; legacy state без applied generation не запускать; legacy `running` без applied -> `recovery_required`; миграция идемпотентна.

Тесты: desired не меняет applied; write failure; atomic replacement; legacy migration; deep clone; race.

**Stop:** Store tests и race должны быть зелеными.

## Gate 2 — транзакционный Apply/Stop

Файлы: `service.go`, `executor.go`, `process.go`, новый `service_tx_test.go`.

Для failure injection заменить concrete dependencies Service узкими внутренними интерфейсами executor/datapath/process/installer/slot. Production-типы продолжают их реализовывать.

`Apply(ctx, desired)` под service mutex:

1. normalize/validate desired;
2. resolve egress и policy source до мутаций;
3. сохранить desired draft;
4. snapshot previous applied/state и runtime inventory;
5. executor Prepare;
6. park старого owner, запомнив успех этой попытки;
7. executor Commit, проверить interface exists/up;
8. создать datapath;
9. WriteConfigFiles — ошибка fatal;
10. EnsureInstalled — ошибка fatal;
11. Start — ошибка fatal;
12. health/inventory check;
13. только теперь atomically commit applied generation, running, RoutingOwnerSusanin.

Rollback: stop только нового процесса; убрать только новый datapath; rollback executor; restore slot только если эта попытка park; при previous applied реально восстановить его через `restoreAppliedLocked`; при неудаче сохранить previous applied и `recovery_required`; вернуть primary и rollback errors через `errors.Join`.

`Stop` также транзакционный: teardown -> restore slot -> commit disabled/clear applied. При частичной ошибке сохранить applied и recovery_required.

Failure tests для resolve/save/prepare/park/commit/interface/sets/chain/rules/config/install/start/health/store commit; проверить порядок rollback, owner, previous generation и идемпотентность.

## Gate 3 — fail-closed source и owned datapath

Файлы: `datapath.go`, `datapath_script.go`, `datapath_test.go`.

Policy mode:

- nil resolver/error/empty mark -> fatal до первой mutation command;
- ноль all-LAN jumps;
- проверить пересечение NDMS mark с Susanin marks;
- до live acceptance пометить policy mode experimental.

All-LAN использует только discovery/wiring interfaces. Жестко заданные LAN/router IP в `NewService` не authoritative.

Datapath:

- все chains/jumps/rules/routes/firewall additions имеют AWGM ownership;
- teardown удаляет только owned artifacts;
- удалить broad `INPUT -i <egress> ACCEPT`;
- FORWARD/MASQUERADE/TCPMSS добавлять только при доказанной необходимости и с точным cleanup;
- persistent hook не воскрешает disabled/stale generation.

Добавить `Inventory(ctx, expected)`: sets, chain+jump, ip rules, route table default, owned firewall, duplicates.

Тесты: resolver failure = zero mutations; policy/all-LAN distinct; чужие rules не удаляются; Ensure/Teardown idempotent; missing inventory items detected.

## Gate 4 — health, reconcile и boot

Файлы: `service.go`, `cmd/awg-manager/boot.go`, при необходимости wiring.

`GetStatus`/reconcile проверяет process, executor, interface, full datapath inventory, applied generation и owner.

Состояния:

- stopped: desired disabled и owned artifacts отсутствуют;
- running: весь inventory совпадает;
- degraded: applied есть, runtime неполон;
- recovery_required: конфликт или неполный rollback.

Boot читает только `GetApplied()`. Desired draft не запускается. Ошибка restore сохраняет recovery reason без бесконечного boot-loop.

### Mihomo stack

Не менять assertion на `mixed` ради зеленого теста. Для Susanin kernel routing baseline — `stack: system`. Исправить production config до контракта. Если нужен `mixed`, остановить Gate и оформить ADR с packet-flow, TCP+UDP test и доказательством отсутствия loop.

```bash
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/adaptiverouting ./internal/mihomo ./internal/api'
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/adaptiverouting'
```

До зеленого Gate 4 не переходить к UX и не готовить деплой.

## Gate 5 — полный dashboard Туннели

Файлы: `tunnelDashboardFlat.ts`, его test, `routes/+page.svelte`; переиспользовать существующие Mihomo cards/sections.

Добавить flat item kinds для текущих реальных типов:

- standalone Mihomo proxy;
- native Mihomo subscription/provider;
- Mihomo proxy group.

Передать `mihomoStandaloneProxies`, `mihomoSubscriptions`, `mihomoGroups` в builder. Stable key строить по resource ID, не display name.

Оба dashboard режима должны показывать типы, учитывать counts/search/tags/order, не скрывать groups через `!dashboardOn`, не делать запрос на каждую карточку и не дублировать subscription как sing-box.

Тест: по объекту каждого типа, count/kind/key/sort/no duplicates.

## Gate 6 — Susanin UX

Файлы: `SusaninAdaptiveTab.svelte`, test, store/API client.

Не маскировать backend race таймерами. Poll/refetch обновляет форму только если pristine или после successful Apply. Source/policy/egress/failure policy не отпрыгивают.

Primary view: status, source, egress, fail policy, Apply/Start/Stop, краткая статистика. Fwmark/table/priorities/intervals/TTL/cache — Expert drawer. Routing table ID не показывать обычному пользователю как обязательное поле.

Start доступен только после successful Apply с valid generation. Полные egress labels, inline persistent errors.

Тесты: policy и kill-switch переживают refetch; полный egress list; dirty draft не затирается; Apply sync; degraded/recovery блокирует Start.

## Gate 7 — отдельная регрессия VOX

Не смешивать с Susanin transaction:

- найти источник «30 серверов / ни одной валидной ссылки»;
- разделить last successful snapshot и latest refresh error;
- failed refresh не уничтожает LKG;
- UI показывает LKG time и latest error;
- добавить обезличенный parser fixture текущего формата VOX.

## Финальная локальная проверка

```bash
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/adaptiverouting ./internal/mihomo ./internal/api ./cmd/awg-manager'
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/adaptiverouting'
cd E:\AWGM\awg-manager\frontend
npm run check
npm test
npm run build
git diff --check
```

Не писать «все тесты проходят», если выполнен только targeted subset.

## Отчет на ревью

Один отчет в `reports/susanin/`: files/purpose, before-after state model, failure matrix, exact commands/exit codes/duration, remaining risks, diff stat/check и явная отметка: IPK/deploy/live traffic не выполнялись.

Не создавать новый implementation plan вместо кода. При blocker остановиться, указать file/line/test output и не переходить к следующему Gate.
