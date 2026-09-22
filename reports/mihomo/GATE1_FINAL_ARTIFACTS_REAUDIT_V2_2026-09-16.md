# Gate 1 Final Resolution v2 — повторный аудит

Дата: 2026-09-16

Проверены обновлённые:

- `GATE1_FINAL_DIFF_2026-09-16.patch`;
- `GATE1_PRO_FINAL_BLOCKER_RESOLUTION_2026-09-16.md`;
- фактически применённый код в `internal/mihomo` и `internal/mihomonative`.

## Вердикт

**Gate 1 всё ещё не закрыт.** Большая часть замечаний предыдущего аудита исправлена корректно, и заявленные целевые тесты действительно проходят. Однако остался как минимум один P0 crash-window до создания первого durable manifest, а заявленная строгая проверка путей фактически не обеспечивает принадлежность файлов разрешённому каталогу.

Статус отчёта «ВСЕ P0 И P1 ПОЛНОСТЬЮ УСТРАНЕНЫ» не подтверждён.

## Подтверждённая верификация

Фактически выполнено в WSL:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo ./internal/mihomonative
ok github.com/hoaxisr/awg-manager/internal/strictfs
ok github.com/hoaxisr/awg-manager/internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomonative

go test -race -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo

git diff --check -- internal/mihomo internal/strictfs internal/mihomonative
PASS

