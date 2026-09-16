# Final recheck: Mihomo Stage 1 Remediation Plan v5.2

Дата: 2026-09-14  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

Версия v5.2 закрыла почти всю обязательную дельту v5.1: REST create/update разделены, full-set revision contract определён, повторная блокировка устранена, fixture path стал явным, а fixture validator возвращает ошибки.

**План пока нельзя запускать без ещё одной короткой правки.** Остался один критический дефект reproducibility и несколько конкретных пробелов компиляции/проверки. После исправления пунктов P1 ниже план можно считать окончательно одобренным без нового большого перепроектирования.

## P1 — обязательные исправления

### 1. Закреплённые checksum несовместимы с URL `latest`

Manifest использует:

```text
https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/ASN.mmdb
https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat
https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat
```

и одновременно фиксированные SHA-256. `latest` является плавающей ссылкой: при следующем релизе содержимое изменится, checksum перестанет совпадать и bootstrap сломается. Это противоречит hermetic/pinned acceptance contract.

**Исправить:** закрепить immutable release tag или commit-derived URL для каждого geodata artifact. В manifest добавить `releaseTag`/`sourceRevision`, размер файла и checksum. Bootstrap не должен следовать плавающему `latest`. Если upstream не предоставляет стабильный tag URL, артефакты должны храниться в собственном versioned CI cache/artifact repository с зафиксированным digest.

Перед кодингом обязательно вручную/скриптом подтвердить все четыре пары:

- точное имя бинарного asset;
- checksum `.gz`;
- checksum распакованного бинарника;
- checksum каждого файла именно по закреплённому URL.

### 2. Изменение `SaveRulesBatch` ломает существующий caller

План меняет сигнатуру:

```go
SaveRulesBatch(items []RuleInput)
```

но существующий код `internal/api/mihomo_handler.go:183-185` содержит:

```go
func (h *MihomoHandler) BatchSaveRules(ctx context.Context, rules []mihomonative.Rule) error {
    ... h.nativeStore.SaveRulesBatch(rules)
}
```

Без изменения этого API проект не скомпилируется. Кроме того, прямые тестовые и compatibility-вызовы `SaveRule(Rule)` сейчас иногда полагаются на старый default `Enabled=true`.

**Исправить план:**

1. перечислить и мигрировать `MihomoHandler.BatchSaveRules` и все callers;
2. решить, принимает batch `[]RuleInput` или остаётся внутренним snapshot API на `[]Rule`;
3. не смешивать batch import/replace и REST create/update semantics;
4. провести `rg '\.SaveRule\(|SaveRulesBatch\('` и обновить каждый caller/test;
5. добавить compile-time/round-trip tests для batch;
6. удалить или чётко ограничить старый `SaveRule(Rule)` — иначе два публичных API сохранят разные defaults.

Рекомендуемый вариант: REST использует `CreateRule/UpdateRule` с `RuleInput`, а внутренний transactional replacement получает отдельное имя вроде `ReplaceRulesBatch([]Rule)` и сохраняет точные persisted значения без defaults.

### 3. Manifest path для Go-теста всё ещё не определён

Fixture directory теперь передаётся абсолютно, но тест также должен прочитать `scripts/mihomo-acceptance-manifest.json`. При запуске пакета его current working directory не обязан быть корнем репозитория.

**Исправить:** определить один из вариантов:

- отдельный обязательный `MIHOMO_ACCEPTANCE_MANIFEST` с абсолютным путём;
- вычисление manifest path от `runtime.Caller` с проверкой ожидаемого расположения;
- `go:embed` immutable manifest в test binary, при этом bootstrap читает тот же исходный файл.

Acceptance-команда должна явно показывать оба пути, если используются две env-переменные.

### 4. Version helper имеет несогласованный контракт

План объявляет:

```go
func inspectMihomoVersion(binPath string) (string, error)
```

но предлагает тест `TestInspectMihomoVersion_VersionMismatchReturnsError`. Функция не получает ожидаемую версию и поэтому сама не может определить mismatch.

**Исправить:** либо:

```go
func validateMihomoVersion(binPath, expectedVersion string) (actual string, err error)
```

либо `inspect` только возвращает строку, а отдельный `validateVersion(actual, expected) error` тестируется независимо. Версию брать из manifest, не дублировать константой в Go.

## P2 — уточнения, которые желательно внести сразу

### 5. Two-pass OpenAPI запуск сам по себе ничего не сравнивает

Два последовательных запуска generator не доказывают idempotence, если между ними не сравниваются bytes/hash/status.

Нужно:

1. выполнить первый generation;
2. вычислить SHA-256 или скопировать generated outputs;
3. выполнить второй generation;
4. сравнить все generated outputs byte-for-byte;
5. отдельно показать scoped diff относительно исходного состояния.

Учитывать нужно не только `schemas.gen.ts`, но и синхронизированный `frontend/static/openapi.yaml` и любые другие outputs команды.

### 6. Acceptance job всё ещё не доказывает отсутствие Skip

Команда с `-run` завершится кодом 0 даже при `t.Skip`. Требование «Hard Verification» должно проверяться машинно.

Добавить wrapper/CI step с `go test -json`, который подтверждает:

- целевой test event присутствует;
- итоговое действие `pass`, не `skip`;
- fixture validation и реальный запуск binary были выполнены;
- версия и digest записаны в artifact/log.

### 7. Bootstrap publication требует конкретизации

«Atomically publishes directory» недостаточно. Для уже существующего непустого каталога directory rename/replace ведёт себя по-разному. Безопаснее публиковать versioned immutable directory с manifest completion marker и никогда не изменять готовый каталог на месте. При наличии валидного cache bootstrap завершается idempotently; повреждённый cache помещается в новый staging и только после полной проверки заменяется безопасным способом.

Также задать timeout, retry, максимальный размер загрузки и проверку HTTP status для `curl`/`wget`.

### 8. DNS acceptance corpus должен войти в бинарный тест

План описывает unit tests parser, но не закрепляет проверку всех разрешённых DNS forms через `mihomo -t`. Добавить representative config/corpus как часть acceptance, особенно для `hosts`, `system://`, `dhcp://...`, raw IPv6 и `rcode://...`. Это исключит ситуацию, когда локальный validator принимает синтаксис, отвергаемый ядром, или наоборот.

## Что теперь сделано правильно

- POST и PUT имеют строгие разные значения, implicit upsert запрещён.
- Full-set deletion и typed stale/selection errors определены.
- Revision включает все важные поля и version prefix.
- Locked helper отделён от публичного RLock wrapper.
- Tri-state create/update semantics сформулированы корректно.
- Acceptance mode не использует PATH или `$HOME`.
- Negative fixture tests переведены на helper, возвращающий error.
- Frontend modal обрабатывает повторное подтверждение после 409.
- Генератор больше не должен предварительно удалять target.

## Минимальная финальная дельта v5.3

Перед запуском агенту достаточно внести:

1. immutable geodata URLs вместо `latest` и проверенные digests;
2. миграцию `BatchSaveRules` и полный список Store callers;
3. однозначный manifest path для Go acceptance test;
4. согласованный version validation helper;
5. реальное byte/hash comparison для OpenAPI two-pass;
6. машинную проверку, что acceptance test не был skipped.

Пункты про immutable cache publication и DNS acceptance corpus желательно включить туда же. После этого повторное плановое ревью не требуется: можно реализовывать и затем проверять walkthrough и фактический diff.
