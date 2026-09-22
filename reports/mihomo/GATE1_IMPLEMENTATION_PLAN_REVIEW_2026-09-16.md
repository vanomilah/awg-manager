# Review: Gate 1 blocker resolution implementation plan

Дата: 2026-09-16  
Проверен план:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Сопоставлено с:

- `GATE1_FINAL_ARTIFACTS_AUDIT_2026-09-16.md`;
- текущими `coordinator.go`, `generation_store.go`, `types.go`;
- production-реализацией `mihomonative.Store.CreateSnapshotFile`.

## Вердикт

**План пока не запускать без корректировки.** Он перечисляет все найденные P0/P1, но два предложенных решения создают новые проблемы. После исправлений ниже план можно отдавать агенту.

## Обязательные исправления плана

### P0 — не использовать сканирование `ConfigDir` по подстроке TxID как замену durable intent

План предлагает после recovery сканировать `c.cfg.ConfigDir` и удалять все файлы, содержащие `m.TxID`. Это решение нельзя принимать:

1. `NativeStoreTx` не гарантирует, что snapshots находятся в `CoordinatorConfig.ConfigDir`.
2. Интерфейс гарантирует только возврат пути из `CreateSnapshotFile`; соглашение об имени файла отсутствует.
3. Поиск по подстроке и удаление файлов расширяет область destructive cleanup и не доказывает владение файлом.
4. Crash-window всё равно существует между созданием snapshot и записью его пути в manifest.

Нужно применить write-ahead intent: путь или строгое логическое имя post-snapshot должно быть durably известно **до** файлового side effect.

Рекомендуемый вариант:

- расширить `NativeStoreTx` безопасным методом подготовки детерминированного пути, например `SnapshotFilePath(txid string) (string, error)`;
- до `CreateSnapshotFile(txid+"-post")` записать этот точный путь в manifest и durably checkpoint-ить отдельное состояние `StatePostSnapshotWriteIntent` либо расширенный `StateCandidateWriteIntent`;
- затем создать snapshot строго по зафиксированному имени;
- recovery удаляет только точный путь из manifest;
- production Store и fake Store обязаны возвращать один и тот же canonical path для одинакового snapshot ID.

Ещё чище — изменить контракт на `CreateSnapshotFileAt(snapshotID, expectedPath)`, но нельзя вычислять путь в coordinator через знание внутреннего формата `mihomonative.Store`.

Перенос создания post-snapshot после `compileFn` уменьшает окно и полезен, но сам по себе crash-consistency не обеспечивает.

### P0 — разделить failpoints предварительной очистки и cleanup после неудачной публикации

План использует существующий `FailStagingRemoval` перед первым `RemoveAll(stagingName)` в `PublishStagedBundle`. Это ломает смысл уже добавленного теста:

- сейчас `FailStagingRemoval` позволяет создать staging, получить ошибку публикации и затем эмулирует невозможность удалить оставшийся staging через `RemoveCandidateGeneration`;
- после предложенной правки публикация завершится до создания staging;
- тест `GenerationBundle_StagingRemovalFailure` перестанет проверять реальный orphan после частичной публикации.

Нужны независимые hooks, например:

- `FailPreexistingStagingRemoval` — ошибка удаления старого `.tmp.<genID>` до новой публикации;
- `FailCandidateStagingRemoval` — ошибка cleanup staging, созданного текущей публикацией.

Существующий `FailStagingRemoval` можно переименовать только с одновременным обновлением всех тестов. Тесты должны раздельно доказать оба случая.

### P1 — переходы state machine должны быть минимальными, без сохранения старого bypass

План оставляет переход `StateSnapshotSecured -> StateCandidateBuilt`. После введения обязательного durable `StateCandidateWriteIntent` этот переход является обходом write-ahead протокола и должен быть запрещён.

Также необходимо не просто обновить `IsValidNext`, а начать вызывать его в `transitionManifestLocked`. Иначе таблица переходов остаётся декларативным мёртвым кодом, как сейчас.

