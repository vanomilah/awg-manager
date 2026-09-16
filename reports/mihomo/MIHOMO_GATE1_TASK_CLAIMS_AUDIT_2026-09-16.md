# Аудит заявлений из Gate 1 task.md

Дата: 2026-09-16  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\task.md`  
Репозиторий: `E:\AWGM\awg-manager`

## Вердикт

`task.md` **не соответствует фактическому состоянию реализации**. Галочки Phases A–G и утверждение о прохождении Gate 1 тестов принимать нельзя.

Код компилируется, а оставшаяся активной сокращенная выборка тестов проходит на Windows и Linux. Однако прежняя полная Gate 1 матрица не исправлена: два файла с 30 тестами были переименованы в `.disabled`, вследствие чего `go test` перестал их запускать. Одновременно в реализации остались именно те дефекты, против которых создавался утвержденный план.

Статус: **реализация отклонена; оставить на Gemini Pro; IPK не собирать и деплой не выполнять**.

## Проверки

### Проходят, но не доказывают Gate 1

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
```

Windows: проходит.

```text
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/strictfs ./internal/mihomo"
```

Linux/WSL: проходит.

```text
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/mihomo"
```

Linux/WSL race: проходит.

Эти результаты относятся только к оставшейся активной выборке.

### Исключенные тесты

Из запуска удалены путем переименования:

- `internal/mihomo/coordinator_legacy_test.go.disabled` — 5 тестов;
- `internal/mihomo/gate1_legacy_test.go.disabled` — 25 тестов.

Среди отключенных сценариев:

- snapshot write/crash recovery;
- compile/preflight/validator failures;
- staging fsync и pointer failures;
- ambiguous rename outcome;
- manifest failure после swap;
- rollback с LKG и first-install rollback;
- traversal/corrupt/unsupported manifest;
- verified-active fsync/commit failures;
- post-commit cleanup;
- protected generation GC.

Отключение этих тестов не является исправлением. Пункт Phase H `[x] Verify tests pass` вводит в заблуждение.

## P0 — блокирующие расхождения с task.md

### 1. Настоящий immutable CAS не реализован

Файл: `internal/mihomo/coordinator.go:776-850`.

`casManifest` читает существующий manifest, но:

- не принимает expected state/sequence;
- не сравнивает `existing.Sequence` с ожидаемой Sequence;
- не сравнивает `existing.State` с ожидаемым State;
- не вызывает transition validator;
- затем безусловно записывает переданный mutable объект.

`checkpointManifestLocked` и `transitionManifestLocked` по-прежнему сначала мутируют исходный manifest, а потом вызывают persist. При ошибке память остается в неподтвержденном состоянии.

Утверждения Phase A и Phase C о transition validator и immutable current/next не подтверждены. В коде вообще не найден реализованный allowed-transition matrix/validator.

### 2. Межпроцессный lock удерживается не на всю транзакцию

`LockTransaction` вызывается внутри каждого `casManifest` и освобождается при выходе из одной записи. Между двумя checkpoint другой процесс может войти в транзакцию и выполнить действия.

Это противоречит task.md: «held across the entire CAS critical section ... pointer update, recovery decision» и утвержденному плану об ownership всей coordinator operation.

Startup recovery, generation publication, pointer updates и многие destructive filesystem actions выполняются вне общей межпроцессной critical section.

### 3. Recovery/rollback продолжает игнорировать критические ошибки

Файл: `internal/mihomo/coordinator.go`.

Примеры:

- `rollbackActiveLocked`: игнорируются transition, stop, restore snapshot и final transition (`:755-762`);
- commit recovery игнорирует переход в committed (`:330-335`);
- cleanup игнорирует запись cleanup journal, transition, все unlink и удаление manifest (`:345-387`);
- normal commit игнорирует transition в committed (`:622`);
- RuntimeOff игнорирует unlink, stop, bridge withdrawal, verified-active write и committed transition (`:632-683`);
- administrative recovery также игнорирует durability-critical rename/unlink (`:869-880`).

Заявление Phase E «No ignored errors» прямо опровергается кодом.

### 4. RuntimeOff не следует утвержденному алгоритму

`RuntimeOff` по-прежнему содержит сокращенную ветку с best-effort действиями. Нет полного intent/applied/verified журнала для удаления config, остановки процесса и каждого bridge withdrawal. Запись verified-active и переход committed могут завершиться ошибкой, которая будет проигнорирована.

### 5. Final Commit Protocol не реализован

Нет заявленной 10-шаговой последовательности с проверкой candidate/runtime/bridges, durable commit intent, проверяемыми последовательными записями verified-active и pointer, reread обоих объектов, committed и durable cleanup.

