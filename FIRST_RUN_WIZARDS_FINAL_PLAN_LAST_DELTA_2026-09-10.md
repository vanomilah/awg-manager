# First-Run Wizards Final Plan: Last Delta Review

## Вердикт

Все три обязательные правки предыдущего review внесены корректно:

- старый pre-boundary `OnPhaseChange` удаляется полностью;
- ошибка записи `PhaseCommitting` приводит к pre-boundary rollback;
- предусмотрен `archiveJournalFn` для детерминированного теста.

Перед реализацией осталось добавить в план **два небольших, но обязательных уточнения**. После них дополнительных архитектурных согласований не требуется.

## 1. Все journal I/O внутри rollback должны использовать seam и проверять ошибки

Текущий `failAndRollback` содержит:

- `internal/serveringress/coordinator.go:800` — `_ = writeJournal(jPath, j)` при переходе в `PhaseRollingBack`;
- `internal/serveringress/coordinator.go:853` — `_ = writeJournal(jPath, j)` после ошибок rollback;
- `internal/serveringress/coordinator.go:857` — прямой вызов `archiveJournal(...)`.

Финальный план упоминает замену archive-вызова, но не требует заменить и обработать две записи journal. В результате новый `writeJournalFn` не перехватит весь transaction path, а ошибка сохранения rollback state снова будет молча потеряна.

Добавить явное требование:

- все вызовы `writeJournal` и `archiveJournal` в обоих coordinator paths и recovery helpers идут только через `c.writeJournal`/`c.archiveJournal`;
- ошибка первой записи `PhaseRollingBack` не должна препятствовать попытке откатить компоненты, но должна быть накоплена;
- ошибка записи результата rollback объединяется с исходной и rollback errors;
- любая ошибка persistence/archive после выполненного rollback выставляет `recoveryNeeded` и возвращает ошибку, содержащую `ErrRecoveryRequired` и все исходные причины;
- тест `PhaseCommittingWriteFailure_RollsBack` должен иметь дополнительный вариант, где ломается также запись `PhaseRollingBack`, и проверять сохранение обеих ошибок.

Это также делает правдивым заявленное условие: обычная ошибка записи `PhaseCommitting` возвращает pre-commit failure, а `ErrRecoveryRequired` появляется, если rollback либо его persistence действительно не удалось надёжно завершить.

## 2. Нельзя считать plan consumed, если post-boundary `MarkConsumed` сам вернул ошибку

В плане сказано, что при `ErrRecoveryRequired` plan «ALREADY in consumed state». Обычно это верно для ошибок finalize/committed-write/archive, но неверно для ветки:

1. durable `PhaseCommitting` записан;
2. `OnPointOfNoReturn` вызывает `MarkConsumed`;
3. `MarkConsumed` возвращает ownership/state error;
4. coordinator возвращает `ErrRecoveryRequired`, а plan остался reserved/в другом состоянии.

Нужно зафиксировать отдельную обработку:

- перед запуском транзакции reservation повторно проверяется под mutex либо предоставляется метод `ConsumeReserved(planID, jobID)` с гарантированным атомарным переходом;
- если post-boundary consume неожиданно не удался, job получает `recovery_required`, а состояние плана не описывается как гарантированно consumed;
- reservation нельзя освобождать или разрешать повторное применение, поскольку инфраструктурная транзакция уже пересекла точку невозврата;
- диагностический статус должен сохранить причину `plan_consume_failed_after_commit_boundary`;
- добавить тест с принудительной ошибкой consume в callback: компоненты не откатываются, coordinator требует recovery, plan не становится повторно применимым.

Практически при корректном reservation эта ветка является нарушением внутреннего инварианта, но именно поэтому её нельзя маскировать общим утверждением «уже consumed».

## Финальное разрешение

После добавления этих двух пунктов план можно выполнять. Повторно присылать его на архитектурное согласование не обязательно: достаточно затем предоставить обновлённый walkthrough и результаты fault-injection/race-тестов для проверки фактической реализации.
