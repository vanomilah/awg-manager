# Аудит walkthrough и реализации Xray Safety Foundation

Дата: 2026-09-09  
Walkthrough: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Вердикт

**Walkthrough преждевременно объявляет remediation полностью реализованной и проверенной.** Код компилируется, а заявленный целевой race-набор действительно проходит, но в реализации остаются критические нарушения fail-closed/crash-safe контракта. Перед сборкой и деплоем их необходимо исправить.

## Найденные проблемы

### P0. Обычный shutdown AWG Manager постоянно отключает Xray и Telegram

Файл: `cmd/awg-manager/wiring_server.go:136-145`.

Shutdown hook по-прежнему вызывает:

```go
a.xrayServerService.Stop()
a.tgWebProxyService.Stop()
```

Оба метода являются конфигурационными: записывают `Enabled=false` на диск. Это сводит на нет введённый runtime-only lifecycle. При штатном restart/shutdown пользовательская настройка включения будет потеряна.

Исправление: shutdown hook должен вызывать `ShutdownRuntime(ctx)` для Xray/TG и runtime `Stop` dispatcher. Для Telegram `Close()` выполнять после runtime shutdown. Добавить тест, что graceful shutdown не изменяет persistent `Enabled`.

### P0. Legacy ownership probe фактически fail-open

Файл: `internal/serveringress/migration_saga.go:461-497`.

Если TCP-порт открыт, но PID определить или `/proc/<pid>/cmdline` прочитать не удалось, код возвращает `nil` (`lines 491-492`). Это прямо противоречит walkthrough, где заявлена проверка PID ownership.

Кроме того:

- проверяется лишь наличие строки `xray`, но не путь legacy config;
- поиск разбирает только `/proc/net/tcp`, игнорируя IPv6;
- PID callback получает постоянный `0`, а не найденный PID;
- поиск inode фильтрует только порт и не проверяет конкретный listen address;
- при недоступности `/proc` чужой процесс на порту принимается как legacy Xray.

Исправление: при невозможности доказать ownership возвращать ошибку. Проверять address, TCP4/TCP6, executable/cmdline и legacy config path либо доверенный pidfile.

### P0. Rollback может остановить legacy-сервис, который saga не запускала

Файл: `internal/serveringress/migration_saga.go:338-355`.

`rollbackSaga()` вызывает `initActive stop` всегда, если active script существует. Ошибка может возникнуть ещё на `PrepareCandidate`, до запуска legacy. Если script был active до операции и legacy уже работал, rollback остановит чужое исходное состояние.

В journal отсутствуют `PreviousLegacyRunning`/`LegacyStartedBySaga`. `InitScriptWasRenamed` недостаточно.

Исправление: snapshot исходного runtime-state legacy и отдельный persisted-флаг запуска текущей saga. Останавливать только daemon, запущенный saga; если исходно legacy работал, сохранять/восстанавливать его состояние.

### P0. Crash между rename и записью фазы теряет факт переименования

Файл: `internal/serveringress/migration_saga.go`, шаг `legacy_script_restored`.

Последовательность сейчас:

1. `os.Rename(disabled, active)`;
2. `journal.InitScriptWasRenamed = true` только в памяти;
3. затем `advanceSagaPhase()` пишет journal.

При отключении питания между 1 и 3 на диске останется `InitScriptWasRenamed=false`. Startup rollback не вернёт script в `.disabled`.

Исправление: journal должен хранить исходное состояние файлов до мутации, а recovery обязан сверять фактическое наличие active/disabled и checksum. Либо добавить persisted intent-фазу до rename и completion-фазу после него.

### P1. Ошибки rollback всё ещё проглатываются во многих ветках Xray commit

Файл: `internal/xrayserver/transaction.go`.

Несмотря на утверждение walkthrough «never swallowed», результат `rollbackLocked()` игнорируется при:

- ошибке записи active runtime config (`193-196`);
- ошибке запуска candidate (`206-209`);
- преждевременном завершении процесса (`247-251`);
- timeout listener probe (`267-269`);
- ошибке marshal/write settings (`275-283`).

Только новые PID/manifest ветки объединяют ошибки корректно. Во всех остальных случаях rollback failure должен возвращаться через `errors.Join`, иначе coordinator не узнает, что требуется recovery.

### P1. TG readiness failure оставляет запущенные workers вне компенсации

Файлы: `internal/tgwebproxy/service.go:825-844`, `internal/serveringress/autostart.go:82-90`.

`StartConfigured()` сначала применяет workers, затем вызывает `CheckReadiness()`. Если readiness вернёт ошибку после успешного запуска workers, helper не добавит TG в `startedByHelper`, потому что append выполняется только после успешного возврата. Эти workers останутся запущенными.

