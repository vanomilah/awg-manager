# Final review: Mihomo Stage 1 Remediation Plan v5.1

Дата: 2026-09-14  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План v5.1 исправил основные архитектурные недостатки v5 и **почти готов к реализации**. Revision-based migration, строгий acceptance mode, scheme-specific DNS parsing, store-level tri-state и OpenAPI/UI flow описаны правильно.

Однако до запуска нужно внести несколько точечных обязательных исправлений. Два из них иначе непосредственно сломают verification: несовпадение пути fixtures и невозможная модель negative tests через ожидаемый `t.Fatalf`.

**Статус: условно одобрен после внесения дельты ниже (v5.2).**

## Обязательные исправления

### P1 — fixture path в bootstrap и Go-тесте неоднозначен и, вероятно, не совпадёт

Bootstrap кладёт данные в `E:/AWGM/awg-manager/testdata/mihomo/`. Во время `go test ./internal/singbox/router` относительный путь `testdata/mihomo` обычно разрешается относительно каталога пакета:

```text
E:/AWGM/awg-manager/internal/singbox/router/testdata/mihomo
```

Таким образом, приведённая acceptance-команда не передаёт `MIHOMO_FIXTURE_DIR` и может искать fixtures не там, куда их положил bootstrap.

**Исправить одним явным контрактом:**

- предпочтительно bootstrap публикует в repo-local `.cache/mihomo/v1.19.29/...`;
- verification передаёт абсолютный путь:

```bash
MIHOMO_FIXTURE_DIR="$PWD/.cache/mihomo/v1.19.29" MIHOMO_ACCEPTANCE=1 go test ...
```

- Go-тест требует абсолютный `MIHOMO_FIXTURE_DIR` в acceptance mode и не угадывает repo root;
- либо использовать `runtime.Caller`/надёжный repo-root helper, но путь всё равно должен быть единственным и протестированным;
- `.gitignore` должен соответствовать фактическому cache path.

### P1 — тесты, которые «ожидают t.Fatalf», спроектированы неправильно

План предлагает:

- `TestMihomoAcceptance_FailsOnCorruptedChecksum`;
- `TestMihomoAcceptance_FailsOnMissingFixture`;

с ожидаемым `t.Fatalf`. Обычный тест не может считать вызов `t.Fatalf` успешным ожидаемым результатом: он пометит тест/подтест failed.

**Исправление:** вынести подготовку и проверку fixtures в чистую функцию:

```go
func validateMihomoAcceptanceFixtures(dir string, expected fixtureManifest) (validatedFixtures, error)
```

Production acceptance test преобразует возвращённую ошибку в `t.Fatal`. Unit tests вызывают helper напрямую и утверждают конкретную ошибку для missing file/checksum/version metadata. Проверку реального `mihomo -v` также лучше вынести в helper, возвращающий error.

### P1 — HTTP create/update нельзя выбирать по `body.id`

Формулировка «если path имеет id или body имеет id — UpdateRule, иначе CreateRule» делает `POST` с ошибочно/враждебно переданным ID операцией изменения существующего правила.

Нужен строгий REST-контракт:

- `POST /rules` всегда вызывает CreateRule;
- body `id` либо запрещён, либо игнорируется и всегда генерируется сервером (предпочтительно запретить с 400);
- `PUT /rules/{id}` всегда вызывает UpdateRule;
- если body содержит ID и он отличается от path ID — `400 ID_MISMATCH`;
- update отсутствующего ID — `404`, а не общий 400;
- никакого неявного upsert.

Добавить API-тесты на все эти случаи.

### P1 — snapshot helper под блокировкой должен исключать повторный lock/deadlock

План задаёт публичный `ComputeUnsupportedRulesSnapshot()` с `RLock`, а Delete должен вычислить snapshot под `Lock`. Если Delete вызовет публичный helper, получится повторный `RLock` при удержании write lock и deadlock.

Определить два уровня:

```go
func (s *Store) ComputeUnsupportedRulesSnapshot() ([]Rule, string) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.computeUnsupportedRulesSnapshotLocked()
}

func (s *Store) computeUnsupportedRulesSnapshotLocked() ([]Rule, string) { ... }
```

Delete вызывает только locked helper под единственным `mu.Lock`.

Revision должна включать versioned canonical representation всех подтверждаемых полей: как минимум `id`, normalized `type`, `payload`, `outbound`, `noResolve`, `enabled`. Нельзя строить hash неоднозначной конкатенацией строк; использовать deterministic JSON/length-prefix. Добавить тесты на стабильность порядка и изменение каждого поля.

### P1 — подтверждение удаления должно определять полный или частичный режим

Revision теперь защищает snapshot от изменения, но план всё ещё разрешает удалить любое подмножество IDs. UI отправляет весь список. Это допустимо, если такое поведение намеренное, однако его нужно записать явно.