Минимальная цепочка должна быть единственной:

```text
idle -> snapshot_secured -> post_snapshot_write_intent -> candidate_write_intent
idle -------------------------------> candidate_write_intent   (без mutation)
candidate_write_intent -> candidate_built -> candidate_published
```

Точные состояния можно сократить, но ни один файловый side effect не должен появляться до durable intent, содержащего точный cleanup target.

Для abort/rollback переходов нужно сверить реальный control flow: `abortEarlyLocked` сейчас не переводит manifest через каждое состояние, поэтому нельзя добавлять переходы только ради таблицы, не изменив production-код и тесты.

### P1 — phase validation должна проверять canonical и принадлежащие транзакции пути

Проверки только на `!= ""` недостаточно. Для snapshot/candidate путей необходимо подтвердить:

- абсолютный canonical path или безопасный basename согласно принятому формату;
- принадлежность разрешённому каталогу;
- связь имени с текущим `TxID`;
- отсутствие path traversal/symlink escape;
- обязательность config digest на той фазе, где содержимое уже записано.

Иначе повреждённый manifest сможет направить cleanup на посторонний файл.

### P1 — runtime preservation тестировать по отсутствию вызова Stop, а не только по `IsRunning`

Предложение сохранить runtime правильное, но тест должен проверять минимум два свойства:

1. `StopAndWait` не вызывался для crash до swap.
2. Идентичность/generation работающего процесса не изменилась.

Проверка `IsRunning()==true` недостаточна: процесс можно остановить и запустить заново, сохранив итоговый boolean. Желательно ввести небольшой operator interface в coordinator и использовать spy/fake с counters; это также избавит тест от запуска helper subprocess.

Нужно отдельно определить семантику настоящего рестарта awg-manager: свежий `Operator` теряет in-memory PID. Если внешний Mihomo может пережить процесс менеджера, recovery обязано идентифицировать/adopt либо безопасно reconcile этот процесс. Нельзя моделировать OS restart только созданием второго coordinator поверх того же объекта Operator.

### P1 — уточнить baseline итогового patch

Рабочее дерево содержит staged/untracked изменения, а Gate 1 файлы имеют статус `AM`. Команда «regenerate unified patch» без baseline неоднозначна.

В плане нужно явно указать:

- какой commit/index является baseline;
- какие ровно файлы входят в Gate 1 patch;
- что временные каталоги, cache и посторонние изменения не включаются;
- проверку patch в отдельном чистом worktree или временном клоне: forward apply к baseline и запуск тестов после apply.

Одна только проверка `git apply --reverse --check` в грязном дереве не доказывает воспроизводимость patch.

## Что в плане оставить

- перенос `StateCandidateWriteIntent` в `types.go`;
- строгую обработку `os.Stat`: успех только при `os.IsNotExist`;
- запрет безусловного `StopAndWait` до swap;
- отдельные crash/restart тесты;
- `go test`, `go test -race`, `git diff --check`;
- обновление итогового отчёта только после повторной приёмки.

## Исправленный порядок реализации

1. Зафиксировать baseline и область Gate 1.
2. Спроектировать write-ahead контракт snapshot path без directory scan.
3. Встроить новые состояния в единственную state machine и включить enforcement переходов.
4. Реализовать строгую path/schema validation.
5. Разделить failpoints pre-clean и abort-cleanup staging.
6. Исправить все проверки удаления на `os.IsNotExist`.
7. Сохранить runtime до swap и добавить spy-based тест отсутствия stop/restart.
8. Добавить crash matrix для каждого промежутка между intent, side effect и checkpoint.
9. Прогнать тесты в WSL и проверить patch в чистом дереве от заявленного baseline.

## Решение для агента

Текущий `implementation_plan.md` вернуть на доработку. Реализацию начинать только после удаления идеи broad scan по TxID, разделения failpoints и явного включения `IsValidNext` в production transition path.