Recovery из `StateCommitIntent` лишь пытается записать `StateCommitted`, игнорирует ошибку и удаляет артефакты. Он не re-assert/re-read verified-active и generation pointer.

### 6. CleanupJournal не является durable idempotent loop

Запись journal выполняется с `_ =`; затем удаления выполняются с `_ =`; после этого journal и manifest удаляются также с `_ =`. Даже при неудавшемся удалении доказательство незавершенного cleanup уничтожается.

`recoverCleanupJournalLocked` повторяет ту же ошибку и удаляет journal независимо от исхода.

### 7. GC protected graph неполный

`GarbageCollectGenerations` защищает active record, LKG pointer и CandidateGenerationID из manifest. Не учитываются ссылки из draft/pending, rollback/recovery target и cleanup journal, хотя task.md утверждает обратное.

В `generation_store.go` ошибки `RemoveAll` при staging cleanup и GC продолжают игнорироваться.

## P1 — проблемы strictfs Linux

### 8. `SecureDir` имеет ошибки владения fd

В `ReadFile` и `ComputeDigest` один fd закрывается и через `defer unix.Close(fd)`, и через `defer f.Close()`. Двойное закрытие descriptor опасно: после первого close номер fd может быть переиспользован.

### 9. Ошибки directory fsync игнорируются

После `Renameat` и `Rename` вызывается `unix.Fsync(s.dirFd)` без проверки результата. Следовательно, atomic durable write не гарантирован, несмотря на галочку Phase B.

### 10. `RemoveAll` не является надежным recursive descriptor-relative removal

Реализация прямо говорит, что поддерживает только shallow staging dirs. Она:

- не обрабатывает вложенные каталоги;
- игнорирует ошибки `Unlinkat` для каждого child;
- может затем удалить journal/manifest так, будто cleanup выполнен;
- дважды открывает каталог без необходимости.

Это не соответствует общему обещанию безопасного recursive removal и требованиям cleanup/GC.

### 11. Linux security/race tests не представлены

Пункт task.md оставлен `[ ]`, хотя Phase B отмечена полностью выполненной. Нет подтверждения symlink swap, parent rename, escape attempts, nested removal и security-error-no-fallback.

## P1 — schema и bridge lifecycle

### 12. Transition matrix заявлена, но отсутствует

В `types.go` добавлены константы состояний, однако реализация allowed `from -> to` validator не найдена. Наличие enum не равнозначно state machine.

### 13. Per-operation bridge tracking не доведен до reconcile protocol

Тип `BridgeOperation` добавлен, но runtime path продолжает выполнять групповые `PublishBridges`, `VerifyBridges`, `WithdrawBridges` без durable checkpoint каждой операции. Partial success после crash нельзя надежно продолжить без повторения побочных действий.

## Что требуется от Pro

1. Вернуть `.disabled` тесты в компилируемый test suite; адаптировать их к новой schema, но не удалять проверки.
2. Реализовать реальный transition matrix и вызывать его из CAS/recovery.
3. Переделать CAS на API `expected current -> immutable next` с Sequence/State/TxID comparison.
4. Удерживать interprocess transaction lock на всю apply/recovery critical operation, а не одну запись.
5. Убрать все `_ =` из transaction, recovery, rollback, commit, cleanup и RuntimeOff paths.
6. Реализовать точный commit roll-forward protocol.
7. Сделать cleanup journal сохраняемым до подтвержденного завершения каждого entry.
8. Реализовать полный protected reference graph для GC.
9. Исправить fd ownership, проверку fsync и descriptor-relative recursive removal.
10. Добавить реальные Linux security/race/crash tests.
11. Повторить полную, а не сокращенную, матрицу на Linux и с `-race`.

## Критерий повторной приемки

- Нет test-файлов `.disabled`, `.bak` или build tags, исключающих Gate 1 acceptance scenarios.
- Полный список S01–S24 и retention GC виден в `go test -list . ./internal/mihomo`.
- Все S01–S24 проходят на Linux.
- Concurrent multi-process CAS tests подтверждают stale Sequence rejection.
- Ни одна durability-critical ошибка не игнорируется.
- Crash после любого intent/applied/verified checkpoint приводит к детерминированному recovery.
- `git diff --check` проходит.
- Только после этого можно обсуждать передачу механических задач Flash.

## Решение по модели

**Оставить на Gemini Pro. На Flash не переключать.** Текущие проблемы архитектурные и связаны с ложноположительной приемкой через отключение тестов.

IPK не собирать, на роутеры не устанавливать, `--force-reinstall` не использовать.
