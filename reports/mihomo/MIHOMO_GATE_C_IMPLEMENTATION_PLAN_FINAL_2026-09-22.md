# Mihomo Gate C — обязательный план реализации

**Режим работы:** реализовать код и тесты сейчас. Не возвращать новый план на ревью.  
**Репозиторий:** `E:\AWGM\awg-manager`  
**Ветка:** `feature/mihomo-ai-proxyrt`  
**Основание:** Gate B закрыт документом `reports/mihomo/MIHOMO_GATE_B_FINAL_CLOSURE_2026-09-22.md`.

## 1. Цель Gate C

Закрыть доказательство соответствия запущенного процесса Mihomo выбранному поколению, корректное определение владельца TCP/UDP-сокетов, гарантированное завершение/reap процесса и production-контракт наблюдения за NDMS `ProxyN` для `ExactBridgeRuntime`.

Gate C не меняет UI, правила маршрутизации, подписки, `wdtt`, `qwdtt`, Xray и Telegram Proxy. IPK не собирать, на роутеры не устанавливать.

## 2. Обязательные ограничения

- Сохранить все посторонние изменения грязного рабочего дерева.
- Не использовать `git reset`, `git checkout`, автоматический cleanup и `--force-reinstall`.
- Не изменять порты `1099`, `Wireguard2` и `telemt :8443`.
- Не использовать `TransactionManifest.Sequence` как номер поколения. Это порядковый номер CAS-записи, а не generation.
- Не заменять существующий `procnet.FindListeningProcess(procDir, addr, port)` несовместимой сигнатурой. Добавить новый network-aware API, а старый оставить TCP-совместимой обёрткой.
- Не считать desired store доказательством текущего состояния NDMS.
- Не подставлять transaction metadata (`Generation`, `OwnerUUID`) в наблюдаемые OS/NDMS-факты, если ОС их не сообщает.
- Не возвращать очередной документ-план. Результат Gate C — фактический diff, тесты и честный resolution report.

## 3. Перед началом

Часть bridge-кода уже реализована в `cmd/awg-manager/mihomo_bridge_runtime.go`: существуют `InspectBridge`, точечные `PublishBridge`/`WithdrawBridge` и `ListObservedBridges`. Не переписывать их вслепую. Сначала зафиксировать текущее поведение тестами, затем изменить только то, что требуется настоящим typed-observer контрактом ниже.

## 4. Gate C1 — поколение процесса и ProcessReceipt

### Дефект

`restartControlledLocked` сейчас берёт generation из старого `c.appliedRecord`. Во время применения поколения `N+1` это ещё запись поколения `N`, поэтому process identity и receipt могут быть помечены неверным поколением.

### Реализация

1. Изменить сигнатуру:

   ```go
   func (c *ApplyCoordinator) restartControlledLocked(
       ctx context.Context,
       targetGeneration uint64,
       expectedDigest string,
       listeners []ListenerSpec,
   ) error
   ```

2. Передавать только авторитетный номер целевого поколения:

   - обычный apply: локальную переменную `newGen`, которой уже помечается `AppliedGenerationRecord` и staged generation bundle;
   - rollback/recovery LKG: `gm.GenerationNumber` из проверенного generation manifest;
   - regenerate/reapply: локальный номер поколения, которым публикуется соответствующий staged bundle;
   - никогда не передавать `manifest.Sequence` и никогда не вычислять target из старого `c.appliedRecord` внутри restart helper.

3. `CaptureIdentity(..., targetGeneration)` и `ProcessReceipt.AppliedGeneration` обязаны получить один и тот же `targetGeneration`.

4. До финального durable commit проверить:

   - для работающего runtime receipt присутствует;
   - `receipt.AppliedGeneration == rec.Generation`;
   - `receipt.RuntimeProcessIdentity.Generation == rec.Generation`;
   - PID, start ticks, executable/config dir и требуемые listeners уже доказаны;
   - для `RuntimeOff` receipt строго `nil`, процесс остановлен, новый процесс не запускается.

