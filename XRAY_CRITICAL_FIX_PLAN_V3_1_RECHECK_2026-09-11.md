# Повторная проверка Critical Fix Implementation Plan v3.1

Дата: 2026-09-11  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
SHA-256: `88A3019A286AE261CCDA99B0FA95F9DD68B60A8C5BA049EF49217BA5ED2929A4`  
Размер: 56899 байт

## Вердикт

**План пока не одобрен.** В начале документа добавлена таблица, утверждающая, что шесть замечаний v3 исправлены, но основное тело плана осталось старым и прямо противоречит этой таблице. Реализующий агент почти наверняка будет следовать подробным этапам и псевдокоду, поэтому одной сводки недостаточно.

Нужно не добавлять ещё одну секцию с поправками, а заменить устаревшие фрагменты в основном тексте и оставить единственную непротиворечивую спецификацию.

## Подтверждённые противоречия

### 1. Durable manifest всё ещё описан старым небезопасным кодом

В сводке сказано, что `_ = writeManifest` устранено и появился `metadata_committed`. Но в фактическом разделе D2 остался код:

```go
manifest.State = TxStateAppliedStateWritten
_ = writeManifest(txPath, &manifest)
```

В enum/flow раздела D2 нет `TxStateMetadataCommitted`, нет обязательной проверки checksum перед roll-forward и нет нового recovery decision table. Следовательно, R5 отражён только декларативно.

### 2. Manifest всё ещё содержит старые неполные ID

Сводка обещает `*AppliedState` и `*ActivePointer`, однако подробная структура `SnapshotManifest` по-прежнему содержит:

```go
OldHeadGenID    string
OldAppliedGenID string
```

И rollback ниже по-прежнему восстанавливает состояние по этим ID. R4 фактически не внесён в спецификацию.

### 3. Миграция осталась in-place и fail-open

Сводка обещает copy-on-write/fail-closed. Раздел B6 всё ещё требует:

- переписать существующие `managed.json` и `raw.json`;
- при ошибке только записать warning и продолжить startup.

Нет temp generation, atomic publication, migration status/quarantine, обработки applied generation и failure-injection boundaries. R3 не внесён.

### 4. Finalize error по-прежнему игнорируется

В `ApplyGeneration` осталось:

```go
_ = s.FinalizePrepared(txID)
return nil
```

Это противоречит требованию возвращать cleanup warning/structured result и оставлять артефакт для recovery.

### 5. Retention ownership спроектирован в неверном слое

Псевдокод `func (s *ProfileStore) buildProtectedSet` обращается к:

- `s.getAppliedState(profileID)`;
- `s.pendingTransactions`;
- transaction manifests.

Но applied-state и transaction state принадлежат `xrayserver.Service`, а не автономному `DiskProfileStore`. Такой код либо не соберётся, либо заставит создать циклические зависимости и дополнительные locks. Внутри `pruneGenerationsLocked` уже удерживается store mutex, поэтому показанный повторный `s.mu.RLock()` также создаёт deadlock.

Правильный контракт: service/repository coordinator вычисляет protected set без удержания profile-store lock и передаёт его в `SaveGeneration/Prune`, либо ProfileStore получает callback/provider без обратного входа под mutex. Persistent pending manifests должны сканироваться с диска, а не только из in-memory `pendingTransactions`.

### 6. Delete guard проверяет профиль недостаточно точно

Псевдокод вызывает `s.profiles.GetAppliedState`, хотя applied-state принадлежит Service. Также условие `appliedState != nil` блокирует удаление любого профиля, а не проверяет `appliedState.ProfileID == profileID`.

Нужно дополнительно блокировать профиль, упомянутый любым pending transaction manifest, включая old/target state. `DeactivateRuntime` упомянут, но не определён как транзакционная операция и не включён в список файлов/тестов.

### 7. Atomic generation publication заявлена, но подробный SaveProfile-контракт неоднозначен

В B4 всё ещё передаётся внешний `vaultDir` и говорится, что vault будет «moved/linked into genDir». Это противоречит новой декларации «no separate vault tmpdir» и создаёт риск cross-filesystem move или link/symlink.

`SaveProfileWithSecrets` должен создать один sibling temp generation directory через ProfileStore, писать vault непосредственно внутрь него и затем поручить ProfileStore выполнить validate/fsync/rename/pointer switch. Внешний произвольный `vaultDir string` из API метода хранилища нужно удалить.

### 8. Recovery для `applied_state_written` всё ещё безусловно считает транзакцию успешной

Старый раздел говорит просто удалить transaction directory. Это опасно при сбое до head pointer или durable manifest transition. Нужна таблица:

- проверить runtime checksum;
- проверить точное содержимое applied-state;
- при `UpdateHeadPointer` проверить head;
- только при полном совпадении roll-forward to committed;
- иначе rollback либо `recovery_required`.

## Что нужно сделать агенту

1. Удалить старые фрагменты D2/D4/B4/B6/AD7/AD8, а не оставлять их рядом с поправками.
2. Вставить в соответствующие этапы окончательные структуры и алгоритмы из `XRAY_CRITICAL_FIX_PLAN_V3_REVIEW_2026-09-11.md`.
3. Перенести вычисление protected generations на service/coordinator level и исключить повторный lock.
4. Полностью определить `DeactivateRuntime`, либо убрать обещание и оставить только `409 PROFILE_IS_APPLIED`.
5. Добавить защиту pending-transaction references при delete/prune.
6. Сделать один однозначный temp-generation publication API без внешнего vaultDir.
7. Добавить recovery decision table для каждого durable state.
8. Поиск по итоговому документу не должен находить:
   - `_ = writeManifest`;
   - `_ = s.FinalizePrepared`;
   - `OldHeadGenID`/`OldAppliedGenID` вместо полных snapshots;
   - `log warning, skip (don't block startup)` для migration;
   - `moved/linked into genDir`.

## Статус допуска

Архитектурное направление v3.1 принято, но документ как исполняемый план **не прошёл consistency gate**. После механического устранения противоречий и исправления ownership retention/delete можно начинать реализацию. Новый полный архитектурный цикл ревью после этого не нужен — достаточно проверить согласованность обновлённого файла и новый SHA-256.

## Сообщение агенту

> v3.1 не прошёл consistency gate: поправки добавлены только в сводку, а подробные разделы сохранили старый небезопасный псевдокод. Исправьте сам текст плана по `XRAY_CRITICAL_FIX_PLAN_V3_1_RECHECK_2026-09-11.md`, удалив все противоречащие фрагменты. Особое внимание: ownership protected set на Service level, disk manifests для pending references, полные nullable snapshots в manifest, copy-on-write migration, checked manifest/finalize writes и единый temp generation без внешнего vaultDir. Реализацию пока не начинать.

