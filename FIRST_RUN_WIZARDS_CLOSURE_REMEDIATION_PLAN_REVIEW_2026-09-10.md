# Review: First-Run Wizards Closure Remediation Plan

Проверен `implementation_plan.md` от 2026-09-10, подготовленный по результатам `FIRST_RUN_WIZARDS_WALKTHROUGH_CLOSURE_AUDIT_2026-09-10.md`.

## Вердикт

**План можно отдавать в реализацию только после одной обязательной архитектурной корректировки.** Он правильно охватывает исходные P0/P1/P2, но предложенная «единственная граница consume после успешного возврата координатора» не учитывает post-commit сбои и оставляет план навсегда в `reserved`.

## Обязательная корректировка

### [P0] Нельзя связывать `MarkConsumed` только с успешным возвратом `ExecuteIngressTransaction`

План предлагает удалить `MarkConsumed` из callback и выполнить его только после `ExecuteIngressTransaction(...) == nil`.

Это некорректно для сценария:

1. `PhaseCommitting` успешно и долговечно записан в journal — точка невозврата пройдена;
2. `FinalizePrepared`, запись `PhaseCommitted` или архивирование journal завершается ошибкой;
3. координатор возвращает `ErrRecoveryRequired`;
4. конфигурация уже не может быть безопасно откатана, но PlanStore остаётся в `reserved`.

После recovery компоненты будут доведены вперёд, а план всё равно не станет `consumed`. Повторное применение заблокировано чужой/старой reservation, reveal-результат не завершается нормальным lifecycle, а освобождать такой план тоже нельзя.

Нужна граница, совпадающая именно с **успешной durable-записью `PhaseCommitting`**, а не с полным успехом всей транзакции.

Рекомендуемый контракт:

- coordinator сначала записывает `journal.Phase = PhaseCommitting` и проверяет успех `writeJournal`;
- только после этого вызывает отдельный post-boundary callback, например `OnPointOfNoReturn`;
- callback синхронно переводит job в non-cancellable и вызывает ownership-checked `MarkConsumed`;
- ошибка callback после точки невозврата возвращается как `ErrRecoveryRequired`, rollback не выполняется;
- повторный callback/recovery должен быть идемпотентным: consume тем же `jobID` либо возвращает текущую consumed-запись как success, либо сервис явно распознаёт это состояние;
- окончательный `SucceedJob` по-прежнему выполняется лишь после полного успеха coordinator.

Альтернатива — вернуть из coordinator типизированный результат с признаком `PointOfNoReturnReached`, но одного `error` для корректного решения недостаточно.

Нужные тесты:

- ошибка `FinalizePrepared` после durable `PhaseCommitting`: plan consumed, job non-cancellable, результат `ErrRecoveryRequired`;
- ошибка записи `PhaseCommitted`: то же поведение;
- ошибка архивирования committed journal: то же поведение;
- ошибка записи самого `PhaseCommitting`: plan остаётся reserved/освобождается согласно pre-commit политике, компоненты не считаются окончательно принятыми;
- повторный post-boundary callback с тем же job ID идемпотентен.

## Дополнительные уточнения плана

### [P1] Fault injection для journal нужно спроектировать явно

Простого перечисления тестов недостаточно: `writeJournal` сейчас является package-level функцией. План должен предусмотреть seam — например, `Coordinator.writeJournalFn` с production default — и восстановление default в тестах. Иначе тесты либо не смогут детерминированно попасть в запись после конкретного `PrepareCandidate`, либо будут вынуждены ломать файловую систему слишком грубо.

Проверить необходимо обе реализации, упомянутые в плане: legacy `applyLocked` и `ExecuteIngressTransaction`. Если legacy-путь больше не используется, предпочтительнее удалить/свести его к единой реализации вместо дублирования транзакционного алгоритма.

### [P1] Readiness лучше тестировать через `HTTPDoer`, а не подменять `DialTimeout`

Сигнатура существующего `dialFn(network, address string, timeout time.Duration)` не совпадает с `http.Transport.DialContext`. Нужен явный адаптер либо, что чище, внедряемый интерфейс:

```go
type HTTPDoer interface {
    Do(*http.Request) (*http.Response, error)
}
```

Production-клиент должен ходить только на `127.0.0.1:<dispatcherPort>`, иметь общий deadline/малый per-attempt timeout и закрывать response body на каждой попытке. Тесты должны проверять последовательность «ошибка -> неверный header -> верный header», а не только постоянный успех/провал.

Если настроены оба маршрута на общем hostname, должны проверяться оба URL. Dynamic Xray path следует нормализовать тем же общим helper, который используется при построении dispatcher config, чтобы readiness не проверял иной путь.

### [P1] `MarkConsumed` должен быть ownership-safe и идемпотентным для владельца

Предложение возвращать `ErrPlanAlreadyUsed` для любого consumed-плана конфликтует с безопасным повторением post-boundary операции после crash/recovery. Лучше закрепить контракт:

- `reserved` + совпадающий `jobID` -> переход в consumed;
- `reserved` + другой `jobID` -> ownership error;
- `available` -> invalid state;
- `consumed` + тот же `ReservedJobID`/`ConsumedJobID` -> idempotent success;
- `consumed` другим job -> `ErrPlanAlreadyUsed`.

Для этого после consume нельзя терять identity владельца либо нужно отдельное поле `ConsumedJobID`.

### [P2] Команда ARM64 test compilation в плане непереносима

Команда PowerShell с `-o /dev/null` не является надёжной Windows-командой. Следует запускать её в WSL либо выводить test binary во временный файл внутри workspace и затем удалять проверенным путём. Это не блокирует код, но доказательство verification должно быть воспроизводимым.

## Что в плане сделано правильно

- Закрыты все места с `_ = writeJournal(...)` после component prepare в обоих coordinator paths.
- Предусмотрен rollback подготовленных компонентов при pre-commit ошибке записи.
- Readiness требует строгий route identity, polling и пользовательский Xray path.
- Предусмотрены отрицательные тесты отсутствующего/неверного header и сетевой ошибки.
- Вводится проверка состояния и владельца reservation.
- Сохранены ограничения по секретам, роутерам и запрету `--force-reinstall`.
- Выбран правильный целевой race-набор тестов.

## Итоговое условие запуска

Перед началом кодирования изменить Component 3 так, чтобы consume происходил сразу после **успешной durable-записи точки невозврата** и корректно переживал post-commit `ErrRecoveryRequired`. Добавить перечисленные post-boundary fault tests и явные injection seams. После этого план архитектурно достаточен для реализации.
