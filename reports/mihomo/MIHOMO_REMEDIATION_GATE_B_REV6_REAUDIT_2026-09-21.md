# Mihomo remediation Gate B — повторный аудит Revision 6

Дата: 2026-09-21  
Репозиторий: `E:\AWGM\awg-manager`  
Ветка по отчёту: `feature/mihomo-ai-proxyrt`

Проверены:

- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_RESOLUTION_REPORT_2026-09-21.md` (Revision 6);
- `reports/mihomo/GATE_B_DIFF_2026-09-21.patch`;
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`;
- фактическое состояние `internal/mihomo` и production wiring/runtime в `cmd/awg-manager`.

## Вердикт

**Revision 6 исправляет три замечания предыдущего аудита на уровне координатора, но Gate B всё ещё нельзя принимать для production.**

Главный новый блокер интеграционный: координатор теперь запрещает любые bridge-мутации без `ExactBridgeRuntime`, однако реальный runtime, который AWG Manager передаёт координатору на роутере, этот интерфейс не реализует. Тесты проходят, потому что критические сценарии используют exact mock, а `cmd/awg-manager` проверяется только на компиляцию.

Дополнительно проверка отсутствия active config после RuntimeOff остаётся fail-open для ошибок `os.Stat`, отличных от `ENOENT`.

Статус: **Gate B не принят — 2 production blocker.**

## Что из Revision 5 действительно исправлено

### RuntimeOff: порядок stop → unlink

В обоих основных путях runtime сначала останавливается и проверяется, затем вызывается `strictfs.StrictUnlink`. Ошибка unlink не позволяет выполнить commit.

Места:

- `internal/mihomo/coordinator.go:1663-1693`;
- `internal/mihomo/coordinator.go:3345-3390`;
- `internal/mihomo/coordinator.go:2811-2817` — rollback-путь также больше не игнорирует unlink.

### Не-Exact bridge runtime отклоняется координатором

`verifyBridgePublishAllowedLocked`, `verifyBridgeWithdrawAllowedLocked` и mutation path в `trackOp` действительно требуют `ExactBridgeRuntime` и fail closed до `PublishBridge`/`WithdrawBridge`.

Места:

- `internal/mihomo/coordinator.go:1857-1900`;
- `internal/mihomo/coordinator.go:1908-1951`;
- `internal/mihomo/coordinator.go:2029-2047`.

### AuthoritativeWriter введён и используется в двух commit-путях

Запись/rename конечных `verified-active.json` и `lkg.pointer.json` в `persistAndVerifyGenerationLocked` и `executeStagedCommitLocked` перенаправлены через `AuthoritativeWriter`.

Места:

- `internal/mihomo/coordinator.go:53-68`;
- `internal/mihomo/coordinator.go:128-137`;
- `internal/mihomo/coordinator.go:2414-2455`;
- `internal/mihomo/coordinator.go:2582-2617`.

Это закрывает прежнее замечание о добровольных hook в рассматриваемых commit-путях. Формулировку всё равно следует ограничивать этими путями: `GenerationStore.AdvanceLKGPointer` остаётся отдельным публичным writer, хотя production-вызовов его вне координатора сейчас не найдено.

## P0-A. Production `mihomoBridgeRuntime` не реализует обязательный ExactBridgeRuntime

### Фактический конфликт

Координатор Revision 6 выполняет type assertion:

```go
exactRuntime, isExact := c.cfg.BridgeRuntime.(ExactBridgeRuntime)
if !isExact {
    // recovery_required + ErrForeignBridgeOwnership
}
```

Но production-тип объявляет только:

```go
var _ mihomo.BridgeRuntime = (*mihomoBridgeRuntime)(nil)
```

и реализует лишь базовые методы:

- `ApplyBridges`;
- `WithdrawBridges`;
- `VerifyBridges`;
- `ListActiveBridges`.

Методов `PublishBridge`, `WithdrawBridge` и `InspectBridge`, необходимых для `mihomo.ExactBridgeRuntime`, в `cmd/awg-manager/mihomo_bridge_runtime.go` нет.

Production wiring при этом передаёт именно этот объект:

- `cmd/awg-manager/wiring_server.go:492` — `BridgeRuntime: a.mihomoBridgeRuntime`;
- `cmd/awg-manager/wiring_server.go:501` — создание coordinator.

### Последствие на роутере

При первой транзакции, которая добавляет или удаляет bridge, координатор неизбежно получит `isExact == false`, запишет recovery marker и завершит операцию с `ErrForeignBridgeOwnership`. То есть исправление безопасности сделало реальный bridge lifecycle неработоспособным.

`go test ./cmd/awg-manager` этого не обнаруживает: поле конфигурации имеет базовый тип `BridgeRuntime`, поэтому код компилируется. Exact-контракт проверяется только динамически во время мутации.

### Требуемое исправление

