# Mihomo remediation Gate B — повторный аудит Revision 5

Дата: 2026-09-21  
Репозиторий: `E:\AWGM\awg-manager`  
Проверены:

- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_RESOLUTION_REPORT_2026-09-21.md`
- `reports/mihomo/GATE_B_DIFF_2026-09-21.patch`
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`
- фактическое состояние рабочей директории, прежде всего `internal/mihomo`

## Итоговый вердикт

**Gate B пока нельзя принимать как полностью закрытый.**

Все пять замечаний предыдущего аудита Revision 4 получили реальные изменения в коде. Целевые, race- и смежные тесты проходят. Однако повторная проверка обнаружила два оставшихся эксплуатационных разрыва и одно завышенное утверждение в доказательной базе:

1. переход в `RuntimeOff` удаляет активный конфиг с игнорированием ошибки и делает это до подтверждённой остановки процесса;
2. bridge-runtime, не реализующий `ExactBridgeRuntime`, по-прежнему допускается без доказательства владения системным интерфейсом;
3. тест «zero second authoritative writes» учитывает только добровольные вызовы hook и не способен заметить произвольную новую запись в обход hook.

То есть Revision 5 стала существенно лучше и исправила заявленные дефекты, но формулировки `fully accepted`, `0 fail-open` и «математически доказано отсутствие второй записи» фактическому коду пока не соответствуют.

## Что действительно исправлено

### 1. Повторная проверка владения bridge непосредственно перед CreateBridge

`verifyBridgePublishAllowedLocked` вызывается как при планировании, так и непосредственно перед публикацией bridge. Для `ExactBridgeRuntime` ошибка инспекции, чужой UUID, неизвестный legacy owner и существующий unmanaged-интерфейс переводят координатор в `recovery_required`.

Основные места: `internal/mihomo/coordinator.go:1855-1900`, `:1926`, `:1983`.

### 2. Повреждённый LKG pointer больше не трактуется как отсутствие pointer перед staged commit

`executeStagedCommitLocked` различает `ENOENT` и остальные ошибки чтения/декодирования pointer. При повреждении происходит fail-closed с recovery marker.

Основные места: `internal/mihomo/coordinator.go:2416-2447`.

### 3. Startup не создаёт first generation поверх повреждённого pointer

Startup-путь теперь отдельно обрабатывает ошибку чтения и допускает first-generation только при подтверждённом отсутствии файла.

Основные места: `internal/mihomo/coordinator.go:898-943`.

### 4. RuntimeOff проверяет ошибку остановки и postcondition

`StopAndWait` больше не игнорируется; после него выполняется повторная проверка `IsRunning`. Ошибка или продолжающий работать процесс приводят к `recovery_required`.

Основные места: `internal/mihomo/coordinator.go:3282-3298`.

### 5. Добавлена регрессия на ожидаемые promotion-операции

Тест фиксирует две ожидаемые операции продвижения: rename `verified-active.json` и rename `lkg.pointer.json`.

Основные места: `internal/mihomo/gate4_crash_test.go:692-760`.

## Оставшиеся блокеры

### P0-A. RuntimeOff может зафиксировать выключенное состояние, не удалив активный конфиг

В `regenerateFromDesiredLocked` используется:

```go
} else {
    _ = os.Remove(c.activeConfigFile)
}
```

Ошибка удаления полностью игнорируется, после чего manifest переводится в `config_promoted`, процесс останавливается и транзакция может быть зафиксирована. При `EPERM`, read-only filesystem, ошибке каталога либо другом I/O-сбое получится ложное состояние: generation говорит `RuntimeOff`, а прежний `config.yaml` остаётся авторитетным артефактом.

Кроме того, файл удаляется **до** успешного `StopAndWait`. Если остановка завершается ошибкой, старый процесс остаётся запущенным, но его активный конфиг уже удалён. Текущий тест проверяет только ошибку, degraded-state и recovery marker; он не проверяет сохранность/восстановление активного конфига.

Места:

- `internal/mihomo/coordinator.go:3257-3265`
- `internal/mihomo/coordinator.go:3282-3298`
- `internal/mihomo/gate4_crash_test.go:1332-1370`

Требуемое исправление:

1. Не игнорировать удаление: использовать строгий unlink/remove с проверкой результата и допустимым `ENOENT`.
2. Определить транзакционный порядок RuntimeOff. Безопасный вариант: сначала подтверждённо остановить runtime, затем удалить active config и durable-зафиксировать promotion; либо гарантированно восстанавливать файл при ошибке остановки.
3. При ошибке удаления переходить в `recovery_required`, не выполнять commit.
4. Добавить тест инъекции ошибки unlink и тест, что stop failure не оставляет систему без прежнего active config.

