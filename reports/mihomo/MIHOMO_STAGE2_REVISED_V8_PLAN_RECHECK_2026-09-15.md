# Повторная проверка плана Mihomo Stage 2 Revised v8

**Дата:** 2026-09-15  
**Проверенный документ:** `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
**Версия плана:** `Revised v8`  
**Рабочая копия:** `E:\AWGM\awg-manager`, ветка `feature/mihomo-ai-proxyrt`

## Итоговый вердикт

План v8 существенно лучше v7: основная архитектура транзакционного применения, LKG, controlled restart, fail-closed recovery, degraded startup и сохранение предыдущего draft теперь описаны достаточно последовательно.

**Архитектура условно одобрена. Полностью переписывать план и делать v9 не требуется.** Перед началом реализации агент должен внести перечисленную ниже обязательную дельту непосредственно в рабочие контракты и тесты. До этого утверждение из строки 98, что все P0 полностью закрыты, не подтверждается.

Главный риск сейчас не в общей идее, а в нескольких незавершённых переходах состояния. Если реализовать текст буквально, возможны потеря pending-конфигурации, запуск Mihomo из оставшегося `config.yaml` после перехода в `RuntimeOff` и ложное признание новой генерации применённой после перезапуска демона.

## Обязательная дельта перед кодированием (P0)

### P0-1. Для draft-журнала нет собственного crash-recovery протокола

План ввёл состояния `snapshot_secured`, `store_mutated`, `pending`, `consuming`, но таблица восстановления в разделе 6 описывает только `TransactionManifest`. Не определено, что делать после падения:

- между записью draft snapshot и `store.draft.json`;
- после `snapshot_secured`, когда store уже изменён, но состояние ещё не обновлено;
- после `store_mutated`, но до `pending`;
- во время `consuming` до создания основного manifest;
- после успешного основного commit, но до удаления draft-журнала;
- при повреждённом, неподдерживаемом или неполном `store.draft.json`.

**Нужно:** добавить отдельную таблицу восстановления `DraftJournal`, точные допустимые переходы и инварианты digest. `consuming` должен хранить связанный `TxID` основного manifest. Удалять draft и его snapshot разрешено только после durable `verified-active.json`/`committed`. Неизвестная комбинация должна вести в `recovery_required`, а не угадывать состояние.

### P0-2. В manifest отсутствует digest целевого compiler input

В `TransactionManifest` есть только поле `AppliedInputDigest`. Оно не разделяет:

- input предыдущей применённой генерации;
- input, из которого собран текущий candidate;
- текущий desired input после внешних изменений.

Из-за этого recovery для `published` проверяет target store, config и runtime, но не может доказать, что target config был собран из нужных settings/slots/tunnels.

**Нужно:** заменить неоднозначное поле минимум на:

- `BaseAppliedInputDigest`;
- `TargetInputDigest`.

`CompileResult` также должен возвращать `InputDigest`, а `AppliedGenerationRecord.AppliedInputDigest` записывается только после проверки равенства target digest. В recovery `published -> committed` обязательна проверка `TargetInputDigest`, а не только store digest.

### P0-3. `input.pending.json` упомянут, но его протокол отсутствует

Для durable convergence внешних источников нет схемы файла, версий, атомарной записи, состояний, владельца изменения, target digest, startup reconciliation и условий удаления. Сейчас это только маркер в прозе, поэтому после рестарта система не знает, что именно нужно повторить и что считать применённым.

**Нужно:** определить versioned `PendingInputRecord` как минимум с `TargetInputDigest`, `BaseAppliedInputDigest`, причиной/источником, временем и состоянием. Описать:

- запись до попытки apply либо до возврата ошибки вызывающему коду;
- идемпотентное восстановление при старте;
- поведение при новых изменениях поверх pending;
- удаление только после durable commit соответствующего digest;
- переход в degraded/recovery при повреждении или конфликте.

Добавить отдельные тесты для settings, slots и tunnel-source, включая crash/restart.

### P0-4. `RuntimeOff` не удаляет активный конфигурационный файл

Матрица требует `FileAbsent`, но для `RuntimeOff` указывает `Active Swap Action: None`. Простого `StopAndWait()` недостаточно: оставшийся `config.yaml` может быть снова подхвачен при следующем старте процесса или демона.

**Нужно:** сделать удаление активного файла частью транзакции:

1. защитить verified active как LKG;
2. durable rename активного файла во временный transaction artifact либо strict unlink после сохранения доказуемого LKG;
3. вызвать `InspectOutcome` при неоднозначном результате;
4. проверить `FileAbsent` и `ProcessStopped`;
5. при rollback неразрушающе восстановить LKG и прежний process state.

Этот переход должен быть отражён в manifest, phase table, crash table и тестах для обычного выключения, zero bridge demand и last bridge deletion.

### P0-5. Имена digest-полей в таблицах не совпадают со схемой

Crash table использует `StorePreviousDigest` и `StoreTargetDigest`, которых нет в `TransactionManifest`. В схеме определены `BaseAppliedStoreDigest`, `BaseDesiredStoreDigest`, `TargetDesiredStoreDigest`. Это не косметика: для rollback при наличии старого pending draft выбирать нужно разные состояния.

**Нужно:** для каждой строки таблиц явно указать:

- какой store должен остаться desired после неуспешного apply;
- какой store соответствует running config;
- какой digest проверяется перед удалением snapshot;
- чем отличается apply нового mutation от consume уже существующего draft.

Использовать во всём документе только имена полей из реальной структуры.

### P0-6. В примере `UpdateSettings` есть ошибка компиляции

Текущая сигнатура репозитория:

```go
func (s *ServiceImpl) UpdateSettings(ctx context.Context, sr storage.SingboxRouterSettings) error
```

Но в строках 398-400 плана показан `return nil, fmt.Errorf(...)`. Такой код не компилируется.

**Нужно:** использовать:

```go
return fmt.Errorf("apply mihomo settings: %w", err)
```

Одновременно нельзя считать возврат ошибки достаточной транзакцией для `settings.json`: план должен указать, кто сохраняет desired settings, кто пишет `input.pending.json`, и почему ошибка применения не теряет durable desired state.

### P0-7. Startup identity после рестарта демона всё ещё не формализована

В summary заявлена очистка stale child, но в component list и тестовой матрице нет отдельного контракта. `VerifiedActiveGeneration uint64` и `Operator.CurrentGeneration()` имеют смысл только внутри одного процесса AWG Manager. После рестарта счётчик может снова начаться с нуля, а найденный живой Mihomo может относиться к старой эпохе.

**Нужно:** определить daemon epoch и process receipt (`daemon_epoch`, PID, `/proc/<pid>/stat` start time либо другой Linux process identity). При startup recovery нельзя доверять persisted process generation. Сначала следует остановить/идентифицировать прежний owned process, затем выполнить fresh controlled start и проверить PID-owned sockets.

Добавить явный тест: AWG Manager падает после запуска target Mihomo, новый экземпляр имеет новый epoch, на машине остаётся старый процесс/controller.

### P0-8. `InspectOutcome` заявлен глобально, но таблица покрывает только swap active

Строка 58 требует классифицировать ошибку любого `StrictRename`/`StrictWriteAtomic`, однако phase table разбирает outcome только для `SwapActive`. Не определено поведение после post-rename fsync error для:

- manifest update;
- verified-active record;
- draft journal и pending input marker;
- native store persistence и restore;
- LKG creation/restore;
- перехода active config в absent;
- удаления/карантина artifacts.

**Нужно:** ввести единый helper уровня координатора `writeAndResolve`/`renameAndResolve` и перечислить expected old/new digest для каждого durable объекта. Для manifest нельзя просто читать поле state без проверки полного содержимого и TxID. `OutcomeAmbiguous` всегда сохраняет evidence и закрывает bridge gates.

## Высокий приоритет, допускается закрыть при реализации (P1)

### P1-1. Immutable input должен иметь конкретный состав и канонизацию

`MihomoCompileInput` назван, но его поля не приведены. Нужно перечислить все источники текущего генератора: router settings, native resources/rules/groups/providers, routing slots, AWG catalog fallback, subscriptions, standalone tunnels, dynamic cloud CIDRs и режим runtime. Deep copy должен происходить под общей generation barrier; набор независимых `RLock` не создаёт согласованный snapshot.

Digest следует вычислять по канонической сериализации с фиксированным порядком коллекций. Нельзя включать timestamps, map iteration order, callbacks или runtime-only данные.

### P1-2. Legacy migration нуждается в durable migration journal

Фраза «atomic one-time migration boundary» и один snapshot-тест недостаточны. Нужны version/state, crash recovery, idempotency и проверка того, что legacy flags и импортированные сущности фиксируются одной логической транзакцией. Миграция должна завершиться до сборки baseline/applied record.

### P1-3. Полный lifecycle degraded startup не описан

После успешного административного reconcile должны ровно один раз запуститься отложенные schedulers/callbacks и открыться mutable API. Нужен state machine `starting -> degraded|ready -> ready`, single-flight recovery и тест повторного вызова recovery. Recovery endpoint должен иметь admin authorization, CSRF-защиту и audit log.

### P1-4. Evidence export требует явного безопасного контракта

Упомянут raw secret export, но endpoint list содержит только `/evidence`. Безопаснее не реализовывать raw export в Stage 2 вообще. Если он остаётся, нужны отдельный endpoint, повторная аутентификация/подтверждение, одноразовый короткоживущий artifact, `0600`, запрет кеширования и гарантированное удаление.

### P1-5. План изменений не включает OpenAPI и frontend degraded UX

Новые поля статуса, коды `503 RECOVERY_REQUIRED` и recovery endpoints требуют изменений `internal/openapi/swagger.yaml`, generated schemas/client и UI состояния. Иначе backend может быть исправен, а интерфейс снова зависнет или покажет старый статус.

### P1-6. Нужны проверки отсутствующих и несовпадающих artifacts

Crash table в основном описывает идеальную раскладку файлов. Добавить table-driven проверки для отсутствующего snapshot/LKG/candidate, неверного basename/TxID, digest mismatch, одновременно существующих draft и main manifest, orphaned pending marker и unsupported record version. Никакая такая комбинация не должна автоматически считаться Stable New.

## Что в v8 уже принято и не требует нового пересмотра

- общий coordinator как единственная точка compile/apply/rollback;
- strict durable filesystem primitives;
- pure compiler и immutable input как направление;
- LKG только из verified active generation;
- controlled restart вместо недоказуемого hot reload;
- PID-owned TCP/UDP listener verification;
- bridge publication с поэтапным журналированием;
- запрет `force_stable_new`;
- fail-closed recovery и отдельный `recovery.marker`;
- синхронный startup gate до HTTP и фоновых callback;
- сохранение предыдущего desired draft при неуспехе новой операции;
- отсутствие сборки IPK и деплоя в рамках Stage 2 разработки.

## Как запускать реализацию без создания v9

Агенту не нужно снова переписывать весь plan. Следует:

1. Считать v8 базовым архитектурным документом.
2. Перед кодом добавить к нему короткий раздел `Mandatory implementation delta` с P0-1—P0-8 из этого отчёта.
3. Сначала реализовать schemas/state machines и table-driven recovery tests без подключения HTTP/UI.
4. Затем strict filesystem и store transaction layer.
5. Затем pure compiler/input digest.
6. Затем coordinator и RuntimeOff transition.
7. Затем startup/degraded/recovery API.
8. В последнюю очередь перевести существующие writers/callbacks и обновить OpenAPI/frontend.

После каждого шага проверять, что старые прямые пути `GenerateMihomoConfig`, `OnMihomoReload` и callback из `OnRoutingSlotsChanged` больше не могут обойти coordinator.

## Минимальные дополнительные acceptance tests

К имеющимся 52 сценариям добавить как минимум:

1. crash recovery для каждого состояния `DraftJournal`, включая `consuming`;
2. apply существующего pending draft без нового mutation;
3. target input digest меняется из-за settings/slot, хотя native store не менялся;
4. `input.pending.json` переживает restart и удаляется только после соответствующего commit;
5. RuntimeOff durable удаляет active config и не запускается после daemon restart;
6. rollback RuntimeOff восстанавливает прежний file/process state;
7. daemon epoch не принимает stale Mihomo PID/controller за новую генерацию;
8. ambiguous outcomes для manifest, store, LKG, applied record, draft и pending marker;
9. corrupted/missing/mismatched artifact matrix;
10. degraded -> successful recovery -> schedulers start exactly once;
11. OpenAPI contract и frontend rendering для degraded/recovery-required;
12. grep-аудит: ни один mutation/reload callback не обходит coordinator.

## Границы выполненной проверки

Это review документа и сверка его ключевых сигнатур с текущей рабочей копией. Реализация Stage 2 ещё не выполнена, поэтому тесты новой архитектуры не запускались. Рабочее дерево уже содержит большое количество чужих изменённых и untracked файлов; они не изменялись. IPK не собирался, установка на роутеры не выполнялась.

