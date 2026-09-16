# Повторная проверка Revised Remediation Plan

Дата: 2026-09-10  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

**Почти готов к реализации, но требуется ещё одна небольшая обязательная редакция.**

Агент корректно внёс четыре ключевых решения предыдущего ревью: coordinator-owned transaction, secret-free plan, асинхронный `PLAN_STALE` и явные compatibility/egress matrices. Однако последовательность transaction phases пока технически невыполнима, в Xray matrix указан неверный внутренний порт, а механизм пересчёта fingerprint не определён на уровне зависимостей.

После исправления P0 ниже план можно запускать в работу.

## P0 — обязательные исправления плана

### 1. Readiness должна выполняться после активации candidate, но до finalize

Сейчас план одновременно утверждает:

- применить staged configuration к runtime (строка 41);
- выполнить readiness (строка 42);
- только после этого commit components (строки 44, 113).

Это противоречие. Проверить runtime sockets до запуска/перезагрузки candidate невозможно. В существующем Xray transaction API:

- `PrepareCandidate` создаёт snapshot и staging;
- `CommitPrepared` активирует конфигурацию, но snapshot ещё сохраняется;
- `FinalizePrepared` удаляет возможность штатного rollback;
- `RollbackPrepared` может вернуть прежнее состояние после неудачной readiness.

В плане нужно закрепить следующую последовательность:

1. acquire ingress lock;
2. проверить recovery и fingerprint;
3. записать durable journal `staged`;
4. prepare всех component candidates/snapshots;
5. journal `applying` или `candidate_active`;
6. активировать candidates (`CommitPrepared`, dispatcher/Telegram candidate activation), **не удаляя rollback state**;
7. выполнить readiness с deadline;
8. при ошибке — rollback всех уже активированных компонентов в обратном порядке;
9. при успехе — journal `committing`, запретить cancel;
10. выполнить `FinalizePrepared`, зафиксировать topology/fingerprints и архивировать journal.

То есть `CommitPrepared` в текущем Xray API является скорее activation, а настоящая точка невозврата для мастера — `FinalizePrepared`. Либо методы следует переименовать, либо это различие явно описать в плане и тестах.

Для Telegram и dispatcher также нужен rollback-capable candidate contract. Нельзя использовать обычный `tgSvc.UpdateConfig` как prepare: он уже меняет persistent/runtime state. Следует либо использовать/расширить существующий `tgwebproxy.TransactionCoordinator`, либо создать coordinator snapshot и гарантировать восстановление **всей** Telegram config, файлов и worker states при ошибке следующего компонента.

### 2. В Matrix 2 ошибочно указан Xray target `:8081`

Строки 81-82 направляют dispatcher в Xray `:8081`. Фактический проектный контракт использует Xray listen port `9008`:

- `internal/xrayserver/service.go` — default `ListenPort: 9008`;
- `internal/serveringress/types.go` — Xray port example `9008`;
- `internal/cdndispatcher/dispatcher.go` — default target `http://127.0.0.1:9008`;
- существующие coordinator и migration tests также ожидают `9008`.

Матрицу исправить на `127.0.0.1:<DesiredXray.ListenPort>`, по умолчанию `9008`. Не дублировать порт литералом в materialization code.

### 3. Coordinator сам не умеет вычислять wizard fingerprint

`WizardApplyParams` содержит только `ExpectedFingerprint`, но план требует от coordinator «re-evaluate live SHA-256 fingerprint». Сейчас fingerprint реализован в `internal/serverwizard/FingerprintEngine`; `serveringress.Coordinator` этой зависимостью не владеет.

Не следует заставлять `serveringress` импортировать `serverwizard`. Нужен нейтральный callback/interface, например:

```go
type StateFingerprintFunc func(context.Context) (string, error)
```

или предварительно вычисленный coordinator-owned revision, если coordinator действительно владеет всеми измерениями состояния. Callback передаётся в transaction params и вызывается **внутри ingress lock**. Ошибка вычисления fingerprint должна блокировать apply (fail closed), а не превращаться в пустую/частичную сумму.

