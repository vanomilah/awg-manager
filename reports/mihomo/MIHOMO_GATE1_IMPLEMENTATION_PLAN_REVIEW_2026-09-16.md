# Ревью плана исправления Mihomo Gate 1

Дата: 2026-09-16  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Основание: `MIHOMO_GATE1_AGENT_WORK_AUDIT_2026-09-16.md`

## Вердикт

План движется в правильном направлении, но **в текущем виде не готов к запуску в работу как полный план Gate 1**.

Он закрывает названия нескольких обнаруженных проблем, однако не задает достаточно точную архитектуру и критерии приемки. Если начать реализацию буквально по этому тексту, агент сможет снова отметить пункты выполненными, сохранив критические пробелы в durability, recovery и защите файловой системы.

Статус: **условно одобрить только после внесения перечисленных ниже обязательных дополнений**.

## Что в плане сделано правильно

- UI явно отложен до исправления backend.
- Признана необходимость descriptor-relative Linux filesystem API.
- CAS предполагает проверку `TxID`, `Sequence` и `State` по durable manifest.
- Учтен post-mutation snapshot кандидата.
- Учтен `RuntimeOff` без обязательного `config.yaml`.
- Упомянута защита поколений от GC.
- В verification включены уже падающие S10, S15–S17 и S23.

## Обязательные исправления плана

### 1. Одних `Openat`, `Unlinkat` и `Renameat` недостаточно

Нужно явно зафиксировать:

- корневой каталог открывается один раз и хранится как fd;
- `SecureDir` реализует `Close()` и корректное владение fd;
- каждый компонент пути открывается descriptor-relative;
- запрещены symlink, magic link и выход выше корня;
- предпочтительно использовать `openat2` с `RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS | RESOLVE_NO_MAGICLINKS`;
- fallback на `openat` обязан использовать `O_NOFOLLOW`, проверку `fstat` и только одноуровневые validated basenames там, где это возможно;
- atomic write выполняется через временный файл в том же directory fd, `fsync(file)`, `renameat`, `fsync(directory)`;
- recursive removal не должен переходить по symlink;
- Windows fallback не должен называться эквивалентно защищенным и используется только для разработки/тестов.

Нужны Linux-тесты на symlink swap, parent rename и попытку выхода из корня. Одного Windows fallback test недостаточно.

### 2. CAS должен работать с immutable current/next

Сигнатуры недостаточно. План должен потребовать:

1. прочитать durable current;
2. провалидировать schema и ownership;
3. сравнить expected `(TxID, Sequence, State)`;
4. проверить допустимость перехода state machine;
5. создать и валидировать immutable `next` с `Sequence = current.Sequence + 1`;
6. выполнить atomic durable write;
7. при любой неоднозначной ошибке перечитать manifest;
8. классифицировать результат как committed, not committed либо recovery required;
9. менять in-memory state только после подтвержденного результата.

Нужны тесты stale writer, duplicate retry, failure before rename, failure after rename/before directory fsync и corrupted durable manifest.

### 3. Нужна полная таблица state machine

Фраза «strengthen validation» слишком расплывчата. До кодинга нужно перечислить состояния и разрешенные переходы, включая как минимум:

- prepared/snapshot secured;
- candidate built/validated/published;
- swap intent/applied/verified;
- runtime intent/applied/verified;
- bridge create/withdraw intent/applied/verified;
- commit intent/committed;
- abort/rollback in progress и terminal state;
- recovery required.

Следует явно определить point of no return: после durable `CommitIntent` автоматический rollback кандидата запрещен; разрешено только завершение commit/recovery.

### 4. `RuntimeOff` описан недостаточно

«Properly records intents» нельзя считать задачей с проверяемым результатом. План должен расписать порядок:

1. опубликовать candidate generation bundle с `ConfigPresent=false`;
2. записать intent удаления active config;
3. удалить/переименовать config и подтвердить результат;
4. записать stop intent;
5. остановить процесс и проверить, что он действительно остановлен;
6. записать bridge withdrawal intents по каждому bridge;
7. выполнить и верифицировать withdrawal;
8. записать commit intent;
9. атомарно обновить verified-active и generation pointer;
10. завершить commit;
11. выполнить повторяемый cleanup через журнал.

