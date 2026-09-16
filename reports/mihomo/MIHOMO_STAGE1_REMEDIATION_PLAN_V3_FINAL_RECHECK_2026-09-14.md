# Final Recheck: Mihomo Stage 1 Remediation Plan v3

Дата: 2026-09-14  
Проверен файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Предыдущая проверка: `MIHOMO_STAGE1_REMEDIATION_PLAN_V2_RECHECK_2026-09-14.md`.

## Вердикт

**Архитектура v3 одобрена. План можно отдавать в реализацию после внесения двух обязательных технических поправок ниже. Новый полный пересмотр плана не требуется.**

Редакция v3 закрыла четыре прежних P0-контракта:

- DNS normalizer теперь возвращает `(string, error)` и предусматривает запись результата обратно;
- DNS grammar сохраняет plain IP, special resolvers, parameter-only fragments и forward-compatible параметры;
- legacy `SUB-RULE` больше не требует несуществующий `GetRule` и не ломает сигнатуру `ListRules()`;
- frontend получает generated artifact из Go registry, то есть появляется реальный машинный parity bridge.

Также добавлены semantic composite checks, socket capability table, UTF-8-safe truncation, обязательный `mihomo -t` и production frontend build.

## Две обязательные поправки перед кодированием

### P0. Нельзя тихо фильтровать legacy rules в `ConfigRules()`

В текущем коде:

```go
func (s *Store) ConfigRules() []string
```

а `Store` содержит только `path`, mutex и state — logger в него не внедрён. Поэтому пункт «filters out unsupported rules, logging a warning» невозможно реализовать буквально без скрытого global logger. Если просто пропустить правило, пользователь увидит его в UI, но runtime его не применит. Это опасное расхождение desired state и applied state.

Исправить контракт одним из способов.

#### Рекомендуемый вариант

Добавить отдельную проверку до компиляции:

```go
func (s *Store) ValidateRuntimeRules() error
```

Router adapter перед `ConfigRules()` вызывает её и прекращает apply fail-closed со структурированной ошибкой, перечисляющей ID/type неподдерживаемых правил. `ListRules()` остаётся неизменным, поэтому UI может показать и удалить legacy rule.

Дополнительно:

- `UnsupportedRules()` остаётся read-only методом;
- удаление выполняется только явным пользовательским действием;
- `DeleteUnsupportedRules()` не вызывается автоматически;
- endpoint/UI для удаления требует подтверждения и показывает число/ID удаляемых правил;
- `ConfigRules()` не должен самостоятельно логировать и продолжать применение частичной конфигурации.

Допустимая альтернатива — изменить `ConfigRules()` на `([]string, error)` и обновить интерфейс `MihomoNativeProxySource`, router adapter, mocks и тесты. Главное требование: unsupported enabled rule блокирует apply, а не исчезает молча.

### P1. Обычный `go test` не должен генерировать tracked frontend-файл

Пункт `rules_manifest_test.go generates ... and asserts parity` смешивает генерацию и проверку. Тесты должны быть идемпотентными и не менять рабочее дерево. Кроме того, рабочая директория Go test — каталог пакета, поэтому относительный путь к `frontend` легко вычислить неверно.

Разделить механизм:

1. Production generator, например:

   ```text
   internal/mihomo/cmd/genrules
   ```

   либо `go:generate` рядом с registry.
2. Generator атомарно создаёт `frontend/src/lib/types/mihomoRuleTypes.generated.ts` из детерминированно отсортированного `AllRuleSpecs()`.
3. `rules_manifest_test.go` только строит ожидаемые bytes в памяти, читает committed generated file и сравнивает их; файлов не записывает.
4. Verification выполняет generator, затем проверяет отсутствие diff generated-файла.
5. В generated-файле разместить заголовок `Code generated ... DO NOT EDIT.`.

## Реализационные замечания, не блокирующие старт

### Composite parser

Для дочерних matcher'ов недостаточно проверить только известность их type. Нужно вызвать matcher-specific payload validation без требования внешнего outbound target. В частности:

