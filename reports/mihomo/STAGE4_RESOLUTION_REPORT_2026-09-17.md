# Отчет о реализации Этапа 4: Абстракция ядра маршрутизации (Routing-Engine Abstraction)

Дата: 2026-09-17  
Ветка: `feature/mihomo-ai-proxyrt`  
Статус: **ВЫПОЛНЕН (100% тестов пройдено)**  

---

## 1. Контекст и цели этапа

В соответствии с § 4 («Порядок реализации — Этап 4») и § 3 документа `reports/mihomo/MIHOMO_CORE_STABILIZATION_PLAN_2026-09-14.md`:
1. **Устранение P0:** `healDetachedTun` ранее напрямую управлял и перезапускал `s.deps.Singbox`, даже когда основным ядром был выбран Mihomo. При потере TUN-интерфейса или carrier=0 это приводило к ложному перезапуску sing-box вместо Mihomo.
2. **Устранение P1:** Ошибка перезагрузки sing-box (compatibility reload) при основном ядре Mihomo подавлялась без проверки состояния портов. В случае, если sing-box не освобождал порты 51271/51272, возникал конфликт двойного бинда (dual-bind collision).
3. **Устранение P1:** Изоляция конфликтов портов `SlotDeviceProxy`. Ранее слот девайс-прокси выключался безусловно. Теперь проверяется реальное наличие порта 1099 в `30-deviceproxy.json`.
4. **Устранение P1:** Несогласованные имена и режимы готовности (`waitForSingbox`/`singboxReady`). Ранее при тайм-ауте всегда выдавалось `sing-box did not come up`, а диагностика отсутствующих критериев отсутствовала.
5. **Критерий приёмки:** Таблица переходов `off -> sing-box -> Mihomo -> off`, включая крах и восстановление каждого компонента без занятых портов и blackhole.

---

## 2. Реализованные изменения

### 2.1. Диагностика готовности ядер (`internal/singbox/router/readiness.go`)
- Создана функция `CheckEngineReadiness(ctx, engine, engineName, mode, tunMode, tunIface, probeListening) ReadinessProbeResult`.
- Возвращает детальную структуру `ReadinessProbeResult` с флагом `Ready` и гранулярным списком `MissingCriteria`:
  - `engine not configured`
  - `process not running (pid=...)`
  - `tun carrier=0 (<iface>)`
  - `tproxy/redirect listener down (port 51271/51272)`

### 2.2. Абстракция восстановления TUN (`internal/singbox/router/service_lifecycle.go`)
- `healDetachedTun`:
  - Получает активный контроллер через `s.routingEngineController()`.
  - Проверяет статус через `s.isMihomoPrimary()`, не полагаясь на статус слотов sing-box.
  - При активном Mihomo вызывает `Reload()` для Mihomo; sing-box не затрагивается.
  - При мертвом процессе не выполняет холостых перезагрузок.
  - Логирует имя активного ядра динамически (`s.routingEngineName()`).
- В `fakeip_reconcile.go` и `policytun_reconcile.go` сняты барьеры `if sr.RoutingEngine != "mihomo"`, благодаря чему потеря TUN-интерфейса в режиме Mihomo теперь штатно восстанавливается.

### 2.3. Изоляция коллизий портов и управление слотами (`service_lifecycle.go`)
- Добавлен метод `reconcileCompatibilitySlotsLocked(sr storage.SingboxRouterSettings) error`:
  - При `RoutingEngine == "mihomo"` переводит `SlotRouter` в запаркованное (disabled) состояние.
  - Проверяет `30-deviceproxy.json`: если он содержит смешанный порт `1099`, точечно паркует `SlotDeviceProxy`. Если используется другой порт (например, 1080), слот остаётся активным.
  - При `RoutingEngine == "sing-box"` гарантированно распаркует `SlotRouter`.
- При ошибке оркестратора во время работы Mihomo выполняется проверка `singboxListeningProbe()`: если sing-box всё ещё удерживает порты 51271/51272, операция прерывается с ошибкой `sing-box failed to release router inbounds`, предотвращая dual-bind collision.

