# План стабилизации ядра Mihomo

Дата аудита: 2026-09-14  
Репозиторий: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`  
Проверенный HEAD: `87a563eb`  

## 1. Вердикт

Mihomo уже подключён как маршрутизирующее ядро и как sidecar для нативных прокси, но текущую реализацию нельзя считать отказоустойчивой. Главная проблема не в одном дефекте: генерация, применение конфигурации, жизненный цикл процесса и старый sing-box-код пока не образуют одну транзакционную модель.

Исправления следует выполнять по этапам ниже. Не начинать с косметики UI: сначала исключить потерю правил и blackhole трафика.

## 2. Границы работы

- Не затрагивать CDN, Xray, Telegram Proxy, WDTT и ИИ-помощник, кроме мест, где они непосредственно используют общий routing engine.
- Не переписывать и не удалять посторонние изменения из грязного рабочего дерева.
- Не использовать `--force-reinstall`.
- Не собирать IPK и не выполнять деплой до прохождения локальных проверок этапов 1–5.
- Сохранять два режима Mihomo: primary router и exports-only sidecar.
- Сохранять совместимость standalone-туннелей sing-box, пока они ещё не перенесены на Mihomo.

## 3. Подтверждённые дефекты

### P0. Ошибка чтения конфигурации превращается в пустую маршрутизацию

Файл: `internal/singbox/router/service_mihomo.go:45-49`.

`GenerateMihomoConfig` подавляет любую ошибку `loadRouterConfig()` и продолжает с `NewEmptyConfig()`. Повреждение JSON, ошибка диска или ошибка staging может незаметно удалить пользовательские rules/outbounds из сгенерированного YAML. Это fail-open поведение.

Требуется:

1. Возвращать ошибку с контекстом и не менять действующий `config.yaml`.
2. Различать допустимое отсутствие первого конфига и ошибку чтения/декодирования.
3. Добавить тесты: отсутствующий конфиг; повреждённый active; повреждённый pending; ошибка orchestrator store; действующий YAML остаётся byte-equal.

### P0. Неизвестный outbound молча становится DIRECT-интерфейсом

Файл: `internal/mihomo/config.go:812-850`.

`ensureReferencedProxiesExist` создаёт для любого неизвестного имени прокси типа `direct` с `interface-name`, полученным из этого имени. Опечатка или удалённый прокси не отклоняется, а меняет семантику маршрута и потенциально выпускает трафик напрямую.

Требуется:

1. Удалить общий fallback «неизвестное имя -> direct».
2. Создавать interface-bound direct только из проверенного каталога AWG/system tunnels с явной парой `tag/interface`.
3. Для неизвестных целей rules/groups возвращать структурированную ошибку с именем правила/группы.
4. Проверять циклы групп, отсутствующие providers и RULE-SET до записи файла.
5. Добавить negative tests на опечатку, удалённый tunnel/provider, цикл групп и dangling rule-set.

### P0. Конфигурация применяется неатомарно и без runtime rollback

Файлы: `internal/singbox/router/service_mihomo.go:245-251`, `cmd/awg-manager/dynamic_engine.go:123-205`.

Рабочий `config.yaml` перезаписывается напрямую. Валидация выполняется только когда процесс не запущен; для live reload новый файл не проходит обязательный `mihomo -t` до PUT. При ошибке hot reload предыдущий файл и прежнее runtime-состояние не восстанавливаются.

Требуется единый pipeline:

1. Сгенерировать `config.yaml.candidate` в том же каталоге.
2. `fsync` файла и каталога, права `0644`.
3. Проверить candidate отдельной командой Mihomo.
4. Сохранить last-known-good, атомарно переименовать candidate.
5. Reload/Start и проверить process + требуемые listeners + controller generation.
6. При любой ошибке восстановить last-known-good и реально вернуть runtime к нему.
7. Только после readiness публиковать NDMS bridges и считать операцию успешной.
8. Сериализовать весь цикл тем же transition lock.

Обязательные fault-injection tests: write/rename/fsync failure, invalid candidate, reload HTTP 4xx/timeout, процесс умер после PUT, bridge publication failed, rollback failed.

### P0. Восстановление TUN управляет напрямую sing-box

Файл: `internal/singbox/router/service_lifecycle.go:421-482`.

`healDetachedTun` проверяет и перезапускает `s.deps.Singbox`, хотя остальная readiness-логика уже использует `routingEngineController()`. При выбранном Mihomo этот путь лечит не то ядро.

Требуется:

1. Получать активный engine через `routingEngineController()`.
2. Учитывать поддерживаемый конкретным ядром тип TUN и интерфейс.
3. Не применять sing-box carrier assumptions к Mihomo без отдельного adapter/probe.
4. Добавить matrix tests: sing-box/Mihomo x TPROXY/policy-tun/fakeip-tun x process down/carrier down.

### P1. Reload TUN игнорирует ошибку Stop

Файл: `internal/mihomo/operator.go:101-110`.

`_ = o.Stop()` допускает запуск нового процесса после неудачного завершения старого. Возможны два процесса, занятые порты и ложная readiness.

Требуется: при ошибке Stop прервать переход; подтвердить завершение PID/controller; только затем Start. Добавить тест stop failure и concurrent reload/start.

### P1. Утечки памяти/дескрипторов в процессе Mihomo

Файл: `internal/mihomo/operator.go:204-213`, `wait()`.

Stdout/stderr каждого долгоживущего процесса накапливаются в неограниченных `bytes.Buffer`. Открытый `/tmp/mihomo.log` не закрывается в показанном lifecycle. На роутере с 1–2 ГБ это эксплуатационный дефект.

Требуется:

- bounded ring buffer только для последних N KiB ошибки;
- закрывать файл после `cmd.Wait()` и при ошибке `cmd.Start()`;
- применить ротацию/ограничение размера журнала;
- добавить длительный output test и FD-leak test.

### P1. `cache.db` удаляется при каждом Start/Reload

Файл: `internal/mihomo/operator.go:113-115,200-202`.

Это сбрасывает runtime-выбор selector/url-test и противоречит ожиданию управления группами в реальном времени. Очистка не должна быть частью обычного reload.

Требуется: сохранять кэш штатно; очищать только при несовместимой миграции схемы либо отдельным явным действием пользователя. Добавить persistence test выбора группы после reload/restart.

### P1. Ошибка совместимого sing-box reload подавляется при Mihomo primary

Файл: `internal/singbox/router/service_lifecycle.go:805-810`.

Ошибка orchestrator reload только пишется в журнал. Но sing-box остаётся владельцем части standalone/device-proxy функциональности, поэтому успешный ответ API может оставить её в устаревшем состоянии.

Требуется определить контракт:

- если compatibility-компоненты обязательны — операция завершается ошибкой и откатывается;
- если необязательны — API возвращает `degraded` с конкретными неработающими возможностями, а не success;
- запретить безусловное выключение всего `SlotDeviceProxy` лишь из-за конфликта одного mixed-порта; развести ownership портов или валидировать точечный конфликт.

### P1. Несогласованные имена/режимы readiness

Файл: `internal/singbox/router/service_lifecycle.go:485-575`.

Функции называются `waitForSingbox`/`singboxReady`, а реально проверяют DynamicEngine. Timeout всегда сообщает `sing-box did not come up`, даже когда выбран Mihomo. Определение `tunMode` основано на sing-box routing mode, а socket probe общий и непрозрачный.

Требуется:

1. Ввести `EngineReadiness` adapter с `Name`, `Mode`, `Probe` и диагностикой каждого listener.
2. Для Mihomo проверять PID, controller, TCP REDIRECT 51272, UDP TPROXY 51271 или TUN carrier согласно режиму.
3. Возвращать пользователю имя ядра и точную отсутствующую точку готовности.

### P1. Controller захардкожен и readiness не доказывает принадлежность процессу

Файл: `internal/mihomo/operator.go:125` и startup readiness.

Адрес `127.0.0.1:9090` повторяется как константа. Ответ старого/чужого процесса может быть принят за готовность нового поколения.

Требуется единый controller endpoint из конфигурации, проверка PID/generation или уникального secret, обнаружение занятого порта до старта и тест stale-controller.

### P2. Ошибки источников данных подавляются избирательно

Файл: `internal/singbox/router/service_mihomo.go:26-103`.

Ошибки загрузки subscription/tunnel/AWG slots иногда трактуются как отсутствие данных, иногда приводят к fallback. Это способно сгенерировать частичный конфиг.

Требуется: формализовать `not found` отдельно от I/O/decode errors; запрещать применение частичной конфигурации; логировать выбранный источник каждого набора данных.

### P2. UI и тексты всё ещё смешивают два ядра

Файл: `frontend/src/routes/routing/+page.svelte:464-479` и компоненты `sb-router`.

Mihomo отображается внутри ветки/страницы sing-box, а диалог несохранённых изменений прямо говорит «Правки sing-box». Это затрудняет диагностику состояния и ранее уже приводило к ложным сообщениям.

Требуется после backend-стабилизации:

- нейтральные названия `Маршрутизатор`, `активное ядро`;
- engine-aware draft guard и сообщения;
- FakeIP показывать только для ядра/режима, который реально его использует;
- статус primary и sidecar показывать раздельно;
- удалить или окончательно подключить оставшийся `frontend/src/routes/routing/MihomoTab.svelte`, чтобы не было двух UI-реализаций.

### P2. Непереносимые/неполные тесты Operator

Текущие operator-тесты на Windows зависят от Unix process semantics и helper с `select {}`, который завершается runtime deadlock. Это не доказательство сбоя Linux runtime, но мешает воспроизводимой проверке.

Требуется вынести OS-specific process tests под build tags, инъецировать command/HTTP/filesystem adapters и обязательно прогонять Linux arm64 tests в CI.

## 4. Порядок реализации

### Этап 1 — fail-closed генератор

Исправить P0 чтения и неизвестных ссылок. Ввести `MihomoCompileError` с полями source/resource/rule. Никаких runtime-действий на ошибке компиляции.

Критерий: повреждение любого входного slot не меняет текущий YAML и работающий процесс.

### Этап 2 — транзакционный compile/apply/rollback

Создать отдельный coordinator, не размазывать операции между handler, router service и Operator. Candidate -> validate -> commit -> reload -> readiness -> publish; обратный порядок для rollback.

Критерий: fault-injection suite доказывает сохранение last-known-good во всех точках отказа.

### Этап 3 — lifecycle и ресурсы процесса

Исправить Stop/Start, controller identity, bounded logs, FD close, cache persistence, graceful TERM с ограниченным ожиданием и KILL fallback.

Критерий: 100 последовательных reload/restart без роста FD/RSS, дублей PID и потери selector choice.

### Этап 4 — routing-engine abstraction

Убрать прямые вызовы Singbox из общих heal/readiness/enable путей. Compatibility sing-box оформить отдельной зависимостью, не выдавать его за активное routing core.

Критерий: таблица переходов `off -> sing-box -> Mihomo -> off`, включая reboot и crash каждого компонента, проходит без занятых портов и blackhole.

### Этап 5 — netfilter и реальные потоки

Проверить атомарность установки/удаления TCP REDIRECT 51272 и UDP TPROXY 51271, policy marks, conntrack cleanup, DNS и rollback при failed readiness.

Критерий: TCP+UDP/DNS с policy device видны в Mihomo connections; после отключения или неудачного apply правила полностью удалены; direct policy продолжает работать.

### Этап 6 — UI/API согласованность

Только после стабилизации backend унифицировать статусы, draft semantics, сообщения, вкладки connections/logs и признаки primary/sidecar/degraded.

## 5. Обязательная матрица приёмки

1. Fresh install без нативных ресурсов.
2. Mihomo sidecar при primary sing-box.
3. Mihomo primary TPROXY с TCP, UDP, DNS и Private DNS клиента.
4. Mihomo primary policy-tun, если режим официально поддерживается.
5. Переключение ядер в обе стороны с остановленным и запущенным процессом.
6. Reboot при каждом режиме.
7. Повреждённый config/slot и недоступный outbound/provider.
8. Занятые 9090/51271/51272/1099.
9. Crash Mihomo при живом compatibility sing-box и наоборот.
10. Native subscription/group selection сохраняется после reload/restart.
11. NDMS policy device: соединение показывается у правильного ядра и идёт через выбранную группу.
12. Отмена/параллельные apply запросы не оставляют смешанное состояние.

На роутере до установки снимать backup текущих settings, native store, sing-box slots, Mihomo YAML/cache и netfilter dump. Установка только обычным `opkg install <ipk>`, без `--force-reinstall`. Проверять сначала на одном роутере; второй — только после прохождения smoke suite.

## 6. Проверки, выполненные при аудите

- `go test ./internal/mihomo -run "Test(Generate|Convert|Format|Ensure|Sidecar)" -count=1` — PASS.
- `go test ./internal/mihomonative -count=1` — не выполнен: Windows Go build cache вернул `Access is denied`; это ограничение среды, а не подтверждённый дефект пакета.
- Linux/arm64 compile `go build ./cmd/awg-manager` — PASS в ходе текущего аудита до оформления файла.
- Frontend `npm run check` — 0 errors, 118 warnings; предупреждения требуют отдельной чистки, но сборку не блокируют.
- `git diff --check` — FAIL: лишняя пустая строка в конце `frontend/src/lib/api/clientServers.ts:695`; также множество CRLF/LF warnings.
- IPK не собирался, на роутеры ничего не устанавливалось.

## 7. Что агент должен предоставить после каждого этапа

1. Перечень изменённых файлов и краткий runtime-контракт.
2. Новые regression/fault-injection tests и полный вывод проверок.
3. Отдельный список непроверенных сценариев — не называть этап готовым при частичной проверке.
4. `git diff --check` без ошибок.
5. Обновление этого документа: закрытые пункты, commit SHA, остающиеся риски.

Финальный деплой разрешать только после отдельного code review и подтверждения пользователя.
