# Финальное ревью плана исправления Mihomo Gate B — Revision 3

**Дата:** 2026-09-21  
**Проверен:** `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
**Вердикт:** **APPROVE AFTER TARGETED AMENDMENT — архитектура почти готова, но до начала кодирования необходимо закрыть одно commit-window и три точных неоднозначных места.**

Revision 3 корректно устранила основные недостатки Revision 2:

- восстановлена обязательная rollback-to-LKG семантика Binding Amendment 2;
- rename-before-CAS заменён copy + fsync + CAS;
- crash tests переведены в subprocess с `os.Exit(42)`;
- добавлены границы после side effect до checkpoint;
- ранний abort regenerate больше не маскирует degraded state как `Idle`;
- `StateRollbackIntent` возобновляется;
- manifest хранит concrete previous/target bridge sets;
- legacy migration исключена из Gate B.

Полная Revision 4 не требуется. Достаточно внести следующие обязательные поправки непосредственно в план или приложить их как Binding Amendment 3.

## P0 — незафиксированная commit boundary

### Проблема

В regenerate flow порядок сейчас такой:

1. manifest остаётся в `StateBridgesReconciling`;
2. вызывается `persistAndVerifyGenerationLocked(rec, advanceLKG=true)`;
3. helper записывает `verified-active.json` и продвигает LKG pointer;
4. только затем записывается `StateRecoveryCommitted`.

Если процесс погибнет:

- после записи `verified-active.json`, но до продвижения pointer; или
- после продвижения LKG pointer, но до `StateRecoveryCommitted`,

startup увидит `StateBridgesReconciling` и по Binding Amendment 2 начнёт rollback. Однако во втором случае LKG pointer уже указывает на candidate. Значит «rollback к LKG» восстановит тот самый недокоммиченный candidate, а не предыдущее проверенное поколение.

### Обязательное исправление дизайна

Перед первым commit side effect нужен durable `StateCommitIntent` также для `OperationRegenerate`. Manifest должен содержать:

- `PreviousLKGGenerationID` и полную копию/digest предыдущего LKG pointer;
- candidate generation ID;
- ожидаемый candidate pointer/record digest;
- флаги или granular substates commit progress.

Нужно выбрать один детерминированный протокол:

**Вариант A — staged commit (предпочтительно):**

1. записать candidate verified-active/pointer во временные staged-файлы;
2. перечитать и проверить staged proofs;
3. durable `StateCommitIntent` со всеми digests;
4. атомарно заменить authoritative files в заданном порядке;
5. после crash в `StateCommitIntent` детерминированно завершить commit;
6. durable `StateRecoveryCommitted`.

**Вариант B — rollbackable commit:**

1. durable `StateCommitIntent` с предыдущим pointer/verified-active;
2. обновить authoritative files;
3. при crash до `StateRecoveryCommitted` восстановить предыдущие authoritative records и выполнить rollback именно к `PreviousLKGGenerationID` из manifest, а не читать текущий pointer как источник истины.

Нельзя оставлять helper как непрозрачную многофайловую операцию без промежуточного durable intent.

### Обязательные crash tests

Добавить:

- crash после записи candidate `verified-active.json`, до LKG pointer;
- crash после записи LKG pointer, до его readback verification;
- crash после успешного readback pointer, до `StateRecoveryCommitted`;
- повреждение staged/authoritative verified-active на каждой границе;
- доказательство, что rollback использует `PreviousLKGGenerationID`, даже если authoritative pointer уже указывает на candidate;
- второй restart после каждого случая.

## P1 — три точных уточнения

### 1. Durable признак исходного degraded state

Таблица различает «if degraded before» и «if clean», но не определяет поле, по которому новый coordinator это узнаёт после crash.

Добавить в administrative manifest typed факт, например:

```go
StartedFromRecoveryRequired bool
PreviousRecoveryMarkerDigest string
```

Либо выводить это только из обязательного archived manifest + marker digest. Логика не должна зависеть от утраченного in-memory состояния.

### 2. Foreign bridge policy должна быть однозначной

`TC-CR-22` сейчас говорит: «Sync removes foreign bridge (or fails-closed if unmanaged)».

Правильное обязательное поведение: **никогда не удалять foreign/unmanaged bridge; остановиться fail-closed и сохранить marker.** Удалять разрешается только bridge с доказанным ownership текущего AWG Manager и соответствующим transaction receipt.

### 3. Terminal cleanup требует строгого порядка

`TC-CR-23` должен зафиксировать порядок, при котором всегда остаётся хотя бы один durable recovery anchor:

1. записать и fsync cleanup journal;
2. удалить временные/archived artifacts с обновлением journal;
3. удалить recovery marker только после terminal proof;
4. удалить transaction manifest;
5. cleanup journal удалить последним.

Если manifest уже удалён, startup обязан обнаружить cleanup journal самостоятельно и закончить cleanup. Ошибки unlink/fsync оставляют journal и fail-closed state.

## Неблокирующее уточнение тестового декодирования

После `DisallowUnknownFields()` выполнить второй `Decode` и требовать `io.EOF`, чтобы JSON с двумя последовательными объектами или trailing garbage не принимался как корректный.

## Решение по запуску

План можно передавать исполнителю **только вместе с этим amendment**. Исполнитель обязан:

1. сначала реализовать и протестировать explicit commit protocol;
2. затем остальные пункты Revision 3;
3. не начинать Gate C;
4. не собирать IPK и не выполнять deploy;
5. не изменять `wdtt` и `qwdtt`;
6. в resolution report сопоставить каждый P0/P1 и каждый crash boundary конкретному тесту и коду.

При соблюдении этих поправок дополнительная редакция архитектурного плана не нужна; следующим этапом должно быть ревью фактической реализации Gate B.

