# Проверка финального remediation plan First-Run Wizards

## Вердикт

План закрывает предыдущий архитектурный блокер и **может быть запущен в реализацию после внесения трех точечных уточнений ниже**. Повторное проектирование не требуется.

Статус: **CONDITIONALLY APPROVED — добавить три уточнения и выполнять**.

## Обязательные уточнения

### 1. Component transaction должна хранить old и new state

Для Telegram сейчас указан только `prev_config.json`, хотя интерфейс имеет `PrepareCandidate(txID, candidate)` и затем `CommitPrepared(txID)` без передачи candidate. Следовательно, новый candidate обязан быть сохранен при prepare.

Для каждого компонента manifest должен содержать или однозначно ссылаться на:

- полный предыдущий config;
- полный candidate config;
- previous running/enabled state;
- candidate running/enabled state;
- transaction state: `prepared`, `active`, `finalized` или `rolled_back`;
- checksums файлов;
- schema version и tx ID.

Права каталога `0700`, файлов `0600`, запись atomic + fsync. Telegram secret допустимо хранить только в private component transaction storage, но не в coordinator journal или PlanStore.

`CommitPrepared(txID)` читает durable candidate. Поэтому crash после prepare не зависит от памяти процесса, а recovery может однозначно выполнить rollback/finalize.

### 2. Зафиксировать точную host-aware таблицу dispatcher

Фразы «Host routes to corresponding target» недостаточно. Реализовать и протестировать явный порядок:

```text
Host == XrayPublicHost && path matches XrayPathPrefix -> Xray
Host == TgPublicHost   && path is a Telegram endpoint -> Telegram
shared host            && path matches XrayPathPrefix -> Xray
shared host            && path is a Telegram endpoint -> Telegram
unknown host or unsupported path                     -> 404
```

Дополнительно:

- сравнивать hostname без регистра и без входного port;
- корректно обрабатывать IPv6 Host parsing;
- не маршрутизировать Xray path на чужом Telegram-only host;
- не использовать Telegram как catch-all для неизвестного Host;
- запретить пересекающиеся/неоднозначные route definitions на prepare;
- readiness должна выполнить HTTP probes с правильными Host + path, а не только проверить socket.

### 3. Вернуть в финальный план явный immutable Desired contract

В этой редакции упоминаются нормализованные `Path` и `PublicPort`, но пропала явная модификация `DesiredWizardConfig`, из-за отсутствия которой исходная реализация и потеряла пользовательские параметры.

Добавить в план:

```go
type DesiredWizardConfig struct {
    // existing normalized fields
    Path           string
    PublicPort     int
    DispatcherPort int
    ResolvedEgress ResolvedEgress
}
```

Точные поля могут быть плоскими, но immutable Desired обязан содержать все данные, влияющие на runtime, dispatcher и ссылки. `executeApplyJob` не имеет права подставлять собственные `/cdn-bridge/`, `443`, `9009`, `nwg1` или домены.

Также включить в реализацию из первоначального аудита:

- `BuildDesiredConfig` выполняет строгую profile/mode validation и typed egress resolution;
- preflight принимает уже построенный Desired;
- PlanStore сохраняет тот же Desired;
- Apply материализует ровно сохраненный Desired;
- public links строятся из того же candidate.

## Неблокирующие уточнения при реализации

- Добавить `PhaseRollingBack` в enum и checksum validation, раз он используется recovery state machine.
- Negative readiness должна брать фактические адреса и порты из полной topology, а не жесткие `8443/8085/9009`, если пользовательские порты разрешены.
- Для `PhaseCommitting` применяется только roll-forward; до него — только reverse rollback.
- Ошибка `OnPhaseChange` должна приводить к rollback до записи durable `PhaseCommitting`.
- Полный frontend/API контракт также должен получить `path`, `public_port`, `dispatcher_port` либо явно скрыть неизменяемые параметры от UI.
- После реализации тестировать обновление одного сервера при уже работающем втором с разными hostnames и custom ports.

## Разрешение

После добавления трех обязательных уточнений план получает статус:

**APPROVED FOR IMPLEMENTATION.**

Сборка IPK и deployment по-прежнему не входят в эту задачу без отдельного указания пользователя.
