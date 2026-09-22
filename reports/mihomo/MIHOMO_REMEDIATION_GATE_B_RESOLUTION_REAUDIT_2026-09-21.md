# Повторный аудит реализации Mihomo Remediation Gate B

Дата: 2026-09-21  
Объект проверки:

- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_RESOLUTION_REPORT_2026-09-21.md`
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`
- фактический код текущей рабочей директории `E:\AWGM\awg-manager`

## Вердикт

**Gate B не принят. Заявление о полном закрытии P0/P1 не подтверждено.**

Новые тесты действительно существуют и проходят, включая subprocess crash matrix и race-прогон. Однако в production-коде остались критические окна согласованности и fail-open ветви, которые тестовая матрица не покрывает. Деплой этой реализации как окончательного исправления Gate B пока не рекомендуется.

## Подтверждённые проверки

В WSL выполнено:

```text
go test -count=1 ./internal/mihomo -run 'TestGate4_(Crash|Corruption)'
ok github.com/hoaxisr/awg-manager/internal/mihomo 0.331s

go test -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 31.983s

go test -race -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 35.540s
```

`git diff --check -- internal/mihomo` не выявил whitespace-errors. Рабочее дерево существенно грязное; аудит ничего из существующих изменений не откатывал.

## Блокирующие замечания

### P0-1. Staged commit после promotion повторно выполняет обычную двухфайловую запись

Файл: `internal/mihomo/coordinator.go:2194-2366`.

`executeStagedCommitLocked` сначала корректно готовит staged `verified-active` и LKG pointer, затем поочерёдно продвигает их через rename. Но после этого на строке 2336 вызывается:

```go
c.persistAndVerifyGenerationLocked(rec, advanceLKG)
```

Этот helper не является read-only verifier. На строках 2085 и 2097 он снова:

1. переписывает `verified-active.json` через `StrictWriteAtomic`;
2. отдельно переписывает LKG pointer через `AdvanceLKGPointer`;
3. только затем делает readback.

Тем самым новый staged-протокол сам себя отменяет: после первой согласованной promotion-последовательности появляется второе независимое окно между двумя authoritative writes. Дополнительно второй pointer получает другой `UpdatedAt`, поэтому его содержимое уже не совпадает с digest staged pointer, сохранённым в manifest.

**Требуемое исправление:** разделить операции на `promote` и чистый `verify`. После staged rename разрешён только readback/strict decode/digest/equality check без любой записи authoritative-файлов. Старый helper нельзя вызывать из staged commit в текущем виде.

### P0-2. Recovery в `StateCommitIntent` может уничтожить ссылку на настоящий Previous LKG

Файл: `internal/mihomo/coordinator.go:885-932, 2210-2219`.

При старте `StateCommitIntent` вызывает `executeFinalCommitLocked`, а тот повторно входит в `executeStagedCommitLocked`. В начале этого метода выполняется `ReadLKGPointer()`, после чего `manifest.PreviousLKGGenerationID` безусловно заменяется текущим pointer.

Если падение произошло после promotion нового pointer (`regenerate_pointer_promoted`), текущий pointer уже указывает на candidate. После рестарта поле `PreviousLKGGenerationID` будет перезаписано candidate ID. При последующей ошибке roll-forward строки 925-929 попытаются «откатиться» на тот же candidate, а не на предыдущий LKG.

**Требуемое исправление:** previous LKG фиксируется один раз до commit intent и после этого immutable. В recovery запрещено вычислять его заново из текущего authoritative pointer. Нужна проверка digest/ID сохранённого previous pointer либо отдельная durable backup-копия.

### P0-3. Политика foreign bridge остаётся fail-open

Файл: `internal/mihomo/coordinator.go:1724-1743, 1787-1814`.

Проблемы:

- ошибка `InspectBridge` игнорируется, после чего bridge всё равно попадает в `toWithdraw` и вызывается `WithdrawBridge`;
- условие `obs.LegacyOwner != ""` считается достаточным доказательством владения независимо от совпадения с ожидаемым владельцем;
- аналогичная логика повторяется непосредственно перед withdraw.

При невозможности подтвердить ownership destructive operation должна останавливаться. Текущий код способен удалить bridge при ошибке инспекции или при наличии произвольной непустой legacy-метки.

**Требуемое исправление:** любое `InspectBridge` error -> `ErrForeignBridgeOwnership`/`RecoveryRequired`, без withdraw. Ownership должен подтверждаться точным совпадением stable owner identity; legacy ownership допустим только по явно определённому и проверяемому точному формату, а не по признаку «непусто».

### P0-4. Regenerate молча игнорирует невозможность получить активные bridges

Файл: `internal/mihomo/coordinator.go:3030-3044`.

