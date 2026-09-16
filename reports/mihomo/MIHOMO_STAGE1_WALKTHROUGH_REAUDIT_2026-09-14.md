# Повторный аудит реализации Mihomo Stage 1

Дата: 2026-09-14  
Проверены:

- `implementation_plan.md` (редакция 2);
- `walkthrough.md` с заявлением о 100% завершении;
- фактические изменения `internal/mihomo` и `internal/singbox/router/service_mihomo.go`;
- тесты в Linux/WSL и Windows.

## Вердикт

**Этап 1 пока не принят. Требуется короткий remediation pass.**

Основные fail-open дефекты действительно исправлены, и Linux-тесты проходят. Однако новый статический валидатор несовместим с набором правил, который уже разрешают backend store и UI. Кроме того, `SUB-RULE` проверяется по неверной семантике. Поэтому утверждение walkthrough «все 10 требований выполнены, 100%» не подтверждено.

## Подтверждённо реализовано

1. `loadRouterConfig()` больше не заменяется молча на `NewEmptyConfig()` при ошибке.
2. Ошибки чтения optional slots возвращаются вызывающему коду; `(nil, nil)` остаётся допустимым отсутствием данных.
3. Пустой `SlotAwg` теперь вызывает fallback к `AWGTags.ListTags()`; заполненный slot остаётся авторитетным.
4. `ensureReferencedProxiesExist` удалён, неизвестная ссылка больше не синтезирует `direct` с выдуманным интерфейсом.
5. Реальные `direct + bind_interface` продолжают конвертироваться в Mihomo interface-bound proxy.
6. Нормализация legacy `geosite-*`/`geoip-*` выполняется перед статической валидацией.
7. Реализованы проверки group member/provider/listener, dangling rule-set и циклов групп.
8. Ошибки до `os.WriteFile` не меняют существующий `config.yaml`.

## Найденные дефекты

### P0. Валидатор отклоняет правила, которые разрешают Store и UI

Файлы:

- `internal/mihomo/config.go:1059-1064`;
- `internal/mihomonative/store.go:796-803`;
- `frontend/src/lib/components/sb-router/mihomo/MihomoRuleEditModal.svelte`.

`validateCompiledConfig` не включает ряд уже поддерживаемых типов:

- `DOMAIN-WILDCARD`;
- `IP-ASN`;
- `SRC-IP-ASN`;
- `SRC-IP-SUFFIX`;
- `IN-USER`;
- `REMATCH-NAME`;
- `PROCESS-PATH-WILDCARD`;
- `PROCESS-NAME-WILDCARD`;
- `UID`;
- `DSCP`.

`mihomonative.SaveRule` принимает эти значения, UI часть из них предлагает пользователю, но генератор после текущего изменения вернёт `unsupported rule type`. Это новая регрессия: сохранённое правило может остановить генерацию и применение всей конфигурации.

Исправление:

1. Вынести единый список rule types и описание grammar/target position в общий пакет или функцию, используемую и Store, и compiler.
2. Не поддерживать два расходящихся ручных whitelist.
3. Добавить table test для **каждого** типа из `allowedRuleTypes`, а не только 11 выбранных строк.
4. Проверить положительные примеры из официальной документации Mihomo.

### P0. `SUB-RULE` интерпретируется как обычный proxy target

Файл: `internal/mihomo/config.go:1064-1079`.

Официальный формат:

```yaml
SUB-RULE,(NETWORK,tcp),sub-rule
```

Третий аргумент здесь — имя набора `sub-rules`, а не proxy или proxy-group. Текущий код вызывает `validRuleTarget(tokens[2])`. Следовательно:

- валидное имя sub-rule будет отклонено как неизвестный outbound;
- совпадение имени sub-rule с именем proxy/group ошибочно сделает правило «валидным»;
- структура `Config` вообще не содержит поля `sub-rules`, то есть полноценную поддержку невозможно доказать.

Исправление:

- либо добавить типизированное `SubRules map[string][]string`, проверять существование имени и рекурсивные ссылки;
- либо временно удалить `SUB-RULE` из разрешённых Store/UI и возвращать явную ошибку «SUB-RULE пока не поддерживается AWG Manager»;
- не маскировать отсутствие реализации проверкой proxy target.

Добавить тесты: существующий sub-rule, отсутствующий sub-rule, совпадение имени с proxy, циклическая ссылка sub-rules.

### P1. Заявленный comprehensive grammar test неполон

`TestCompileRuleGrammar_Comprehensive` проверяет лишь небольшой набор и не сравнивает compiler с `mihomonative.allowedRuleTypes`. Поэтому тесты проходят при описанной P0-регрессии.

Исправление: data-driven contract test должен проходить по общему реестру rule specs и формировать минимальный валидный пример каждого типа.

### P1. Не проверяются ссылки `proxy` внутри providers

