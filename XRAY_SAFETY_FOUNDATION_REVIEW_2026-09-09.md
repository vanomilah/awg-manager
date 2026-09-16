# Ревью этапа 1: Xray Safety Foundation

Дата: 2026-09-09  
Проверены:

- `implementation_plan.md`;
- `walkthrough.md`;
- `internal/xrayserver/service.go`;
- `internal/xrayserver/service_test.go`;
- `internal/api/xray_server.go`;
- `internal/api/xray_handler.go`;
- wiring и пользовательский интерфейс системных интеграций.

## Итог

**Этап не принят.** Локальные тесты проходят, но в приложении одновременно работают две
независимые реализации управления Xray. Старый небезопасный control plane остаётся
подключён к API и используется экраном настроек. Поэтому утверждение walkthrough о полном
устранении персональных defaults, `pidof xray`, глобального управления процессом и записи
конфига с правами `0644` не соответствует фактическому состоянию приложения.

Проверено в WSL:

```text
go test ./internal/xrayserver ./internal/api
ok github.com/hoaxisr/awg-manager/internal/xrayserver
ok github.com/hoaxisr/awg-manager/internal/api
```

`gofmt -d` для проверенных Go-файлов не выявил расхождений.

## Блокирующие замечания

### 1. P0: одновременно подключены два Xray control plane

Новый серверный сервис зарегистрирован на `/api/servers/xray/*`, но одновременно
`api.NewXrayHandler()` подключается в `cmd/awg-manager/wiring_server.go`, а его маршруты
`/api/xray/*` регистрируются в `internal/server/server_routes.go`.

Старый handler:

- использует `/opt/etc/xray-cdn/config.json`;
- создаёт `/opt/etc/init.d/S99xray-cdn`;
- ищет процесс через `pidof xray`;
- пишет runtime config с правами `0644`;
- содержит персональный внешний IP и специализированные defaults;
- устанавливает пакет и самостоятельно создаёт init script;
- может принять за свой процесс Xray-сервер или будущий Xray-клиент.

Экран `Настройки → Интеграции` продолжает вызывать именно старые `api.xrayStatus()`,
`xrayStart()`, `xrayStop()`, `xrayConfig()` и показывает старую форму конфигурации.

Это создаёт риск конфликта процессов, портов, конфигов и остановки чужого экземпляра.

**Исправление:** оставить единственный installer/status API для бинарника Xray-core и
единственный серверный service API. Удалить регистрацию старого `/api/xray/*` control
plane либо мигрировать необходимые install/uninstall операции в отдельный нейтральный
installer. Карточка интеграции не должна управлять серверной конфигурацией.

### 2. P0: завершение AWG Manager сохраняет Xray как выключенный

В `wiring_server.go` shutdown callback вызывает `xrayServerService.Stop()`. Метод `Stop()`
устанавливает `config.Enabled=false` и сохраняет это на диск. Обычный restart/upgrade AWG
Manager поэтому меняет пользовательскую настройку и препятствует автоматическому запуску
Xray после следующего старта.

**Исправление:** разделить пользовательскую операцию `Disable/Stop` и lifecycle-операцию
`Shutdown`, которая завершает дочерний процесс, но не меняет persisted `Enabled`.

### 3. P1: конфигурация сохраняется до проверки и не откатывается

`UpdateConfig()` сначала изменяет `s.config` и записывает settings-файл, затем вызывает
`restartLocked()`, внутри которого выполняется `TestConfig()`. При ошибке проверки
невалидное состояние уже сохранено. Старый рабочий процесс может продолжить работу, но
после reboot сервер не запустится.

Это не соответствует заявленной схеме render/check/apply/rollback.

**Исправление:** формировать candidate без изменения active config, выполнить
`Validate -> Render -> TestConfig`, затем атомарно применить runtime и settings. При
ошибке запуска восстановить предыдущий runtime/config и состояние процесса.

### 4. P1: CRUD клиентов скрывает ошибки применения

`AddClient`, `DeleteClient` и `ToggleClient` сохраняют изменение, но игнорируют ошибку
`restartLocked()` через `_ = s.restartLocked()`. API отвечает успехом, хотя работающий
Xray может продолжать использовать старый список клиентов.

**Исправление:** применять те же candidate transaction и rollback, возвращать ошибку
клиенту и не заявлять успех до подтверждения нового runtime.