Если `ListActiveBridges(ctx)` возвращает ошибку, код пропускает reconciliation и продолжает staged commit. В результате generation может быть объявлена committed, хотя bridge state неизвестен и не проверен.

**Требуемое исправление:** ошибка списка bridge после runtime mutation должна быть блокирующей, с durable recovery state/rollback согласно протоколу.

### P1-1. CAS допускает исчезновение ожидаемого manifest

Файл: `internal/mihomo/coordinator.go:2608-2627`.

При `expected != nil` и `os.IsNotExist(err)` функция не возвращает conflict, а продолжает и создаёт новый manifest. Это не compare-and-swap: отсутствие ожидаемого объекта должно считаться несовпадением состояния.

**Требуемое исправление:** `expected != nil` + `ENOENT` -> явная concurrent modification/recovery error; запись `next` запрещена.

### P1-2. Strict JSON фактически не применяется ко всем authoritative journals

Файлы:

- `internal/mihomo/coordinator.go:722-743` — startup manifest читается обычным `json.Unmarshal`;
- `internal/mihomo/coordinator.go:1214-1222` — cleanup journal читается обычным `json.Unmarshal`;
- `internal/mihomo/coordinator.go:2383-2409` и `2608-2638` — manifest archive/CAS также используют обычный `json.Unmarshal`.

Следовательно, duplicate/unknown/trailing fields в ключевых journal-файлах не везде fail-closed, хотя resolution report заявляет strict JSON authoritative records.

**Требуемое исправление:** использовать единый strict decoder во всех authoritative read/CAS/archive/recovery paths и добавить corruption fixtures на duplicate keys, unknown fields и trailing JSON.

### P1-3. Часть startup rollback ошибок намеренно отбрасывается

Файл: `internal/mihomo/coordinator.go:766-837, 854-875`.

В recovery присутствуют `_ = StrictWriteAtomic`, `_ = RestoreSnapshotFile`, `_ = Operator.StopAndWait`, `_ = StrictUnlink`. После неудачного восстановления код может продолжить cleanup и перейти в `Idle`, хотя требуемое состояние не было доказано.

**Требуемое исправление:** все ошибки восстановления/остановки/unlink агрегировать; cleanup authoritative manifest разрешать только после подтверждённых postconditions. Иначе сохранять evidence и переходить в `RecoveryRequired`.

## Почему зелёная crash matrix не закрывает замечания

Crash cases `regenerate_verified_active_promoted` и `regenerate_pointer_promoted` существуют, но обе точки расположены **до** повторного вызова `persistAndVerifyGenerationLocked`. Внутри повторной пары authoritative writes отдельной crash point нет. Поэтому тесты подтверждают recovery вокруг первого staged promotion, но не проверяют второе окно, созданное старым helper.

Также нет обязательных сценариев:

- crash после повторной записи verified-active, но до повторной записи pointer;
- restart после pointer promotion + ошибка нового roll-forward с проверкой отката именно на original Previous LKG;
- `InspectBridge` error перед withdraw;
- несовпадающий legacy owner;
- `ListActiveBridges` error после runtime verification;
- исчезновение manifest при CAS с `expected != nil`;
- unknown/duplicate JSON fields во всех authoritative journals.

## Минимальный порядок исправления

1. Сделать post-promotion verification строго read-only; удалить повторную authoritative запись.
2. Сделать `PreviousLKGGenerationID` immutable после первого durable capture и исправить recovery roll-forward.
3. Закрыть все bridge inspection/list branches fail-closed.
4. Исправить CAS semantics для отсутствующего expected manifest.
5. Перевести все authoritative decoders на strict JSON.
6. Убрать discarded errors из startup recovery и доказать postconditions до cleanup.
7. Добавить перечисленные crash/fault tests, затем повторить полный test/race/cross-package прогон.

## Критерии повторной приёмки

- После входа в commit protocol нет ни одной второй записи authoritative records вне staged promotion.
- На любой точке падения состояние после рестарта равно либо полностью previous generation, либо полностью candidate generation; смешанная пара невозможна.
- Roll-forward failure после promoted candidate pointer возвращает original Previous LKG.
- Ни один bridge не изменяется без доказанного exact ownership.
- Ошибка observation/reconciliation не может завершиться committed/idle.
- Все authoritative JSON reads fail-closed на unknown, duplicate и trailing content.
- Все recovery-side-effect errors сохраняют manifest/evidence и приводят к `RecoveryRequired`.
- Проходят targeted crash tests, весь `internal/mihomo`, `-race` и зависимые пакеты.

## Итог для следующего агента

Не считать текущий resolution report доказательством завершения Gate B. Сначала исправить P0-1 — P0-4, затем P1 и расширить тестовую матрицу. До этого не собирать IPK и не выполнять деплой как «принятую» версию.
