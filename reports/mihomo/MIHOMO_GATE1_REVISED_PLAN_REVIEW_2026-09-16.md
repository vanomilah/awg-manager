# Ревью обновлённого плана Mihomo Gate 1

Дата: 2026-09-16  
Проверенный документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Репозиторий: `E:\AWGM\awg-manager`

## Вердикт

План действительно относится к транзакционному механизму Mihomo Gate 1, а не к ИИ-помощнику. Большая часть предыдущих замечаний учтена, но в текущем виде план **ещё нельзя запускать без корректировок**: остаются два блокирующих дефекта контракта и несколько существенных пробелов в recovery/verification.

## Что исправлено правильно

- Добавлен write-ahead state до создания pre-mutation snapshot.
- Snapshot и digest предлагается получать из одного набора сериализованных байтов под одним `RLock`.
- Для `pre_snapshot_write_intent` убран переход в rollback: до мутации допустимы только завершение snapshot, cleanup либо recovery required.
- Уточнено, что после удаления manifest выполняется только изменение in-memory state, а не новая запись перехода.
- Добавлена проверка каталогов с учётом symlink.
- Для временной clean-проверки выбран `mktemp`.
- Добавлены crash-recovery и path-ownership тесты.

## P0 — обязательные исправления плана

### 1. Нельзя заменять существующий интерфейс `NativeStoreTx` сокращённой версией

В реальном `internal/mihomo/types.go` интерфейс уже содержит:

```go
type NativeStoreTx interface {
    SnapshotFilePath(txid string) (string, error)
    CreateSnapshotFile(txid string) (string, error)
    RestoreSnapshotFile(snapshotPath string) error
    RemoveSnapshotFile(snapshotPath string) error
    CurrentDigest() (string, error)
    ListBridges() []BridgeRef
}
```

План показывает другой интерфейс, в котором исчезают `RemoveSnapshotFile` и `ListBridges`, зато появляется отсутствующий сейчас `Mutate`. Это сломает coordinator и реализации.

Требуемая формулировка:

```go
type NativeStoreTx interface {
    SnapshotFilePath(txid string) (string, error)
    CreateSnapshotFile(txid string) (string, error)
    CreateSnapshotFileAt(txid, targetPath string) (digest string, err error)
    RestoreSnapshotFile(snapshotPath string) error
    RemoveSnapshotFile(snapshotPath string) error
    CurrentDigest() (string, error)
    ListBridges() []BridgeRef
}
```

Никакие существующие методы не удалять. `Mutate` не добавлять, если для него нет отдельного обоснованного изменения архитектуры.

### 2. Snapshot digest нельзя записывать вместо `BaseDesiredStoreDigest`

Сейчас `BaseDesiredStoreDigest` вычисляется на preflight в начале `MutateAndApply` и участвует в CAS/invariant-проверках. Его нельзя молча перезаписывать результатом создания snapshot.

После `CreateSnapshotFileAt` необходимо:

1. записать возвращённый digest в `manifest.PreMutationStoreDigest`;
2. сравнить его с уже полученным `manifest.BaseDesiredStoreDigest`;
3. при несовпадении не продолжать мутацию;
4. выполнить безопасный abort/cleanup, а при невозможности cleanup перейти в `recovery_required`.

То есть требуется логика вида:

```go
snapshotDigest, err := c.cfg.StoreTx.CreateSnapshotFileAt(txid, snapPath)
if err != nil {
    return c.abortPreMutationIntentLocked(&manifest, err)
}
manifest.PreMutationStoreDigest = snapshotDigest
if snapshotDigest != manifest.BaseDesiredStoreDigest {
    return c.abortPreMutationIntentLocked(&manifest,
        fmt.Errorf("store changed while securing snapshot: preflight=%s snapshot=%s",
            manifest.BaseDesiredStoreDigest, snapshotDigest))
}
```

Обновлённый manifest с digest должен быть durably записан переходом в `StateSnapshotSecured`.

## P1 — существенные уточнения

### 3. Ошибка создания snapshot после write-ahead intent требует отдельного безопасного cleanup

Фраза «If error, clean up and return error» недостаточна. После записи manifest snapshot-файл мог быть создан частично либо атомарная запись могла оставить временный файл. Нужен конкретный helper, например `abortPreMutationIntentLocked`, который:

- не пытается восстанавливать store;
- создаёт/использует cleanup journal;
- удаляет только артефакты, принадлежащие текущему TxID;
- удаляет manifest последним;
- при любой ошибке cleanup пишет recovery marker и возвращает `ErrRecoveryRequired`.

### 4. Failpoint должен иметь явную crash-семантику

