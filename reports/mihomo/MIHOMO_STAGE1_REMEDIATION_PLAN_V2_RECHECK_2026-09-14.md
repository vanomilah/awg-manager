# Recheck: Mihomo Stage 1 Remediation Plan v2

Дата: 2026-09-14  
Проверен файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Предыдущая рецензия: `MIHOMO_STAGE1_REMEDIATION_PLAN_REVIEW_2026-09-14.md`.

## Вердикт

**План существенно улучшен, но пока не готов к буквальному исполнению.**

Закрыты предыдущие замечания о выделенном DNS parser, расширенном `RuleSpec`, проверке динамических provider-полей, endpoint-aware listener validation, полной санитизации полей ошибки, отсутствии import cycle в Go-тесте и frontend contract test.

Остались четыре блокирующие несогласованности. После их исправления план можно запускать без ещё одного архитектурного пересмотра.

## Блокирующие замечания

### P0. DNS parser не может выполнить обещанную нормализацию

В плане объявлена функция:

```go
ParseAndValidateDNSServerURI(entry string, validTarget func(string) bool) error
```

При этом план требует нормализовать `#rules` в `#RULES`. Функция, возвращающая только `error`, не может вернуть нормализованное значение, а строки в `cfg.DNS` неизменяемы.

Нужно выбрать явный контракт:

```go
func NormalizeAndValidateDNSServerURI(
    entry string,
    validTarget func(string) bool,
) (string, error)
```

После этого `normalizeCompiledConfig` или отдельная функция обязана записать результат обратно во все четыре места:

- `DefaultNS[i]`;
- `Nameserver[i]`;
- `Fallback[i]`;
- `NameserverPolicy[key]`.

Тест должен проверять не только отсутствие ошибки, но и точное нормализованное значение.

### P0. DNS grammar по-прежнему неполна и может отклонить рабочий конфиг

Пункты 64–68 перечисляют только `udp`, `tcp`, `tls`, `https`, `quic` и четыре fragment-параметра. В Mihomo также существуют специальные/дополнительные DNS-формы и параметры, включая `system`, plain IP, `dhcp://`, `rcode://`, `hosts`, `name-cert-verify`, `disable-ipv4`, `disable-ipv6`, `disable-qtype-<int>` и другие документированные варианты.

Кроме того, fragment может содержать параметры без outbound selector. Нельзя считать первый fragment token маршрутом, если он имеет форму `key=value`.

Официальный контракт: <https://wiki.metacubex.one/en/config/dns/#additional-parameters>.

Исправить план:

1. Не вводить неполный whitelist base schemes.
2. Сохранить поддержку plain IP и специальных значений, уже используемых генератором.
3. Различать первый positional selector и `key=value`.
4. Включить полный актуальный набор документированных параметров, включая шаблон `disable-qtype-<int>`.
5. Определить политику forward compatibility: неизвестный корректно сформированный параметр либо сохраняется, либо отклоняется с точным сообщением; выбранное поведение закрепить тестом.
6. Добавить positive tests для plain IP, `system`, параметра без selector, `name-cert-verify`, `disable-ipv4/6` и `disable-qtype-65`.

### P0. Миграция legacy `SUB-RULE` описана через API, которого нет

В текущем `mihomonative.Store` существует:

```go
func (s *Store) ListRules() []Rule
```

Метода `GetRule` нет. `ListRules()` используется не только HTTP handler, но также inspector, router adapter, traffic matcher, traffic service и многочисленные тесты. Формулировка «`ListRules` и `GetRule` возвращают migration error» не реализуема без изменения публичного контракта и всех потребителей.

Нужно выбрать один вариант и полностью расписать его:

#### Предпочтительный вариант

- валидировать persisted rules при `OpenStore`/load;
- сохранить store доступным для восстановления;
- выставить структурированную health/migration issue с ID проблемных записей;
- не передавать неподдерживаемое правило в compiler/runtime;
- предоставить явное действие удаления или конвертации;
- `ListRules()` оставить read-only методом без изменения сигнатуры.

#### Альтернативный вариант

- изменить сигнатуру на `ListRules() ([]Rule, error)`;
- перечислить и обновить **все** вызовы в `internal/api`, `internal/singbox/router`, `internal/sys/traffic` и тестах;
- описать HTTP status/error body и поведение UI.

Несуществующий `GetRule` из плана удалить либо отдельно запланировать его API и потребителей.

### P0. Frontend parity всё ещё не является сквозной parity

`MIHOMO_SUPPORTED_RULE_TYPES` и Vitest, проверяющий этот же TypeScript-массив, гарантируют внутреннюю целостность frontend, но не совпадение с Go registry. Backend и frontend всё равно будут двумя вручную поддерживаемыми списками.

Для заявленной «frontend/backend parity» нужен машинный мост:

