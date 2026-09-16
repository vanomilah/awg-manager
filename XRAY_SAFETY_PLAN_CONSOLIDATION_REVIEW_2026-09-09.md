# Проверка консолидации финального плана Xray Safety Foundation

Дата: 2026-09-09  
Проверен файл:
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Итог

Последний набор замечаний по dispatcher учтён корректно:

- формализована совместимая таблица маршрутов;
- bind dispatcher выполняется синхронно;
- добавлен persistent transaction journal;
- Xray получил отдельный `ListenAddress`;
- readiness разделена на process/socket/router/worker уровни;
- coordinator использует affected-components;
- `SourceManaged` подтверждается только манифестом;
- персональный hostname удаляется.

Однако документ больше не является полным финальным планом: при сокращении из него
исчезла значительная часть ранее согласованных обязательных требований Stage 1. Если
агент реализует только текущую редакцию, исходные P0/P1 дефекты останутся. План требуется
сначала консолидировать, а не продолжать заменять предыдущую редакцию очередным delta.

## P0: вернуть в единый план полную legacy migration

В текущем разделе состава файлов отсутствует самостоятельный `migration.go`, а также
детальная процедура миграции старого окружения:

- обнаружение `/opt/etc/xray-cdn/config.json`;
- обнаружение `/opt/etc/init.d/S99xray-cdn`;
- ограниченное сканирование `/proc` без `pidof`;
- fingerprint `<pid, start_time, binary, config_path>`;
- symlink-safe snapshot и отключение legacy init script;
- immutable backups с правами `0600`;
- mapping legacy clients/XHTTP fields;
- `migration_conflict` и три стратегии разрешения;
- идемпотентный migration marker;
- rollback legacy process/init state;
- сохранение неизвестных legacy-полей в отчёте;
- обезличенные migration fixtures.

Topology classifier и coordinator не заменяют эту процедуру. Вернуть в `Proposed Changes`:

```text
internal/xrayserver/migration.go
internal/xrayserver/migration_test.go
```

с полным контрактом discovery, plan, apply, rollback и idempotency.

## P0: вернуть transactional candidate apply Xray

Из текущего плана исчезли обязательные свойства Xray settings/runtime transaction:

- schema validation портов, адреса, path и UUID;
- `RenderRuntimeConfig(candidate)`;
- `xray run -test -c <candidate>` до остановки рабочего процесса;
- snapshot предыдущих settings/runtime/process state;
- атомарная запись `0600` с `O_EXCL`, file sync и directory sync;
- commit settings только после успешного readiness;
- rollback предыдущего runtime и процесса;
- возврат original error и rollback status;
- запрет рассинхронизации persisted settings и runtime revision.

Coordinator отвечает за межкомпонентную транзакцию, но Xray service всё равно обязан
предоставлять корректную локальную prepared transaction. В плане нужно определить
двухуровневую модель:

```text
Xray PrepareCandidate/CommitPrepared/RollbackPrepared
Dispatcher PrepareCandidate/CommitPrepared/RollbackPrepared
Coordinator journal + ordering + cross-component recovery
```

Компонентные snapshots должны быть durable до первой общей мутации.

## P0: вернуть безопасный process lifecycle Xray

В текущем файле упомянут `managedProc`, но отсутствуют ранее согласованные гарантии:

- единственная lifecycle goroutine вызывает `cmd.Wait()`;
- broadcast `exited chan struct{}` и сохранённый `exitErr`;
- PID record содержит PID, start time, config path и binary fingerprint;
- unreadable `/proc` работает fail-closed;
- identity повторно проверяется перед `SIGTERM` и `SIGKILL`;
- `Shutdown()` не меняет persisted `Enabled`;
- пользовательский `Stop()` меняет `Enabled`;
- startup при `RecoveryRequired` блокируется;
- early process exit очищает runtime state/PID record;
- сигналы не могут быть отправлены другому Xray runtime.

Вернуть эти требования явно в `service.go` и тестовую программу.

## P0: вернуть устранение двойного control plane

Текущий план говорит об удалении старых мутаций, но больше не перечисляет полный контракт:

- `/api/xray/start`, `/stop`, `/restart`, `/config` не регистрируются;
- основной mux возвращает `404` для этих маршрутов;
- `/api/xray/status|install|uninstall` управляют только бинарником;
- `/api/servers/xray/*` является единственным server control plane;
- `IntegrationsCard` не показывает PID, server state, порт, link или QR;
- frontend удаляет старые методы `xrayStart/xrayStop/xrayRestart/xrayConfig`;
- старый handler не создаёт init script и не пишет `/opt/etc/xray-cdn/config.json`;
- hardcoded IP/version/defaults удаляются.

Эти пункты должны присутствовать в финальном acceptance checklist и основном mux test.

## P1: вернуть config recovery и обратную совместимость

В последней версии отсутствуют:

- `rawFields` с приоритетом typed fields;
- одноразовый schema migration backup;
- `.corrupt.<timestamp>` при повреждённом JSON;
- `RecoveryRequired` и `LastError` в API;
- запрет автозапуска, uninstall и destructive migration в recovery state;
- deep copy всех reference-полей `GetConfig()`;
- безопасная обработка ошибки `crypto/rand.Read`;
- синтетические legacy fixtures без реальных данных.

