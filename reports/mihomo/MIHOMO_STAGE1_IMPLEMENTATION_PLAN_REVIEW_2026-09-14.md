# Ревью плана реализации: Mihomo, этап 1 (fail-closed generator)

Проверенный документ:  
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Дата: 2026-09-14  
Основание: `MIHOMO_CORE_STABILIZATION_PLAN_2026-09-14.md`

## Вердикт

**Условно одобрено после обязательной корректировки пунктов ниже.**

План верно ограничивает работу этапом 1 и закрывает два наиболее опасных fail-open дефекта. Однако без уточнений агент может:

- ошибочно считать обычное отсутствие slot ошибкой;
- отключить fallback живого AWG-каталога;
- написать некорректный универсальный parser строк правил;
- объявить YAML «проверенным», хотя бинарник Mihomo его не проверял;
- сломать допустимые legacy/geodata правила;
- не суметь выполнить заявленную WSL-проверку.

После внесения перечисленных поправок план можно отдавать в реализацию.

## Что в плане сделано правильно

1. Работа разделена на `internal/mihomo` и адаптер `internal/singbox/router`.
2. Удаляется опасная семантика «неизвестный outbound превращается в direct».
3. Предусмотрена типизированная `MihomoCompileError`.
4. Учтены ссылки rules, groups, providers, listeners и циклы групп.
5. Запланированы negative tests и сохранение прежнего `config.yaml` при ошибке генерации.
6. Этап не включает сборку IPK и деплой.

## Обязательные поправки

### 1. Не считать `(nil, nil)` от `LoadEffective` ошибкой

`orchestrator.LoadEffective()` возвращает `(nil, nil)`, если зарегистрированный slot отсутствует во всех каталогах. Это нормальное состояние fresh install, а не I/O failure.

Для каждого slot контракт должен быть таким:

```go
raw, err := orch.LoadEffective(slot)
if err != nil {
    return fmt.Errorf("load %s: %w", slot, err)
}
if len(raw) == 0 {
    // slot legitimately absent
}
```

Нельзя требовать ошибку только потому, что `raw == nil`.

### 2. Исправить отдельный дефект `SlotAwg` fallback

Сейчас `awgLoaded = true` устанавливается при `loadErr == nil`, даже если `raw` пуст. В результате на fresh/legacy install fallback к `AWGTags.ListTags()` не выполняется.

Нужно считать slot загруженным только при наличии содержимого:

```go
if len(raw) != 0 {
    // decode and append
    awgLoaded = true
}
```

Добавить тест:

- `TestGenerateMihomoConfig_AbsentAwgSlot_UsesLiveCatalog`.

Отдельно проверить:

- отсутствующий slot + пустой live catalog — допустимо;
- ошибка чтения slot — fail closed, без fallback;
- валидный пустой slot — заранее определить контракт: либо это авторитетное «нет AWG», либо повод использовать каталог. Зафиксировать выбор в тесте.

### 3. Не обещать binary validation в этапе 1

Предлагаемый `validateCompiledConfig` — статическая проверка ссылочной целостности, а не эквивалент `mihomo -t`.

В этапе 1 допустимая гарантия:

> `config.yaml` записывается только после успешной загрузки всех обязательных источников, преобразования и статической валидации модели.

Проверка candidate реальным бинарником, атомарная замена и runtime rollback относятся к этапу 2. Не писать в отчёте этапа 1, что конфиг полностью валидирован Mihomo.

### 4. Валидатор должен получать структурированную модель, а не угадывать грамматику через `strings.Split`

В `cfg.Rules` встречаются как минимум двухчастный `MATCH,target`, трёхчастные правила и правила с дополнительными параметрами (`no-resolve`). Возможны логические/расширенные форматы Mihomo, где target не обязательно находится в `parts[2]`.

Нужно выбрать один из вариантов:

1. предпочтительно — валидировать ссылки до сериализации, пока правила представлены типизированными структурами;
2. либо создать отдельный parser только для явно поддерживаемого AWG Manager подмножества rule grammar с таблицей типов и позиции target;
3. неизвестный тип правила не интерпретировать эвристически: вернуть понятную compile error либо передать его только на этап binary validation по заранее определённому контракту.

Обязательные тесты:

- `MATCH,DIRECT`;
- `DOMAIN-SUFFIX,example.org,group`;
- `IP-CIDR,10.0.0.0/8,group,no-resolve`;
- `RULE-SET,name,group`;
- malformed 0/1/2-field rules;
- запятая/пробелы и пустой target;
- каждый расширенный тип, который UI уже разрешает сохранять.

### 5. Сначала выполнить legacy RULE-SET normalization, затем строгую валидацию

Сейчас `geosite-*`/`geoip-*` преобразуются в `GEOSITE`/`GEOIP` после `ensureReferencedProxiesExist`. Новый порядок должен быть явно указан:

1. собрать модель;
2. нормализовать terminal names и legacy rule-set/geodata записи;
3. валидировать итоговую модель;
4. сериализовать YAML.