git apply --check --reverse GATE1_FINAL_DIFF_2026-09-16.patch
PASS
```

Прохождение этих тестов подтверждено, но тесты не моделируют найденный ниже P0.

## Findings

### P0 — pre-mutation snapshot создаётся до первого durable intent

В `internal/mihomo/coordinator.go:821-841` выполняется следующая последовательность:

1. вычисляется путь pre-mutation snapshot;
2. путь записывается только в in-memory `manifest`;
3. вызывается `CreateSnapshotFile(txid)` — файл физически создаётся;
4. только после этого вызывается `initManifestLocked(..., StateSnapshotSecured)`.

При аварии процесса, отключении питания или kernel panic между шагами 3 и 4 на диске останется `store.snapshot.<txid>.json`, но durable manifest ещё отсутствует. Startup recovery не знает TxID и точный путь, поэтому удалить этот snapshot не сможет.

Это тот же класс ошибки, который был исправлен для post-mutation snapshot, но не для pre-mutation snapshot. Формулировка отчёта «Write-Ahead Intent для всех файловых артефактов» неверна.

#### Требуемое исправление

До `CreateSnapshotFile(txid)` должен быть durably записан точный cleanup target. Нужен отдельный этап, например:

```text
idle -> pre_snapshot_write_intent -> snapshot_secured
```

`pre_snapshot_write_intent` обязан содержать canonical `PreMutationStoreSnapshotFile`, но ещё не утверждать, что snapshot существует и fsync завершён. Recovery этого состояния:

- удаляет snapshot, если он успел появиться;
- не пытается восстанавливать store из потенциально отсутствующего/незавершённого snapshot;
- очищает manifest только после подтверждённого удаления либо переходит в recovery_required.

Нужен crash/restart test, возвращающий управление сразу после успешного `CreateSnapshotFile(txid)`, но до `StateSnapshotSecured`, без вызова обычного abort cleanup.

### P1 — path validation не является canonical ownership validation

В `internal/mihomo/types.go:441-459` путь считается безопасным, если он не содержит буквальную подстроку `..`:

```go
if strings.Contains(path, "..") {
    return fmt.Errorf(...)
}
```

Эта проверка:

- принимает абсолютный путь в любой другой каталог;
- не проверяет, что snapshot принадлежит каталогу native store;
- не проверяет, что candidate config принадлежит `ConfigDir`;
- не связывает basename с текущим `TxID`;
- не учитывает symlink/reparse-point escape;
- одновременно отвергает легитимные имена с двумя точками, хотя это не обязательно traversal.

В отчёте заявлена «строгая проверка path traversal», но реализована лишь проверка строки.

#### Требуемое исправление

Проверка должна выполняться там, где известны разрешённые roots, а не только в `TransactionManifest.ValidateSchemaForPhase()` без контекста каталогов.

Минимум:

1. `filepath.Clean` и абсолютное разрешение root/target.
2. Проверка через `filepath.Rel`, что target не выходит за ожидаемый root.
3. Проверка точного ожидаемого basename, сформированного из валидированного `TxID`.
4. Для candidate config — точное равенство ожидаемому `ConfigDir/config.yaml.candidate.<txid>`.
5. Для snapshots — точное равенство пути, возвращённому `SnapshotFilePath(txid)` и `SnapshotFilePath(txid+"-post")`.
6. Файловые операции — через secure-dir/no-follow primitives, где это применимо.

Нужны тесты с абсолютным внешним путём без `..`, sibling-prefix каталогом и symlink escape.

### P1 — контракт `SnapshotFilePath` и `CreateSnapshotFile` не доказывает создание по одному пути

Coordinator сначала получает expected path через `SnapshotFilePath`, затем вызывает `CreateSnapshotFile`, но игнорирует возвращённый им фактический путь (`coordinator.go:830`, `901-903`). Интерфейс не гарантирует на уровне типа, что оба вызова используют одно и то же местоположение.

Production `mihomonative.Store` сейчас ведёт себя согласованно, но другая реализация `NativeStoreTx` может создать файл по другому пути. При crash этот фактический файл станет orphan, а manifest будет указывать на expected path.

Предпочтительно заменить двухшаговый контракт на создание по заранее зафиксированному target, например `CreateSnapshotFileAt(snapshotID, expectedPath)`. Если интерфейс пока сохраняется, необходимо как минимум сверять returned path с expected path и документировать строгую инварианту; однако сравнение после создания не закрывает crash между созданием и возвратом, поэтому `Create...At` надёжнее.

### P1 — phase validation не соответствует заявлению отчёта о digest

Отчёт утверждает, что для `StateCandidateWriteIntent` при `ConfigPresent=true` проверяются `CandidateConfigFile` и `CandidateConfigDigest`. Реальный код `types.go:466-472` требует только `CandidateConfigFile`; digest там не проверяется.

Здесь возможны два корректных варианта:

- если digest относится к скомпилированным bytes и уже известен до file write, проверять его в write-intent;
- если протокол намеренно допускает отсутствие digest до `candidate_built`, исправить текст отчёта и добавить явное объяснение инварианты.

Сейчас код и итоговый документ расходятся.

### P1 — baseline patch остаётся локальным индексным снимком без идентификатора

Отчёт заявляет проверку forward apply против `git write-tree`, но не фиксирует полученный tree hash. Рабочее дерево существенно грязное, файлы Gate 1 имеют статус `AM`, поэтому «index baseline» невозможно воспроизвести другому агенту без точного tree object/commit и списка подготовительных изменений.

Для передаваемого patch требуется указать:

- точный commit либо tree hash baseline;
- точный hash итогового дерева;
- SHA-256 patch;
- проверку forward apply в чистом worktree, созданном именно от опубликованного baseline.

`reverse --check` в текущем грязном дереве подтверждает наличие изменений, но не переносимость артефакта.

## Что исправлено корректно

- Post-mutation snapshot теперь имеет durable path до файлового side effect.
- Broad scan каталога по TxID не используется.
- Failpoints pre-existing staging cleanup и abort cleanup разделены.
- Подавленная ошибка первичного `RemoveAll` устранена.
- Проверки удаления staging/final generation принимают успех только при `os.IsNotExist`.
- `StateCandidateWriteIntent` перенесено в общую state machine.
- Старый переход `snapshot_secured -> candidate_built` запрещён.
- `transitionManifestLocked` теперь применяет `IsValidNext` и phase validation.
- До swap `rollbackActiveLocked` больше не вызывает `StopAndWait`; spy-тест проверяет отсутствие stop/start.

## Минимальный порядок завершения Gate 1

1. Добавить durable `pre_snapshot_write_intent` до создания pre-mutation snapshot.
2. Добавить crash test между созданием pre-snapshot и `snapshot_secured`.
3. Заменить строковую проверку `..` на root-aware canonical ownership validation.
4. Устранить разрыв контракта `SnapshotFilePath` / `CreateSnapshotFile`.
5. Согласовать digest-инварианту кода и отчёта.
6. Зафиксировать воспроизводимый baseline patch точным hash.
7. Повторить unit/race/failpoint tests и clean forward apply.

## Итог для агента

Не переписывать уже корректно сделанные изменения. Сосредоточиться на самом раннем pre-snapshot crash-window и реальной проверке владения путями. После этого обновить итоговый отчёт без утверждений, которые не совпадают с кодом.
