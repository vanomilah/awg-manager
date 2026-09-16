# First-Run Wizards Plan: Execution-Ready Check

## Вердикт

Транзакционная архитектура плана теперь согласована: полная journal I/O abstraction, rollback до durable point of no return, единственный post-boundary callback и ownership-aware consume описаны правильно.

Перед запуском реализации нужно вернуть в verification checklist три конкретных пункта. После их добавления план считается окончательно одобренным; новый архитектурный review не нужен.

## Обязательные дополнения

### 1. Вернуть тест ошибки consume после commit boundary

Предыдущий review требовал сценарий, где `MarkConsumed` внутри `OnPointOfNoReturn` принудительно возвращает ошибку. В текущем плане описана диагностическая ветка `plan_consume_failed_after_commit_boundary`, но тест этой ветки отсутствует.

Добавить service/coordinator integration test, подтверждающий:

- durable `PhaseCommitting` уже записан;
- callback consume возвращает ошибку;
- rollback компонентов не вызывается;
- coordinator/job переходят в `recovery_required`;
- `Release` не вызывается и план не становится повторно применимым;
- причина диагностики содержит `plan_consume_failed_after_commit_boundary`.

Для детерминированного теста нужен seam/interface вокруг PlanStore либо контролируемое изменение reservation owner непосредственно перед callback.

### 2. Вернуть тест shared-host dual paths

В реализации заявлено, что при общем hostname проверяются оба маршрута, но из текущего списка acceptance tests исчез `SharedHostDualPaths`.

Добавить тест, который требует два успешных probe в одной readiness-проверке:

- Xray path возвращает `X-CDN-Route: xray`;
- `/` возвращает `X-CDN-Route: tgwebproxy`;
- отсутствие или ошибка любого из двух ответов проваливает readiness.

### 3. Один общий helper нормализации Xray path

План размещает `NormalizeXrayPathPrefix` в `internal/serverwizard/service.go`, но не закрепляет его использование при построении dispatcher topology/config. Это допускает разные URL у dispatcher и readiness.

Helper должен находиться в нейтральном пакете без import cycle и использоваться всеми участниками:

- desired/config builder;
- coordinator/dispatcher configuration;
- readiness probe.

Тест с пользовательским path должен сравнивать фактически зарегистрированный dispatcher prefix и URL readiness probe, включая политику завершающего `/`.

## Неблокирующее улучшение

В `failAndRollback` вместо `fmt.Errorf("%w: %v", ErrRecoveryRequired, errors.Join(...))` предпочтительно использовать `errors.Join(ErrRecoveryRequired, origErr, ...)`. Тогда `errors.Is` сохранит не только `ErrRecoveryRequired`, но и типизированные исходные причины.

## Разрешение

После добавления трёх пунктов выше план можно немедленно выполнять. Следующая проверка должна проводиться уже по реализации и свежему walkthrough, а не по ещё одной редакции плана.