У существующего `FingerprintEngine.Compute` нет ошибки возврата, и ошибки `os.Stat`/procnet кодируются в hash. План должен явно решить, какие probe errors считаются состоянием, а какие делают атомарную проверку ненадёжной и блокируют применение.

### 4. Transaction DTO должен принадлежать правильному пакету

В строках 27-35 не указано, где объявлены `WizardApplyParams`, `AppliedTelegramConfig` и `AppliedXrayConfig`. Если они находятся в `serverwizard`, импорт из `serveringress` создаст неверное направление зависимости и потенциальный import cycle.

Рекомендуется:

- в `serveringress` объявить нейтральные ingress transaction DTO, не содержащие UI/wizard терминов;
- в `serverwizard` преобразовать `DesiredWizardConfig` в эти DTO;
- readiness/fingerprint передавать как узкие callbacks/interfaces;
- не помещать frontend scenario/profile semantics в coordinator.

## P1 — уточнения, которые следует внести одновременно

### 5. Direct profile необходимо отличить от CDN profiles

Matrix 2 допускает `direct` для обоих transport modes, но всё равно пишет «Dispatcher Target & Path». Для прямого входа нужно явно указать:

- используется ли dispatcher вообще;
- какой адрес/порт публикуется клиенту;
- где завершается TLS;
- какие поля ссылки отличаются от CDN-варианта.

Если текущая реализация direct XHTTP/WS не поддерживается, profile `direct` следует временно убрать из capabilities, а не показывать фиктивную возможность.

### 6. Генерация секретов происходит не буквально внутри coordinator transaction

Строка 9 говорит «inside apply transaction», а строка 50 — внутри `executeApplyJob` непосредственно перед вызовом transaction. Второй вариант приемлем с точки зрения PlanStore, но формулировки нужно согласовать.

Практический контракт:

- генерировать после получения job context, до мутации;
- никогда не сохранять в plan/status/log/error;
- передавать candidate credential только в transaction params;
- публиковать в reveal store только после finalize;
- при ошибке убрать все ссылки на credential.

Обещание «zeroed out» для Go `string` невыполнимо надёжно из-за копий и immutable strings. Для чувствительных промежуточных данных использовать `[]byte` там, где это возможно, очищать буфер best-effort и не заявлять гарантированное стирание памяти.

### 7. Cancel во время ожидания ingress lock

Обычный `sync.Mutex.Lock()` не прерывается через context. План должен хотя бы гарантировать повторную проверку `ctx.Err()` сразу после получения lock и до fingerprint/mutation. Иначе отменённая job может спустя долгое время всё равно начать apply.

### 8. Readiness должна проверять владельца и ожидаемую топологию

Одного факта «порт занят» недостаточно. Probe должен проверять:

- ожидаемый процесс/worker владеет listener;
- bind address соответствует сценарию (`127.0.0.1` для internal backend, внешний bind для public listener);
- dispatcher route соответствует path и target;
- отключённые сценарием public listeners действительно отсутствуют;
- process status относится к только что применённому поколению/config fingerprint.

Особенно важно проверить, что в `cdn_http` порт direct MTProxy не остаётся доступным после смены сценария.

### 9. Recovery test должен покрывать каждую durable phase

`TestWizardRestartDuringTransactionRecoversConsistently` разделить минимум на случаи:

- staged/prepared;
- candidate partially activated;
- readiness in progress;
- committing/finalizing;
- rollback interrupted.

После recovery проверять не только отсутствие journal, но также configs, процессы, listener ownership и отсутствие выданных credentials.

## Финальный критерий запуска

План можно передавать на реализацию после следующих текстовых правок:

1. заменить последовательность `apply -> readiness -> commit` на `prepare -> activate with rollback state -> readiness -> finalize`;
2. заменить Xray `:8081` на `:<DesiredXray.ListenPort>` с default `9008`;
3. определить callback/interface для live fingerprint внутри lock и fail-closed ошибки;
4. разместить transaction DTO в `serveringress`, не создавая зависимость coordinator от wizard;
5. явно описать direct Xray profile или удалить его из advertised capabilities до поддержки.

После этого архитектурных блокеров для начала реализации не останется. IPK и deployment в данный этап по-прежнему не входят.
