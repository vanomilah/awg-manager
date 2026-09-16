# Проверка remediation plan после отклоненного walkthrough

## Вердикт

План правильно перечисляет большинство дефектов последней реализации, однако **пока не готов к выполнению буквально**. Требуется исправить пять контрактных ошибок и неоднозначностей ниже. После этого план можно запускать без очередного архитектурного цикла.

Статус: **CONDITIONAL — UPDATE PLAN BEFORE IMPLEMENTATION**.

## P0 — обязательные изменения плана

### 1. Readiness должна получать итоговую объединенную topology от coordinator

План по-прежнему описывает `probeTopologyReadiness` через поля `DesiredWizardConfig` выбранного мастера. Это не позволяет проверить уже работающий второй сервер и общие компоненты.

Например, при применении Telegram `direct_fake_tls` dispatcher может оставаться включенным для Xray. Проверка только Telegram Desired ошибочно потребует отсутствия dispatcher.

Изменить контракт:

```go
type ReadinessProbeFunc func(ctx context.Context, topology IngressTopology) error
```

Coordinator сначала вычисляет полную `desiredTopo`, активирует candidates и только затем вызывает:

```go
params.ReadinessProbe(ctx, desiredTopo)
```

Положительные и отрицательные проверки строятся исключительно по итоговой `IngressTopology`. Wizard не должен самостоятельно угадывать состояние второго сервера.

### 2. Исправить порядок аргументов typed egress resolver

Фактический и логически правильный контракт:

```go
Resolve(ctx, serverKind, egressID)
```

В плане ошибочно записано:

```go
Resolve(ctx, req.UpstreamDevice, req.Kind)
```

Должно быть:

```go
Resolve(ctx, req.Kind, req.UpstreamDevice)
```

Лучше заменить позиционные строки типизированным request:

```go
Resolve(ctx context.Context, req ResolveRequest) (ResolvedEgress, error)
```

с полями `ServerKind` и `EgressID`, чтобы исключить повторение ошибки.

### 3. Apply требует атомарного резервирования плана до запуска goroutine

Предложение перенести `MarkUsed` внутрь `executeApplyJob` создает окно, в котором два одновременных `POST /apply` успеют создать два job для одного plan ID. Один job затем упадет как duplicate, хотя API уже вернул два успешных job IDs.

Нужен state machine PlanStore:

```text
available -> reserved(jobID) -> consumed
                         \-> released только если job не начал mutation
```

В `Apply` под одним lock:

1. создать job ID либо заранее зарезервировать его;
2. атомарно `Reserve(planID, sessionID, jobID)`;
3. вернуть ровно один job;
4. второй Apply получает `ErrPlanAlreadyUsed/Reserved` синхронно.

Внутри job coordinator выполняет единственную authoritative fingerprint check. Plan считается consumed перед первой mutation. Политику release при отмене/ошибке до mutation нужно определить явно и покрыть тестами.

### 4. Не добавлять неподтвержденный `cdn_post/xhttp_post`

Утвержденная ранее матрица и текущий каталог содержат:

- `cdn_get`;
- `cdn_ws`;
- `cdn_full`.

План неожиданно заменяет `cdn_full` на новый `cdn_post` и `xhttp_post`. В текущем коде нет profile `cdn_post`, а генераторы и тесты ориентированы на GET. Это расширение задачи, не исправление аудита.

Оставить строгую матрицу:

```text
cdn_get  + xhttp_get
cdn_ws   + ws
cdn_full + xhttp_get либо ws (по явному выбору mode)
```

Поддержку POST добавлять отдельной задачей только после проверки Xray runtime, всех export formats и реального CDN. Не смешивать ее с remediation.

### 5. Durable journal должен знать component tx IDs до первой активации

Перед первым `CommitPrepared` coordinator обязан надежно записать все возвращенные component transaction IDs в journal и fsync его.

Точный порядок:

