# Финальное ревью консолидированного плана Xray Safety Foundation

Дата: 2026-09-09  
Проверен файл:
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Решение

**Условно одобрено после пяти точечных поправок ниже.**

План впервые стал самодостаточным и вернул все основные требования предыдущих ревью:

- единственный Xray control plane;
- безопасный binary resolver и uninstall gate;
- typed config, recovery и backward compatibility;
- fail-closed process identity;
- компонентную candidate transaction;
- topology-aware legacy migration;
- формальные контракты dispatcher;
- cross-component coordinator и crash journal;
- frontend/API contracts;
- полную матрицу тестирования.

Новая архитектурная переработка после внесения поправок не требуется. Далее можно
переходить к реализации небольшими проверяемыми блоками.

## Обязательные поправки перед реализацией

### 1. Side-effect-free загрузка противоречит перемещению повреждённого файла

В плане одновременно заявлены:

- безопасная загрузка/конструктор без системных мутаций;
- немедленное перемещение повреждённого settings-файла в
  `xray-server-settings.json.corrupt.<timestamp>`.

Чтение повреждённого файла должно только:

- сохранить ошибку и `RecoveryRequired=true` в памяти;
- сохранить путь и fingerprint исходного файла;
- не подставлять рабочие defaults и не запускать процесс;
- не переименовывать и не перезаписывать файл в `New()`.

Архивирование `.corrupt` выполняется отдельной recovery-операцией после инициализации
координатора и durable journal либо по явному действию пользователя. Предпочтительно
сначала создать защищённую копию `0600`, проверить её checksum/fsync и оставить исходник
на месте до подтверждённого восстановления.

Добавить тест: создание `Service` с malformed JSON не изменяет directory entries, bytes,
mode и mtime исходного файла.

### 2. `process-wide lock` недостаточен — нужен межпроцессный lock

Два экземпляра AWG Manager могут кратковременно пересечься при upgrade/restart или ручном
запуске. Mutex внутри процесса не защищает transaction journal и компоненты от второго
процесса.

Coordinator должен использовать:

- in-process mutex для goroutines;
- Linux advisory file lock на отдельном lock-файле в управляемом runtime-каталоге;
- owner metadata: PID, start time и transaction ID;
- fail-closed поведение при невозможности получить lock;
- автоматическое освобождение kernel lock при завершении процесса;
- timeout/context cancellation без принудительного снятия живого чужого lock.

Не полагаться только на существование lock-файла: stale файл сам по себе не является
активным lock.

Добавить Linux integration test с двумя coordinator/process contenders.

### 3. Snapshots должны быть уникальными и принадлежать transaction ID

Фиксированные имена `config.json.bak` и `settings.json.bak` могут столкнуться с migration
backup, предыдущей транзакцией или ручным восстановлением.

Каждый `PreparedXray` получает уникальный `transaction_id`; snapshots размещаются внутри
каталога вроде:

```text
xray/transactions/<transaction_id>/
  runtime.before
  settings.before
  metadata.json
```

Требования:

- каталог и файлы создаются exclusive с `0700/0600`;
- journal ссылается только на нормализованные пути внутри этого каталога;
- checksum каждого snapshot хранится в metadata/journal;
- rollback проверяет checksum до восстановления;
- cleanup выполняется только после durable commit/recovery marker;
- migration backup `.v1.bak` остаётся отдельным долговременным объектом и не смешивается
  с временными transaction snapshots.

### 4. Уточнить terminal-state lifecycle журнала

Текущая формулировка «журнал удаляется/архивируется как `.committed`» недостаточна для
однозначного startup recovery.

Зафиксировать протокол:

```text
write phase=committing + fsync
  → commit all component settings and runtime decision + fsync
  → write phase=committed + final fingerprints + fsync
  → archive/remove active journal atomically + fsync directory
  → cleanup transaction snapshots
```

При старте:

- `phase < committed` — выполнить rollback или продолжение только по явно определённой
  таблице фаз;
- `phase == committed` и fingerprints совпадают — завершить archival/cleanup, не
  откатывать успешную транзакцию;
- `phase == committed`, но fingerprints не совпадают — recovery conflict, никаких
  автоматических мутаций;
- повреждённый journal/checksum/version — recovery state и ручное разрешение;
- `.committed`/`.recovered` archive не считается active transaction.

Добавить crash tests непосредственно до и после записи `phase=committed`.

### 5. Согласовать область нейтрализации персональных defaults

План удаляет персональный hostname только из `cdndispatcher`, но тот же hostname сейчас
присутствует как `DefaultPublicHostname` в `internal/tgwebproxy`. В результате dispatcher
станет нейтральным, а Telegram Proxy продолжит автоматически подставлять персональный
домен.

Даже если Stage 1 сфокусирован на Xray, общая ingress topology использует Telegram config.
Нужно выбрать и явно записать один вариант:

1. в рамках Stage 1 удалить персональный default также из Telegram Proxy и обработать
   пустой `PublicHostname` через existing-config migration; либо
2. объявить это отдельной обязательной prerequisite-задачей, без которой новая общая
   topology не считается готовой к публикации.

Нельзя оставлять персональное значение скрытым fallback. Тест нейтральной терминологии
должен сканировать как минимум `internal/cdndispatcher`, `internal/tgwebproxy`, Xray/TG UI,
API messages и generated labels.

## Уточнения, которые можно внести во время реализации

### Нормализация Xray prefix

Перед проверкой границы:

- требовать leading `/`;
- нормализовать ровно один trailing `/` или удалить его в canonical representation;
- запретить пустой prefix, query/fragment, NUL и traversal segments;
- проверить отсутствие пересечения с `/.cdndisp/health`;
- использовать escaped path semantics осознанно, чтобы encoded slash не обходил matcher.

### Reconfigure dispatcher

Разделить реализацию:

- target-only update — immutable routing snapshot через `atomic.Pointer`/эквивалент;
- address change — bind нового listener, publish candidate, graceful drain старого;
- same address:port не открывается повторно и не требует `SO_REUSEPORT`;
- ошибка после publish должна возвращать предыдущий routing snapshot и listener state.

### HTTP ошибки

Ошибки пользовательского ввода и конфликты применять с точными кодами:

- malformed request/schema — `400`;
- resource/client not found — `404`;
- dependency/migration/revision conflict — `409`;
- валидный, но неподдерживаемый capability — `422`;
- recovery/unavailable runtime — `503`;
- внутренний сбой — `500`.

Не сводить все ошибки CRUD/apply к `500`.

## Рекомендуемый порядок реализации

1. Tests/fixtures и API contracts без production mutations.
2. Binary resolver и neutral integration card.
3. Xray config loading/recovery/atomic primitives.
4. Managed process lifecycle и listener ownership.
5. Component-level prepared transaction и CRUD rollback.
6. Dispatcher routing, synchronous bind и reconfigure.
7. Coordinator lock, journal и crash recovery.
8. Legacy discovery/migration/conflict UI.
9. Удаление старого control plane после прохождения migration tests.
10. WSL race suite, frontend check, ARM64 build и отдельная router verification.

Старый control plane нельзя удалять в начале работы: сначала должен быть реализован и
протестирован migration path, затем выполняется переключение wiring/routes.

## Финальный критерий допуска

После добавления пяти обязательных поправок план можно запускать без очередного полного
архитектурного ревью. При реализации проверять каждый блок отдельно и не заявлять Stage 1
завершённым до прохождения всей acceptance matrix.
