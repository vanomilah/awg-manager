# Final Approval — First-Run Setup Wizards Plan

Дата: 2026-09-10  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

**План одобрен и может быть запущен в реализацию.**

Последняя редакция закрывает все архитектурные блокеры предыдущих ревью:

- coordinator-owned transaction удерживает единственную ingress-блокировку;
- candidate активируется с сохранённым rollback state;
- readiness выполняется до finalize;
- fingerprint пересчитывается fail-closed под transaction lock;
- Xray использует фактический внутренний порт 9008;
- wizard plan не хранит secret/UUID;
- Xray runtime и client exports перерабатываются для XHTTP и WS;
- предусмотрены direct/SOCKS/interface egress;
- основной `tgwebproxy` очищается от встроенного `ya.ru` с сохранением legacy-конфигураций;
- async `PLAN_STALE`, cancellation boundary и recovery phases определены явно;
- IPK и deployment исключены без отдельного запроса пользователя.

## Обязательные implementation notes

Это не требует ещё одной редакции архитектуры, но должно проверяться при code review.

### 1. Правильная форма Xray interface binding

Для interface egress недостаточно добавить произвольное поле к `freedom.settings`. Привязка должна находиться в outbound transport settings:

```json
{
  "protocol": "freedom",
  "settings": {},
  "streamSettings": {
    "sockopt": {
      "interface": "nwg1"
    }
  }
}
```

Golden test `TestXrayOutboundMaterialization/interface` обязан проверять именно `streamSettings.sockopt.interface`.

### 2. Candidate secret не должен попасть в общий journal

`IngressTelegramCandidate` содержит `Secret`, необходимый для применения, но:

- его нельзя сериализовать в `serveringress.TransactionJournal`;
- его нельзя включать в fingerprint error, plan, job status или логи;
- staged Telegram config может содержать secret только в защищённом component transaction storage с правами 0600;
- recovery должен использовать защищённый component snapshot, а не открытое поле общего journal.

Добавить тест, который читает активный/архивный ingress journal после apply и после fault injection и подтверждает отсутствие secret/UUID.

### 3. Проверка отсутствия hardcoded-доменов не должна ломать legacy test

`TestTelegramLegacyTlsDomainPreservedOnMigration` закономерно содержит строку `ya.ru`, поэтому `TestNoPersonalDomainsInDefaults` не должен быть наивным поиском строки по всему репозиторию. Он должен проверять значения production defaults и fresh configuration. Допустимо отдельно сканировать production `.go/.svelte` с исключением тестовых fixtures, но лучше делать структурные assertions.

### 4. Fingerprint error classification

Ошибка `ComputeStrict` должна блокировать transaction, но её не следует выдавать пользователю как доказанный state drift. Сохранить машинный код `PLAN_STALE` для несовпадения fingerprint, а для невозможности проверить состояние лучше использовать отдельный код, например `STATE_PROBE_FAILED`. Оба результата требуют пересоздания/повтора preflight, но имеют разные причины и диагностику.

## Условие приёмки реализации

Реализацию принимать только после прохождения всех 15 acceptance tests из плана, полного race suite, ARM64 cross-build, frontend check/build и `git diff --check`. Особенно важны тесты с реальным coordinator, rollback после candidate activation, recovery из каждой durable phase и semantic golden tests экспортов XHTTP/WS.

До этого walkthrough не должен утверждать, что мастер готов к установке на роутер.