5. Проверка после перезапуска демона не может доверять только PID из файла. На Linux повторно доказать `/proc/<pid>/stat`, `/proc/<pid>/exe`, `cmdline` и ownership всех сокетов. `DaemonEpoch` — дополнительный anti-replay факт, а не замена OS proof.

6. Не изменять уже опубликованный generation bundle задним числом. Если durable manifest/bundle должен содержать receipt, порядок операции должен быть явным: получить runtime proof, сформировать окончательную запись, атомарно опубликовать/подтвердить её согласно существующему Gate B протоколу. Не мутировать committed-файлы in-place.

### Тесты C1

- apply `N -> N+1`: record, identity и receipt равны `N+1`, а не `N`;
- rollback с `N+1` на LKG `N`: receipt равен `gm.GenerationNumber == N`;
- regenerate/reapply использует номер реально опубликованного bundle;
- `RuntimeOff`: receipt отсутствует и spawn не вызывается;
- `manifest.Sequence != generation`: тест доказывает, что sequence никогда не попадает в receipt;
- повторный запуск с тем же PID, но другими start ticks/cmdline, отклоняется.

## 5. Gate C2 — network-aware procfs proof

### Новый совместимый API

В `internal/sys/procnet/listener.go` добавить отдельный API, например:

```go
func FindListeningProcessNetwork(
    procDir string,
    network string,
    addr string,
    port int,
) (ListenerLookup, error)
```

Существующий `FindListeningProcess(procDir, addr, port)` сохранить как TCP-обёртку. Существующие `FindListeningPIDForAddressPort` и `FindListeningPIDForPort` сохраняют TCP-семантику. Перевести на новый API только callers, у которых уже есть `network`, прежде всего `LinuxProcessVerifier.VerifySocketOwnership`.

### Семантика таблиц

- `tcp4`: `/proc/net/tcp`, состояние `0A`;
- `tcp6`: `/proc/net/tcp6`, состояние `0A`;
- `udp4`: `/proc/net/udp`, bound/unconnected состояние `07`;
- `udp6`: `/proc/net/udp6`, bound/unconnected состояние `07`;
- `tcp` и `udp`: выбрать семейство по адресу; для wildcard/unspecified корректно проверить обе доступные таблицы и объединить результат;
- неизвестный network — явная ошибка.

Отсутствующая таблица конкретно запрошенного семейства (`tcp6`, `udp6`) — ошибка наблюдения. Для общего `tcp`/`udp` отсутствие опциональной таблицы второго семейства допустимо только если таблица требуемого/доступного семейства успешно прочитана. IPv4-only система без `tcp6/udp6` не должна ложно переходить в recovery.

### Fail-closed правила

- unreadable table, directory вместо файла, malformed matching row или невозможность достоверно сопоставить inode владельцу — ошибка, а не «listener отсутствует»;
- отсутствие matching socket при успешно прочитанных таблицах — нормальный `SocketFound=false`;
- если одному совпадению соответствуют разные PID или найдено несколько неоднозначных владельцев — ошибка ambiguity; нельзя возвращать первый PID;
- ошибки чтения `/proc/<pid>/fd` нельзя все молча игнорировать: если socket найден, но владелец из-за ошибок не доказан, вернуть observation error;
- wildcard и specific bind должны сравниваться осознанно для IPv4/IPv6, без совпадения произвольного адреса только по порту.

### Process identity и cmdline

- `/proc/<pid>/exe` после canonicalization является главным доказательством executable;
- `cmdline` должен быть непустым NUL-separated argv и явно содержать поддерживаемую Mihomo форму `-d <canonical-config-dir>`;
- basename `argv[0]` сам по себе не является достаточным доказательством бинарника;
- unreadable/malformed cmdline в `CaptureIdentity` — ошибка. Текущее поведение, которое пропускает проверку при ошибке чтения, исправить;
- поддерживать `-d=<dir>` только если реальный CLI Mihomo и существующие вызовы AWG Manager действительно используют эту форму; иначе не расширять контракт предположением.

