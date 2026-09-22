# Ревью плана исправления Mihomo от 2026-09-21

Проверенный документ:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Основание:

`reports/mihomo/MIHOMO_FLASH38_IMPLEMENTATION_AUDIT_2026-09-21.md`

## Вердикт

План движется в правильном направлении и охватывает большинство найденных дефектов, но **пока не готов к запуску в работу без исправлений**.

В нём есть технически неверная инструкция, неполная модель recovery, пропущенные требования аудита и новое противоречие с generation process receipt. Если выполнить документ буквально, часть кода не соберётся, а часть заявленных гарантий останется недоказанной.

## Обязательные исправления плана

### 1. Исправить сигнатуру `AdvanceLKGPointer`

В плане указано:

```go
c.genStore.AdvanceLKGPointer(genID, nextGen, c.DaemonEpoch())
```

Фактическая сигнатура в `internal/mihomo/generation_store.go`:

```go
AdvanceLKGPointer(genID string, genNum uint64, appliedRec AppliedGenerationRecord, epoch string) error
```

Должно быть:

```go
c.genStore.AdvanceLKGPointer(genID, nextGen, rec, c.DaemonEpoch())
```

Но простого добавления аргумента недостаточно: recovery должен использовать единый durable final-commit protocol, а не копировать его частично.

### 2. Gate B должен переиспользовать единый commit-протокол

В основном apply уже существует `executeFinalCommitLocked`, который:

1. требует durable `StateCommitIntent`;
2. пишет `verified-active.json`;
3. продвигает LKG pointer;
4. перечитывает и сверяет обе записи;
5. фиксирует `StateCommitted`;
6. выполняет terminal cleanup.

План предлагает вручную повторить лишь часть этих шагов в `Reconcile`. Это снова создаст две расходящиеся реализации commit.

Нужно добавить отдельный общий helper, пригодный и для apply, и для recovery, например:

```text
finalizeVerifiedGenerationLocked(...)
```

Он обязан:

- работать только при наличии durable recovery/transaction intent;
- записывать verified-active;
- продвигать LKG pointer, когда это требуется сценарием;
- перечитывать и проверять полное равенство record и pointer;
- записывать terminal state;
- только затем удалять marker/journal;
- при любой ошибке вызывать не только `setState(StateRecoveryRequired)`, но и durable `writeRecoveryMarkerLocked(...)`;
- считать ошибку удаления marker/journal ошибкой recovery, а не успехом.

Для `rollback_to_lkg` LKG pointer уже указывает на целевое поколение: его не нужно бессмысленно продвигать заново, но pointer и bundle необходимо перечитать и сверить после runtime recovery.

### 3. Добавить recovery journal/state machine

План говорит «проверять ошибки», но этого недостаточно при отключении питания между шагами.

Для обоих административных действий нужны durable состояния, например:

```text
recovery_intent
store_restored
config_promoted
runtime_verified
verified_active_written
lkg_verified/advanced
recovery_committed
```

На старте daemon незавершённое административное recovery должно продолжаться или оставаться fail-closed, а не начинаться вслепую заново.

Failpoint-тесты должны проверять crash/restart между каждым соседним состоянием, а не только возврат ошибки в одном процессе.

### 4. Исправить generation в process receipt до добавления новой проверки

План предлагает внутри текущей daemon epoch сверять `receipt.Generation`. Это правильно как требование, но текущий код формирует receipt неверно:

- `restartControlledLocked` вычисляет `currentGen` из старого `c.appliedRecord`;
- новый `rec` создаётся лишь после рестарта;
- `ProcessReceipt.AppliedGeneration` в новом record поэтому содержит предыдущее поколение.

Если просто добавить предложенную проверку generation, каждый новый apply будет считаться недействительным.

Нужно:

- передавать target generation в `restartControlledLocked` явно;
- записывать этот target generation в `CaptureIdentity` и `ProcessReceipt.AppliedGeneration`;
- после commit проверять:

```text
receipt.AppliedGeneration == appliedRecord.Generation
receipt.RuntimeProcessIdentity.Generation == appliedRecord.Generation
```

- добавить тест перехода `generation N -> N+1` и проверку после рестарта daemon.

### 5. Gate A не должен предлагать слепой `git checkout dev/null`

`dev/null` — tracked-файл размером около 25 МБ. Его происхождение и назначение сначала нужно выяснить по Git history и содержимому базового blob. Слепое восстановление большого подозрительного файла — не «чистка дерева».

План должен требовать:

1. определить commit, добавивший `dev/null`;
2. установить, является ли это случайным артефактом перенаправления;
3. отдельно решить: корректно удалить его отдельным коммитом или восстановить;
4. не использовать `git checkout`/reset поверх пользовательских изменений без отдельного разрешения.

Также Gate A обязан явно зафиксировать судьбу dirty `wdtt` и `qwdtt`, не изменяя их автоматически.

### 6. Gate C: network-aware API должен быть новым API, а не скрытой сменой сигнатуры

Нужно явно описать миграцию всех вызовов и тестов. Предпочтительнее:

```go
FindListeningProcess(procDir, network, addr string, port int)
```

или typed enum для network/family.

Для UDP состояние `07` нужно называть `UDP_UNCONN/bound`, а не `TCP_CLOSE`. Тесты должны включать:

- TCP и UDP на одном порту с разными PID;
- UDP-only listener;
- IPv4 и IPv6;
- wildcard bind и конкретный address;
- отсутствующую/нечитаемую `/proc/net/udp*` таблицу;
- неизвестное значение network должно fail closed.

### 7. `StopAndWait`: отдельный timeout не должен обещать невозможное

