# Приёмочное ревью Mihomo Remediation Gate B

**Дата:** 2026-09-21  
**Проверенный отчёт:** `MIHOMO_REMEDIATION_GATE_B_RESOLUTION_REPORT_2026-09-21.md`  
**Вердикт:** **REJECTED — Gate B не принят, Gate C начинать нельзя.**

Заявленные тесты проходят, однако они не доказывают crash consistency протокола. В реализации остаются окна, в которых recovery может удалить или заменить активную конфигурацию, оставить процесс остановленным либо завершиться успешно без доказательства требуемого состояния.

## P0 — блокирующие дефекты

### 1. Верификация поколения допускает отсутствие или повреждение generation bundle

`persistAndVerifyGenerationLocked` вызывает `ReadGenerationBundle`, но выполняет проверку только при `bErr == nil && gm != nil`. Любая ошибка чтения, разбора или проверки bundle просто игнорируется, после чего функция обновляет `appliedRecord` и возвращает успех.

**Код:** `internal/mihomo/coordinator.go:1959-1989`.

Это противоречит fail-closed модели и позволяет признать generation verified/LKG, когда его архивное доказательство отсутствует или повреждено.

**Исправление:** для непустого `GenerationID` любой `ReadGenerationBundle` error или `gm == nil` обязан установить `StateRecoveryRequired`, сохранить marker и вернуть ошибку. Нужен тест с удалённым и отдельно повреждённым `generation.manifest.json`.

### 2. Readback не подтверждает полный объект и daemon epoch

Проверка `verified-active.json` сравнивает только ID, номер, три digest и RuntimeMode. Не сверяются `AppliedListeners`, `AppliedBridges`, `ProcessReceipt`, версия записи и другие поля. Проверка LKG pointer не сверяет `UpdatedEpoch`, хотя pointer записывается с `DaemonEpoch()`.

**Код:** `internal/mihomo/coordinator.go:1923-1957`; структура epoch — `internal/mihomo/types.go:263-271`.

Кроме того, `GenerationManifest` содержит `ProcessReceipt`, но `PublishStagedBundle` его не копирует.

**Код:** `internal/mihomo/types.go:248-260`, `internal/mihomo/generation_store.go:167-178`.

**Исправление:** каноническое полное сравнение persisted record; проверка `UpdatedEpoch == c.DaemonEpoch()`; сохранение и проверка process receipt в bundle. Поля времени допускается нормализовать явно, но это должно быть задокументировано и протестировано.

### 3. `regenerate_from_desired` публикует успех при ошибке bridge reconciliation

После durable `StateRuntimeVerified` ошибки `ListActiveBridges` и `syncBridgesLocked` игнорируются. Затем LKG pointer продвигается и recovery объявляется завершённым.

**Код:** `internal/mihomo/coordinator.go:2541-2555`.

Это позволяет зафиксировать generation, которая не соответствует реально применённым bridge-интерфейсам.

**Исправление:** отдельное durable состояние `StateBridgesReconciling`; обе ошибки обязательны к возврату; перед commit требуется readback/proof фактических bridges. При ошибке сохранять marker и recovery state, LKG не продвигать.

### 4. Новые recovery-состояния не содержат данных для безопасного startup rollback

Manifest для `regenerate_from_desired` не заполняет pre-mutation snapshot, LKG generation ID и полные факты исходного состояния. При старте `StateRecoveryIntent` и `StateConfigPromoted` передаются в общий `rollbackActiveLocked`.

**Код:** `internal/mihomo/coordinator.go:2452-2469`, `internal/mihomo/coordinator.go:747-767`.

`rollbackActiveLocked`:

- не может восстановить store без `PreMutationStoreSnapshotFile`;
- не может выбрать точный LKG bundle без `LKGGenerationID`;
- после остановки оператора не запускает восстановленное поколение заново;
- берёт целевые bridges из `c.appliedRecord`, который после холодного старта может быть пуст;
- способен удалить active config и завершить rollback как успешный.

**Код:** `internal/mihomo/coordinator.go:1715-1846`.

**Исправление:** административная recovery-операция должна иметь собственный полный durable manifest до первого side effect: pre-store snapshot, previous/LKG generation, active-config fact/digest, process/runtime fact, bridges before/target. Для каждого состояния нужен детерминированный resume/rollback с итоговым runtime proof, а не включение новых состояний в старый общий switch.

### 5. `rollback_to_lkg` сам не является crash-consistent транзакцией