В `proxy-providers` и `rule-providers` допускается поле `proxy`, задающее маршрут загрузки/обновления. Текущий валидатор проверяет `group.Use`, но не проверяет provider `proxy`. Неизвестная ссылка останется до binary validation следующего этапа.

Исправление:

- проверить `proxy` каждого proxy-provider и rule-provider по соответствующему множеству допустимых targets;
- нормализовать `direct/block`;
- добавить negative tests и тест provider download через существующую группу.

### P1. Не проверяется DNS detour

`FormatMihomoDNSServer` добавляет `#<Detour>` к адресу DNS, но `validateCompiledConfig` не проверяет, что detour существует. Так как после форматирования исходная типизированная связь теряется, эту проверку нужно выполнить до преобразования `DNSServerSpec` в строку либо сохранить связь в промежуточной модели.

### P1. Не проверяются дубликаты listener names

Проверяются пустое имя, порт и proxy target, но два listeners с одинаковым `name` проходят статическую валидацию. Добавить множество listener names и проверку дублей. Желательно также выявлять конфликт listener ports/top-level proxy ports на уровне compile model или оставить это явно этапу 2.

### P1. Ошибка JSON-преобразования собственных outbounds всё ещё подавляется

Файл: `internal/singbox/router/service_mihomo.go:52-55`.

Остаётся конструкция:

```go
if ownJSON, err := json.Marshal(cfg.Outbounds); err == nil {
    _ = json.Unmarshal(ownJSON, &ownOutbounds)
}
```

Обе ошибки должны возвращаться с контекстом. Fail-closed generator не должен молча продолжать без own outbounds, даже если ошибка для текущих типов маловероятна.

### P2. Гарантия «MihomoCompileError безопасна для логов» слишком широкая

Структура сохраняет сырые `Resource` и `Rule`. Текущий тест доказывает лишь, что конкретная ошибка unknown outbound не включает UUID/password из объекта proxy. Он не доказывает безопасность произвольного пользовательского rule/resource.

Исправление: либо сузить комментарий до фактической гарантии, либо централизованно очищать/ограничивать значения и покрыть fuzz/property tests.

### P2. В diff присутствует незаявленное изменение cloud routing

В `service_mihomo.go` добавлено заполнение `sr.DynamicCloudCIDRs`. Оно не описано в walkthrough Stage 1 и не относится к fail-closed генератору. В грязном дереве нельзя автоматически приписывать его текущему агенту, но перед отдельным commit Stage 1 необходимо установить владельца изменения: включить с отдельным обоснованием/тестом либо не захватывать в commit.

## Проверка заявлений walkthrough

| Заявление | Результат аудита |
|---|---|
| Основной unsafe fallback удалён | Подтверждено |
| Ошибки slots обрабатываются fail closed | Подтверждено для проверенных путей |
| AWG fallback исправлен | Подтверждено |
| Все поддерживаемые rules валидируются корректно | Не подтверждено; есть P0-регрессия |
| SUB-RULE поддержан | Опровергнуто |
| Все 10 требований выполнены | Не подтверждено |
| Linux package tests проходят | Подтверждено повторным запуском |
| `git diff --check` без ошибок | Exit code 0; остаются предупреждения CRLF/LF |
| Stage 1 завершён на 100% | Опровергнуто |

## Фактически выполненная проверка

```text
WSL Ubuntu: go version go1.26.0 linux/amd64
/mnt/e/AWGM/awg-manager существует
go test ./internal/mihomo ./internal/singbox/router: PASS
  internal/mihomo          3.660s
  internal/singbox/router  5.096s
```

Windows `go test ./internal/mihomo` падает только на известных platform-specific тестах `Operator` из-за helper `select {}`/process kill semantics. Целевой router generator suite на Windows проходит. Это не опровергает Linux результаты, но walkthrough должен различать platform-independent compiler tests и supervisor tests.

`git diff --check` вернул exit code 0, однако сообщил предупреждения о будущей замене CRLF на LF. Формулировка walkthrough «zero warnings/errors» неточна; корректно: «ошибок whitespace нет, имеются line-ending warnings».

## Минимальный remediation pass

1. Создать общий registry поддерживаемых rule specs.
2. Добавить пропущенные типы и полный contract test Store -> ConfigRules -> compiler.
3. Исправить/явно отключить `SUB-RULE` до появления `Config.SubRules`.
4. Валидировать provider proxy, DNS detour и дубли listeners.
5. Перестать подавлять JSON marshal/unmarshal errors в `service_mihomo.go`.
6. Повторить Linux tests и `git diff --check`.
7. Обновить walkthrough без заявления 100%, пока пункты выше не закрыты.

## Решение о следующем этапе

**Не переходить к Stage 2 до закрытия двух P0 и повторного ревью Stage 1.** Остальные P1 желательно закрыть в том же remediation pass, поскольку они относятся непосредственно к обещанной статической ссылочной целостности.

