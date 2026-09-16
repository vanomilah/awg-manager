# Final recheck: Revised First-Run Wizards Closure Plan

## Вердикт

Переработанный план исправляет главный дефект предыдущей версии: `MarkConsumed` теперь привязан к успешно записанному `PhaseCommitting`, а ownership/idempotency описаны явно. Однако перед запуском остаются **две обязательные корректировки транзакционной границы** и один недостающий test seam.

## Обязательные изменения

### [P0] Удалить старый pre-boundary перевод job в non-cancellable

В разделе Component 3 сказано: «In `OnPhaseChange`: remove duplicate `MarkConsumed`». Это оставляет неоднозначность: существующий callback также вызывает `SetPhaseNonCancellable` **до** записи `PhaseCommitting`.

Одновременно новый `OnPointOfNoReturn` снова вызывает `SetPhaseNonCancellable`. Если старый вызов оставить, job по-прежнему станет non-cancellable до durable point of no return, а новый контракт будет лишь дублировать переход.

План должен явно потребовать:

- удалить из pre-boundary `OnPhaseChange(PhaseCommitting)` и `MarkConsumed`, и `SetPhaseNonCancellable`;
- либо полностью удалить этот hook из wizard transaction params;
- единственным местом перевода job в non-cancellable и consume сделать `OnPointOfNoReturn`, вызываемый после успешной записи `PhaseCommitting`;
- тест должен блокировать coordinator непосредственно перед записью `PhaseCommitting` и подтверждать, что job ещё отменяем; после успешной записи и callback — уже нет.

### [P0] Ошибка записи `PhaseCommitting` находится ДО точки невозврата и должна запускать rollback

Предложенный фрагмент сохраняет текущее поведение: при ошибке `writeJournal(PhaseCommitting)` устанавливает recovery required и возвращается без rollback. Но сам план определяет point of no return как **успешную durable-запись** этой фазы. Значит при неуспешной записи граница ещё не пройдена.

На диске остаётся последняя успешная фаза `candidate_active`; кандидаты уже активированы, но ещё могут и должны быть откатаны. Оставлять их активными до отдельного recovery нельзя.

Требуемое поведение:

```go
if err := c.writeJournal(jPath, journal); err != nil {
    return c.failAndRollback(journal, fmt.Errorf("write committing journal: %w", err))
}
```

При этом `failAndRollback` должен получить journal с component transaction IDs, выполнить обратный rollback и сохранить/архивировать failed state. Если сам rollback или запись failed journal не удались, тогда результат оборачивается в `ErrRecoveryRequired` с сохранением исходной ошибки.

Соответственно тест `TestCoordinator_PrePointOfNoReturn_PhaseCommittingWriteFailure_TriggersRecoveryWithoutCallback` нужно заменить/уточнить:

- post-boundary callback не вызван;
- все активированные candidates откатаны;
- plan не consumed;
- job остаётся/становится cancellable и reservation освобождается;
- `ErrRecoveryRequired` ожидается только при дополнительном сбое rollback/recovery persistence, а не от одной ошибки записи `PhaseCommitting`.

### [P1] Для archive failure test нужен отдельный seam

План заявляет `TestCoordinator_PostPointOfNoReturn_ArchiveFailure_TriggersRecovery`, но добавляет injection только для `writeJournal`.

Добавить в `Coordinator` внутренний `archiveJournalFn` с production default `archiveJournal` (или общий интерфейс journal store). Через него должны проходить все вызовы архивирования в тестируемых coordinator paths. Публичный runtime-setter необязателен и нежелателен; безопаснее constructor option/package-private field в package tests.

## Рекомендуемые уточнения

- `SetWriteJournalFn` как экспортируемый mutable setter создаёт ненужную production API и потенциальную гонку при замене функции во время работы. Предпочтителен constructor dependency или package-private seam.
- Нормализацию Xray path вынести в единый helper, используемый builder, dispatcher и readiness. Формула `"/" + strings.Trim(path, "/")` теряет завершающий `/`; нельзя допустить расхождения между реально зарегистрированным prefix и probe URL.
- В readiness-тестах отдельно настроить TCP dial и HTTP doer: успешный mock HTTP не должен случайно маскировать провал socket readiness.
- В post-boundary callback сначала выполнить ownership-checked `MarkConsumed`, затем установить job non-cancellable либо предусмотреть компенсацию. Иначе при неожиданной ошибке consume UI покажет non-cancellable job при ещё незафиксированном PlanStore. Так как journal уже пересёк границу, ошибка всё равно должна стать `ErrRecoveryRequired`.

## Что теперь закрыто корректно

- Проверка ошибок journal после каждого `PrepareCandidate` предусмотрена в обоих transaction paths.
- Предложена детерминированная journal fault injection.
- Strict HTTP route identity, polling, shared-host dual paths и `HTTPDoer` включены.
- `MarkConsumed` проверяет state и owner и идемпотентен для исходного job.
- Post-finalize/post-commit ошибки не должны возвращать план в available.
- ARM64 verification больше не использует Windows-несовместимый `/dev/null`.

## Итог

После явного удаления pre-boundary `SetPhaseNonCancellable`, rollback при неуспешной записи `PhaseCommitting` и добавления archive injection seam план можно запускать в реализацию. Остальная архитектура плана согласована с предыдущими аудитами.