- один language-neutral manifest, из которого читают Go и TypeScript; либо
- Go generator, создающий `mihomoRuleTypes.generated.ts`; либо
- backend capability endpoint и UI, загружающий rule specs; либо
- тест, который получает/генерирует backend manifest и сравнивает его с TypeScript export.

Если на Stage 1 оставляется ручной TypeScript-массив, формулировку следует понизить до «frontend regression list», а не «single source of truth» и не «full parity».

## Важные уточнения

### P1. Composite rules требуют семантического parser

Проверка только сбалансированности скобок недостаточна:

- `NOT` должен иметь ровно один вложенный matcher;
- `AND` и `OR` должны иметь допустимое число вложенных matcher'ов;
- вложенные элементы должны быть разрешёнными rule matchers, а не произвольным текстом;
- target внешнего правила нельзя перепутать с target вложенного выражения.

Если это не реализуется сейчас, убрать из цели утверждение `complete grammar rules` и назвать проверку базовой структурной валидацией.

### P1. Listener protocol нельзя надёжно вывести из текущей модели без таблицы

Текущий `Listener` содержит строковый `Type`, `Listen`, `Port`, `Proxy`, `UDP`. План должен определить функцию, переводящую listener type/UDP в множество сокетов `{tcp, udp}`. Строковое значение `protocol` в endpoint key без этой таблицы не выявит пересечения mixed/TProxy/HTTP/SOCKS.

Для global ports также задать transport capabilities:

- `port`: TCP;
- `socks-port`: как минимум TCP, UDP согласно фактическому режиму;
- `mixed-port`: TCP и поддерживаемый UDP path;
- `redir-port`: TCP;
- `tproxy-port`: TCP и UDP.

Нужны тесты перекрытия разных listener types на одном адресе/порту, IPv4 wildcard, IPv6 wildcard и loopback.

### P1. Error sanitization должна быть UTF-8-safe

Ограничение длины нельзя делать простым byte slice, иначе русское сообщение может стать невалидным UTF-8. В плане следует зафиксировать truncation по rune либо безопасное ограничение байтов с откатом до корректной UTF-8 boundary.

### P1. Binary integration test должен быть герметичным

`GEOIP`, `GEOSITE` и некоторые rule families могут потребовать geodata/runtime assets. Тест `mihomo -t` не должен случайно обращаться в сеть или зависеть от данных роутера.

Нужно:

- использовать зафиксированный test binary/version;
- предоставить локальные fixture assets либо разделить syntax-only и data-dependent cases;
- запретить network download в тесте;
- считать skip допустимым в обычном unit run, но **не** в финальной Stage 1 acceptance.

### P2. Проверка frontend неполна

Команды `npm test -- --run` и `npm run check` допустимы, но необходимо добавить `npm run build`, поскольку меняется импорт production-компонента. Сборка frontend не означает сборку IPK.

### P2. `DynamicCloudCIDRs` остаётся отдельной дельтой

Одного пояснения в walkthrough недостаточно. Либо:

- показать, какое конкретное замечание Stage 1 закрывает этот тест и оставить его;
- либо вынести изменение и тест в отдельный commit/task.

## Точная дельта для редакции v3

1. Изменить DNS parser на `(normalized string, error)` и описать запись результата обратно в config.
2. Расширить DNS grammar и не ломать plain IP/special resolvers/parameter-only fragments.
3. Заменить несуществующий `GetRule` и неопределённое изменение `ListRules` на реализуемую migration strategy.
4. Сделать реальный cross-language parity bridge либо честно ослабить обещание frontend parity.
5. Определить semantic parser для composite rules или ограничить заявленный scope.
6. Добавить таблицу socket capabilities для listener/global port collision detection.
7. Зафиксировать UTF-8-safe truncation.
8. Сделать binary validation hermetic и обязательной в финальной приёмке.
9. Добавить `npm run build`.
10. Развязать `DynamicCloudCIDRs` с remediation либо доказать принадлежность к нему.

## Что уже можно реализовывать без риска

Параллельно с уточнением блокеров безопасно выполнять:

- создание backend rule registry;
- перевод compiler/store whitelist на registry;
- удаление `SUB-RULE` из формы создания;
- fail-closed проверку типа provider `proxy`;
- возврат ошибок `json.Marshal`/`json.Unmarshal`;
- санитизацию всех полей ошибки с UTF-8-safe реализацией;
- unit tests для перечисленного.

Но менять persisted-rule migration и DNS validation до исправления плана не следует: именно эти части способны сделать существующую конфигурацию нечитаемой или отклонить рабочий runtime config.

## Условие окончательного одобрения

После внесения перечисленной дельты v3 план можно отдавать агенту в полную реализацию. Повторный большой архитектурный аудит после этого не потребуется; достаточно короткой проверки четырёх P0-контрактов и затем аудита фактически выполненного `walkthrough.md`.
