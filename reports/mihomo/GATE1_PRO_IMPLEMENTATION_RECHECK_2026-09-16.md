# Повторная проверка реализации Gate 1 после execution order

Дата: 2026-09-16  
Источник заявленных результатов: attachment `81aaa9f6-008a-49f5-b78c-331b8da61f5f/pasted-text.txt`

## Вердикт

Прогресс существенный, но Gate 1 **еще не принят**. Основной commit boundary и общий final commit helper стали заметно правильнее, S01–S24 возвращены, новые тесты добавлены, Linux unit/race проходят. Однако остаются блокирующие дефекты rollback, cleanup и bridge recovery, а staged snapshot неполон и не является самодостаточным изменением.

Статус: **оставить на Gemini Pro; выполнить короткий исправительный проход; Flash пока не использовать**.

## Независимо подтверждено

На Linux/WSL проходят:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
```

В `go test -list` присутствуют S01–S24, retention GC и девять новых Gate 1 тестов.

Windows-прогон в текущей сессии не состоялся из-за `Access is denied` в системном Go build cache. Это не доказательство дефекта кода, но приложенный результат должен явно сообщать, в какой ОС выполнялась каждая команда.

## Что действительно исправлено

- `StateCommitIntent` больше не разрешает переход в rollback.
- RuntimeOn и RuntimeOff используют общий `executeFinalCommitLocked`.
- Pointer failure после commit intent приводит к recovery error, а не к успеху.
- Final commit перечитывает verified-active и pointer.
- RuntimeOff обновляет pointer.
- Checkpoint/transition строят отдельный `next` и публикуют его в память после CAS.
- Legacy Gate 1 tests возвращены.
- Linux fd double-close и directory fsync в `SecureDir` исправлены.
- Появился per-operation bridge progress и базовый тест partial success.

## P0 — оставшиеся блокирующие дефекты

### 1. Rollback по-прежнему возвращает успех при фактическом провале

Файл: `internal/mihomo/coordinator.go:1119-1170`.

`rollbackActiveLocked` подавляет ошибки:

- перехода в rollback state;
- остановки процесса;
- восстановления store snapshot;
- чтения generation bundle/config;
- записи active config;
- удаления active config;
- перехода в rolled_back.

Ошибки лишь пытаются записаться в marker, после чего функция всегда возвращает `nil`. Затем `handleApplyFailureLocked` считает rollback успешным, запускает cleanup и ставит `idle`.

Это может уничтожить manifest после неудавшегося восстановления и оставить config/store/runtime в разных поколениях.

Требование:

- агрегировать/возвращать первую durability-critical ошибку;
- не переходить в rolled_back без проверки store/config/runtime/bridges;
- при ошибке сохранить manifest и cleanup artifacts, выставить recovery required;
- не ставить idle и не удалять manifest.

### 2. Cleanup API не возвращает результат и final commit всегда ставит idle

Файлы: `coordinator.go:447-571`, `executeFinalCommitLocked:1434-1437`.

`processCleanupJournalFilesLocked` и `cleanupTxArtifactsLocked` имеют `void`-сигнатуры. Даже если:

- journal не записался;
- transition в terminal cleanup не записался;
- файл не удалился;
- manifest/journal не удалился,

`executeFinalCommitLocked` безусловно делает `StateIdle` и возвращает `nil`.

Особенно опасно: если transition в `StateTerminalCleanup` не удался, код все равно продолжает физическое удаление и может удалить manifest со `StateCommitted`.

Требование:

- оба helper должны возвращать error/result;
- при partial cleanup сохранять journal и manifest;
- StateIdle ставить только после подтвержденного отсутствия всех entries, cleanup journal и terminal manifest;
- cleanup failure после committed не отменяет commit, но возвращает observable cleanup-pending/recovery результат.

Текущий тест `CleanupPartialFailure_PreservesJournal` вызывает низкоуровневый helper напрямую и не проверяет, что `executeFinalCommitLocked` не ставит idle при partial failure. Добавить end-to-end test.

### 3. RuntimeOff bridge withdrawal обходит per-operation journal

Файл: `coordinator.go:947-958`.

RuntimeOn использует `syncBridgesLocked`, но RuntimeOff вызывает один групповой `WithdrawBridges` напрямую. Intent/applied/verified для каждого bridge не сохраняются. При partial success/crash нельзя понять, какие withdrawal уже выполнены.

RuntimeOff должен вызвать тот же per-operation reconciler с empty target set.

### 4. Bridge error checkpoints все еще подавляются

Файл: `coordinator.go:1076-1095`.

При apply/verify error запись `LastError` выполняется через:

```text
_ = c.transitionManifestLocked(m, m.State)
```

Если checkpoint не записался после побочного эффекта, функция возвращает только исходную bridge error, скрывая невозможность durable зафиксировать наблюдаемый результат.

Нужно объединять ошибку операции и checkpoint error, переводить транзакцию в recovery required и сохранять manifest. Тест должен инъецировать failure именно в checkpoint после applied side effect.

### 5. Recovery rollback не откатывает bridge operations

`rollbackActiveLocked` восстанавливает store/config/process, но не выполняет компенсирующие операции по `BridgeOperations`. До commit boundary созданные текущей транзакцией bridges должны быть удалены, а withdrawn previous bridges восстановлены и verified.

Иначе rollback не возвращает систему к previous generation.

### 6. Staged diff неполон

Приложенный `git diff --cached --stat` содержит только шесть файлов. При этом важные новые зависимости остаются untracked и не staged, включая:

- `internal/strictfs/fs.go`;
- `internal/strictfs/lock_linux.go`;
- `internal/strictfs/lock_windows.go`;
- `internal/strictfs/secure_dir_windows.go`;
- `internal/mihomo/coordinator_cas_test.go`;
- `internal/mihomo/types_test.go`;
- `internal/mihomo/error.go`, `validator.go` и другие используемые файлы.

Если передать/закоммитить только staged snapshot, сборка будет неполной. Проверять нужно весь working tree, а не только `--cached`.

Требование:

- не выполнять бездумный `git add .`, так как дерево содержит множество иных изменений;
- составить точный allowlist файлов Gate 1;
- показать `git status --short`, `git diff --check` и полный scoped diff/stat, включающий необходимые untracked dependencies;
- не заявлять «6 files changed» как полный объём реализации.

## P1 — дополнительные замечания

### 7. Recovery required остается только in-memory при некоторых commit failures

`executeFinalCommitLocked` сохраняет durable manifest в `commit_intent` и выставляет `c.state = recovery_required`. Это допустимо для roll-forward, но marker write errors часто игнорируются. Следует явно считать durable `commit_intent` источником истины и проверить restart test без marker.

### 8. Проверка verified-active неполная

После reread сравниваются GenerationID и Generation. Нужно также сравнить store/config/input digests, runtime mode, listeners/bridges либо полный canonical digest записи. Pointer проверяет config digest, но не все поля.

### 9. Bridge TargetDigest выбран неверно

`TargetDigest` устанавливается равным digest всего desired store. Для точной идемпотентности bridge operation нужен digest canonical BridgeRef/spec конкретной операции. Иначе изменение несвязанной настройки меняет identity операции.

### 10. Отчет о `gofmt` неоднозначен

Команда `gofmt -w ./internal/strictfs ./internal/mihomo` обычно должна получать файлы, а не каталоги. Для воспроизводимости показать реальную команду со списком измененных `.go` файлов либо использовать безопасное перечисление файлов в одной среде.

## Обязательный следующий проход Pro

1. Сделать rollback fail-closed и верифицируемым.
2. Вернуть errors из cleanup helpers; запретить idle при partial cleanup.
3. Провести RuntimeOff withdrawals через `syncBridgesLocked`.
4. Не подавлять bridge checkpoint errors.
5. Добавить compensating bridge rollback/reconcile.
6. Усилить reread equality final commit.
7. Использовать per-bridge spec digest.
8. Добавить end-to-end tests на rollback failure, cleanup failure и bridge compensation.
9. Показать полный scoped working-tree diff, а не только cached шесть файлов.

## Повторная приемка

Нужны:

```text
git status --short
git diff --check
go test -count=1 ./internal/strictfs ./internal/mihomo
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/strictfs ./internal/mihomo"
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/mihomo"
go test -list . ./internal/mihomo
```

И отдельные тесты:

- rollback restore failure сохраняет manifest и recovery state;
- committed cleanup partial failure не приводит к idle;
- RuntimeOff partial bridge withdrawal восстанавливается после restart;
- bridge checkpoint failure после side effect восстанавливается детерминированно;
- rollback компенсирует bridge partial success.

## Решение

- Оставить на **Gemini Pro**.
- Flash пока не использовать.
- IPK не собирать.
- Деплой не выполнять.
- `--force-reinstall` не использовать.