Простой `return errors.New(...)` оставляет manifest на диске, но сам процесс и объект coordinator продолжают жить. Следующий вызов `MutateAndApply` сейчас проверяет только `StateRecoveryRequired`, поэтому тест может моделировать состояние, невозможное в нормальной работе, либо разрешить вторую транзакцию поверх незавершённой.

Нужно:

- ввести отдельную ошибку `ErrSimulatedCrash` только для тестов;
- после её возврата не использовать прежний coordinator;
- в тесте обязательно создать новый `Store`/`ApplyCoordinator` с тех же файлов и вызвать `RecoverOnStartup`;
- убедиться, что failpoint недоступен через production API;
- желательно запретить новый `MutateAndApply`, если manifest уже существует, независимо от in-memory state.

### 5. Ownership validation должна проверять точный ожидаемый путь

Для всех управляемых артефактов основной проверкой должно быть равенство с путём, вычисленным владельцем:

- candidate config — точный `Join(ConfigDir, "config.yaml.candidate."+TxID)`;
- snapshots — точный результат `SnapshotFilePath`;
- generation — точный ожидаемый путь/ID из generation store.

`EvalSymlinks` использовать как дополнительную защиту каталогов, а не вместо exact-match. Следует корректно обрабатывать ошибку `EvalSymlinks`; игнорировать её через `_` нельзя.

### 6. Schema validation не должна изображать root ownership

`ValidateSchemaForPhase` не знает конфигурационные корни. В нём оставить только структурные проверки: absolute/clean path, корректный TxID и допустимый basename. Проверку принадлежности `ConfigDir`, store root и generations root выполнять только в coordinator/store, где известны реальные корни.

### 7. Проверить все реализации интерфейса автоматически

Помимо перечисленных вручную реализаций, после изменения интерфейса выполнить поиск всех типов и fake:

```bash
rg -n "NativeStoreTx|CreateSnapshotFile\(|StoreTx:" internal
```

Добавить compile-time assertions для production adapter и, где уместно, тестовых fake.

## P2 — verification plan

### 8. Пункт «Full test suite» всё ещё не является полной проверкой

Перечень нескольких пакетов — расширенная targeted-проверка, но не full suite. Названия должны быть честными:

- targeted: `./internal/strictfs ./internal/mihomo ./internal/mihomonative`;
- related integration: перечисленные sing-box/tgwebproxy/xrayserver пакеты;
- full backend: `go test -count=1 ./...`.

Если полный прогон не проходит по независимой причине, зафиксировать точную ошибку и не объявлять его успешным.

### 9. Нулевое число untracked-файлов не является корректным критерием в грязном worktree

В репозитории могут находиться пользовательские untracked-файлы. Их нельзя удалять или считать дефектом реализации. Нужно:

- записать baseline `git status --short` до работы;
- после работы сравнить status;
- требовать отсутствие **новых неожиданных** файлов в затронутых пакетах;
- ожидаемые новые тесты/документы явно включить в patch artifact.

### 10. Patch reproduction должен включать новые файлы и проверять реальный baseline

Обычный `git diff` не включает untracked-файлы. Агент должен сформировать patch так, чтобы туда вошли все намеренные новые файлы, не меняя пользовательский index без необходимости. В отчёте отдельно указать:

- baseline commit/index tree;
- список намеренных файлов;
- target tree, построенный во временном index/worktree;
- SHA-256 patch;
- `git apply --check` и reverse-check;
- результат тестов в изолированной копии.

## Исправленные критерии приёмки

План разрешено выполнять после внесения перечисленных выше правок. Реализация принимается только если:

1. существующие методы `NativeStoreTx` сохранены;
2. snapshot digest записывается в `PreMutationStoreDigest`, а не заменяет preflight digest;
3. snapshot digest равен `BaseDesiredStoreDigest` до начала mutation;
4. crash test создаёт новый coordinator и реально проходит startup recovery;
5. ошибка создания/удаления snapshot имеет детерминированный путь в `recovery_required`;
6. exact-path и symlink tests проходят;
7. targeted и race tests проходят;
8. `go test -count=1 ./...` запущен, а его результат честно отражён;
9. пользовательские unrelated/untracked изменения не удалены и не включены в Gate 1 patch;
10. новая версия `GATE1_FINAL_DIFF_2026-09-16.patch` воспроизводится на зафиксированном baseline.

## Итоговая команда другому агенту

Не приступать к изменению кода по текущей редакции `implementation_plan.md`. Сначала обновить план с учётом P0/P1 этого ревью, показать изменённые пункты, затем реализовывать строго ограниченный Gate 1 scope. Не собирать IPK и не выполнять deployment.
