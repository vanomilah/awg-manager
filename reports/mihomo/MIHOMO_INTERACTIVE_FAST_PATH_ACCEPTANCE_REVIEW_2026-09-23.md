# Mihomo Interactive Fast Path — независимое acceptance review

Дата: 2026-09-23  
Проверяемый отчёт: `MIHOMO_INTERACTIVE_FAST_PATH_AND_ACCEPTANCE_REPORT_2026-09-23.md`

## Вердикт

**НЕ ПРИНЯТО.** Формулировки `ACCEPTED & FULLY VERIFIED`, `under 50ms` и «все пять фаз завершены» не подтверждены.

Реализация содержит полезную основу: transactional hot reload, optimistic reorder, CAS revision, singleflight/cache и тесты. Однако найдены блокирующие ошибки в классификации fast path и digest-модели, а часть утверждений отчёта не соответствует коду.

Сборка IPK и деплой в рамках аудита не выполнялись.

## Подтверждённая проверка

```text
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/mihomonative ./internal/mihomo ./internal/api ./internal/adaptiverouting"
```

Результат: PASS для всех четырёх пакетов.

```text
npm run check
```

Результат: 0 errors, 117 warnings в 24 файлах.

```text
npx vitest run src/lib/components/tunnels/ProxyGroupsTabSection.test.ts src/lib/components/routing/SusaninAdaptiveTab.test.ts
```

Результат: 2 файла, 8/8 тестов PASS.

`git diff --check` завершён без ошибок.

## P0 — блокирующие дефекты

### 1. Ошибка сортировки listener-ов может разрешить небезопасный hot reload

Файл: `internal/mihomo/change_kind.go`, функция `ListenersEqual`, сортировка `cB`.

Текущий код сравнивает элементы второго списка с самими собой:

```go
if cB[i].Address != cB[i].Address { ... }
if cB[i].Port != cB[i].Port { ... }
if cB[i].GetNetwork() != cB[i].GetNetwork() { ... }
```

Эти условия всегда ложны. Фактически `cB` сортируется только по `Purpose`. При различном исходном порядке эквивалентные наборы могут быть признаны различными, а опаснее — разные наборы могут сравниваться в неверном сопоставлении.

Исправить сравнения на `cB[i]` против `cB[j]`. Добавить table-driven тесты:

- одинаковые listeners в разном порядке;
- изменённый address;
- изменённый port;
- изменённый network/family/purpose;
- дубликаты;
- IPv4/IPv6.

### 2. False Recovery digest separation фактически не подключена к coordinator

В store добавлены `CurrentDesiredDigest()` и `CurrentSnapshotDigest()`, но:

- `CurrentDigest()` возвращает полный `CurrentSnapshotDigest()`;
- `ApplyMutationWithOutcome()` вызывает `StoreTx.CurrentDigest()` в preflight;
- полученное значение сравнивается с `AppliedRecord.AppliedStoreDigest`;
- то же значение сохраняется как `TargetDesiredStoreDigest` и затем как `AppliedStoreDigest`.

То есть семантический desired digest в основном transaction path не используется. Обновление `LastFetched`, `LastError`, `UpdatedAt` снова способно изменить preflight digest без изменения желаемой конфигурации и вызвать ложный `RecoveryRequired`.

Нужно развести два инварианта во всех структурах и переходах:

- `AppliedDesiredDigest` — только семантическая конфигурация, используется для preflight drift detection;
- `StoreSnapshotDigest` — точные байты snapshot, используется для snapshot/restore/bundle integrity.

Не маскировать это возвратом snapshot digest из метода с неоднозначным именем `CurrentDigest()`.

Добавить интеграционный тест coordinator:

1. применить поколение;
2. вызвать `RecordSubscriptionRefresh` с изменением timestamps/status;
3. выполнить rule mutation;
4. убедиться, что Recovery Mode не включён;
5. убедиться, что snapshot digest изменился, desired digest — нет.

### 3. Классификация RuleOnly не доказывает, что candidate отличается только rules

`ClassifyMutation()` сравнивает store snapshots и отдельно проверяет mode, listeners и bridges. Она не сравнивает предыдущую активную конфигурацию с candidate конфигурацией после исключения `rules`.

Если одновременно или косвенно изменились DNS, controller settings, sniffing, routing defaults либо другая генерируемая часть, но store mutation затронула только rules и listeners/bridges остались прежними, код всё равно может выбрать hot reload.

Нужно канонически сравнить active/candidate YAML или их typed representation, удалив только допустимое поле `rules`. Hot path разрешается лишь когда все остальные поля идентичны. Неизвестные/неразобранные поля должны приводить к full restart.

### 4. Отчёт заявляет runtime-проверку `/rules`, которой нет

В отчёте сказано, что после reload выполняется запрос к Mihomo `/rules` и проверяется порядок/количество правил. В `reloadControlledLocked()` выполняются только:

