# Отчёт о реализации: Этап 3 — Lifecycle и ресурсы процесса Mihomo

**Дата:** 17 сентября 2026 г.  
**Ветка:** `feature/mihomo-ai-proxyrt`  
**Статус:** 100% ВЫПОЛНЕНО (PASS)  
**Ссылка на мастер-план:** `reports/mihomo/MIHOMO_CORE_STABILIZATION_PLAN_2026-09-14.md` (§ 4 "Этап 3 — lifecycle и ресурсы процесса")

---

## 1. Контекст и решённые проблемы (P1)

В рамках Этапа 3 устранены критические архитектурные дефекты жизненного цикла и потребления ресурсов процессом `Mihomo`:

1. **P1. Stop Failure & Graceful Termination:**
   - *Было:* `Operator.StopAndWait()` вызывал `cmd.Process.Kill()` напрямую, посылая `SIGKILL` без предварительного `SIGTERM`. Mihomo не успевал закрыть сокеты, сохранить состояние и освободить интерфейсы TUN. При `Reload()` ошибки `Stop()` игнорировались (`_ = o.Stop()`), что приводило к коллизиям и запуску поверх незавершённого процесса.
   - *Стало:* Реализовано разделение по платформам (`operator_unix.go` и `operator_windows.go`). На Unix/Linux посылается `SIGTERM`, процесс ожидает корректного завершения с настраиваемым таймаутом (`gracefulTimeout`, по умолчанию 3с). Если процесс завис, выполняется эскалация до `SIGKILL`. В `Reload()` при включённом TUN ошибка `Stop()` немедленно прерывает переход и возвращает ошибку.

2. **P1. Утечки памяти и файловых дескрипторов (OOM & FD Leaks):**
   - *Было:* `cmd.Stdout` и `cmd.Stderr` писали в неограниченные `bytes.Buffer`. При долгой работе демона на роутере с 1-2 ГБ ОЗУ это вызывало OOM. Дескриптор открываемого `/tmp/mihomo.log` никогда не закрывался в `wait()` или при ошибке `Start()`, вызывая постоянную утечку FD при перезапусках. Лог рос без ограничений.
   - *Стало:*
     - Создан `BoundedRingBuffer` с фиксированной ёмкостью (64 KiB), гарантирующий точный потолок $O(1)$ по памяти.
     - Создан `RotatingLogWriter` с ротацией по размеру (512 KiB) и хранением не более одного файла бэкапа (`.1`), что ограничивает размер лога на диске роутера максимум 1 MiB.
     - Дескриптор лога гарантированно закрывается в `wait()` через `defer` и при любых сбоях `cmd.Start()`.

3. **P1. Удаление `cache.db` при каждом Start/Reload:**
   - *Было:* `Reload()` и `Start()` безусловно вызывали `os.Remove("cache.db")`, сбрасывая сделанный пользователем в рантайме выбор серверов в selector-группах, результаты latency-тестов URLTest и кэш Fake-IP.
   - *Стало:* Безусловное удаление полностью исключено. `cache.db` сохраняется между перезапусками и перезагрузками. Для явной ручной очистки добавлен административный метод `ResetCache()`.

4. **P1. Controller Identity & Authentication:**
   - *Было:* Адрес `http://127.0.0.1:9090` был жёстко захардкожен. Взаимодействие с внешним контроллером с авторизацией по токену (`secret`) не поддерживалось.
   - *Стало:* Динамическое определение эндпоинта контроллера (чтение из `config.yaml` либо явная установка через `SetController`). При наличии `secret` во все запросы автоматически инжектируется заголовок `Authorization: Bearer <secret>`. Перед HTTP-запросом выполняется проверка владения слушающим сокетом по PID.

---

## 2. Перечень изменённых и созданных файлов