### 5. P1: проверка PID fail-open и подвержена PID reuse/TOCTOU

Если чтение `/proc/<pid>/cmdline` завершилось ошибкой, `checkRunningLocked()` всё равно
считает PID своим. Перед отправкой SIGTERM/SIGKILL identity повторно не проверяется.
PID-файл хранит только число без start time/executable identity.

**Исправление:** хранить PID вместе с process start time и ожидаемым config path; при
невозможности подтвердить identity считать процесс чужим. Повторно проверять identity
непосредственно перед каждым сигналом. Добавить Linux-тесты PID reuse и unreadable proc.

## Существенные замечания

### 6. P1: заявленная обратная совместимость не доказана

Загрузка неизвестных полей не сохраняет их: JSON декодируется в закрытую структуру и при
следующей записи неизвестные поля исчезнут. Нулевые значения ряда полей автоматически
заменяются defaults. Повреждённый JSON молча игнорируется вместо recovery state.

**Исправление:** определить поддерживаемую схему и миграции, сохранять backup перед
первой перезаписью legacy config, возвращать parse/recovery error, добавить fixture-тесты
реальных конфигураций обоих роутеров без секретов.

### 7. P1: выдаются неподтверждённые клиентские форматы

`GenerateLinks()` всегда формирует Sing-box JSON и Mihomo YAML с `xhttp`, а тест проверяет
только синтаксическую валидность JSON. Реальная поддержка транспорта конкретными ядрами
не проверяется. Пользователь может получить формально корректный, но нерабочий профиль.

**Исправление:** capability detection и validation целевым ядром. Неподдерживаемые
форматы не возвращать и не показывать в UI. Для текущего XHTTP всегда оставлять валидный
VLESS URI/Xray-compatible export.

### 8. P2: небезопасные или неполные вспомогательные операции

- `generateUUID()` игнорирует ошибку `crypto/rand.Read`;
- `atomicWriteFile()` не удаляет временный файл при ошибке `WriteFile`, не использует
  exclusive create и не выполняет fsync файла/каталога;
- `GetConfig()` возвращает shallow copy со shared slice `Clients`;
- после `cmd.Start()` отсутствуют readiness/early-exit check и очистка stale PID;
- `Configured` проверяет только домен и число клиентов, но не валидность остальных полей;
- `WriteJSON()` игнорирует переданный HTTP status, поэтому `AddClient` фактически не
  гарантирует заявленный `201 Created`;
- route handlers недостаточно строго ограничивают HTTP methods.

## Недостающие тесты

Добавить тесты:

1. invalid candidate не меняет сохранённый или работающий config;
2. ошибка restart в client CRUD возвращается API и вызывает rollback;
3. shutdown процесса не меняет persisted `Enabled`;
4. два Xray runtime не управляют PID друг друга;
5. PID reuse и ошибка чтения `/proc` работают fail-closed;
6. immediate child exit не считается успешным запуском;
7. malformed settings переводят сервис в recovery state;
8. неизвестные legacy-поля и реальные migration fixtures;
9. HTTP method/status contracts Xray API;
10. generated client profiles проходят проверку соответствующим реальным ядром;
11. интеграционный тест подтверждает отсутствие старых `/api/xray/*` mutation routes;
12. UI интеграций содержит только install/uninstall и сведения о бинарнике.

## Рекомендуемый порядок исправления

1. Устранить двойной control plane и отвязать Integrations UI от старого handler.
2. Разделить `Shutdown` и пользовательский `Stop/Disable`.
3. Реализовать transactional candidate apply с rollback для config и client CRUD.
4. Усилить process identity и readiness.
5. Добавить schema migration/recovery и реальные compatibility fixtures.
6. Сделать выдачу клиентских форматов capability-dependent.
7. После этого повторить WSL tests, ARM64 build и безопасную проверку на роутере без
   принудительной переустановки.

## Оценка документов

`implementation_plan.md` разумно описывает часть safety foundation, но не учитывает
существование старого Xray handler и lifecycle shutdown. `walkthrough.md` преждевременно
заявляет этап успешно завершённым. После устранения замечаний walkthrough следует
переписать по фактическим проверкам и отдельно перечислить ещё не реализованные этапы:
мастер, статистика, Telegram per-channel metrics и полноценный Xray client runtime.