### Тесты C2

- TCP и UDP на одном порту принадлежат разным PID;
- UDP-only listener;
- IPv4, IPv6, wildcard и specific bind;
- IPv4-only fixture без `tcp6/udp6` для общего network;
- явный `udp6` при отсутствии таблицы — ошибка;
- socket найден, inode не разрешён в PID — fail closed;
- один inode/порт с неоднозначными владельцами — fail closed;
- unreadable/directory/malformed proc table;
- PID reuse: совпадает PID, но не start ticks;
- неверный executable, пустой/unreadable cmdline и неверный `-d` отклоняются.

Для Linux integration tests использовать реальные локальные TCP/UDP-сокеты под build tag `linux`; unit fixtures остаются детерминированными.

## 6. Gate C3 — Stop/Kill/Reap

### Контракт

1. Добавить sentinel `ErrProcessNotReaped`.
2. `StopAndWait` посылает graceful stop и ждёт graceful timeout либо исходный context.
3. После timeout/cancel эскалирует в kill.
4. После kill всегда использует отдельный внутренний bounded reap timeout, не уже отменённый caller context.
5. Только получение результата единственного `cmd.Wait()`/закрытие единственного `done` доказывает reap.
6. Если reap не доказан в срок — вернуть ошибку, оборачивающую `ErrProcessNotReaped`, с PID.
7. Не создавать второй goroutine `Wait`, не закрывать канал дважды и не вызывать `Wait` параллельно.
8. `os.ErrProcessDone`/ESRCH при signal не считать автоматически доказательством reap: всё равно дождаться owner `done` либо вернуть bounded error.
9. Сохранить компиляцию Windows: Unix signal/reap детали держать в platform-specific файлах или существующей abstraction.

### Реакция coordinator

Все пути, вызывающие `StopAndWait` перед новым spawn (apply, rollback, recovery, regenerate, RuntimeOff), должны использовать общий fail-closed helper или эквивалентное единообразное поведение. При `ErrProcessNotReaped`:

- не запускать новый Mihomo;
- перейти в `StateRecoveryRequired`;
- записать recovery marker с transaction ID, если активна транзакция;
- сохранить исходную ошибку через `%w`, чтобы `errors.Is` работал;
- не выполнять destructive rollback, предполагающий, что старый процесс остановлен.

### Тесты C3 process lifecycle

- graceful exit;
- graceful timeout -> kill -> reap;
- caller context canceled -> kill -> внутренний reap завершается;
- kill/ESRCH + done завершается;
- kill выполнен, done не завершился -> `ErrProcessNotReaped`;
- coordinator не вызывает Start после not-reaped и пишет recovery marker;
- повторный `StopAndWait` и конкурентные stop не создают двойной Wait/deadlock.

Сигналы/waiter лучше инъецировать тестовым seam, а не пытаться создавать недетерминированные zombie-процессы.

## 7. Gate C4 — production ExactBridgeRuntime

### Typed observer в `internal/singbox`

Добавить read-only DTO и API:

```go
type ProxyObservation struct {
    Name        string
    Exists      bool
    Description string
    State       string
    Link        string
    Up          bool
    SystemName  string
    Address     string
}

func (pm *ProxyManager) InspectProxy(ctx context.Context, index int) (ProxyObservation, error)
func (pm *ProxyManager) ListProxyObservations(ctx context.Context) ([]ProxyObservation, error)
```

Реализация читает live NDMS через существующие query interfaces. Не экспортировать приватное поле `queries`. Не брать State/Link/Up/SystemName/Address из native desired store.

### Интеграция bridge runtime

