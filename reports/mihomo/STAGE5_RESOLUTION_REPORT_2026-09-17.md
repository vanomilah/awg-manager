# Отчет о реализации Этапа 5: Netfilter и реальные потоки трафика (Mihomo Core Stabilization)

Дата: 2026-09-17  
Ветка: `feature/mihomo-ai-proxyrt`  
Статус: **ВЫПОЛНЕН (100% тестов пройдено)**  

---

## 1. Контекст и цели этапа

В соответствии с § 4 («Порядок реализации — Этап 5») и § 3, 5 документа `reports/mihomo/MIHOMO_CORE_STABILIZATION_PLAN_2026-09-14.md`:
1. **Атомарность правил перехвата:**
   - Проверка и закрепление правил netfilter: UDP TPROXY на порт `51271` (`--tproxy-mark 0x1/0x1`) в таблице `mangle` (цепочка `AWGM-TPROXY`) и TCP REDIRECT на порт `51272` в таблице `nat` (цепочка `AWGM-REDIRECT`).
   - Исключение зацикливания пакетов: гарантированный `RETURN` для трафика с меткой `MihomoRoutingMark` (666) и `Fwmark` (0x1) в output-цепочках `AWGM-OUTPUT` и `AWGM-OUTPUT-UDP`.
2. **Атомарный откат при сбое готовности (Fail-Closed Rollback):**
   - При сбое readiness probe (тайм-аут `waitForSingbox`) или сбое `IPTables.Install` при выбранном Mihomo ядро Mihomo штатно останавливается, предотвращая зависание процессов-зомби и занятых портов.
   - Частично применённые цепочки и правила iptables гарантированно сносятся через `IPTables.Uninstall`.
3. **Синхронизация TUN-режимов:**
   - В `policytun_enable.go` и `fakeip_enable.go` добавлен синхронный запуск/перезагрузка ядра Mihomo перед переходом к ожиданиям готовности (`waitForSingbox`), устраняя гонку асинхронных уведомлений шины.
4. **Сброс conntrack и MTK PPE Hardware Offload:**
   - Проверена корректность генерации скрипта `ctCleanScript()`: гарантированный сброс таблицы аппаратного ускорения (`/proc/sys/net/hwnat/ppe_flush`) и точечная эвакуация зависших UDP-потоков через `/opt/sbin/conntrack -D` по метке политики.
5. **Защита DNS и обход NDMS-петель:**
   - Генерация правил `AWGM-DNS-RESCUE` в `nat PREROUTING` с наивысшим приоритетом (`-I PREROUTING 1`) предотвращает попадание DNS в петлю NDMS `_NDM_DNS_FLT_REDIR`.

---

## 2. Реализованные изменения в кодовой базе

1. **`internal/singbox/router/policytun_enable.go`:**
   - Добавлен блок синхронного старта/перезагрузки Mihomo (`engine.Reload()`) при `mihomoPrimary == true` перед `waitForSingbox`.
   - В случае ошибки тайм-аута `waitForSingbox` вызывается `engine.Stop()`.
2. **`internal/singbox/router/fakeip_enable.go`:**
   - Аналогично добавлен синхронный старт/перезагрузка Mihomo при `mihomoPrimary == true`.
   - При тайм-ауте готовности вызывается `engine.Stop()`.
3. **`internal/singbox/router/service_lifecycle.go`:**
   - В `Enable()` при ошибке `waitForSingbox` для Mihomo вызывается `engine.Stop()`.
   - При ошибке `IPTables.Install` вызывается `IPTables.Uninstall(ctx)` и `engine.Stop()`, гарантируя отсутствие частичных netfilter-правил и остановку ядра.
4. **`internal/singbox/router/stage5_netfilter_test.go`:**
   - Создан полный комплект из 7 регрессионных тестов под `-race`:
     - `TestStage5_NetfilterRules_TProxyAndRedirectPorts`: проверка генерации TPROXY 51271 и REDIRECT 51272.
     - `TestStage5_OutputChain_MihomoMarkBypass`: проверка байпаса метки 666 в `AWGM-OUTPUT` и `AWGM-OUTPUT-UDP`.
     - `TestStage5_RollbackOnFailedReadiness_CleansNetfilterAndStopsEngine`: проверка отката и остановки Mihomo при сбое готовности.
     - `TestStage5_ConntrackAndPPEFlush_ScriptIntegrity`: проверка синтаксиса и путей conntrack / PPE flush.
     - `TestStage5_DNSRescueAndCloudOutput`: проверка DNS rescue и Keenetic Cloud редиректа.
     - `TestStage5_TeardownCleanliness_ZeroLeftovers`: проверка полной очистки всех цепочек при Uninstall.
     - `TestStage5_PolicyTun_MihomoSynchronousReload`: проверка синхронной готовности Mihomo в policy-tun.

---

## 3. Результаты проверок

- **Stage 5 Suite:**
  ```bash
  go test -v -race ./internal/singbox/router -run 'TestStage5_'
  === RUN   TestStage5_NetfilterRules_TProxyAndRedirectPorts
  --- PASS: TestStage5_NetfilterRules_TProxyAndRedirectPorts (0.00s)
  === RUN   TestStage5_OutputChain_MihomoMarkBypass
  --- PASS: TestStage5_OutputChain_MihomoMarkBypass (0.00s)
  === RUN   TestStage5_RollbackOnFailedReadiness_CleansNetfilterAndStopsEngine
  --- PASS: TestStage5_RollbackOnFailedReadiness_CleansNetfilterAndStopsEngine (0.11s)
  === RUN   TestStage5_ConntrackAndPPEFlush_ScriptIntegrity
  --- PASS: TestStage5_ConntrackAndPPEFlush_ScriptIntegrity (0.00s)
  === RUN   TestStage5_DNSRescueAndCloudOutput
  --- PASS: TestStage5_DNSRescueAndCloudOutput (0.00s)
  === RUN   TestStage5_TeardownCleanliness_ZeroLeftovers
  --- PASS: TestStage5_TeardownCleanliness_ZeroLeftovers (0.01s)
  === RUN   TestStage5_PolicyTun_MihomoSynchronousReload
  --- PASS: TestStage5_PolicyTun_MihomoSynchronousReload (0.00s)
  PASS
  ok  	github.com/hoaxisr/awg-manager/internal/singbox/router	1.157s
  ```
- **Пакет `singbox/router` целиком:** `PASS` (6.953s).
- **Сквозной multi-package test с Race Detector:**
  - `internal/mihomo`: `PASS` (37.975s).
  - `internal/mihomonative`: `PASS` (cached).
  - `internal/singbox/router`: `PASS` (9.774s).
- **Компиляция бинарника:** `go build ./cmd/awg-manager` — `PASS` (0 ошибок).
- **Проверка форматирования:** `git diff --check` — `PASS` (0 ошибок).

---

## 4. Непроверенные сценарии и ограничения этапа

1. Живой деплой IPK на роутер Keenetic (будет выполнен после утверждения Stage 6 и согласования с пользователем).
2. Поведение при экстремальной нагрузке conntrack (более 65k параллельных UDP-потоков на mips-архитектуре).

---

## 5. Заключение

Этап 5 полностью завершён. Кодовая база ядра, абстракции и netfilter полностью стабилизирована и готова к переходу к **Этапу 6: UI/API согласованность** (унификация статусов, нейтральные названия, удаление устаревших диалогов).