| Файл | Статус | Назначение |
|---|---|---|
| `internal/mihomo/ringbuffer.go` | **NEW** | `BoundedRingBuffer` (64 KiB) и `RotatingLogWriter` (512 KiB ротация, до 1 MiB) |
| `internal/mihomo/operator_unix.go` | **NEW** | Реализация `sendGracefulStop` через `syscall.SIGTERM` для Unix/Linux (`//go:build !windows`) |
| `internal/mihomo/operator_windows.go` | **NEW** | Реализация `sendGracefulStop` через `Process.Kill` для Windows (`//go:build windows`) |
| `internal/mihomo/operator.go` | **MODIFIED** | Интеграция `BoundedRingBuffer`, закрытие дескрипторов, сохранение `cache.db`, graceful shutdown с таймаутом и kill fallback, динамический controller и bearer auth |
| `internal/mihomo/stage3_lifecycle_test.go` | **NEW** | Комплексный набор тестов Stage 3, включая 100-цикловый стресс-тест |

---

## 3. Результаты верификации и тестов

### А. Модульные и стресс-тесты Stage 3 (`go test -v -race ./internal/mihomo -run 'TestStage3_'`)

```
=== RUN   TestStage3_BoundedRingBuffer_MemoryCeiling
--- PASS: TestStage3_BoundedRingBuffer_MemoryCeiling (0.01s)
=== RUN   TestStage3_RotatingLogWriter_SizeLimit
--- PASS: TestStage3_RotatingLogWriter_SizeLimit (0.00s)
=== RUN   TestStage3_LogFileDescriptorCleanup
--- PASS: TestStage3_LogFileDescriptorCleanup (0.44s)
=== RUN   TestStage3_CacheDbPreservation
--- PASS: TestStage3_CacheDbPreservation (0.41s)
=== RUN   TestStage3_GracefulTerminationSequence
--- PASS: TestStage3_GracefulTerminationSequence (1.56s)
=== RUN   TestStage3_ReloadTunStopErrorHandling
--- PASS: TestStage3_ReloadTunStopErrorHandling (0.20s)
=== RUN   TestStage3_ControllerSecretAuthentication
--- PASS: TestStage3_ControllerSecretAuthentication (0.21s)
=== RUN   TestStage3_ConsecutiveRestartStress_100Cycles
--- PASS: TestStage3_ConsecutiveRestartStress_100Cycles (20.56s)
PASS
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	24.435s
```

*Ключевой критерий плана:* 100 последовательных reload/restart циклов успешно пройдены под гоночным детектором:
- Роста файловых дескрипторов (FD) нет ($\Delta \le 5$).
- Дублирующих / зомби PID нет.
- Содержимое `cache.db` сохранено на всех 100 итерациях.

### Б. Полный прогон пакета `internal/mihomo` (`go test -race ./internal/mihomo`)
```
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	38.208s
```
Все тесты (Gate 1, Gate 2, Gate 3, Gate 4, Stage 3) завершились со статусом **PASS**.

### В. Связанные пакеты под гоночным детектором
```
ok  	github.com/hoaxisr/awg-manager/internal/mihomonative	1.119s
ok  	github.com/hoaxisr/awg-manager/internal/singbox/router	8.763s
ok  	github.com/hoaxisr/awg-manager/internal/api	4.425s
```

### Г. Сборка бинарного файла (`go build ./cmd/awg-manager`)
Сборка выполнена чисто (Exit code: 0, 0 ошибок).

### Д. Проверка фронтенда (`npm run check`)
`svelte-check found 0 errors and 118 warnings in 25 files` (0 ошибок).

### Е. Проверка форматирования и git diff (`git diff --check internal/mihomo`)
0 ошибок форматирования / пробелов.

---

## 4. Непроверенные сценарии (Out of Scope / Next Stages)

В соответствии с правилами аудита фиксируются сценарии, относящиеся к последующим этапам:
1. **Этап 4 (Routing-Engine Abstraction):** Полное разведение `healDetachedTun` между sing-box и Mihomo; матрица `sing-box <-> Mihomo` x TPROXY/policy-tun при перезагрузке роутера.
2. **Этап 5 (Netfilter & реальные потоки):** Проверка правил iptables TCP REDIRECT 51272 и UDP TPROXY 51271 на реальном ядре Linux router.

---

## 5. Заключение

Этап 3 полностью реализован и закрыт в строгом соответствии с `MIHOMO_CORE_STABILIZATION_PLAN_2026-09-14.md`. Готовность к переходу на **Этап 4 — routing-engine abstraction**.