```text
write PhaseStaged base journal
Prepare Xray -> record tx ID
Prepare Telegram -> record tx ID
Prepare Dispatcher -> record tx ID
write+fsync PhaseStaged journal with all component tx IDs
CommitPrepared Xray
CommitPrepared Telegram
CommitPrepared Dispatcher
write+fsync PhaseCandidateActive
readiness(full topology)
...
```

Если prepare частично падает, journal также должен содержать IDs уже подготовленных компонентов до rollback. Самый надежный вариант — переписывать/fsync journal после каждого успешного prepare. Нельзя полагаться на совпадение component tx ID с coordinator tx ID.

## P1 — важные уточнения

### 6. Commit и Finalize должны иметь четкую idempotency semantics

После переноса activation до readiness:

- `CommitPrepared` означает activate candidate, сохраняя rollback snapshot;
- повторный `CommitPrepared` для state `active` — no-op;
- `RollbackPrepared` допустим для `prepared|active`;
- `FinalizePrepared` допустим только для `active` и превращает его в finalized/удаляет snapshot;
- recovery `PhaseCommitting` вызывает только `FinalizePrepared`, не `CommitPrepared`;
- отсутствующий tx при finalize считается успехом только если есть durable доказательство, что он ранее был finalized. Иначе безусловный missing=no-op может скрыть потерянный transaction directory.

### 7. `PhaseCandidateActive` записывать после активации, но crash до записи должен быть восстанавливаемым

Если процесс падает после одного или всех `CommitPrepared`, но до записи `PhaseCandidateActive`, journal остается `PhaseStaged`. Recovery должен выполнить rollback всех component tx IDs. Поэтому rollback state каждого активированного компонента должен оставаться доступным, а rollback prepared-компонента быть безопасным.

Добавить fault injection после каждой активации и перед записью phase.

### 8. HTTP readiness проверяет identity маршрута, а не произвольный status

Условие «200/400, но не 404» недостаточно: 500/502, redirect или ответ чужого backend также неоднозначны. Добавить внутренний readiness endpoint/token либо диагностический header dispatcher, позволяющий доказать выбранный route без отправки пользовательского трафика.

Минимум:

- dispatcher подтверждает host/path match собственным route ID;
- backend readiness проверяется отдельно;
- неизвестный Host/path обязан вернуть 404;
- Telegram-only Host не принимает Xray path и наоборот.

### 9. Ошибки после point of no return должны возвращать `ErrRecoveryRequired`

Если finalize, запись `PhaseCommitted` или archive терпит ошибку, coordinator выставляет `recoveryNeeded` и возвращает ошибку, содержащую `ErrRecoveryRequired`. Иначе Wizard классифицирует ее как обычный `INVALID_REQUEST`.

### 10. Удаление legacy topology требует миграции journal schema

План говорит удалить legacy shared fields, но должен определить:

- повышение `TransactionJournal.Version`;
- чтение старого journal;
- однозначное преобразование или fail-closed recovery;
- checksum canonical form отдельно для каждой версии.

Нельзя просто удалить JSON fields: незавершенная транзакция старой версии может стать невосстановимой после обновления.

## Необходимые тесты сверх перечисленных в плане

1. Два конкурентных Apply одного plan ID — создается только один job.
2. Readiness получает combined topology и сохраняет активный второй server.
3. Crash после каждого Prepare до записи полного списка tx IDs.
4. Crash после каждого CommitPrepared до `PhaseCandidateActive`.
5. Missing component transaction directory в `PhaseCommitting` не маскируется без доказательства finalize.
6. Старый journal schema читается/мигрирует либо переводит систему в явный recovery-required.
7. Dispatcher route identity для shared и distinct hosts.

## Решение

После внесения пунктов 1-5 план можно запускать в реализацию. Пункты 6-10 и перечисленные тесты являются обязательными критериями приемки.

IPK не собирать и router deployment не выполнять до отдельной команды пользователя.
