# First-Run Wizards: Final Implementation Closure Audit

## Вердикт

Реализация подтверждает почти все заявления свежего `walkthrough.md`, а полный целевой набор с race detector проходит. Закрытие пока нельзя принять из-за **одного post-boundary cancellation race** в `WizardService`. Исправление локальное и не требует изменения согласованной архитектуры.

## Finding

### [P0] Запрос отмены может замаскировать `ErrRecoveryRequired` после точки невозврата и освободить план

Файл: `internal/serverwizard/service.go:553-571`.

После возврата `ExecuteIngressTransaction` код сначала проверяет:

```go
if s.jobRunner.IsCancelRequested(jobID) {
    _ = s.planStore.Release(rec.PlanID, jobID)
    s.jobRunner.MarkCancelled(jobID)
    return
}
```

и только затем обрабатывает `errors.Is(err, serveringress.ErrRecoveryRequired)`.

Опасный сценарий:

1. `PhaseCommitting` уже успешно записан — rollback запрещён;
2. `OnPointOfNoReturn` вызывает `MarkConsumed`, но тот возвращает ошибку;
3. coordinator правильно возвращает `ErrRecoveryRequired`, а plan остаётся `reserved`;
4. между ошибкой callback и обработкой результата пользователь успевает запросить cancel, поскольку job ещё не был переведён в non-cancellable;
5. первая ветка принимает cancel, вызывает `Release`, переводит plan обратно в `available` и помечает job `cancelled`;
6. инфраструктурная транзакция при этом уже находится после точки невозврата и требует roll-forward recovery.

Итог: теряется обязательный статус recovery, а тот же plan становится повторно применимым к уже активированной candidate-конфигурации.

#### Исправление

Обрабатывать тип результата coordinator раньше состояния cancel:

```go
if err != nil {
    if errors.Is(err, serveringress.ErrRecoveryRequired) {
        // Никогда не release и не MarkCancelled после durable boundary.
        s.jobRunner.MarkRecoveryRequired(jobID, recoveryMessage(err))
        return
    }
    if s.jobRunner.IsCancelRequested(jobID) {
        _ = s.planStore.Release(rec.PlanID, jobID)
        s.jobRunner.MarkCancelled(jobID)
        return
    }
    // Остальные pre-boundary ошибки...
}
```

Дополнительно желательно синхронно переводить job в специальное post-boundary/recovery состояние в ветке ошибки callback, но приоритет `ErrRecoveryRequired` над cancel уже устраняет нарушение безопасности.

#### Обязательный тест

Расширить `TestWizardService_ConsumeErrorAfterCommitBoundary` либо добавить отдельный тест:

- `MarkConsumed` seam запрашивает cancel перед возвратом ошибки;
- coordinator возвращает `ErrRecoveryRequired`;
- итоговая фаза job — только `recovery_required`, не `cancelled`;
- plan остаётся `reserved` и не резервируется другим job;
- `Release` не меняет состояние;
- component rollback не вызывается.

## Подтверждённые исправления

- Все journal operations основного ingress coordinator проходят через `c.writeJournal`/`c.archiveJournal`.
- Ошибки persistence во время rollback накапливаются и сохраняют `ErrRecoveryRequired` через `errors.Join`.
- Component transaction IDs записываются с обязательной проверкой ошибки.
- Ошибка записи `PhaseCommitting` выполняет pre-boundary rollback.
- `OnPointOfNoReturn` вызывается только после durable-записи `PhaseCommitting`.
- `MarkConsumed` проверяет reservation owner и идемпотентен для исходного job.
- Есть тест принудительной ошибки consume после границы; ему не хватает только одновременной отмены.
- `cdndispatcher.NormalizePathPrefix` используется builder, coordinator, dispatcher и readiness.
- Readiness строго проверяет `X-CDN-Route`, пользовательский path, оба shared-host маршрута и независимость TCP probe.
- Fault-injection тесты journal/finalize/archive присутствуют.

## Проверка

Выполнено:

```bash
go test -count=1 -race ./internal/serverwizard/... ./internal/serveringress/... ./internal/xrayserver/... ./internal/tgwebproxy/... ./internal/cdndispatcher/...
```

Результат: **PASS** для всех перечисленных пакетов.

Статический просмотр показал, что существующий `TestWizardService_ConsumeErrorAfterCommitBoundary` проверяет recovery без конкурентного cancel, поэтому найденная ветка им не покрывается.

## Условие окончательного принятия

Поменять приоритет обработки `ErrRecoveryRequired` и cancel, добавить описанный regression test и повторить целевой race-набор. После этого findings текущего цикла можно считать закрытыми.
