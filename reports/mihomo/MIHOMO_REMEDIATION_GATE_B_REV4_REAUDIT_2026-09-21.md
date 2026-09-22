# Повторный аудит Mihomo Gate B Revision 4

Дата: 2026-09-21

Проверены:

- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_RESOLUTION_REPORT_2026-09-21.md` (Revision 4);
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`;
- фактический код текущей рабочей директории.

> Переданный путь `walkthrough.m` не существует. Проверен актуальный `walkthrough.md` с временем изменения 2026-09-21 18:50:19.

## Вердикт

**Семь замечаний предыдущего реаудита P0-1 — P1-3 исправлены, но Gate B всё ещё не может быть принят как полностью fail-closed.**

Revision 4 заметно лучше предыдущей. Повторная authoritative-запись действительно убрана из staged commit, Previous LKG больше не перезаписывается безусловно, withdraw bridge стал строже, CAS и strict JSON исправлены, startup recovery errors больше не игнорируются в ранее указанных ветвях.

Однако рядом с исправленными участками остались новые блокирующие fail-open пути. Поэтому утверждения отчёта `100% PASS`, `0 FAIL-OPEN BRANCHES` и «полностью принят» не подтверждаются.

## Независимая проверка

Выполнено в WSL:

```text
go test -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 32.329s

go test -race -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 35.932s

go test -count=1 ./internal/mihomonative ./internal/singbox/router ./internal/api ./internal/proxyrt
ok github.com/hoaxisr/awg-manager/internal/mihomonative 0.037s
ok github.com/hoaxisr/awg-manager/internal/singbox/router 5.766s
ok github.com/hoaxisr/awg-manager/internal/api 1.635s
ok github.com/hoaxisr/awg-manager/internal/proxyrt 0.392s
```

Зелёные тесты подтверждены, но они не покрывают замечания ниже.

## Подтверждение закрытия предыдущих замечаний

| ID | Результат проверки Revision 4 |
|---|---|
| P0-1 | Исправлено: `executeStagedCommitLocked` после promotion вызывает read-only `verifyAuthoritativeGenerationLocked`, а не повторную двухфайловую запись. |
| P0-2 | Исправлено для описанного сценария: `PreviousLKGGenerationID` заполняется только если пуст, rollback способен вернуть pointer на target generation. |
| P0-3 | Исправлено для withdraw: ошибка inspection и неподтверждённый owner останавливают удаление. |
| P0-4 | Исправлено: ошибка `ListActiveBridges` в regenerate теперь блокирует commit. |
| P1-1 | Исправлено: expected manifest + ENOENT теперь CAS conflict. |
| P1-2 | Исправлено в production-файлах coordinator/generation store: применяется strict decoder, включая duplicate keys. |
| P1-3 | Исправлено в ранее отмеченных startup recovery branches: ошибки side effects больше не ведут к cleanup/Idle. |

## Оставшиеся блокеры Revision 4

### P0-A. Создание bridge всё ещё допускает конфликт с чужим или unmanaged bridge

Файл: `internal/mihomo/coordinator.go:1822-1840, 1893-1908`.

Для `toCreate` проверяется только частный случай:

```go
if obs.OwnerUUID != "" && b.OwnerUUID != "" && obs.OwnerUUID != b.OwnerUUID
```

Если существующий bridge:

- имеет `OwnerUUID`, а target `OwnerUUID` пуст;
- не имеет owner metadata вообще;
- имеет чужой или неизвестный `LegacyOwner`;

проверка проходит, bridge добавляется в `toCreate`, затем вызывается `PublishBridge`. Это не соответствует заявленной строгой foreign bridge policy и способно изменить уже существующий чужой интерфейс.

**Требование:** создать общий exact-ownership guard для любого изменения существующего bridge, не только withdraw. При `obs.Exists` разрешать idempotent publish исключительно при доказанном точном владельце и совпадении требуемой конфигурации/digest; иначе fail closed до side effect.

Нужны тесты минимум для:

- existing foreign OwnerUUID + empty target OwnerUUID;
- existing unmanaged bridge;
- existing unrecognized LegacyOwner;
- inspection error непосредственно перед publish;
- доказательство нулевого количества вызовов `PublishBridge`.

### P0-B. Ошибка чтения текущего LKG pointer игнорируется при подготовке commit

Файл: `internal/mihomo/coordinator.go:2321-2332`.