Критерий приёмки: `RuntimeOff` не может быть committed, пока одновременно не доказаны `process stopped` и `active config absent`.

### P0-B. Не-Exact bridge runtime остаётся fail-open

`verifyBridgePublishAllowedLocked` строго проверяет владение только если `BridgeRuntime` реализует `ExactBridgeRuntime`. Для любой другой реализации выполнение доходит до безусловного `return nil`:

```go
exactRuntime, isExact := c.cfg.BridgeRuntime.(ExactBridgeRuntime)
if isExact {
    // строгая проверка
}
return nil
```

Следовательно, общий контракт координатора не гарантирует заявленное правило «невозможно доказать ownership — не мутировать ОС». Тесты Revision 5 подтверждают Exact-путь, но не запрещают fallback-путь.

Место: `internal/mihomo/coordinator.go:1860-1900`.

Требуемое исправление:

1. Для операций создания/перепубликации/удаления bridge требовать `ExactBridgeRuntime`.
2. Если точная инспекция недоступна — fail closed до OS mutation с `ErrForeignBridgeOwnership` или отдельной typed error.
3. Либо заменить раздельные inspect/create на атомарный ownership-aware API runtime.
4. Добавить регрессионный тест: обычный `BridgeRuntime`, не реализующий Exact-интерфейс, не может создать или удалить bridge.

Критерий приёмки: ни одна поддерживаемая реализация bridge runtime не может изменить интерфейс без доказанного ownership.

### P1. Инструментация не доказывает отсутствие любой второй записи

`OnAuthoritativeWrite` вызывается вручную в известных местах кода. `FailAuthoritativeWriteAfterPromote` также проверяется внутри конкретного helper. Если позже появится прямой `StrictWriteAtomic`, `os.WriteFile`, rename или другой путь записи без вызова hook, текущий тест его не увидит.

Поэтому тест доказывает только: **в текущем инструментированном пути зарегистрированы ровно два promotion rename**. Он не доказывает сформулированное в комментарии `ANY write` и не является filesystem-wide гарантией.

Места:

- `internal/mihomo/coordinator.go:41-42`
- `internal/mihomo/coordinator.go:2349-2393`
- `internal/mihomo/coordinator.go:2525-2555`
- `internal/mihomo/gate4_crash_test.go:711-743`

Требуемое усиление:

- инъецировать единый интерфейс authoritative filesystem/writer и проводить все записи/rename через него; либо
- снимать фактический before/after snapshot и trace файловых операций на authoritative paths;
- переименовать текущий тест/комментарий так, чтобы доказательство не было сильнее механизма наблюдения.

Это не подтверждённая повторная запись в production-коде, а недостаточность заявленного proof.

## Независимая проверка

Выполнено 2026-09-21 из WSL Ubuntu:

```text
go test -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 32.392s

go test -race -count=1 ./internal/mihomo
ok github.com/hoaxisr/awg-manager/internal/mihomo 36.296s

go test -count=1 ./internal/mihomonative ./internal/singbox/router ./internal/api ./internal/proxyrt
ok github.com/hoaxisr/awg-manager/internal/mihomonative
ok github.com/hoaxisr/awg-manager/internal/singbox/router
ok github.com/hoaxisr/awg-manager/internal/api
ok github.com/hoaxisr/awg-manager/internal/proxyrt

git diff --check -- internal/mihomo
ошибок whitespace нет; выдано только предупреждение о будущем CRLF -> LF для operator.go
```

Тесты подтверждают отсутствие обнаруживаемой регрессии, но не закрывают описанные выше неохваченные ветви.

## Порядок исправления для следующего агента

1. Закрыть P0-A: транзакционный RuntimeOff, строгая обработка unlink и тесты ошибок удаления/остановки.
2. Закрыть P0-B: запретить OS bridge mutation без Exact ownership proof.
3. Усилить P1 instrumentation либо ослабить формулировку доказательства до реально проверяемой гарантии.
4. Повторить три набора тестов из раздела выше.
5. Обновить resolution report только после прохождения новых негативных тестов.

## Условия окончательного принятия Gate B

- ошибка удаления active config не может завершиться успешным RuntimeOff commit;
- stop failure не уничтожает единственный рабочий active config без немедленного восстановления;
- bridge mutation невозможна для runtime без точной проверки владения;
- тест авторитетных записей наблюдает все реальные операции, а не только вручную instrumented места;
- целевые, race- и смежные тесты проходят повторно.

До выполнения этих условий статус: **условно улучшено, но Gate B не принят**.