1. Реализовать на `*mihomoBridgeRuntime` полный `mihomo.ExactBridgeRuntime`:
   - `PublishBridge(ctx, ref)`;
   - `WithdrawBridge(ctx, ref)`;
   - `InspectBridge(ctx, ref) (ObservedBridge, error)`.
2. Инспекция должна читать фактическое состояние NDMS/kernel и возвращать доказуемого owner, а не просто отражать desired store.
3. Добавить compile-time контракт:

```go
var _ mihomo.ExactBridgeRuntime = (*mihomoBridgeRuntime)(nil)
```

4. Добавить integration test с production runtime/wiring, который выполняет реальный coordinator bridge delta и доказывает:
   - свой bridge публикуется;
   - чужой/unmanaged bridge отклоняется;
   - withdraw удаляет только bridge с совпавшим owner;
   - повторное применение идемпотентно.
5. Не ослаблять обратно fail-closed проверку координатора ради прохождения теста.

Критерий приёмки: production instance из `wiring_server.go` удовлетворяет `ExactBridgeRuntime` на этапе компиляции и проходит mutation integration test.

## P0-B. Postcondition RuntimeOff принимает любую ошибку os.Stat за отсутствие файла

В обоих RuntimeOff-путях используется проверка:

```go
if _, statErr := os.Stat(c.activeConfigFile); statErr == nil {
    return error
}
```

Если `os.Stat` вернёт `EACCES`, `EIO`, ошибку каталога или иной результат, отличный от `ENOENT`, код продолжит транзакцию так, будто файл доказанно отсутствует.

Места:

- `internal/mihomo/coordinator.go:1686-1688`;
- `internal/mihomo/coordinator.go:3379-3383`.

Это противоречит заявлению отчёта «проверяется постусловие отсутствия файла» и строгому fail-closed контракту.

### Требуемое исправление

В обоих местах различать состояния явно:

```go
if _, err := os.Stat(path); err == nil {
    return fmt.Errorf("active config still exists")
} else if !os.IsNotExist(err) {
    return fmt.Errorf("cannot prove active config absence: %w", err)
}
```

Ошибка доказательства должна переводить транзакцию в `StateRecoveryRequired` и запрещать commit.

Добавить fault-injection test именно для post-unlink stat error. Текущий `FailActiveConfigUnlink` срабатывает до unlink и эту ветку не проверяет.

Критерий приёмки: только подтверждённый `ENOENT` считается доказательством отсутствия active config.

## Замечание к тестовой стратегии

Новый тест `TC-CR-P0-B_non_exact_bridge_runtime_fails_closed` доказывает правильное отклонение coarse mock, но не проверяет, что production runtime после ужесточения остался пригоден к работе. Это типичный разрыв между unit security contract и wiring acceptance.

Нужен отдельный production-contract test, например в `cmd/awg-manager`, содержащий compile-time assertion и coordinator-level bridge mutation через реальный runtime с подменённым NDMS backend.

## Независимые проверки

Выполнено в текущем рабочем дереве:

```text
go test -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 32.725s

go test -count=1 ./cmd/awg-manager
ok github.com/hoaxisr/awg-manager/cmd/awg-manager 0.162s

go test -race -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 36.137s

go test -count=1 ./internal/mihomonative ./internal/singbox/router ./internal/api ./internal/proxyrt
ok github.com/hoaxisr/awg-manager/internal/mihomonative
ok github.com/hoaxisr/awg-manager/internal/singbox/router
ok github.com/hoaxisr/awg-manager/internal/api
ok github.com/hoaxisr/awg-manager/internal/proxyrt
```

Один объединённый длительный запуск завершился внешним timeout после 244 секунд без вывода; поэтому race и cross-package проверки были повторены раздельно и успешно завершились.

`git diff --check` не выявил whitespace errors; имеется только локальное предупреждение Git о будущем преобразовании CRLF/LF в `internal/mihomo/operator.go`.

## Порядок работ для следующего агента

1. Реализовать ExactBridgeRuntime в production `mihomoBridgeRuntime` без ослабления координатора.
2. Добавить compile-time assertion и production integration tests.
3. Исправить обработку post-unlink `os.Stat` в обоих RuntimeOff-путях.
4. Добавить fault injection на stat error после unlink.
5. Повторить unit, race, `cmd/awg-manager` и cross-package тесты.
6. Только после этого обновлять resolution report до статуса accepted.

## Финальные условия принятия Revision 6

- production bridge runtime, реально используемый wiring, реализует Exact-контракт;
- bridge create/withdraw работает в production integration test и остаётся fail closed для чужого owner;
- RuntimeOff принимает только `ENOENT`, а не произвольную ошибку stat;
- все перечисленные тесты проходят повторно.

До выполнения этих условий утверждение `FULLY VERIFIED / EXACT FAIL-CLOSED CONTRACT` не подтверждено.