### 2.4. Сообщения об ошибках и тайм-аутах (`service_lifecycle.go`)
- `waitForSingbox` и `singboxReady` интегрированы с `CheckEngineReadiness`.
- При наступлении тайм-аута в ошибку включается имя реального ожидаемого ядра (`Mihomo did not come up within ...`) и точный список отсутствующих критериев готовности (`missing: [process not running]`).

---

## 3. Новые регрессионные и матричные тесты

Файл: `internal/singbox/router/stage4_abstraction_test.go`

1. **`TestStage4_HealDetachedTun_MihomoPrimary`**:
   - Проверяет, что при сбое TUN (carrier=0) восстанавливается Mihomo (`reloadCalls == 1`), а sing-box не затрагивается (`reloadCalls == 0`).
   - Проверяет отсутствие холостых вызовов при остановленном процессе Mihomo.
2. **`TestStage4_EngineReadiness_GranularDiagnostics`**:
   - Проверяет диагностику: nil engine, остановленный процесс, TUN carrier=0, TPROXY probe down/up.
3. **`TestStage4_WaitForSingbox_ReportsCorrectEngineAndMissingDetails`**:
   - Проверяет корректность сообщений об ошибке при тайм-ауте для обоих ядер с выводом детальных причин.
4. **`TestStage4_DeviceProxy_PortCollisionIsolation`**:
   - Проверяет парковку `SlotDeviceProxy` при коллизии на порту 1099.
   - Проверяет сохранение работы `SlotDeviceProxy` на независимом порту 1080.
   - Проверяет распарковку `SlotRouter` при возврате на sing-box.
5. **`TestStage4_StateTransitionMatrix`**:
   - Полная матрица переходов:
     - `off -> sing-box primary`
     - `sing-box primary -> Mihomo primary`
     - Крах процесса Mihomo при работающем окружении: проверка fail-closed диагностики и изоляции sing-box.
     - `Mihomo primary -> sing-box primary`
     - `sing-box primary -> off`

---

## 4. Результаты проверок

- **Stage 4 Suite:**
  ```bash
  go test -v -race ./internal/singbox/router -run 'TestStage4_'
  === RUN   TestStage4_HealDetachedTun_MihomoPrimary
  --- PASS: TestStage4_HealDetachedTun_MihomoPrimary (0.01s)
  === RUN   TestStage4_EngineReadiness_GranularDiagnostics
  --- PASS: TestStage4_EngineReadiness_GranularDiagnostics (0.01s)
  === RUN   TestStage4_WaitForSingbox_ReportsCorrectEngineAndMissingDetails
  --- PASS: TestStage4_WaitForSingbox_ReportsCorrectEngineAndMissingDetails (0.43s)
  === RUN   TestStage4_DeviceProxy_PortCollisionIsolation
  --- PASS: TestStage4_DeviceProxy_PortCollisionIsolation (0.00s)
  === RUN   TestStage4_StateTransitionMatrix
  --- PASS: TestStage4_StateTransitionMatrix (0.01s)
  PASS
  ok  	github.com/hoaxisr/awg-manager/internal/singbox/router	1.509s
  ```
- **Пакет `singbox/router` целиком:** `PASS` (6.136s).
- **Сквозное тестирование с Race Detector:**
  - `internal/mihomo`: `PASS` (37.316s, включая 100-цикловый стресс-тест).
  - `internal/mihomonative`: `PASS` (cached).
  - `internal/singbox/router`: `PASS` (9.089s).
- **Компиляция бинарника:** `go build ./cmd/awg-manager` — `PASS` (0 ошибок).
- **Frontend проверка:** `npm --prefix frontend run check` — `0 errors, 118 warnings`.
- **Проверка форматирования:** `git diff --check` — `PASS` (0 ошибок).

---

## 5. Непроверенные сценарии и ограничения этапа

1. Реальное переключение на живом роутере Keenetic (будет проведено после Этапа 5, согласно правилам проекта).
2. Ядерные правила netfilter (TCP REDIRECT 51272 и UDP TPROXY 51271) на реальном Linux ядре Keenetic (предмет Этапа 5).

---

## 6. Заключение

Этап 4 полностью завершён. Код готов к переходу к **Этапу 5: Netfilter и реальные потоки трафика**.