Ни одна durability-critical ошибка не должна игнорироваться через `_ =`.

### 5. Candidate bundle должен публиковаться до destructive swap

Нужно прямо добавить отдельный шаг `CandidatePublished` до замены active config или остановки runtime. Bundle должен быть самодостаточным и содержать:

- generation manifest;
- candidate post-mutation store snapshot;
- config только при `ConfigPresent=true`;
- runtime mode;
- listener и bridge state;
- все digests;
- parent/previous generation ID.

`ReadGenerationBundle` обязан сверять все digests, а не только наличие файлов.

### 6. GC protected set указан неполно

Нельзя ограничиваться `LKGPointer` и active manifest. Protected set должен включать ссылки из:

- verified-active;
- LKG pointer;
- transaction manifest;
- draft/pending input;
- rollback/recovery target;
- cleanup journal;
- candidate generation до завершения транзакции.

GC должен возвращать ошибки, не использовать безусловный best-effort `RemoveAll` и быть идемпотентным.

### 7. План пропускает Bridge Lifecycle и Cleanup Journal

В исходном `task.md` Slice 4 и Slice 5 остаются невыполненными. Новый план не может называться исправлением Gate 1, пока не добавит:

- точную модель bridge operations: stable operation ID, desired spec digest, intent/applied/verified, retry/reconcile;
- rollback только операций текущей транзакции до commit boundary;
- cleanup journal с `Version`, `Sequence`, `TxID`, generation IDs и typed entries;
- durable checkpoint до каждого удаления;
- повторный cleanup после рестарта;
- удаление cleanup journal только после подтверждения отсутствия всех объектов.

### 8. Нужен отдельный этап Generation Bundle Finalization и Idle lifecycle

Slice 6 и Slice 7 также отсутствуют. Добавить:

- атомарную публикацию bundle и pointer;
- reconciliation verified-active, pointer и manifest после рестарта;
- terminal manifest lifecycle;
- доказуемый переход в `idle` только при отсутствии незавершенного recovery/cleanup;
- запрет удаления manifest до durable terminal checkpoint.

### 9. Verification plan слишком слабый

Добавить обязательные проверки:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check
```

Основная filesystem/recovery проверка должна выполняться на Linux. Также нужны:

- failpoints для каждой границы write/fsync/rename/directory-fsync;
- crash/restart matrix до и после каждого intent/applied/verified checkpoint;
- RuntimeOn и RuntimeOff;
- first install без предыдущего поколения;
- rollback с предыдущим поколением;
- concurrent cancel после commit boundary;
- bridge partial success;
- cleanup partial failure;
- GC с каждой разновидностью живой ссылки;
- end-to-end equality между bundle, active runtime и verified-active после рестарта.

IPK и деплой разрешать только после зеленых Linux-тестов.

## Рекомендуемая структура выполнения

### Этап A — спецификация

- Полная state-transition table.
- Commit boundary и recovery rules.
- Manifest/generation/cleanup schemas.
- Protected generation reference graph.

### Этап B — strictfs

- Linux descriptor-relative implementation.
- Windows development fallback.
- Race/security/durability tests.

### Этап C — manifest CAS

- Immutable current/next.
- Durable compare.
- Ambiguous outcome resolver.
- Transition validation.

### Этап D — immutable generations

- Candidate post-mutation snapshot.
- RuntimeOn/RuntimeOff bundles.
- Digest verification.
- Candidate publication before swap.

### Этап E — runtime transaction

- Swap/process intent-applied-verified.
- CommitIntent.
- Rollback/recovery matrix.

### Этап F — bridges and cleanup

- Per-operation journal.
- Idempotent reconcile.
- Durable cleanup journal.

### Этап G — GC and idle lifecycle

- Complete protected set.
- Restart reconciliation.
- Safe terminal cleanup and idle.

### Этап H — acceptance

- Full Linux test matrix.
- Race tests and diff check.
- Only then build/package/router integration.

## Указание агенту

Сначала обновить сам `implementation_plan.md` указанными пунктами и показать его повторно. Не начинать частичную реализацию по текущей сокращенной версии. Не отмечать slice выполненным, пока его acceptance tests не проходят. Не собирать IPK и не выполнять деплой до полной backend-приемки. Не использовать `--force-reinstall`.
