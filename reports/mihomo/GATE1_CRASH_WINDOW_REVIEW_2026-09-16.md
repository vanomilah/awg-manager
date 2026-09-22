# Gate 1: ревью crash-window после обновлённого final patch

Дата: 2026-09-16  
Проверены обновлённые:

- `GATE1_FINAL_DIFF_2026-09-16.patch`
- `GATE1_PRO_FINAL_BLOCKER_RESOLUTION_2026-09-16.md`
- фактическое рабочее дерево

## Вердикт

Оба ранее указанных синхронных post-rename дефекта исправлены. Patch полностью применён к дереву, unit- и race-тесты независимо проходят. Но Gate 1 всё ещё нельзя считать crash-consistent: остаётся окно аварийного завершения процесса между atomic rename candidate config и durable checkpoint manifest. Также обнаружено подавление ошибки удаления staging generation.

Проверки:

```text
git apply --check --reverse GATE1_FINAL_DIFF_2026-09-16.patch PASS
go test -count=1 ./internal/strictfs ./internal/mihomo       PASS
go test -race -count=1 ./internal/mihomo                     PASS
```

## Что подтверждено исправленным

1. `CandidateConfigFile` устанавливается до `StrictWriteAtomic`, поэтому синхронная ошибка после rename позволяет `abortEarlyLocked` удалить candidate.
2. `CandidateGenerationID` входит в manifest до `CANDIDATE_BUILT` и до `PublishStagedBundle`.
3. Candidate generation добавляется в durable cleanup journal.
4. Добавлена защита от удаления active/LKG generation.
5. `FailParentDirFsync` теперь покрывает ошибку публикации после rename.
6. Добавлены проверки clean cleanup и `RECOVERY_REQUIRED` при cleanup failure.

## P0. Путь candidate config не durable до файлового side effect

Текущее присваивание:

```go
manifest.CandidateConfigFile = candidatePath
strictfs.StrictWriteAtomic(candidatePath, ...)
```

существует только в памяти. Manifest записывается на диск позже, при переходе в `CANDIDATE_BUILT`.

Crash-сценарий:

1. manifest на диске находится в `SNAPSHOT_SECURED` либо ещё отсутствует при apply без mutation;
2. `StrictWriteAtomic` успешно переименовывает temp в `config.yaml.candidate.<txid>`;
3. процесс/роутер аварийно завершается до `initManifestLocked` / `transitionManifestLocked(...CANDIDATE_BUILT)`;
4. после запуска durable manifest не содержит `CandidateConfigFile` либо manifest отсутствует;
5. recovery не знает о candidate и не удаляет его.

Текущий тест `FPAfterRenamePreSync` моделирует возвращаемую ошибку, после которой тот же процесс успевает вызвать `abortEarlyLocked`. Он не моделирует crash до abort и поэтому не закрывает это окно.

### Требуемое решение

Перед первым файловым side effect candidate должен существовать durable intent/checkpoint, содержащий как минимум:

- `CandidateConfigFile`;
- `CandidateGenerationID`;
- transaction ID и pre-mutation snapshot reference;
- состояние, однозначно трактуемое startup recovery как «runtime ещё не изменён, восстановить store и удалить candidate artifacts».

Предпочтительно добавить состояние вроде `CANDIDATE_WRITE_INTENT` и записывать его до `StrictWriteAtomic`. Для apply без mutation manifest также должен быть создан до записи candidate.

Не использовать cleanup journal как pre-write intent: startup сейчас обрабатывает cleanup journal раньше transaction manifest, поэтому он способен удалить rollback snapshot до восстановления store.

### Обязательный crash-test

Нужен failpoint сразу после успешного rename candidate, который имитирует прекращение текущего apply без вызова `abortEarlyLocked`. Затем новый coordinator выполняет `RecoverOnStartup`.

Проверить:

- candidate config удалён;
- pre-mutation store восстановлен;
- manifest/snapshot очищены только после восстановления;
- runtime не запускался;
- итоговое состояние детерминировано (`IDLE` при успешном recovery либо `RECOVERY_REQUIRED` при ошибке).

## P0. Ошибка удаления staging generation подавляется

В `RemoveCandidateGeneration`:

```go
_ = secureGens.RemoveAll(".tmp." + genID)
```

Ошибка полностью игнорируется. Если существует только staging directory и его удаление не удалось, метод затем выполняет fsync и возвращает `nil`. Cleanup journal и manifest могут быть удалены, а staging orphan останется без учёта.

Требование:

- проверять результат `RemoveAll(stagingName)`;
- при ошибке возвращать её и сохранять элемент cleanup journal;
- после удаления отдельно проверить отсутствие staging directory;
- лишь затем удалять/проверять final directory и fsync parent.

Добавить failpoint/test, где staging removal завершается ошибкой:

- метод возвращает ошибку;
- cleanup journal сохраняет generation ID;
- manifest/marker сохраняются;
- coordinator не переходит в `IDLE`.

## Неблокирующее замечание

`processCleanupJournalFilesLocked` различает generation и обычный файл по строковому префиксу `gen-`. Надёжнее добавить типизированные cleanup entries (`kind`, `name`) и версию schema. Это можно вынести в следующий Gate, если текущие имена строго валидируются и зарезервированы.

## Следующее задание агенту

1. Добавить durable candidate-write intent до `StrictWriteAtomic` для mutation и no-mutation apply.
2. Добавить crash/restart test, не вызывающий `abortEarlyLocked` после rename.
3. Перестать подавлять staging removal error и добавить failpoint test.
4. Не применять `git checkout`, `git restore`, `git reset` и массовые Python-патчи.
5. Запустить:

```bash
gofmt -w internal/mihomo/coordinator.go \
  internal/mihomo/coordinator_legacy_test.go \
  internal/mihomo/gate1_legacy_test.go \
  internal/mihomo/generation_store.go
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check -- internal/mihomo internal/strictfs
```

6. Не собирать IPK и не выполнять деплой.

## Итог

Синхронные post-rename ошибки закрыты. Остались два crash/durability блокера. После их устранения нужен последний короткий аудит; только затем Gate 1 можно принять.
