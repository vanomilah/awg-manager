# First-Run Wizards: аудит заявления о закрытии от 2026-09-10

## Вердикт

**Закрытие пока не принимается.** Основные архитектурные изменения действительно реализованы, а целевой race-набор тестов проходит. Однако walkthrough утверждает более сильные гарантии, чем фактически даёт код. Найдены один критический дефект долговечности транзакции и два существенных нарушения контрактов readiness/PlanStore.

## Findings

### [P0] Идентификаторы подготовленных компонентных транзакций записываются в journal с игнорированием ошибки

Файл: `internal/serveringress/coordinator.go:1273`, `:1283`, `:1329`.

После успешного `PrepareCandidate` идентификатор компонента добавляется в `ComponentTransactionIDs`, но результат `writeJournal(jPath, journal)` отбрасывается через `_ =`. После этого координатор продолжает активацию кандидатов.

Если запись journal не удалась, процесс может активировать компонент, не имея на диске его настоящего transaction ID. После падения recovery использует неполный journal и не гарантирует ни корректный rollback до точки невозврата, ни finalize после неё. Это прямо противоречит заявленным durable transaction IDs и crash-safe recovery.

Что исправить:

- проверять ошибку каждой записи journal сразу после получения component transaction ID;
- при ошибке вызывать rollback уже подготовленных компонентов и возвращать объединённую ошибку;
- добавить fault-injection тесты отказа записи после prepare каждого из трёх компонентов;
- тест должен проверять отсутствие активированного кандидата и отсутствие orphan snapshot/candidate.

### [P1] Readiness не требует успешного HTTP probe и обязательного `X-CDN-Route`

Файл: `internal/serverwizard/service.go:684-714`.

Проверка считается успешной в следующих случаях:

- `http.NewRequestWithContext` вернул ошибку;
- `httpClient.Do` вернул ошибку или timeout;
- ответ пришёл без `X-CDN-Route`;
- проверяется фиксированный Xray path `/cdn-bridge/`, а не `topo.XrayPathPrefix`.

Условие `r != "" && r != "xray"` отвергает только неправильный непустой заголовок. Отсутствующий заголовок проходит. Поэтому утверждение walkthrough о проверке identity маршрута неверно: сейчас подтверждается в основном только открытый TCP-порт dispatcher.

Что исправить:

- любая ошибка создания/выполнения HTTP-запроса должна проваливать текущую попытку polling;
- требовать точное равенство `X-CDN-Route == "xray"` или `"tgwebproxy"`;
- использовать path из topology;
- повторять HTTP probe до общего deadline, а не выполнять его один раз после TCP probe;
- добавить позитивные и негативные тесты: отсутствующий заголовок, неверный заголовок, HTTP timeout/connection reset и пользовательский Xray path.

### [P1] `PlanStore.MarkConsumed` не проверяет владельца reservation

Файл: `internal/serverwizard/planner.go:143-153`.

Метод принимает `jobID`, но вообще его не использует. Любой job ID может перевести найденный план — включая `available` либо зарезервированный другой задачей — в `consumed`. Это нарушает заявленный атомарный lifecycle `available -> reserved(owner) -> consumed(owner)`.

Что исправить:

- разрешать `MarkConsumed` только для `State == reserved` и `ReservedJobID == jobID`;
- возвращать `ErrOperationInProgress`/ошибку владельца для чужого job ID и ошибку состояния для available;
- не игнорировать ошибку `MarkConsumed` в `service.go:395` и `:564`;
- добавить конкурентные тесты чужого владельца, available-плана и повторного consume.

### [P2] Граница «план использован» описана неточно и реализована дважды

Файл: `internal/serverwizard/service.go:392-397`, `:562-565`; `internal/serveringress/coordinator.go:1396-1415`.

План впервые помечается consumed внутри callback **до** записи `PhaseCommitting` в journal, затем повторно после успешного возврата координатора. Ошибки обоих вызовов игнорируются. Walkthrough при этом утверждает, что план расходуется «upon commit».

Нужно определить один авторитетный durable boundary. Предпочтительно callback должен лишь синхронно перевести job в non-cancellable, а consume выполняться с обязательной проверкой результата после того, как координатор подтвердил переход через точку невозврата/успех. Если consume обязан происходить именно на point-of-no-return, контракт callback должен исполняться после успешной записи `PhaseCommitting`, а ошибка должна обрабатываться явно.

## Что подтверждено

- Есть journal schema v2 и topology fields.
- Есть typed `egress.ResolveRequest`.
- Матрица `cdn_get`/`cdn_ws`/`cdn_full` реализована; неизвестный `cdn_post` отвергается тестом.
- Есть атомарный `PlanStore.Reserve`.
- Coordinator активирует candidates до readiness, затем выполняет finalize после `PhaseCommitting`.
- Recovery для `PhaseCommitting` использует roll-forward finalization.
- Dispatcher выставляет `X-CDN-Route`; проблема находится в строгости потребителя readiness.

## Выполненная проверка

Команда:

```bash
go test -count=1 -race ./internal/serverwizard/... ./internal/serveringress/... ./internal/xrayserver/... ./internal/tgwebproxy/... ./internal/cdndispatcher/...
```

Результат: **PASS**, все перечисленные пакеты прошли с race detector.

`git diff --check` завершился без whitespace errors; Git сообщил только предупреждения о будущей нормализации CRLF/LF в существующем грязном working tree.

Тесты не опровергают findings выше: необходимых fault-injection и отрицательных readiness/ownership сценариев в проверенном наборе нет.

## Условие принятия

После исправления P0/P1 и добавления перечисленных регрессионных тестов повторить тот же race-набор. До этого формулировку walkthrough «all findings fully remediated» следует считать неподтверждённой.
