# Ревью плана исправления Mihomo Gate B

**Дата:** 2026-09-21  
**Проверен:** `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
**Вердикт:** **REVISE — в реализацию пока не запускать.**

План правильно перечисляет все 6 P0, 2 P1 и недостаток crash/restart-тестов из приёмочного ревью. Однако ключевые recovery-протоколы описаны только намерениями. В текущем виде исполнитель будет вынужден самостоятельно придумать state machine и снова может получить зелёные тесты при недоказанной crash consistency.

## Блокирующие замечания к плану

### 1. Не определены durable checkpoints для `rollback_to_lkg`

План требует «checkpoint после каждого необратимого шага», но предлагает только общее состояние `StateRollbackInProgress`. Одного состояния недостаточно, чтобы после рестарта отличить:

- store ещё не восстановлен от store уже восстановлен;
- config ещё не заменён от config уже заменён;
- прежний процесс ещё работает от процесса уже остановленного;
- новый процесс запущен, но не доказан;
- bridges частично применены от bridges полностью проверены.

Нужно до начала реализации выбрать один явный вариант:

1. отдельные состояния для каждой границы; либо
2. durable `RecoveryStep`/receipt-поля в manifest с CAS и строгой схемой для каждого шага.

Для каждого checkpoint план обязан задать: данные до side effect, выполняемый side effect, durable proof после него и действие startup recovery.

### 2. Не разрешён конфликт с уже существующим manifest

`regenerate_from_desired` и `rollback_to_lkg` вызываются именно в degraded/recovery состоянии, где на диске обычно уже существует manifest незавершённой транзакции. Одновременно план требует create-only семантику `casManifest(nil, next)`.

Просто «создать новый manifest» невозможно без потери старого доказательства. План должен определить один протокол:

- продолжать существующий manifest, переводя его CAS-переходом в административную recovery-ветку; либо
- атомарно архивировать старый manifest с digest/ссылкой из нового recovery manifest; либо
- хранить отдельный recovery-operation manifest, не перезаписывая исходный transaction manifest.

Без этого исправление Defect 8 сломает начало обеих административных операций либо исполнитель снова разрешит перезапись.

### 3. Snapshot создаётся до durable intent

План пишет «создать `StateRecoveryIntent` до любой мутации», но в предлагаемом порядке сначала снимается store snapshot. Создание snapshot — файловый side effect: crash оставит неучтённый артефакт, а его путь не будет известен recovery.

Нужны `PreSnapshotWriteIntent -> SnapshotSecured` либо эквивалентные durable step receipts. Путь snapshot должен быть вычислен, проверен на confinement и записан до создания файла; digest — checkpoint после создания.

### 4. Указан несуществующий метод recovery

План предлагает изменить `recoverInterruptedTransactionLocked`, но в текущем коде recovery выполняет `recoverManifestLocked` (`internal/mihomo/coordinator.go:706`). Исполнителю нельзя создавать второй конкурирующий startup recovery entrypoint без отдельного архитектурного решения.

Нужно явно указать изменение существующего `recoverManifestLocked` и таблицу поведения для каждого нового состояния/шага.

### 5. `StateBridgesReconciling` уже существует

Это состояние уже определено в `types.go` и участвует в основной apply state machine. План должен не «добавлять» его, а определить его использование в административной ветке и допустимые переходы.

Предложение `StateBridgesReconciling -> StateRecoveryCommitted` конфликтует с текущим переходом `StateBridgesReconciling -> StateCommitIntent`. Нельзя смешивать обычный apply commit и administrative recovery commit без явного разделителя operation kind.

Нужно добавить typed поле вроде `OperationKind: apply | regenerate | rollback`, а `ValidateSchemaForPhase` и startup recovery должны учитывать пару `(OperationKind, State)`.

### 6. Семантика `ProcessReceipt` не определена

План одновременно предлагает сохранить receipt в immutable generation bundle и требовать его совпадения при повторной активации LKG. Но receipt содержит идентичность конкретного процесса. После rollback/restart PID/start-time/daemon epoch закономерно будут другими.

Нужно разделить:

- immutable generation facts: config/store/input digest, listeners, bridges, runtime mode;
- activation/runtime receipt: фактический процесс текущего запуска и его epoch.

Старый receipt можно хранить как историческое доказательство первоначальной активации, но нельзя требовать его равенства новому receipt после rollback. `verified-active.json` должен содержать новый activation receipt; bundle должен проверять декларативную идентичность поколения.

### 7. «Полное структурное равенство» требует канонизации

План не определяет:

- считается ли `nil` равным пустому slice;
- значим ли порядок listeners и bridges;
- какие timestamp сравниваются буквально;
- как сравниваются optional receipt и version;
- должна ли проверяться лишняя/неизвестная JSON-структура.

Нужна одна canonical DTO/serialization функция и тесты на перестановку, `nil`/empty, дубликаты и неизвестные поля. Иначе ручные equality helpers снова дадут частичную проверку.

### 8. Legacy fallback нельзя автоматически превращать в LKG без store proof

Один `config.yaml.lkg` не является полным поколением: у него нет доказанного store snapshot, bridge set, input digest и runtime receipt. План предлагает опубликовать из него bundle и продвинуть LKG pointer, что может легализовать смешанное состояние.

Безопасные варианты:

- legacy fallback только как ручной migration flow: snapshot текущего store, compile/validate, runtime proof, bridge proof, затем создание нового generation; либо
- отказ с `RecoveryRequired` и понятной диагностикой, если полное поколение построить невозможно.

Называть копирование одного YAML `rollback_to_lkg` нельзя.

### 9. Crash-тесты пока описаны названием, а не механизмом

Обычный failpoint, возвращающий ошибку, не моделирует crash: deferred cleanup продолжает выполняться. План тестов должен потребовать crash hooks, которые прекращают протокол после конкретного side effect **без cleanup**, после чего создаётся новый coordinator на той же директории.

Для каждого checkpoint тест обязан проверить:

1. байты store и config;
2. process identity и config digest процесса;
3. listener ownership;
4. точный bridge set;
5. verified-active, bundle, LKG pointer и epoch;
6. отсутствие продвижения LKG до commit proof;
7. второй restart без дополнительных изменений.

Нужна таблица test case -> crash boundary -> ожидаемая ветка recovery -> конечное состояние.

## Дополнительные необходимые уточнения

- Все ошибки marker/manifest/journal cleanup должны приводить не просто к marker, а к durable terminal-cleanup состоянию, которое startup повторяет идемпотентно.
- Для RuntimeOff требуется такой же полный протокол: доказать отсутствие процесса и отсутствие принадлежащих Mihomo listeners, а не только вызвать `StopAndWait`.
- Ошибка записи recovery marker сама не должна теряться: вернуть joined error и оставить coordinator fail-closed в памяти.
- Bridge readback должен проверять точное множество и ownership, а не только успешный возврат `syncBridgesLocked`.
- Верификация LKG pointer epoch выполняется против текущего daemon epoch только сразу после `advanceLKG=true`. При `advanceLKG=false` старый pointer закономерно может принадлежать предыдущему daemon epoch.

## Требуемая редакция плана перед запуском

План должен содержать:

1. точную схему administrative recovery manifest;
2. список новых полей и их обязательность по фазам;
3. полную таблицу state/step transitions для regenerate и rollback;
4. алгоритм обработки уже существующего transaction manifest;
5. алгоритм startup recovery для каждой durable boundary;
6. разделение immutable generation facts и activation receipt;
7. безопасную политику legacy migration;
8. конкретную crash/restart test matrix;
9. запрет Gate C, IPK build и deploy до отдельной приёмки Gate B.

После этих уточнений план можно запускать в работу. Простого выполнения текущего списка изменений недостаточно для приёмки Gate B.