Вернуть эти пункты. Повреждённый config нельзя молча заменять safe defaults.

## P1: вернуть transactional CRUD клиентов

`AddClient`, `DeleteClient` и `ToggleClient` должны:

- формировать candidate без мутации active state;
- проходить render/config test;
- применять runtime;
- сохранять settings после readiness;
- возвращать ошибку при сбое;
- выполнять rollback;
- никогда не использовать `_ = restart...`;
- возвращать корректный HTTP status, включая `201 Created`.

Изменение списка клиентов затрагивает только Xray runtime, но это не отменяет локальную
транзакционность.

## P1: уточнить dispatcher routing contract

### Prefix boundary

`strings.HasPrefix(path, XrayPathPrefix)` направляет также `/cdn-bridge-foreign` в Xray.
Matcher должен нормализовать configured prefix и проверять границу:

```text
path == prefix OR strings.HasPrefix(path, prefix + "/")
```

Если транспорт требует иное сопоставление, оно задаётся явным тестируемым контрактом.
Запрещены пустой Xray prefix, `..`, query в path и пересечение со служебным health path.

### Loopback health

Dispatcher слушает публичный `:P`, поэтому `/.cdndisp/health` физически достижим и извне.
Handler должен проверять фактический peer IP из `RemoteAddr`, не доверять
`X-Forwarded-For`, и возвращать `404`, а не проксировать запрос в Telegram, если peer не
loopback. Лучше дополнительно предоставить отдельный internal listener или прямой метод
`Health()` координатору, чтобы не расширять публичную поверхность.

### `Reconfigure` без ложной атомарности

Если адрес меняется, можно сначала bind нового listener и затем drain старого. Если
меняются targets при том же listener, достаточно атомарно заменить immutable routing
snapshot. Нельзя обещать bind второго listener на том же address:port без `SO_REUSEPORT`.
План должен описывать обе ветки отдельно.

## P1: завершить lifecycle transaction journal

В плане отсутствуют правила:

- `committed` journal должен быть удалён или архивирован только после fsync всех commit
  markers;
- journal schema имеет version и checksum;
- пути snapshots строго валидируются внутри управляемого каталога;
- повреждённый или неизвестной версии journal переводит ingress в recovery state;
- recovery идемпотентен и сам журнал не перезаписывает единственную копию доказательств;
- одновременно может существовать только одна ingress transaction;
- process-wide/file lock предотвращает второй coordinator;
- snapshots не содержат secrets, но сами config snapshots защищены `0600`;
- после успешного rollback результат фиксируется до архивирования journal.

Добавить crash tests для каждой границы между journal fsync и component mutation.

## P1: install/uninstall API и dependency gate

Последняя редакция описывает классификацию binary, но опускает ранее согласованные gates:

- uninstall блокируется при включённом сервере;
- uninstall блокируется при наличии Xray client tunnels;
- uninstall блокируется при ссылках из policies/rules/proxy-groups/service routes;
- неизвестное состояние зависимостей даёт `reference_state_unknown` и fail-closed `409`;
- server/client configs при удалении бинарника сохраняются;
- `SourceExternal` не удаляется;
- `SourceManaged` удаляется только owning installer по валидному manifest;
- запрещены force-флаги;
- UI показывает blockers и ссылки на соответствующие сущности.

Вернуть API tests для всех вариантов.

## P1: capability-dependent client exports

Текущий код безусловно формирует Sing-box JSON и Mihomo YAML с XHTTP. В финальном плане
нужно вернуть:

- эталонный Xray-compatible VLESS export;
- выдачу Sing-box/Mihomo формата только после проверки capabilities конкретной версии;
- отсутствие неподдерживаемого формата в UI;
- validation сгенерированного профиля соответствующим ядром, а не только JSON/YAML parse.

## Нейтральная терминология

Текущая редакция не содержит конкретных названий CDN — это исправлено. Сохранить lint для
UI, API messages, generated labels, docs, presets и screenshots. Сам технический термин
`CDN` допустим.

## Требуемая структура единого финального плана

Следующая редакция должна быть самодостаточной и включать, а не заменять, все слои:

1. Scope и non-goals Stage 1.
2. Единственный Xray control plane.
3. Binary resolver/install/uninstall gates.
4. Xray settings schema/recovery/migration.
5. Xray process lifecycle и identity.
6. Component-level candidate transaction.
7. Legacy discovery/migration/conflict/rollback/idempotency.
8. Dispatcher routing/lifecycle/reconfigure.
9. Cross-component coordinator/journal/startup recovery.
10. Frontend integrations и server conflict UI.
11. HTTP/OpenAPI contracts.
12. Полная test matrix и acceptance checklist.

Не следует удалять требования прошлых recheck после добавления новых уточнений.

## Критерий допуска

Текущую редакцию ещё нельзя запускать как самостоятельный план. Допуск возможен после
консолидации всех ранее согласованных P0/P1 требований в одном файле и устранения деталей
prefix/health/reconfigure/journal выше.

После консолидации дальнейший review должен проверять только внутренние противоречия и
реализуемость, а не заново восстанавливать выпавшие требования из предыдущих файлов.
