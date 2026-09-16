# Повторная проверка Revised Final Remediation Plan

Дата: 2026-09-09  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План существенно улучшен и устраняет два прежних архитектурных блокера: введён отдельный managed-контракт Telegram ingress и описана персистентная migration saga для `keep_legacy`.

**В реализацию запускать можно только после внесения перечисленных ниже уточнений.** Они локальны и не требуют ещё одной смены архитектуры, но без них реализация может терять recovery-артефакты или запускать часть ingress при незавершённом восстановлении.

## Обязательные уточнения

### 1. `ListenPort` в full-state контракте не должен иметь PATCH-семантику

В плане `ManagedIngressConfig` объявлен полным состоянием, но `ListenPort` применяется только при `> 0`. Это снова создаёт двусмысленный zero value.

Нужно выбрать один строгий контракт:

- предпочтительно: отклонять `ListenPort <= 0` с ошибкой и без изменения состояния;
- либо документировать отдельное допустимое значение `0` как «порт отключён» и применять его безусловно.

Сохранять прежний порт при `<= 0` нельзя. Для текущей модели конфигурации безопаснее обязательный положительный порт.

### 2. Rollback Xray не должен удалять snapshot при ошибке

Приведённый в плане вариант `rollbackLocked()` наследует безусловный `_ = os.RemoveAll(txPath)`. Это противоречит требованию сохранить snapshot для последующего recovery.

Требуемое поведение:

```go
if len(rollbackErrs) > 0 {
    return errors.Join(rollbackErrs...)
}
if err := os.RemoveAll(txPath); err != nil {
    return fmt.Errorf("remove completed transaction snapshot: %w", err)
}
return nil
```

Кроме запуска процесса и PID record нужно собирать ошибки чтения/восстановления settings и runtime config: сейчас существующий код их игнорирует. При любой ошибке восстановления каталог транзакции остаётся на диске.

### 3. Ошибка rollback должна возвращаться из coordinator apply

Текущий `Coordinator.rollback(...)` ничего не возвращает. Поэтому `applyLocked()` способен вернуть только исходную ошибку, даже если rollback также сломан.

План должен изменить сигнатуру на `rollback(...) error` и во всех ветках возвращать `errors.Join(originalErr, rollbackErr)`. Одновременно выставлять `recoveryNeeded`, сохранять активный journal и не архивировать его как обычный `failed`, пока восстановление не завершено.

### 4. Migration saga должна фиксировать ошибки каждой записи фазы

Недостаточно перечислить фазы. Каждая смена фазы должна записываться атомарно и проверяться. Нельзя выполнять следующий внешний эффект, если запись предыдущей фазы не удалась.

Recovery должен учитывать падение между внешним действием и записью следующей фазы посредством проверки фактического состояния:

- наличие active/disabled init script;
- checksum/identity файла;
- состояние managed процесса;
- PID-владение ожидаемым listener;
- фактическое содержимое runtime decision.

`InitScriptWasRenamed` описывает исходное состояние и не доказывает, что rename уже завершился.

### 5. Ошибка записи runtime decision требует явного rollback

В перечне тестов этот случай есть, но execution steps после `legacy_started_and_verified` не описывают его полностью. Если atomic write решения не удался, saga обязана:

1. остановить проверенный legacy;
2. вернуть init script в исходное состояние;
3. восстановить managed Xray;
4. восстановить прежний decision;
5. сохранить журнал и объединить все ошибки.

Только после успешной записи decision допустим переход в `decision_committed`.

### 6. Recovery saga должен выполняться раньше общего ingress recovery и под межпроцессным lock

У saga и обычной ingress-транзакции могут остаться артефакты одновременно. В плане нужно зафиксировать порядок:

1. захватить `server-ingress.lock`;
2. восстановить migration saga;
3. затем обработать обычный `server-ingress-transaction.json` либо явно доказать обратный безопасный порядок;
4. освободить lock только после согласования runtime decision и компонентов.

Нельзя вызывать публичный `StartupRecovery()` рекурсивно под уже удерживаемым `c.mu`; нужны внутренние `...Locked` helpers.

### 7. Boot gate должен учитывать recovery самого `tgwebproxy`

В текущем production wiring отдельно проверяется `tgWebProxyService.GetStatus().RecoveryRequired`, но этот статус лишь логируется и не участвует в условии автозапуска. Новый helper проверяет только coordinator gate.

Следовательно, одного `Coordinator.CanAutoStart()` недостаточно. Gate должен агрегировать:

- `Coordinator.IsRecoveryRequired()`;
- `tgwebproxy.Status.RecoveryRequired`;
- при необходимости локальный recovery-state Xray.

Либо `StartIngressIfSafe` должен принимать отдельные recovery gates компонентов. При recovery Telegram proxy нельзя запускать ни TG, ни общий dispatcher. Ошибки `Start()` нельзя игнорировать, как сейчас в goroutine; они должны логироваться и влиять на итоговое boot-состояние.

### 8. Нельзя без аудита менять `Dispatcher.Reconfigure` с PATCH на full replacement

Сейчас `Reconfigure` обновляет непустые поля, то есть имеет PATCH-подобную семантику. Безусловное присваивание только `PublicHostname` делает контракт смешанным и может неожиданно очистить hostname у другого вызывающего кода.

Перед изменением необходимо либо:

- доказать тестами, что все вызовы `Reconfigure` передают полный config;
- предпочтительно добавить явный full-state метод (`ApplyConfig`/`ReplaceConfig`) для coordinator, оставив совместимость `Reconfigure`;
- либо использовать pointer/optional-поля для PATCH API.

Интеграционный тест очистки hostname должен идти через production-метод coordinator.

### 9. Listener probe legacy должен проверять владельца

Проверка «порт открыт» недостаточна: порт может занимать другой процесс. Probe должен подтвердить PID/identity процесса, запущенного конкретным init script, и ожидаемый адрес. Значение `loopback/443` нельзя считать универсальным default без разбора legacy config/init script.

## Что теперь одобрено

- отдельный `ManagedIngressConfig` вместо общего `tgwebproxy.Config`;
- безусловная очистка `PublicHostname` на уровне managed state;
- персистентная saga вместо простой компенсации;
- проверка `pid <= 0` и ошибок PID record;
- обязательная запись committed manifest;
- вынос production boot-решения в тестируемый helper;
- интеграционная проверка фактического состояния dispatcher;
- crash/fault-injection тесты и uncached race validation.

## Итоговая рекомендация агенту

Не переписывать архитектуру плана. Перед кодированием внести девять уточнений выше, особенно пункты 2, 3, 6 и 7. После этого план пригоден для реализации поэтапно:

1. managed Telegram contract и hostname semantics;
2. строгий Xray transaction rollback и manifest/PID ошибки;
3. агрегированный boot gate;
4. migration saga и recovery;
5. fault-injection, race tests и ARM64 build.

IPK и деплой не относятся к этому этапу проверки.