Восстановление store, config, процесса и bridges выполняется последовательно без заранее записанного административного recovery manifest и без durable checkpoint после каждого необратимого шага. Падение между restore store и restore config либо между stop/start/bridge sync оставляет смешанное состояние, которое при рестарте нельзя однозначно продолжить.

**Код:** `internal/mihomo/coordinator.go:2298-2382`.

Временный `bridgeManifest` создаётся только в памяти и не инициализируется на диске, хотя `syncBridgesLocked` использует manifest transitions.

**Код:** `internal/mihomo/coordinator.go:2344-2361`.

**Исправление:** отдельный durable rollback transaction с checkpoint до/после каждого side effect. Использовать один реальный manifest на всём пути, а не несохранённый объект для bridges.

### 6. Legacy fallback объявляет успех без проверки

Fallback на `config.yaml.lkg` игнорирует ошибки `StopAndWait`, `Start` и удаления manifest; не проверяет listeners/process/config digest/store/bridges, но удаляет marker и переводит coordinator в `StateIdle`.

**Код:** `internal/mihomo/coordinator.go:2222-2255`.

**Исправление:** либо удалить legacy fallback из автоматического recovery, либо провести его через тот же строгий протокол: validate config, durable intent, atomic promote, stop/start с проверкой ошибок, listener/process proof, verified-active proof. Без generation/store proof он не должен называться LKG rollback.

## P1 — обязательные исправления

### 7. Критические ошибки файловых и runtime-операций игнорируются

Примеры:

- ошибки `SnapshotFilePath` и `CurrentDigest`: `coordinator.go:2424-2433`;
- удаление snapshot: `coordinator.go:2498-2500`;
- удаление active config в RuntimeOff: `coordinator.go:2519-2521` и `2322-2324`;
- `StopAndWait` в RuntimeOff: `coordinator.go:2535-2538`;
- удаление manifest/journal: `coordinator.go:2395-2396`, `2574-2575`.

Нельзя продолжать commit после ошибки, меняющей доказательство состояния. Cleanup errors должны оставлять терминальное cleanup-состояние и повторяться после рестарта.

### 8. `casManifest(nil, next)` не означает create-only

При `expected == nil` функция не проверяет наличие старого manifest и безусловно перезаписывает файл.

**Код:** `internal/mihomo/coordinator.go:2032-2072`; использование в regeneration — `2468`.

В degraded state это может уничтожить единственное доказательство незавершённой транзакции.

**Исправление:** create-only CAS должен падать при существующем manifest (кроме строго доказанного идемпотентного совпадения). Старый manifest сначала нужно прочитать, валидировать и детерминированно завершить.

## Недостаточность тестов

`TestGate4_RecoveryFailpointMatrix` проверяет возврат ошибок и удержание marker в одном процессе. Он не моделирует жёсткое завершение процесса после каждой durable boundary и новый `ApplyCoordinator` с повторным `RecoverStartupState`.

Обязательная новая матрица:

1. crash сразу после записи каждого manifest state;
2. crash после каждого side effect, но до следующего checkpoint;
3. новый coordinator и startup recovery;
4. проверка точного store/config/process/listener/bridge состояния;
5. проверка immutable bundle, verified-active, LKG pointer и daemon epoch;
6. повторный restart для доказательства идемпотентности;
7. варианты RuntimeOn и RuntimeOff;
8. отсутствующий/повреждённый bundle, pointer, manifest и snapshot.

## Независимая проверка

Выполнено 2026-09-21:

```text
go test -count=1 ./internal/mihomo -run 'TestGate4_|TestPersistAndVerify'
ok (4.040s)

go test -count=1 ./internal/mihomo
ok (31.818s)

go test -race -count=1 ./internal/mihomo
ok (35.332s)

git diff --check
exit 0 (только предупреждения CRLF/LF в чужих файлах)
```

Зелёный результат подтверждает текущие тесты, но не опровергает перечисленные дефекты: соответствующие аварийные сценарии в них отсутствуют.

## Условия повторной приёмки Gate B

- Исправлены все P0 и P1 выше.
- Recovery protocols имеют durable pre-state и checkpoints до/после side effects.
- Ни одна ошибка bundle/store/config/process/listener/bridge/cleanup не игнорируется.
- Полный readback records, pointer epoch и bundle обязателен и fail-closed.
- Добавлена crash/restart acceptance matrix, а не только in-process failpoints.
- Проходят `go test -count=1 ./internal/mihomo`, `go test -race -count=1 ./internal/mihomo` и зависимые пакеты Gate B.
- Gate C не смешивается с исправлением Gate B.