Код использует:

```go
prevPtr, _ := c.genStore.ReadLKGPointer()
```

При повреждённом, нечитаемом или несогласованном существующем pointer ошибка отбрасывается. `PreviousLKGGenerationID` остаётся пустым, после чего staged commit может продвинуть новый pointer без надёжно зафиксированной точки отката.

Это нарушает основную гарантию P0-2: previous LKG immutable только после успешного capture, но сам capture сейчас не fail-closed.

**Требование:** если pointer-файл существует, любая ошибка чтения/strict decode/validation должна остановить commit до `StateCommitIntent`. Отсутствие pointer допустимо только как явно доказанный first-generation case. Сохранить previous pointer ID и digest в manifest до promotion и проверить durable checkpoint.

Нужны corruption/I/O tests на существующий LKG pointer и доказательство, что ни `verified-active`, ни pointer не продвинулись.

### P0-C. Startup rollback может принять ошибку LKG pointer за отсутствие LKG и удалить active config

Файл: `internal/mihomo/coordinator.go:865-929`.

При отсутствии `PreviousLKGGenerationID`/`LKGGenerationID` выполняется:

```go
if ptr, _ := c.genStore.ReadLKGPointer(); ptr != nil {
    targetLKG = ptr.GenerationID
}
```

Ошибка чтения pointer отбрасывается. После этого код переходит к legacy fallback, а при отсутствии legacy-файла — к ветви «First generation»: останавливает процесс и удаляет active config. Повреждённый или временно нечитаемый pointer нельзя интерпретировать как доказательство того, что LKG никогда не существовал.

**Требование:** различать ENOENT и read/decode/validation error. Любая ошибка кроме подтверждённого ENOENT должна сохранять manifest/evidence и переводить систему в `RecoveryRequired`, не останавливая рабочий runtime и не удаляя active config.

Нужен startup crash test с `StateConfigPromoted`, пустыми legacy ID, существующим повреждённым pointer и проверкой сохранности active config/runtime/manifest.

### P1-A. RuntimeOff regenerate по-прежнему игнорирует ошибки процесса

Файл: `internal/mihomo/coordinator.go:3151-3155`.

Обе ошибки отбрасываются:

```go
if running, _ := c.cfg.Operator.IsRunning(); running {
    _ = c.cfg.Operator.StopAndWait(ctx)
}
```

После этого код отмечает runtime verified и может продолжить commit. Если `IsRunning` или `StopAndWait` завершились ошибкой, состояние `RuntimeOff` не доказано.

**Требование:** обе ошибки блокирующие; после успешной остановки нужна postcondition-проверка отсутствия процесса/listeners. При ошибке — `RecoveryRequired`, marker и сохранённый manifest.

### P1-B. Тест P0-1 не доказывает отсутствие второй записи

`TC-CR-P0-1_zero_second_write_window` проверяет только итоговое совпадение generation ID/number. Старый ошибочный код с повторной записью также мог завершить этот тест успешно.

Сам production-код P0-1 сейчас исправлен по inspection, но тест не является regression proof заявленного свойства.

**Требование:** инструментировать filesystem/write hooks или вынести authoritative writer interface и утверждать точное число/порядок write/rename операций. После promotion разрешены только read/stat/digest operations.

## Дополнительное замечание по отчёту

Файл resolution report отображается mojibake при обычном чтении PowerShell без явного UTF-8. Это не дефект runtime, но для передачи между агентами желательно сохранить Markdown как UTF-8 BOM либо гарантировать явное UTF-8 чтение. `walkthrough.md` дополнительно содержит хвост бинарно выглядящего UTF-16/ошибочного текста `Wsl/Service/E_ACCESSDENIED`; его следует очистить.

## Минимальный следующий шаг

1. Закрыть P0-A, P0-B и P0-C.
2. Исправить RuntimeOff error handling.
3. Усилить regression test P0-1 реальным подсчётом writes.
4. Добавить targeted tests и повторить те же unit/race/cross-package команды.
5. Только после этого повторно заявлять Gate B acceptance; IPK/deploy до приёмки не выполнять.

## Итог

Revision 4 корректно устраняет все семь конкретных замечаний предыдущего реаудита. Тем не менее расширенная проверка выявила три новых P0 и два P1/quality blocker. Текущая реализация **не подтверждает полную fail-closed гарантию и пока не готова к окончательной приёмке Gate B**.