Иначе допустимое legacy-правило будет ошибочно отклонено как dangling provider.

Добавить тесты на:

- `RULE-SET,geosite-telegram,target -> GEOSITE,telegram,target`;
- `RULE-SET,geoip-private,target -> GEOIP,private,target,no-resolve` (с учётом фактического текущего формата);
- неизвестный префикс без provider — ошибка.

### 6. Проверять коллизии имён и не принимать список built-ins на веру

До ссылочной проверки валидатор должен отклонять:

- пустые имена proxies/groups/providers/listeners;
- дубли proxies;
- дубли groups после legacy/native merge;
- одинаковое имя proxy и group;
- при необходимости коллизии с зарезервированными именами.

Список `DIRECT`, `REJECT`, `GLOBAL`, `PASS`, `COMPATIBLE` надо подтвердить на фактически упакованной версии Mihomo и в существующей модели AWG Manager. Не следует автоматически считать все пять допустимыми targets во всех контекстах. Например, допустимое действие правила и допустимый член proxy-group могут иметь разные множества.

Ввести отдельные функции/множества, например:

- `validRuleTarget(name)`;
- `validGroupMember(name)`;
- `validListenerTarget(name)`.

### 7. Не потерять реальные interface-bound direct outbounds

Удалить следует только синтез неизвестной ссылки. Реальные direct-outbounds из `SlotAwg`/`AWGTags` должны по-прежнему проходить через `convertSingboxToMihomoProxy` и становиться:

```yaml
type: direct
interface-name: <проверенный iface>
```

Добавить положительный тест, доказывающий, что `awg-*` создаётся только из пары `tag + bind_interface`, а одно лишь упоминание `awg-*` в правиле ничего не синтезирует.

### 8. Проверить sidecar отдельно

`GenerateSidecarConfig` имеет собственный `MATCH,DIRECT`, providers, groups и listeners. Нужны отдельные тесты:

- валидный listener -> proxy;
- валидный listener -> group;
- отсутствующая цель listener;
- group использует отсутствующий provider;
- sidecar без listeners в вызывающем коде не перезаписывает существующий YAML;
- отсутствие routing rules в sidecar не вызывает ложную ошибку.

### 9. Не ограничиваться тестом «config не изменился при ранней ошибке»

В этапе 1 запись остаётся прямой через `os.WriteFile`. Тест byte-equal при ошибке загрузки подтверждает только то, что функция вернулась до записи; он не защищает от частичной записи/ошибки диска. Это будет закрыто только candidate/rename pipeline этапа 2.

В отчёте реализации явно оставить этот риск открытым.

### 10. Исправить команды проверки среды

В текущей WSL рабочая копия ранее определялась как:

```text
/mnt/host/e/AWGM/awg-manager
```

а не `/mnt/e/AWGM/awg-manager`. Кроме того, в доступной WSL не был установлен `go`. Поэтому план не должен обещать WSL tests без предварительной read-only проверки:

```bash
wsl.exe -d Ubuntu -- bash -lc 'command -v go; test -d /mnt/host/e/AWGM/awg-manager'
```

Если Go отсутствует, не устанавливать зависимости самовольно. Использовать доступный Windows Go для платформенно-независимых unit tests и отдельно зафиксировать, что Linux suite ожидает CI/подготовленную среду.

## Дополненный набор acceptance tests этапа 1

Минимально обязательны:

1. Отсутствующий router slot создаёт default model.
2. Повреждённый router pending и active завершаются ошибкой.
3. Отсутствующий subscriptions/tunnels/AWG slot допустим.
4. I/O error каждого slot — fail closed.
5. Пустой AWG slot корректно использует или намеренно не использует live catalog согласно зафиксированному контракту.
6. Неизвестная цель rule/group/listener — fail closed.
7. Неизвестный provider и dangling rule-set — fail closed.
8. Цикл и self-cycle групп — fail closed.
9. Реальный interface-bound AWG direct сохраняется.
10. Опечатка с префиксом `awg-` не синтезирует интерфейс.
11. Legacy geosite/geoip normalization проходит до validation.
12. Валидный full config и валидный sidecar сохраняют прежнюю семантику.
13. При всех ошибках до записи существующий YAML byte-equal.
14. `errors.As(err, *MihomoCompileError)` работает через wrapping.
15. Текст ошибки не содержит секретов proxy/provider.

## Требуемый порядок выполнения агентом

1. Сначала дополнить/исправить сам `implementation_plan.md` по этому ревью.
2. Затем написать failing tests для текущего поведения.
3. Реализовать normalization и typed/static validation.
4. Исправить загрузку router/slots и AWG fallback.
5. Запустить целевые тесты и `git diff --check`.
6. Не исправлять попутно P1/P2, UI, lifecycle или транзакционную запись — это следующие этапы.
7. Подготовить walkthrough с таблицей: требование -> код -> тест -> фактический результат.

## Решение о запуске

**Не запускать текущую редакцию без корректировок. После включения пунктов 1–10 — запускать этап 1 в работу.**

