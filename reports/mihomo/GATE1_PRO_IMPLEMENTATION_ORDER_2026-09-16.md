# EXECUTION ORDER — исправить Gate 1 в коде

## Это не запрос на план, summary или review

**Не создавай и не редактируй отчеты, планы, walkthrough, task.md или review-файлы.**  
**Не пересказывай замечания. Не проси другого агента провести ревью.**  
Твоя задача — изменить исходный код и тесты в `E:\AWGM\awg-manager`, чтобы устранить перечисленные дефекты, затем выполнить команды проверки и показать фактический diff и результаты.

Работать только на Gemini Pro. UI, IPK и роутеры не трогать.

## Контекст ошибки предыдущей попытки

Файл `MIHOMO_GATE1_REVIEW_SUMMARY_AUDIT_2026-09-16.md` был ошибочно перезаписан старым summary. Не использовать его текущее содержимое как требования. В частности, следующие тезисы из него ошибочны:

- pointer failure не может быть non-fatal после `commit_intent`;
- после `commit_intent` rollback запрещен;
- нельзя удалять pointer failpoint tests;
- наличие зеленого Linux-прогона не доказывает корректность ослабленных тестов.

## Обязательные изменения исходного кода

### 1. Запретить rollback после commit boundary

Файл: `internal/mihomo/types.go`.

- `StateCommitIntent.IsValidNext(StateRollbackInProgress)` должен возвращать `false`.
- Из `StateCommitIntent` разрешить только `StateCommitted` и детерминированный путь `StateRecoveryRequired` для последующего roll-forward.
- Добавить unit test transition matrix.

Файл: `internal/mihomo/coordinator.go`.

- После успешной durable записи `StateCommitIntent` не вызывать `handleApplyFailureLocked`/`rollbackActiveLocked` ни при какой ошибке.
- Ошибки verified-active, pointer, reread или committed должны сохранить manifest и привести к recoverable roll-forward состоянию.

### 2. Реализовать единый final commit helper

Создать один helper, используемый и RuntimeOn, и RuntimeOff. Он обязан:

1. требовать durable `StateCommitIntent`;
2. записать verified-active;
3. записать current/LKG generation pointer;
4. перечитать оба файла;
5. проверить generation ID, generation number и digests;
6. записать `StateCommitted`;
7. только после этого запускать terminal cleanup.

Это две последовательные durable записи, а не «атомарное обновление двух файлов». При сбое любого шага helper возвращает recovery error и не удаляет manifest.

### 3. Pointer failure сделать фатальным для текущей попытки commit

Удалить поведение `log warning and proceed` вокруг `AdvanceLKGPointer`.

- Ошибка pointer write/fsync должна оставить `commit_intent` на диске.
- Вызов должен завершиться ошибкой/recovery-required, но не rollback.
- Startup recovery обязан повторно записать verified-active и pointer из candidate generation и проверить их.

### 4. RuntimeOff провести через тот же commit protocol

`applyRuntimeOffLocked` должен:

- использовать candidate generation с `ConfigPresent=false`;
- завершить swap/process/bridge verification;
- записать commit intent;
- вызвать общий final commit helper;
- обновить pointer на RuntimeOff generation;
- не переходить в idle при ошибке.

### 5. Не скрывать ошибку committed transition

Если переход `commit_intent -> committed` не записан durable:

- вернуть ошибку;
- сохранить manifest;
- оставить recovery marker/state;
- не выполнять terminal cleanup;
- не ставить `StateIdle`;
- не возвращать `nil`.

### 6. Реализовать настоящий immutable CAS

`checkpointManifestLocked` и `transitionManifestLocked` не должны мутировать caller object до успешного CAS.

Требуется:

```text
expected := current.Clone()
next := current.Clone()
mutate(next)
cas(expected, next)
on success: *current = *next
on failure: current unchanged
```

Проверять `TxID`, `Sequence`, `State` и `IsValidNext` по durable current.

