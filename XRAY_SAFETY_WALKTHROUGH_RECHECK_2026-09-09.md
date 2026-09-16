# Повторный аудит Xray Safety walkthrough

Дата: 2026-09-09  
Walkthrough: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Вердикт

Большая часть замечаний предыдущего аудита исправлена, но заявление walkthrough о полной готовности и полностью зелёной проверке **не подтверждается**. На текущем рабочем дереве остаются два серьёзных дефекта migration saga, несколько недостатков тестов/документации, а заявленный полный race-набор падает.

Деплой этой версии пока не рекомендуется.

## Подтверждённые исправления

- Production shutdown hook использует `ShutdownRuntime` для Xray/TG и больше не записывает `Enabled=false`.
- `CommitPrepared` использует общий `rollbackAndJoin` во всех основных failure branches.
- Xray rollback прекращает работу до restart при ошибке восстановления файлов.
- TG `StartConfigured` останавливает workers при readiness failure и объединяет ошибки компенсации.
- Ошибки archive/write saga journal больше не игнорируются в проверенных ветках.
- Добавлены `managed_stop_prepared`, исходное состояние init-script и признаки legacy runtime.
- Recovery archive failure возвращается и включает recovery-required.
- Добавлены новые saga fault tests и shutdown persistence test.

## Оставшиеся проблемы

### P0. Одновременное наличие active и disabled init-script не запрещено

Файл: `internal/serveringress/migration_saga.go`, pre-flight и строки около `259-266`.

Pre-flight допускает состояние, когда одновременно существуют:

```text
S99xray-cdn
S99xray-cdn.disabled
```

Затем `os.Rename(initDisabled, initActive)` на Unix способен заменить active-файл disabled-файлом. Это уничтожает исходный active script и делает rollback неоднозначным.

Исправление: fail-closed отклонять конфликт `activeExists && disabledExists` до записи/мутации либо реализовать отдельный безопасный reconciliation с двумя checksum и резервной копией. Для этой migration безопаснее немедленный отказ.

Нужен тест, проверяющий неизменность обоих файлов и отсутствие остановки/запуска процессов.

### P0. Определение `PreviousLegacyRunning` должно быть tri-state

Файл: `internal/serveringress/migration_saga.go:193-196`.

Любая ошибка `probeLegacyOwnership` превращается в `PreviousLegacyRunning=false`. Но ошибка может означать не «не запущен», а:

- порт открыт чужим процессом;
- `/proc` недоступен;
- PID/cmdline невозможно прочитать;
- legacy config распознан неверно;
- ownership временно не доказан.

После этого saga выставляет `LegacyStartedBySaga=true`; при дальнейшей ошибке rollback может остановить ранее работавший, но не распознанный legacy daemon.

Исправление: probe должен возвращать три состояния — `owned/running`, `definitely stopped`, `indeterminate/conflict`. При третьем состоянии migration обязана завершиться до любых мутаций. Открытый порт с недоказанным ownership — конфликт, а не «stopped».

### P1. Ownership всё ещё не проверяет legacy config path и точный address

Walkthrough утверждает строгую process ownership verification, но фактически `probeLegacyOwnership` принимает любой cmdline, содержащий `xray` (`migration_saga.go:517-526`).

Поиск socket inode:

- фильтрует порт, но не конкретный local address;
- может выбрать первый listener на совпавшем порту из TCP4/TCP6;
- не проверяет путь `/opt/etc/xray/config.json`;
- не сверяет executable или trusted pidfile.

Это лучше прежнего fail-open, но ещё не доказывает, что listener принадлежит нужному legacy instance. Нужно передать config path в probe и проверять cmdline/executable либо pidfile плюс start-time/identity.

### P1. Shutdown wiring test копирует hook вместо проверки production helper

Файл: `cmd/awg-manager/wiring_server_test.go`.

Тест вручную повторяет тело hook. Если production wiring снова заменят на `Stop()`, а копию в тесте не изменят, тест останется зелёным. Следует вынести shutdown sequence в production-функцию (`shutdownIngressRuntime`) и вызывать одну и ту же функцию из wiring и теста.

### P1. Crash/fault coverage всё ещё не соответствует заявлению

Добавленные тесты полезны, но это не phase-by-phase fault suite. Не покрыты как минимум:

- конфликт active+disabled;
- indeterminate pre-existing legacy ownership;
- падение записи intent/start intent;
- chmod/start/stop/decision write failures;
- crash после managed commit до следующей записи;
- finalize failure с реальным сохранённым snapshot;
- writeSagaJournal failure в rollback branches;
- exact address/config identity ownership.

Тест `TestMigrationSaga_FailClosedOwnershipProbe` использует custom callback и фиктивный закрытый порт; он не доказывает Linux `/proc` path для alien listener.

### P2. Walkthrough снова перечисляет несуществующие поля Telegram config

В актуальном `tgwebproxy.Config` отсутствуют `UseSocks5` и `Socks5Addr`, хотя walkthrough утверждает их сохранение. Реальные unmanaged fields: `AdminPort`, `DirectHost`, `DirectPort`, `Secret`, `LegacySecret`, `LegacyExpiresAt`, `Backend`, `CarrierMode`, `UpstreamDevice`, `TlsDomain`.

Документ необходимо синхронизировать с фактическим `internal/tgwebproxy/types.go`.

## Независимый результат тестов

Запущена ровно заявленная команда:

```text
go test -count=1 -race ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/api/... ./cmd/awg-manager
```

Прошли:

- `internal/tgwebproxy`;
- `internal/xrayserver` и `xraybin`;
- `internal/cdndispatcher`;
- `internal/serveringress`;
- `internal/api`.

Упал пакет `cmd/awg-manager`:

1. `TestDynamicEngineFailureReturnsOriginalAndWithdrawalErrors` — `Reload() error = nil`.
2. `TestDynamicEngineReload_SingboxPrimaryKeepsExportsRuntime` — отсутствуют ожидаемые validate/reload calls.
3. `TestDynamicEngineSync_RoutingDisabledKeepsOnlyExportsRuntime` — отсутствуют ожидаемые validate/reload calls.
4. `TestDynamicEngineReload_TransitionsMihomoPrimaryToSingboxSidecar` — неверные reload/stop counters.
5. `TestDynamicEngineAdoptRunningProcess` — `mihomo startCalls = 1`, ожидалось `0`.

Итог команды: exit code 1. Поэтому утверждение walkthrough «zero test failures» для текущего состояния неверно. Возможно, рабочее дерево изменилось после формирования walkthrough, но перед приемкой в любом случае требуется актуальный зелёный прогон.

## Что делать дальше

1. Исправить два P0 в saga: конфликт init scripts и tri-state pre-flight ownership.
2. Усилить ownership точным address/config/process identity.
3. Вынести production shutdown helper и тестировать его, а не копию.
4. Добавить недостающие fault/crash tests.
5. Исправить или обоснованно актуализировать пять падающих DynamicEngine tests.
6. Исправить фактические имена полей в walkthrough.
7. Повторить race suite, ARM64 build, frontend typecheck и `git diff --check` на одном неизменном commit/worktree snapshot.

До выполнения этих пунктов walkthrough не является приемочным подтверждением готовности.
