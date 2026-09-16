# Финальная проверка плана Xray Safety Foundation

Дата: 2026-09-09  
Проверен файл:
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Итог

План корректно учитывает девять обязательных поправок предыдущего recheck, однако пока не
готов к безопасной реализации: отсутствует миграция фактически существующего старого
Xray control plane.

## P0: обязательная миграция legacy Xray

Сейчас в проекте и на уже настроенных устройствах могут одновременно существовать:

- старый runtime config: `/opt/etc/xray-cdn/config.json`;
- старый init script: `/opt/etc/init.d/S99xray-cdn`;
- старые endpoints: `/api/xray/*`;
- новый settings-файл: `xray/xray-server-settings.json`;
- новый Xray server runtime и `/api/servers/xray/*`.

Простое удаление старых endpoints и UI оставит старую рабочую конфигурацию без управления
или приведёт к одновременному запуску двух процессов. План должен содержать отдельную
идемпотентную migration procedure.

### Правила обнаружения

При старте AWG Manager определить:

1. существует ли legacy config;
2. существует ли legacy init script и включён ли его автозапуск;
3. запущен ли процесс именно с legacy config;
4. настроен ли новый Xray server service;
5. совпадают или конфликтуют listener ports и client UUID.

Обнаружение процесса выполняется по PID identity, cmdline, config path и start time.
Использование `pidof xray` запрещено.

### Автоматическая миграция

Автоматическая миграция допустима только если:

- legacy config валиден;
- новый Xray server ещё не настроен;
- значения legacy config однозначно преобразуются в новую typed schema;
- отсутствует конфликт с другим listener/process;
- новый бинарник успешно валидирует сгенерированный candidate.

Переносимые поля:

- listen/public port;
- UUID и имя клиента, если имя доступно;
- public host/domain;
- XHTTP path, mode и uplink method;
- поддерживаемые параметры транспорта;
- состояние enabled, определённое из init/runtime state;
- outbound mode только при однозначном соответствии новой модели.

Неподдерживаемые legacy-поля сохраняются в migration report и не отбрасываются молча.

### Транзакция миграции

```text
detect legacy state
  → read and validate legacy config
  → create immutable 0600 backups of config and init script
  → map legacy data into new candidate
  → validate schema
  → render and run xray config test
  → stop only the verified legacy process
  → start new server runtime and perform readiness check
  → commit new settings
  → disable legacy autostart
  → write migration marker and report
```

Legacy config и init script нельзя удалять автоматически. Их очистка выполняется только
отдельным подтверждённым действием после успешной проверки нового runtime.

### Rollback миграции

При любой ошибке после остановки legacy-процесса:

```text
stop failed new runtime if it belongs to AWG Manager
  → restore legacy config/init state
  → restart verified legacy service when it was previously running
  → verify legacy readiness
  → leave new settings uncommitted
  → return original error and rollback result
```

### Конфликт двух настроенных реализаций

Если legacy и новый server config оба настроены, автоматическое объединение запрещено.
Сервис переходит в состояние `migration_conflict` и показывает:

- оба источника конфигурации;
- процессы и занятые порты;
- количество клиентов без раскрытия UUID;
- безопасные варианты `Оставить новую`, `Импортировать старую как отдельный черновик` или
  `Продолжить использовать старую`;
- предварительный план изменений до применения.

Ни один процесс и файл не изменяется до явного выбора пользователя.

### Идемпотентность

После успешной миграции записывается marker с версией миграции и fingerprint исходного
legacy config. Повторный старт AWG Manager не должен повторно импортировать клиентов,
перезаписывать backup или снова изменять init script.

## Дополнительные обязательные уточнения

### 1. Один канонический путь Xray-core

Формулировка «канонический путь `/opt/sbin/xray` и `/opt/bin/xray`» некорректна. Выбрать
один managed canonical path. Второй путь допускается только как legacy fallback для
обнаружения и контролируемой миграции. Installer, server runtime, будущий client runtime,
validator и process identity используют один resolver result.

### 2. Единственный владелец `cmd.Wait()`

Readiness, наблюдение за процессом и остановка не должны параллельно или последовательно
вызывать `cmd.Wait()` для одного `exec.Cmd`. После `cmd.Start()` создаётся один lifecycle
goroutine, который единственный вызывает `Wait()` и публикует результат в сохранённый
done channel. Readiness и stop только наблюдают этот канал.

### 3. Recovery state блокирует автозапуск

При `RecoveryRequired=true` запрещены:

- автоматический запуск Xray при старте AWG Manager;
- применение частично загруженных defaults;
- перезапись повреждённого settings-файла;
- destructive migration или uninstall.

Разрешены только status, экспорт обезличенной диагностики, восстановление из backup и
явно подтверждённый сброс.

### 4. Безопасное удаление при неизвестных ссылках

Если backend не смог получить список Xray client runtimes или ссылок из политик,
маршрутов и proxy-групп, uninstall работает fail-closed и возвращает blocker
`reference_state_unknown`. Неизвестное состояние нельзя трактовать как отсутствие ссылок.

### 5. Проверка migration fixtures

Добавить обезличенные fixtures:

1. только legacy config;
2. только новый config;
3. оба config без конфликта;
4. оба config с конфликтом порта;
5. повреждённый legacy config;
6. legacy process running;
7. повторный запуск после успешной миграции;
8. ошибка readiness нового runtime и успешный rollback legacy;
9. ошибка rollback с корректным аварийным статусом.

Fixtures используют только synthetic domains, documentation IP ranges и тестовые UUID.

## Критерий допуска плана

План можно передавать в реализацию после добавления:

1. транзакционной legacy migration;
2. режима `migration_conflict`;
3. идемпотентного migration marker;
4. одного канонического binary path;
5. единственного владельца `cmd.Wait()`;
6. запрета автозапуска в recovery state;
7. fail-closed uninstall при неизвестном состоянии ссылок;
8. migration/rollback fixtures.

После этих дополнений архитектура Stage 1 будет соответствовать требованиям безопасного
перехода от двойного control plane к единому Xray server service.