### 7. Устранить durability-critical suppressed errors

Обязательно обработать, а не подавлять:

- `CurrentDigest`;
- restore pre-mutation snapshot после compile failure;
- verified-active serialization/write/fsync;
- pointer write/fsync;
- committed transition;
- cleanup journal write/checkpoint;
- cleanup file removals;
- generation staging cleanup и GC removals;
- directory fsync.

Best-effort логирование допустимо только для явно диагностических действий, не влияющих на durable state.

### 8. Cleanup journal не удалять до подтверждения

- Каждая cleanup entry должна удаляться с проверкой результата.
- После удаления проверить отсутствие объекта.
- При partial failure сохранить journal с оставшимися entries и Sequence.
- Manifest сохранять до подтвержденного terminal cleanup.
- `idle` разрешен только при отсутствии незавершенного manifest/journal/recovery.

### 9. Восстановить pointer failpoint coverage

Не удалять проверки из-за переноса pointer update из `PublishStagedBundle`.

- Перенести failpoints `pointer_write` и `pointer_fsync` на `AdvanceLKGPointer`/final commit helper.
- Проверить crash между verified-active и pointer.
- Проверить crash после pointer до committed.
- В обоих случаях restart обязан roll-forward до согласованного поколения.

### 10. Bridge operations checkpoint per operation

Для каждой create/withdraw операции durable сохранять:

- stable OperationID;
- intent;
- applied;
- verified;
- attempts/last error/observed result.

После partial success restart не должен повторять уже verified побочный эффект.

### 11. Исправить strictfs остатки

- Не закрывать один fd одновременно через `unix.Close` и `os.File.Close`.
- Проверять ошибки directory fsync.
- `RemoveAll` должен descriptor-relative обрабатывать вложенные entries либо явно запретить их до начала удаления; нельзя игнорировать child unlink errors.
- Fallback с `openat2` разрешен только на `ENOSYS`/обоснованный unsupported `EINVAL`, не на security errors.

## Тесты нельзя ослаблять под реализацию

Файлы должны оставаться активными:

- `internal/mihomo/coordinator_legacy_test.go`;
- `internal/mihomo/gate1_legacy_test.go`.

Запрещено:

- переименовывать тесты в `.disabled`, `.bak`;
- добавлять build tag для их исключения;
- удалять failpoint subtests;
- менять expected `recovery_required` на `idle`, если durable outcome не доказан;
- объявлять pointer failure успешным commit.

Дополнительно написать тесты:

1. `commit_intent` никогда не переходит в rollback;
2. verified-active failure после commit intent сохраняет manifest;
3. pointer write/fsync failure сохраняет manifest;
4. restart roll-forward выравнивает verified-active и pointer;
5. RuntimeOff обновляет pointer;
6. committed transition failure не запускает cleanup;
7. checkpoint CAS failure не мутирует объект в памяти;
8. cleanup partial failure сохраняет journal;
9. bridge partial success продолжается без повторения verified operation.

## Команды приемки

Выполнить и приложить полный вывод:

```text
gofmt -w <только измененные Go-файлы>
go test -count=1 ./internal/strictfs ./internal/mihomo
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/strictfs ./internal/mihomo"
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/mihomo"
git diff --check
go test -list . ./internal/mihomo
```

Полный список S01–S24 и retention GC должен присутствовать в `go test -list`.

Windows fallback failures также исправить; если для отдельного syscall security test Windows неприменим, пропустить только этот конкретный test через runtime guard с объяснением, а не всю Gate 1 матрицу.

## Что предоставить после выполнения

Не создавать очередной архитектурный summary. Ответ должен содержать только:

1. список реально измененных исходных файлов;
2. краткое соответствие пунктам 1–11;
3. вывод команд приемки;
4. `git diff --stat`;
5. известные оставшиеся failures/blockers без сокрытия.

После этого остановиться и ждать code review. Не собирать IPK, не выполнять деплой и не переходить к UI.