- `NOT` — ровно один корректный child matcher;
- `AND`/`OR` — минимум два корректных child matcher;
- вложенные composite matchers разбираются рекурсивно с ограничением глубины;
- `MATCH` и `SUB-RULE` внутри запрещены;
- malformed commas/parentheses отклоняются.

Добавить max nesting depth и max matcher count, чтобы parser не допускал чрезмерной рекурсии/нагрузки.

### DNS normalizer

- Нормализатор должен вызываться в функции, которая может вернуть ошибку. Текущая `normalizeCompiledConfig(cfg *Config)` ошибки не возвращает, поэтому либо изменить её на `error`, либо разделить pure normalization и fallible DNS validation.
- Не принимать произвольный positional selector как интерфейс без документированного предупреждения: опечатка в имени proxy/group неотличима от имени интерфейса. Это ограничение синтаксиса Mihomo, его нужно отразить в тексте ошибки/документации.
- Проверять корректность IPv6 fragment parsing через `net/url` осторожно: символ `#` отделяет Mihomo options, а не стандартный URL fragment в прикладном смысле.

### Listener endpoint model

- Socket capability определяется таблицей по normalized listener type, а неизвестный type должен завершаться fail-closed либо использовать явно документированный conservative fallback.
- IPv4 wildcard `0.0.0.0` и IPv6 wildcard `::` нельзя безусловно считать одним socket namespace на всех платформах. Для Keenetic/Linux допустима консервативная проверка, но она должна быть подписана как conservative и иметь отдельные тесты.
- В acceptance включить конфликты не только `TProxyPort`, но и `RedirPort`, `MixedPort`, `Port`, `SocksPort`.

### Error sanitization

Rune-safe truncation сохраняет валидный UTF-8, но после замены control characters желательно схлопывать последовательности пробелов. Тест должен проверять итоговый `Error()`, а не только внутренние поля.

### Binary acceptance

Representative `mihomo -t` обязан использовать доступный локальный binary и локальные fixtures. Тест не должен скачивать geodata из сети. Data-dependent `GEOIP`/`GEOSITE` cases следует снабдить fixture data или вынести в отдельную обязательную acceptance-команду.

### `DynamicCloudCIDRs`

Оставлять этот тест в remediation допустимо только как защита уже существующей дельты текущей ветки. В walkthrough явно отметить, что это regression coverage существующего cloud-bypass поведения, а не новая функция rule registry.

## Скорректированный порядок работы

1. Добавить Go rule registry и semantic validators.
2. Перевести compiler и native store на registry.
3. Реализовать fail-closed runtime validation legacy rules; не выполнять автоматическое удаление.
4. Добавить отдельный deterministic generator TypeScript manifest.
5. Подключить UI к generated manifest и добавить read-only parity test.
6. Реализовать fallible DNS normalization с полным набором тестов.
7. Добавить provider reference/type validation.
8. Добавить endpoint-aware listener validation.
9. Исправить подавление JSON errors и санитизацию compile errors.
10. Выполнить Go/frontend/binary acceptance.
11. После тестов подготовить walkthrough с таблицей «требование → код → тест → результат».

## Обязательные команды приёмки

```powershell
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test ./internal/mihomo ./internal/mihomonative ./internal/singbox/router"
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager/frontend && npm test -- --run"
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager/frontend && npm run check"
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager/frontend && npm run build"
```

Дополнительно:

- запустить generator и подтвердить отсутствие неожиданного diff;
- выполнить representative `mihomo -t` без network download;
- `gofmt` только затронутых Go-файлов;
- `git diff --check`;
- проверить, что тесты не изменили tracked files;
- не собирать IPK и не выполнять deploy в рамках Stage 1 remediation.

## Критерий завершения

Stage 1 принимается только если unsupported persisted rule блокирует runtime apply с понятной ошибкой, generated frontend manifest соответствует Go registry, все тесты зелёные и фактический walkthrough содержит точные результаты команд. Удаление пользовательских правил, сборка IPK и установка на роутер в эту задачу не входят.