Исправление: `StartConfigured()` обязан сам выполнить runtime rollback при readiness failure и объединить ошибки. Альтернатива — helper после ошибки проверяет переход состояния, но атомарный контракт компонента надёжнее.

### P1. Recovery игнорирует ошибку archive saga journal

Файл: `internal/serveringress/migration_saga.go:431-448`.

В terminal recovery используется:

```go
_ = archiveJournal(...)
return nil
```

При ошибке archive запуск продолжится как успешный, активный journal останется и на следующем boot recovery повторится. Ошибка должна включать `recoveryNeeded`, сохранять reason и возвращаться вызывающему коду.

Аналогично `rollbackSaga()` игнорирует `writeSagaJournal` на строках 397 и 401. Нельзя архивировать rollback как успешный, если его итог не удалось записать.

### P1. Фаза `managed_stopped` записывается до фактического commit остановки

Файл: `internal/serveringress/migration_saga.go`, около шагов Prepare/Commit.

После `PrepareCandidate` journal переводится в `managed_stopped`, и только затем вызывается `CommitPrepared`. Название фазы не соответствует фактическому состоянию. Recovery сейчас всегда откатывает незавершённые фазы, поэтому частично работает, но журнал не позволяет однозначно различить prepared и committed состояние.

Исправление: добавить `managed_stop_prepared` либо записывать `managed_stopped` только после успешного commit. При необходимости сначала persist intent, затем выполнить commit, после него persist completion и сверять manifest/process при recovery.

### P1. Xray rollback может запустить процесс на невосстановленном config

Файл: `internal/xrayserver/transaction.go:360-404`.

Ошибки восстановления settings/runtime config накапливаются, но код всё равно присваивает `s.config = manifest.OldConfig` и пытается запустить процесс. Если runtime config не восстановлен, Xray может стартовать на candidate/частично повреждённом файле.

Исправление: runtime restart допустим только после успешного восстановления необходимых файлов. Иначе остановить процесс, сохранить snapshot и вернуть joined rollback error.

### P2. Walkthrough перечисляет несуществующие поля Config

Walkthrough утверждает сохранение `MTProtoPort`, `BridgeEnabled`, `ExternalBridgeURL`, `FakeTLSDomain`, `SecretHex`, `Workers`. В фактическом `tgwebproxy.Config` этих полей нет. Реальные поля: `Secret`, `LegacySecret`, `Backend`, `UpstreamDevice`, `TlsDomain` и другие.

Это документационная ошибка, но она мешает проверить контракт. Walkthrough следует привести в соответствие с кодом.

## Недостаточное тестовое покрытие

В `internal/serveringress` присутствуют только два saga-теста:

- успешный happy path;
- failure ownership probe с rollback.

Отсутствуют заявленные/обязательные тесты падения и fault injection для каждой фазы:

- crash после prepared/rename/commit/decision/finalize;
- write/rename/chmod/start/stop/decision/archive failures;
- pre-existing active/running legacy;
- PID ownership unavailable/alien process/IPv6;
- сохранение journal при ошибке его записи;
- повторный idempotent finalize;
- Xray rollback failure во всех commit-ветках;
- TG readiness failure после фактического запуска workers;
- shutdown wiring сохраняет `Enabled`.

Поэтому зелёный race-набор подтверждает отсутствие выявленной гонки в существующих тестах, но не подтверждает заявленную crash-safety.

## Независимо подтверждённые проверки

Повторно выполнено:

```text
go test -count=1 -race ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/api/...
```

Результат: PASS для всех перечисленных пакетов, exit code 0.

Также `git diff --check` завершился с exit code 0; вывод содержит предупреждения о будущей нормализации CRLF/LF, но whitespace errors не обнаружены.

ARM64 build и `svelte-check` в рамках этого повторного аудита не запускались; их результаты пока подтверждены только walkthrough и могут устареть после исправлений.

## Итог исправления

До деплоя обязательно закрыть P0 и P1. Приоритетный порядок:

1. заменить shutdown wiring на runtime-only остановку;
2. сделать legacy ownership строго fail-closed;
3. сохранить pre-existing legacy state и устранить crash-window rename;
4. перестать проглатывать rollback/archive/journal errors;
5. сделать TG `StartConfigured` атомарным относительно readiness;
6. запретить Xray restart после неудачного восстановления runtime config;
7. добавить phase-by-phase crash/fault-injection тесты;
8. повторить uncached race tests, ARM64 build, frontend check и `git diff --check`.

Текущий walkthrough нельзя использовать как подтверждение готовности к установке на роутер.
