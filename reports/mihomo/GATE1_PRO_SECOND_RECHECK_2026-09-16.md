# Второй повторный аудит Gate 1

Дата: 2026-09-16  
Источник: attachment `2cf9db7b-5873-4807-8f9f-bb455e9600c4/pasted-text.txt`

## Вердикт

Исправления продолжают двигаться в правильную сторону, но работа **пока не принята**. Linux unit/race проходят, однако три новых теста частично формальны и не доказывают заявленные гарантии. В коде остаются блокирующие ошибки cleanup/rollback и новая data race в хранении состояния coordinator.

Оставить на **Gemini Pro** еще на один узкий проход. Flash пока не использовать.

## Независимая проверка

Подтверждено на Linux/WSL:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
```

Обе команды проходят.

Приложенный отчет агента неполон:

- запускался только `./internal/mihomo`, без `internal/strictfs`;
- отсутствуют Linux-команда, race output, `go test -list` и scoped `diff --check`;
- `git diff --stat HEAD` показывает все грязное дерево на 169 файлов и не является отчетом по Gate 1;
- команда `go fmt ./internal/mihomo` не заменяет явное форматирование всего scoped набора измененных файлов.

## Что исправлено

- Cleanup helpers теперь возвращают ошибки.
- RuntimeOff использует `syncBridgesLocked`.
- Rollback начал возвращать часть ошибок и пытаться компенсировать bridges.
- Per-bridge `TargetDigest` теперь вычисляется из `BridgeRef`.
- Final commit сравнивает основные digests и runtime mode.
- Добавлены новые failpoints/tests для rollback, cleanup и bridge checkpoint.

## P0 — оставшиеся дефекты

### 1. Data race в `setStateLocked`

`State()` читает `c.state` под `c.mu.RLock`, но новый `setStateLocked` выполняет простое:

```go
c.state = state
```

без `c.mu.Lock`. Название метода не создает синхронизацию. Текущий race test не ловит это лишь потому, что тесты не читают State конкурентно с transition.

Требуется вернуть lock внутри setter либо строго удерживать `c.mu` во всех callers. Первый вариант безопаснее. Добавить конкурентный test reader/writer под `-race`.

### 2. `handleApplyFailureLocked` игнорирует ошибку cleanup

После успешного rollback вызывается:

```go
c.cleanupTxArtifactsLocked(m)
c.setStateLocked(StateIdle)
```

Результат cleanup по-прежнему игнорируется. При partial cleanup функция ставит idle, хотя journal/manifest могут оставаться.

Требование: если cleanup вернул ошибку, не ставить idle; сохранить terminal cleanup/recovery observable state и вернуть объединенную ошибку.

### 3. Cleanup может удалить manifest без durable journal

В `cleanupTxArtifactsLocked` ошибка `json.MarshalIndent(cj)` не обрабатывается. Если marshal когда-либо упадет, journal не будет создан, но код продолжит transition и вызовет `processCleanupJournalFilesLocked(nil)`, который удаляет manifest.

Нужно fail-closed вернуть marshal error до transition/удалений.

### 4. Invalid cleanup journal удаляется

`recoverCleanupJournalLocked` при битом/невалидном JSON переходит к удалению cleanup journal. Это уничтожает единственный список незавершенных удалений.

Требование: quarantine/forensic preserve + recovery required; не удалять invalid journal как будто cleanup завершен.

### 5. Rollback игнорирует ошибку первого transition

Если переход в `rollback_in_progress` не записался, код лишь пишет log и продолжает destructive rollback. Durable manifest остается в предыдущей фазе и после crash recovery может повторить неверную ветку.

При failure первого transition rollback должен остановиться и вернуть recovery error до побочных действий.

### 6. Rollback имеет недоказанные ветки восстановления config

При ошибках `ReadGenerationBundle`/`os.ReadFile` некоторые ветки просто переходят к fallback либо заканчивают `restored=false` и удаляют active config. Нельзя удалять active config только потому, что previous bundle не удалось прочитать.

Если previous generation была заявлена, но bundle/config поврежден или недоступен, требуется recovery required, а не трактовка как first install.

### 7. Rollback RuntimeOff не компенсирует bridges

Bridge compensation выполняется только при:

```go
rollbackErr == nil && m.DesiredMode != RuntimeOff
```

Но failed transition в RuntimeOff также мог уже withdraw previous bridges. Их необходимо восстановить. Условие по DesiredMode следует убрать: rollback всегда должен reconcile observed bridges к previous applied generation.

### 8. Bridge LastError checkpoint по-прежнему подавляется

При apply error и verify error остаются вызовы:

```go
_ = c.transitionManifestLocked(m, m.State)
```

Checkpoint error скрывается. Нужно возвращать комбинированную ошибку и recovery required. Исправлена только ветка checkpoint после успешного side effect, не все error paths.

## P0 — новые тесты дают ложную уверенность

### 9. Cleanup test проверяет не тот файл

`TestCoordinator_Gate1_S11_CleanupPartialFailure` записывает и читает `coord.draftJournalFile`, тогда как тестируемый helper работает с `coord.cleanupJournalFile`.

При failpoint все entries остаются, helper не переписывает файл, а тест затем успешно читает собственный заранее записанный **draft journal**. Тест проходит, ничего не доказывая о сохранности cleanup journal.

Исправить путь на `cleanupJournalFile` и добавить end-to-end проверку через `cleanupTxArtifactsLocked`/final commit: state не idle, manifest и cleanup journal сохраняются.

### 10. Rollback test не проверяет fail-closed семантику

`TestCoordinator_Gate1_S10_Rollback_FailClosed` прямо допускает любой error и проверяет только, что fake runtime получил один bridge. Он не проверяет:

- durable state;
- сохранность manifest;
- отсутствие cleanup/idle при failure;
- восстановление store/config/process;
- результат verification.

Нужен failpoint restore snapshot/config/bridge и строгие assertions recovery required + manifest preserved.

### 11. Bridge checkpoint test не проверяет restart recovery

Тест подтверждает in-memory `StateRecoveryRequired`, но durable manifest после failpoint остается в состоянии до `applied`. Нужен restart coordinator и проверка, что reconciler наблюдает фактически созданный bridge, не повторяет side effect и завершает/компенсирует операцию.

## P1 — область diff и hygiene

### 12. Отчет на 169 файлов непригоден для Gate 1 review

Нужен allowlisted scoped stat/diff только по фактически относящимся файлам. Сейчас в дереве видны многочисленные scratch Python/Go copies (`scratch/coordinator_patched*.go`, patch scripts, `tmp_coord.go`). Они не должны попадать в production commit/package.

`git diff --check` по всему грязному дереву сообщает множество старых whitespace issues. Для Gate 1 показать scoped check по allowlist, не скрывая общий статус репозитория.

## Обязательные исправления Pro

1. Вернуть mutex в `setStateLocked` и добавить race test.
2. Обрабатывать cleanup error в `handleApplyFailureLocked`; idle только после подтвержденного cleanup.
3. Fail-closed на cleanup journal marshal/write/invalid recovery.
4. Немедленно прекращать rollback при failure перехода в rollback_in_progress.
5. Не удалять active config при недоступном заявленном previous bundle.
6. Всегда компенсировать bridges, включая failed RuntimeOff.
7. Устранить оставшиеся suppressed bridge checkpoints.
8. Исправить три слабых теста, описанных выше.
9. Предоставить scoped file allowlist и stat без scratch/других подсистем.

## Следующая приемка

```text
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/strictfs ./internal/mihomo"
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/mihomo"
go test -list . ./internal/mihomo
git status --short -- <Gate 1 allowlist>
git diff --check -- <Gate 1 allowlist>
git diff --stat -- <Gate 1 allowlist>
```

Также предоставить результаты новых строгих tests отдельно по именам.

## Решение

- Еще один проход на **Gemini Pro**.
- Flash пока не использовать.
- IPK не собирать и не устанавливать.
- Не применять `--force-reinstall`.
