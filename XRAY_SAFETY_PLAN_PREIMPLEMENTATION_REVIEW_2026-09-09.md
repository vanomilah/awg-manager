# Предреализационная проверка плана Xray Safety Foundation

Дата: 2026-09-09  
Проверен файл:
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Итог

Предыдущие замечания учтены: порты разделены, конструктор объявлен side-effect free,
интерфейс разрешения конфликта перенесён в `Серверы → Xray`, init script обрабатывается с
учётом symlink, а legacy process обнаруживается без `pidof`.

До начала реализации необходимо устранить четыре технических блокера ниже. Без них план
может пройти unit-тесты, но сломать dispatcher или ошибочно признать чужой listener
успешно запущенным Xray.

## Обязательные поправки

### 1. P0: выбранный `DispatcherPort` должен применяться к реальному dispatcher

Сейчас `cmd/awg-manager/wiring_server.go` создаёт CDN dispatcher с фиксированным
`ListenAddr: ":9009"` до завершения миграции. План допускает выбор другого
`DispatcherPort`, например `9019`, но не описывает передачу этого значения в реально
создаваемый dispatcher.

Необходимо изменить lifecycle:

```text
initialize stores and logging
  → load new Xray settings
  → read-only legacy discovery
  → build or resolve migration plan and ports
  → construct/reconfigure dispatcher with resolved DispatcherPort
  → execute approved/automatic migration transaction
  → start dispatcher and Xray in a defined order
```

Если migration conflict ещё не разрешён, нельзя запускать два listener на потенциально
конфликтующих портах.

`DispatcherPort` должен быть единственным источником для:

- `cdnDispatcher.ListenAddr`;
- preflight port ownership;
- status/diagnostics;
- runtime reload;
- migration report;
- rollback previous dispatcher state.

Если порт меняется во время работающего приложения, требуется транзакционная
reconfigure/restart dispatcher с rollback, а не только изменение Xray settings.

### 2. P0: `net.Dial` не подтверждает принадлежность listener процессу Xray

Успешное соединение с `127.0.0.1:<port>` доказывает только наличие какого-либо listener.
Если порт уже удерживается старым или посторонним процессом, readiness может ошибочно
объявить новый Xray запущенным даже после его раннего сбоя.

Перед запуском preflight обязан подтвердить, что порт свободен либо принадлежит ровно тому
owned process, который транзакция собирается заменить. После запуска readiness должен
связать listener с PID нового Xray:

- получить socket inode из `/proc/<pid>/fd`;
- сопоставить его с ожидаемым адресом/портом в `/proc/net/tcp` и `/proc/net/tcp6`;
- либо использовать другой надёжный platform adapter, возвращающий PID владельца порта;
- только после подтверждения ownership выполнять дополнительный `net.Dial` functional
  probe.

При невозможности доказать ownership запуск работает fail-closed. Для этой логики нужен
инъецируемый `ListenerOwnershipProbe`, чтобы unit-тесты не зависели от реального `/proc`.

### 3. P1: канал `done` не должен использоваться как очередь результата несколькими читателями

Предложенная конструкция:

```go
done chan error
```

опасна, если readiness первым прочитает единственное значение, а Stop/Rollback затем также
попытается получить результат. После закрытия канала следующий читатель получит нулевое
значение `nil`, а не исходную ошибку процесса.

Использовать broadcast завершения и отдельно сохранённый результат:

```go
type managedProc struct {
    cmd       *exec.Cmd
    pid       int
    startTime uint64
    exited    chan struct{}
    exitErr   error
    exitMu    sync.RWMutex
}
```

Единственный lifecycle goroutine вызывает `Wait()`, записывает `exitErr` под lock и
закрывает `exited`. Readiness, Stop и Rollback ждут закрытия `exited`, после чего читают
один и тот же сохранённый `exitErr`. Добавить race-тест одновременного readiness/stop.

### 4. P1: канонический путь нельзя выбирать по неподтверждённому предположению

План объявляет `/opt/bin/xray` стандартным путём пакета Entware, но текущий новый сервис
использует `/opt/sbin/xray`. До изменения необходимо определить фактического владельца и
путь установленного пакета:

```text
opkg status xray-core
opkg files xray-core
```

Resolver должен:

1. предпочитать путь, которым владеет установленный пакет;
2. отличать package-owned, AWG-managed и external binary;
3. выбрать один active path и сохранить его в runtime state;
4. не переключаться на другой найденный binary между validation и start;
5. включить path и fingerprint бинарника в process identity/candidate transaction;
6. никогда не удалять external binary командой `opkg remove` или прямым unlink.

Если проект намеренно выбирает новый managed canonical path, план должен включать
отдельную атомарную migration binary path с проверкой архитектуры/версии и rollback.

## Дополнительные требования

### Read-only constructor

`xrayserver.New()` может читать собственный settings-файл, но сканирование legacy `/proc`
лучше выполнять отдельным `LegacyDiscovery` с `context.Context`. Это позволяет ограничить
время операции, тестировать её независимо и не превращать обычное создание сервиса в
потенциально долгий системный scan.

### `keep_legacy`

Опция `keep_legacy` должна явно переводить новый Xray server service в неактивное
состояние и оставить legacy init/runtime без изменений. Поскольку старый mutation API
удаляется, UI обязан предупредить, что это временный режим только для сохранения текущей
работы и предложить последующую миграцию. Новый runtime в этом режиме не запускается.

### Тестируемость platform-specific логики

Вынести за интерфейсы:

- proc scanner;
- process identity/start time;
- listener ownership;
- port allocator;
- filesystem/init-script operations;
- binary resolver/opkg ownership;
- process launcher/readiness clock.

Linux реализации закрыть build tags, а unit-тесты выполнять на fake adapters. Отдельные
Linux integration tests проверяют реальные `/proc`, sockets и signals.

## Недостающие тесты

Добавить к указанному в плане набору:

1. allocator выбрал `9019`, и dispatcher действительно слушает `9019`, а не `9009`;
2. rollback возвращает dispatcher на предыдущий порт;
3. чужой listener на ожидаемом порту не проходит readiness нового Xray;
4. процесс Xray завершился, но старый listener остался — readiness завершается ошибкой;
5. listener inode принадлежит ожидаемому PID;
6. readiness и Stop одновременно наблюдают одинаковый `exitErr` без race;
7. opkg-owned binary выбирается при наличии второго external binary;
8. resolver не меняет active binary path внутри одной транзакции;
9. uninstall external binary блокируется и не удаляет файл;
10. `keep_legacy` не запускает новый runtime и не изменяет legacy init/process.

## Критерий допуска

План можно запускать после явного включения:

1. конфигурирования реального dispatcher через resolved `DispatcherPort`;
2. listener ownership verification, связанной с PID нового Xray;
3. broadcast-модели завершения процесса с сохранённым `exitErr`;
4. package-aware выбора канонического binary path без предположения о каталоге;
5. дополнительных интеграционных тестов dispatcher, listener ownership и concurrent
   process lifecycle.

После этих уточнений план будет готов к реализации Stage 1.
