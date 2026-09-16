# Финальный аудит повторной ремедиации Xray Safety Foundation

Дата: 2026-09-09  
Проверенный walkthrough: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Вердикт

**Предыдущие блокеры в основном исправлены, тестовый набор теперь действительно проходит, но реализация всё ещё не готова к установке.**

Найден новый критический дефект общей транзакции Telegram proxy и незавершённая логика `keep_legacy`. Несколько заявленных safety-гарантий реализованы только в основном пути, но не в транзакционном Xray-пути.

## Что подтверждено

- Deadlock `ResolveMigration -> ApplyXrayConfig -> Apply` устранён через `applyLocked`.
- Разделяемое поле `pendingXrayConfig` удалено; candidate передаётся аргументом под mutex.
- Ошибка startup recovery блокирует автоматический запуск ingress-компонентов в `wiring_server.go`.
- Ошибки `FinalizePrepared`, dispatcher rollback и terminal archive теперь обрабатываются.
- `PIDRecord` содержит start time и fingerprint, основная проверка process identity стала fail-closed.
- Миграция сохраняет прежний settings-файл и восстанавливает его при ошибке marker/decision.
- Mutating `XrayServerHandler` теперь fail-closed при отсутствии coordinator.
- `TestXrayStatusSnapshot` исправлен.

## Оставшиеся дефекты

### P0. Coordinator не способен включать и выключать Telegram proxy

Coordinator формирует `tgwebproxy.Config` с полем `Enabled` и вызывает `UpdateConfig()`:

- commit: `internal/serveringress/coordinator.go:413-425`;
- startup rollback: `coordinator.go:262-270`;
- обычный rollback: `coordinator.go:493-501`.

Но `tgwebproxy.Service.UpdateConfig()` (`internal/tgwebproxy/service.go:514-572`) копирует текущую конфигурацию и обновляет hostname, порты, secret, backend и другие поля, **не присваивая `newCfg.Enabled = cfg.Enabled`**.

Следствия:

- `desired.TgEnabled=true` может завершиться успешной общей транзакцией, но Telegram proxy останется выключенным;
- `desired.TgEnabled=false` не выключит его;
- rollback не восстановит прежнее значение enabled;
- fingerprint после commit вычисляется от фактического старого состояния, поэтому журнал формально самосогласован и не обнаруживает ошибку.

**Исправление:** нужен явный coordinator-compatible метод полного применения внутренней конфигурации либо отдельный `SetEnabled(bool)` с транзакционным контрактом. Простое присваивание в публичном patch-style `UpdateConfig()` опасно, потому что zero-value request ранее мог означать «поле не передано». Лучше разделить PatchConfig и ApplyManagedState.

Добавить интеграционный тест с реальным `tgwebproxy.Service`, который проверяет фактический `GetConfig().Enabled` после commit и rollback для обоих направлений.

### P0. `keep_legacy` не запускает legacy-сервис и не является единой транзакцией

В `ResolveMigration("keep_legacy")` managed Xray сначала отключается и общая транзакция завершается. Затем код:

- переименовывает `.disabled` init-script обратно, игнорируя ошибку;
- выполняет только `os.Chmod(initActive, 0755)`;
- **не вызывает init-script с `start`**, хотя комментарий утверждает обратное;
- отдельно сохраняет runtime decision.

Таким образом API может вернуть успех и выбрать `ActiveGeneration=legacy`, но legacy Xray фактически не будет запущен. Если rename или сохранение decision завершится ошибкой, managed Xray уже выключен и предыдущая транзакция не откатывается.

**Исправление:** оформить переключение поколения как отдельную журналируемую migration transaction: проверить init-script identity, выполнить rename, запустить его, подтвердить listener/process ownership, сохранить decision и только затем finalize. При любом сбое восстановить managed generation. Не игнорировать rename/chmod/start ошибки.

### P1. PID record ошибки всё ещё игнорируются в транзакционном пути Xray

`restartLocked()` корректно обрабатывает ошибку `writePIDRecord()`, но `CommitPrepared()` всё ещё вызывает его без проверки (`internal/xrayserver/transaction.go:212-215`). Аналогично rollback-восстановление процесса игнорирует ошибку на строках 352-359.

В результате coordinator transaction может признать Xray успешно запущенным без устойчивой записи process identity, хотя walkthrough утверждает, что ошибка записи всегда останавливает процесс и возвращается вызывающему коду.

**Исправление:** в `CommitPrepared()` при ошибке PID record остановить новый процесс и выполнить rollback; в rollback вернуть объединённую ошибку восстановления, не объявлять его успешным.

### P1. Запись committed-состояния Xray manifest выполняется best-effort

В `CommitPrepared()` marshal и `atomicWriteFile(snapshot-manifest.json)` помещены в конструкцию, игнорирующую обе ошибки (`internal/xrayserver/transaction.go:272-276`). Затем метод возвращает success.

Это ослабляет crash recovery: snapshot сохранён, но manifest может не отражать фактический локальный commit.

**Исправление:** считать запись committed manifest обязательной частью локального commit и возвращать/откатывать ошибку согласно выбранному порядку фаз.

### P1. Тест fail-closed boot не тестирует реальную проводку

`TestStartupRecovery_CorruptJournalBlocksServiceStart` проверяет локальное выражение:

```go
canStart := (err == nil && !req)
```

Это повторяет условие из production-кода, но не вызывает bootstrap и не доказывает, что `Start()` не был вызван. Сам production-код сейчас выглядит правильно, однако утверждение walkthrough о полноценном boot-тесте завышено.

**Исправление:** вынести решение об автостарте в тестируемую функцию или добавить wiring test с fake services и счётчиками `Start()`.

### P1. Очистка public hostname не отражается в dispatcher topology

`topologyForXrayConfig()` меняет `desired.PublicHostname` только если `cfg.PublicDomain != ""`. Полный Xray candidate при этом может сохранить пустой домен. В результате Xray очищает домен, а dispatcher продолжает хранить старый hostname.

Похожая patch-семантика действует внутри `tgwebproxy.UpdateConfig()`. Для полного config PUT это создаёт расхождение компонентов.

**Исправление:** явно различать full replacement и patch DTO. `ApplyXrayConfig` должен переносить пустое значение как намеренное очищение и синхронно обновлять dispatcher.

## Независимая проверка тестов

Запущено:

```bash
go test -count=1 -race ./internal/cdndispatcher/... ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/serveringress/... ./internal/api/...
```

Фактический результат: **PASS для всех шести групп пакетов**, включая `internal/api`. Заявление walkthrough о backend PASS теперь подтверждено.

В этом аудите не повторялись `svelte-check` и ARM64 cross-build: найденные runtime-блокеры требуют ещё одной правки и повторного полного прогона после неё.

## Обязательная ремедиация перед установкой

1. Исправить реальное применение `TgEnabled` и покрыть commit/rollback настоящим `tgwebproxy.Service`.
2. Сделать `keep_legacy` рабочим и транзакционным: rename, start, probe, decision, rollback.
3. Обрабатывать ошибки PID record и committed manifest во всех Xray transaction paths.
4. Разделить full replace и patch semantics, включая очистку hostname.
5. Усилить boot wiring test.
6. Повторить uncached `-race`, frontend check и ARM64 build.

## Итог

Walkthrough заметно ближе к реальности, но формулировку «финальная ремедиация всех замечаний» пока следует заменить на «основная ремедиация выполнена, остались интеграционные блокеры Telegram proxy и legacy generation switch».
