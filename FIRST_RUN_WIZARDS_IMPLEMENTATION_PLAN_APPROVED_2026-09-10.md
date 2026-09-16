# Final approval: First-Run Setup Wizards implementation plan

Проверен актуальный `implementation_plan.md` от 2026-09-10 11:41.

## Вердикт

**APPROVED FOR IMPLEMENTATION.**

План устраняет все блокирующие недостатки предыдущей реализации и включает последние обязательные уточнения:

- полные durable old/candidate manifests для Xray, Telegram и dispatcher;
- восстановление running state компонентов;
- строгую host-aware маршрутизацию dispatcher;
- независимые состояния Xray и Telegram в `IngressTopology`;
- immutable `DesiredWizardConfig` с path/public/dispatcher ports и resolved egress;
- pre-commit построение клиентских ссылок и реквизитов;
- reverse rollback до point of no return;
- roll-forward recovery начиная с `PhaseCommitting`;
- синхронизированную границу отмены job;
- scenario-aware lifecycle Telegram workers;
- миграцию legacy Telegram config;
- topology-aware readiness и разделенные уровни тестирования.

## Детали, которые нужно соблюсти при кодировании

Эти пункты не блокируют запуск плана, но являются частью приемки:

1. Helper нормализации Host должен принимать как `example.com`, так и `example.com:443`, `[IPv6]` и `[IPv6]:port`. `net.SplitHostPort` нельзя использовать без fallback для hostname без порта.
2. Component manifest с Telegram secret хранится только в private transaction directory с `0700/0600`, никогда не попадает в coordinator journal, PlanStore, API или лог.
3. `PhaseRollingBack` необходимо добавить в enum, checksum/canonical journal и допустимые значения parser.
4. При `PhaseCommitted` recovery только завершает idempotent finalize/archive и никогда не откатывает уже принятую topology.
5. Negative readiness использует реальные адреса и пользовательские порты из `DesiredTopology`, а не константы.
6. Для distinct CDN hosts запрос на Xray path с Telegram-only Host и запрос Telegram endpoint с Xray-only Host возвращают 404.
7. `BuildShareLinks` и `BuildTgLinks` выполняются до первой runtime mutation; после point of no return reveal payload уже полностью сформирован.
8. Existing server state другого типа должен сохраняться побитово/семантически неизменным при применении мастера выбранного сервера.

## Граница задачи

Одобрение распространяется на реализацию и локальную проверку. Сборка IPK и развертывание на роутеры не разрешены этим планом и требуют отдельной команды пользователя.