После истечения исходного context нельзя гарантировать reap без ограничения. План должен задать точный контракт:

- после Kill ждать `done` в отдельном internal timeout;
- при успехе вернуть исходную cancellation error либо специальный результат, явно сообщающий, что процесс убит и reaped;
- при истечении reap timeout вернуть `ErrProcessNotReaped`;
- coordinator при `ErrProcessNotReaped` обязан оставаться `recovery_required` и не запускать следующий процесс на тех же сокетах.

### 8. Gate D должен покрывать все mutation endpoints, а не один refresh

Один API-тест для refresh не доказывает Global Degraded Gate.

Нужна табличная матрица всех mutating HTTP endpoints и AI/remediation actions:

- create/update/delete proxy;
- create/update/delete/refresh subscription;
- group/rule mutations;
- engine/apply/reload actions;
- bridge allocation/reconciliation;
- AI-confirmed mutation actions.

Для каждого сценария проверить:

- HTTP 503 / `RECOVERY_REQUIRED`;
- mutate callback не вызван;
- store digest не изменился;
- runtime/controller запрос не отправлен;
- bridge side effects отсутствуют.

Refresh следует проводить через общий controller client/operator API. Простая проверка `CheckMutationAllowed` перед прямым PUT оставляет TOCTOU между проверкой и side effect.

### 9. Gate E не реализует обратимую парковку DeviceProxy

План заменяет `bytes.Contains` typed parsing, но пропускает главный lifecycle-дефект:

- при переходе на Mihomo конфликтующий DeviceProxy выключается;
- при возврате на sing-box он не восстанавливается;
- нельзя отличить автоматически припаркованный slot от вручную выключенного.

Нужно добавить persisted record минимум с:

```text
slot
previous_enabled
reason
owner
parked_at
config_digest
```

При возврате восстанавливать только тот slot, который был выключен этим механизмом и чей config digest/ownership не изменился пользователем.

Обязательные тесты:

- `sing-box -> mihomo -> sing-box`;
- повторный idempotent reconcile;
- daemon restart в припаркованном состоянии;
- пользователь вручную выключил slot до переключения;
- пользователь изменил slot после парковки;
- malformed/missing DeviceProxy config должен fail closed без потери состояния.

### 10. Bridge proof описан неполно

Проверить только `obs.OwnerUUID == ref.OwnerUUID` недостаточно.

План должен включать:

- строгую политику для существующего bridge с пустым owner;
- проверку kernel interface, proxy interface, listen port и digest;
- проверку usable/up state, если runtime API это позволяет;
- postcondition после create и withdraw;
- запрет перезаписи foreign/unknown bridge без отдельной migration procedure.

### 11. Redaction: regex не должен быть единственной границей безопасности

Расширение regex полезно, но evidence export безопаснее строить как структурированный allowlist. В план нужно добавить:

- экспортировать только явно разрешённые поля;
- arbitrary error strings пропускать через redaction как вторую линию защиты;
- не выводить query values, headers, raw YAML/JSON и controller URL credentials;
- тестировать Unicode, multiline, JSON, YAML, env, URL, Bearer, base64-like tokens;
- negative tests: обычные UUID объектов, не являющиеся credentials, не должны бессмысленно уничтожаться, если они нужны для диагностики.

### 12. Верификация неполна

В конец плана нужно добавить обязательные команды и условия:

```text
go test -race ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api
go test -count=1 ./internal/sys/procnet
go test -count=1 ./cmd/awg-manager
git diff --check
npm run check
```

Отдельно запускать `internal/mihomo`:

1. с `mihomo` в PATH;
2. без `mihomo` в PATH;
3. с явно injected fake verifier;
4. Linux integration tests с реальными TCP/UDP sockets.

Нужен end-to-end recovery сценарий:

```text
desired mutation
-> candidate publication
-> runtime restart
-> simulated crash на каждом шаге
-> daemon restart
-> recovery action
-> config/store/process/listener/bridge equality
-> marker removed только после полной верификации
```

## Рекомендуемый порядок выполнения

1. **Gate A1:** детерминированная verifier injection и стабильные тесты.
2. **Gate A2:** установить происхождение `dev/null`, зафиксировать границы dirty tree; исправить whitespace.
3. **Gate B1:** спроектировать единый recovery journal и общий final-commit helper.
4. **Gate B2:** исправить `regenerate_from_desired` и `rollback_to_lkg` с crash failpoints.
5. **Gate C1:** target generation/process receipt.
6. **Gate C2:** TCP/UDP ownership proof и fail-closed cmdline.
7. **Gate C3:** Stop/Kill/Reap contract и bridge proof.
8. **Gate D:** единый mutation boundary и controller client.
9. **Gate E1:** обратимая парковка compatibility slots.
10. **Gate E2:** structured evidence allowlist и redaction.
11. Полная unit/race/frontend проверка.
12. Только после code-review — router smoke test; IPK/deploy не входит в автоматическое выполнение этого плана.

## Что можно оставить из исходного плана

Без изменения можно сохранить:

- общий порядок Gate A → B → C → D → E;
- запреты на `--force-reinstall` и автоматический `--cleanup`;
- сохранение 1099, Wireguard2 и telemt:8443;
- требование fail-closed cmdline;
- необходимость network-aware procfs verification;
- перенос subscription refresh на configured controller client;
- typed parsing DeviceProxy config;
- базовые проверки `git diff --check`, backend tests и frontend check.

## Решение

Перед началом кодинга агент должен сначала обновить `implementation_plan.md` по пунктам 1–12 выше. Выполнять текущую версию буквально **не рекомендуется**.