Для текущего UX рекомендуется строгий вариант: множество `ids` должно точно совпасть с IDs snapshot. Тогда пользователь подтверждает именно показанный полный список. Несовпадение множества при корректной revision — `400 SELECTION_MISMATCH`; изменение snapshot — `409 MIHOMO_RULES_STALE`.

Если нужен частичный delete, UI должен позволять выбирать элементы, а API и тесты должны явно поддерживать subset semantics.

## Существенные уточнения

### DNS validation

- Не следует автоматически отвергать `dhcp://1.1.1.1` только потому, что это IP-подобная строка, пока поведение не проверено pinned бинарником: синтаксис после `dhcp://` должен соответствовать фактическому parser Mihomo, а не предположению.
- `hosts` сохранён ради существующей совместимости, но его поддержку тоже нужно подтвердить через acceptance corpus.
- Разделить контракты `default-nameserver` и остальных DNS lists: официально default resolver имеет дополнительные ограничения.
- Добавить тесты raw IPv6, bracketed IPv6, zone/interface suffix, пустого/нулевого порта, query без path, percent encoding и двойного `#`.
- Corpus разрешённых и запрещённых значений прогнать через `mihomo -t`; локальный parser не должен быть строже реального ядра без осознанной причины.

### Rule batch semantics

После изменения `SaveRulesBatch([]RuleInput)` определить:

- является ли batch create-only, update-only или смешанным;
- как обрабатываются duplicate IDs;
- выполняется ли операция полностью транзакционно;
- что происходит с omitted `Enabled` для существующего правила;
- как мигрируют старые прямые callers `SaveRule(Rule)`.

Не оставлять одновременно два публичных API с разными default semantics для `Enabled`.

### Acceptance manifest

Bootstrap обязан содержать:

- точные официальные URL для каждого файла;
- checksum compressed и uncompressed с ясными именами;
- размер/верхнюю границу загрузки;
- timeout/retry policy;
- staging directory и публикацию только после проверки всех файлов;
- manifest version в одном source of truth, используемом bootstrap и Go-тестом.

Не дублировать SHA-256 отдельно в shell и Go: они разойдутся. Удобнее хранить versioned JSON manifest в репозитории, а оба потребителя читают его.

Production router aarch64 не проверяется amd64 acceptance-бинарником. Config syntax в основном общая, но перед релизом нужен отдельный smoke test реально поставляемого `linux-arm64` бинарника либо проверка его версии/checksum в packaging pipeline.

### Windows generator replacement

Фраза `safe rename/replace helper` всё ещё не указывает конкретную реализацию. До кодинга агент должен выбрать и протестировать механизм. Существующий `internal/storage.AtomicWrite` использует `os.Rename` и сам по себе не доказывает корректное замещение существующего файла на Windows.

Минимальные гарантии:

- старый target остаётся при любой ошибке до commit;
- target никогда заранее не удаляется;
- temp создаётся в том же каталоге;
- temp закрывается и удаляется при ошибке;
- успешная операция заменяет существующий файл на Windows и Linux;
- права файла определены.

## Исправления verification plan

1. Acceptance-команда должна явно передавать совпадающий абсолютный `MIHOMO_FIXTURE_DIR`.
2. Negative fixture tests запускаются как unit tests helper, а не через ожидаемый `t.Fatalf`.
3. OpenAPI drift нельзя проверять `git diff --exit-code` сразу после ожидаемой регенерации в уже грязном дереве. После внесения generated изменений запустить generator второй раз и сравнить hash/bytes до и после второго запуска; окончательный diff оценивается отдельно.
4. Добавить `go test -race ./internal/mihomonative ./internal/api`, если API tests не конфликтуют с окружением.
5. Добавить clean-worktree/isolated-worktree acceptance. Текущее дерево содержит множество несвязанных изменений.
6. Проверять skipped tests через `go test -json`; обязательная acceptance job падает, если целевой тест отсутствовал либо был skipped.
7. Добавить frontend test на 409 reload/reconfirmation и блокировку повторного submit.
8. Итоговый walkthrough должен привести version, checksum, fixture directory, executed/skipped test list и scoped diff.

## Минимальная дельта v5.2

Перед передачей в реализацию достаточно внести в план следующие изменения:

1. единый абсолютный fixture/cache path;
2. helper, возвращающий error, вместо тестов ожидаемого `t.Fatalf`;
3. POST=create, PUT=update, запрет body/path ID ambiguity;
4. locked/unlocked snapshot helpers без повторного lock;
5. versioned deterministic revision с полным набором полей;
6. явный full-set или subset deletion contract;
7. один manifest для URL/version/checksums;
8. исправленная idempotence/drift verification.

После этой небольшой дельты план можно запускать. Остальные пункты v5.1 сформулированы достаточно подробно.