- расширить `bridgeProxyRegistrar`/gated adapter только read-only observer-методами;
- `InspectBridge` строит observed proof из live `ProxyObservation`;
- observable identity: `ProxyIndex/ProxyInterface`, canonical description owner, фактический `SystemName`, `Exists`, `Up`; address требовать только если реальный контракт Keenetic гарантирует его для данного ProxyN;
- `Generation` подтверждать durable transaction manifest/receipt, не выдавать его за OS field;
- foreign или пустой description не считать принадлежащим AWG Manager;
- mapping `ProxyIndex -> native resource/listen port` обязан дать ровно одно совпадение; ноль и несколько — fail closed;
- сохранить уже существующие точечные `PublishBridge` и `WithdrawBridge`; убрать оставшиеся broad mutation paths (`manager.Reconcile(ctx, nil)`) из exact operations;
- `ApplyBridges` также не должен глобально затрагивать соседние bridge. Реализовать его как последовательность exact publish с компенсацией только уже опубликованных элементов текущей операции;
- `ListObservedBridges` может брать список кандидатов из durable/current registries, но каждый факт состояния получает из live observer. Не сканировать и не присваивать себе произвольные `ProxyN`;
- после publish выполнить live postcondition; после withdraw доказать отсутствие exact owned resource;
- publish/withdraw одного bridge не меняет соседний и не удаляет foreign proxy.

### Тесты C4

- compile-time assertion production adapter implements `mihomo.ExactBridgeRuntime`;
- desired says Up, NDMS says Down -> observed Down;
- foreign/empty description блокирует mutation;
- ноль/несколько native mappings блокируют mutation;
- отсутствующий IP допустим, если ProxyN реально не имеет address, но owner/up/system name и listener proof подтверждены;
- generation берётся из durable transaction evidence, не из observer;
- publish/withdraw одного bridge не меняет соседние и foreign bridges;
- partial exact batch failure компенсирует только изменения текущего batch;
- observer читает fake NDMS query, а не native store.

## 8. Порядок реализации

1. C1: target generation и receipt.
2. C2: новый совместимый procnet API, затем Linux verifier/cmdline.
3. C3: stop/kill/reap и единая реакция coordinator.
4. C4: typed ProxyObservation и завершение ExactBridgeRuntime.
5. После каждого шага запускать его узкие тесты. При падении не переходить дальше.

## 9. Проверка

Запускать из WSL/Linux, потому что procfs и Unix lifecycle нельзя честно принять только Windows-тестами:

```bash
gofmt -w internal/mihomo internal/sys/procnet internal/singbox cmd/awg-manager
go test -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./internal/mihomonative ./cmd/awg-manager
go test -race -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./cmd/awg-manager
git diff --check -- internal/sys/procnet internal/mihomo internal/singbox internal/mihomonative cmd/awg-manager
```

Также выполнить compile-check non-Linux implementation, не запуская Linux tests на Windows. `npm run check` требуется только если исполнитель вопреки scope изменил frontend; штатно frontend в Gate C не меняется.

## 10. Критерии приёмки

Gate C закрыт только если одновременно доказано:

1. Любой running receipt относится к реально применяемому generation, включая apply, rollback и regenerate.
2. TCP/UDP, IPv4/IPv6 и владелец socket определяются без first-match и fail-open поведения.
3. После kill процесс либо доказанно reaped, либо система остаётся в recovery и не запускает конкурирующий daemon.
4. Bridge proof читается из live NDMS typed observer, а exact mutation не затрагивает соседние/foreign proxies.
5. Все заявленные unit/Linux/race тесты проходят; отсутствие запуска теста явно указано, а не считается успехом.

## 11. Обязательные артефакты результата

- `reports/mihomo/GATE_C_DIFF_2026-09-22.patch` — фактический diff только Gate C;
- `reports/mihomo/MIHOMO_REMEDIATION_GATE_C_RESOLUTION_REPORT_2026-09-22.md` — список изменённых файлов, закрытые acceptance criteria, полный вывод команд и честные ограничения;
- никаких IPK/deploy;
- никаких заявлений «Gate C закрыт», если хотя бы один критерий выше не доказан.