- digest активного файла;
- controller reload;
- повторный digest файла;
- PID/running check;
- process identity capture;
- проверка socket ownership.

Это доказывает работу процесса и listeners, но не доказывает, что Mihomo загрузил именно новое правило/порядок. Либо реализовать runtime verification через controller с проверяемой сигнатурой rules, либо убрать это утверждение и определить другой доказуемый receipt новой конфигурации.

### 5. Snapshot hard timeout сохраняет возможность накопления goroutine

`singleflightGroup.Do()` ожидает `WaitGroup` без возможности отмены. Внешняя goroutine `Build()` возвращает stale result по таймеру, но goroutine, вызвавшая `Do`, продолжает ждать provider. Каждый следующий `Build()` создаёт ещё одну goroutine, которая присоединяется к тому же зависшему singleflight call и блокируется на `wg.Wait()`.

Таким образом, запросы ограничены по времени ответа, но при provider-е, игнорирующем context, число ожидающих goroutine может расти без границ. Заявление «stopping goroutine pileup» неверно.

Нужно сделать ожидание singleflight context-aware либо не создавать отдельного waiter worker для каждого HTTP-запроса. Добавить тест с provider-ом, который никогда не возвращается, и проверить bounded goroutine/worker count после серии запросов.

## P1 — важные дефекты и недоказанные утверждения

### 6. Timeout общего snapshot складывается последовательно

Managed/external/system workers стартуют параллельно, но ожидание использует отдельный `time.After` на каждом последовательном `select`. В худшем случае ответ может ждать примерно 2.5 + 1.5 + 1.5 секунды, а не общий жёсткий бюджет.

Использовать абсолютные deadlines от времени запуска либо один общий budget с оставшимся временем.

### 7. Idempotency cache не привязывает operationId к payload

Повтор того же `operationId` возвращает старый response без проверки, что IDs/baseRevision совпадают. Ошибочное повторное использование ID может подтвердить операцию, которая фактически не выполнялась.

Хранить fingerprint запроса вместе с результатом. При совпадении ID и другом payload возвращать conflict. Предусмотреть ограниченный TTL/LRU и тесты коллизии.

### 8. Create/delete revision обновляется на frontend предположением

После create/delete frontend делает `rulesRevision++`, а backend response этих endpoints не возвращает авторитетную revision. При фоновой или конкурентной mutation локальная revision может разойтись с сервером, после чего reorder получит 409.

Все rule mutation endpoints должны возвращать единый typed response: `items`, `revision`, `generation`, `applyPath`, `transactionId`. Frontend должен принимать серверную revision, а не вычислять её самостоятельно.

### 9. Конфликты между pending reorder и create/delete не координируются

Debounced reorder может содержать список IDs, сформированный до create/delete. Create/delete не отменяет и не пересобирает `pendingReorder`. Это создаёт stale/order failures и визуальные откаты.

Нужна единая клиентская mutation queue либо явное flush/cancel/rebase pending reorder перед другими rule mutations.

### 10. `under 50ms` не доказано

В отчёте прямо указано, что router deployment/live validation не выполнялись. Unit-тест с mock-reloader и искусственным `dur=12.5` не является измерением реального Mihomo на aarch64.

До router acceptance корректная формулировка: «реализован fast-path, локальные тесты проходят; фактическая latency и отсутствие разрыва соединений не проверены».

## Обязательные исправления перед повторной приёмкой

1. Исправить `ListenersEqual` и добавить отрицательные тесты.
2. Реально внедрить desired/snapshot digest separation в coordinator, manifest, generation record и recovery paths.
3. Классифицировать fast path по полному active/candidate config diff за исключением rules.
4. Реализовать доказуемую runtime verification или убрать ложное утверждение о `/rules`.
5. Сделать singleflight waiter context-aware и доказать bounded goroutine behavior.
6. Ввести общий абсолютный timeout snapshot assembly.
7. Связать idempotency ID с fingerprint payload.
8. Возвращать авторитетную revision из всех rule mutations.
9. Сериализовать/rebase frontend rule mutation queue.
10. Обновить исходный acceptance report: убрать `FULLY VERIFIED` и неподтверждённые `<50ms` до live-теста.

## Повторная проверка

Минимум:

```text
go test -count=1 ./internal/mihomonative ./internal/mihomo ./internal/api ./internal/adaptiverouting
go test -race -count=1 ./internal/mihomonative ./internal/mihomo ./internal/api
npm run check
npx vitest run <новые тесты mutation queue/CAS>
git diff --check
```

После отдельного разрешения пользователя на сборку/деплой:

- 20 операций create/update/delete/reorder на aarch64;
- сохранённые `Server-Timing`;
- подтверждение `X-Apply-Path: hot_reload`;
- неизменный PID;
- проверка реального порядка rules;
- отсутствие обрыва существующих TCP/UDP соединений;
- restart persistence;
- контролируемая ошибка reload и подтверждённый rollback.
