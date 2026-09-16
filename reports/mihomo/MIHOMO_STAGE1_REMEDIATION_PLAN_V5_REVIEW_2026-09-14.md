# Review: Mihomo Stage 1 Final Remediation Plan v5

Дата: 2026-09-14  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Основание: `MIHOMO_STAGE1_WALKTHROUGH_FINAL_AUDIT_2026-09-14.md`

## Вердикт

План v5 **существенно улучшен и охватывает все семь замечаний**, но запускать его без корректировки пока не следует. В текущем виде остаются четыре архитектурные ошибки уровня P1 и несколько недоопределённых контрактов, из-за которых реализация может формально закрыть пункты, но оставить гонки, ложную герметичность и несовместимый DNS parser.

После внесения обязательных правок ниже план можно отдавать в работу.

## P1 — обязательные изменения плана

### 1. Stale-state защита migration API всё ещё не определена корректно

В разделе User Review заявлено: переданные IDs должны совпадать с текущим набором unsupported rules. Ниже реализация требует лишь, чтобы каждый переданный ID сейчас существовал и оставался unsupported, после чего удаляет только эти IDs.

Это разные контракты. Такая проверка не обнаруживает:

- появившееся между GET и POST новое unsupported rule;
- изменение других правил;
- повторное использование старого подтверждения после иной мутации;
- гонку между проверкой IDs и сохранением, если проверка не выполняется под одной блокировкой/revision.

**Нужно заменить на revision-based контракт:**

```json
GET -> {
  "items": [...],
  "revision": "sha256-of-canonical-unsupported-snapshot"
}

POST -> {
  "ids": ["..."],
  "revision": "..."
}
```

Под одной блокировкой Store должен повторно вычислить revision, сравнить его с ожидаемым, проверить уникальность IDs и удалить ровно подтверждённые элементы. Несовпадение revision — `409 Conflict` с машинным кодом `MIHOMO_RULES_STALE`. Пустой список и duplicate IDs — `400`.

Если продуктовый контракт требует удалять вообще все unsupported rules, `ids` должны множественно совпадать со snapshot. Если разрешено удалять подмножество, revision всё равно обязан защищать весь увиденный snapshot. Это решение нужно явно записать в план.

### 2. Acceptance mode не должен искать случайный системный бинарник

План предлагает и в acceptance mode искать `testdata/mihomo`, `MIHOMO_BIN` или system locations. Это снова допускает проверку не той версии, хотя ниже заявлены pinned version и checksum.

**Правильный контракт:**

- regular mode может использовать `MIHOMO_BIN`/PATH как необязательный smoke test;
- `MIHOMO_ACCEPTANCE=1` принимает только явно заданный fixture root либо канонический CI cache path;
- checksum вычисляется для точного исполняемого файла и каждого dat-файла до запуска;
- вывод `mihomo -v` обязан соответствовать pinned release, а не просто логироваться;
- bootstrap скачивает во временный/cache-каталог, проверяет HTTPS URL, размер и SHA-256, затем атомарно публикует fixture;
- бинарники и большие geodata не коммитятся в Git;
- тест не копирует ничего из `$HOME` и не имеет fallback к PATH;
- fixture cleanup не должен удалять пользовательские файлы.

Также следует указать точное имя release asset. Формулировка `linux amd64` неоднозначна: у Mihomo есть несколько amd64-вариантов. Для WSL acceptance это допустимая архитектура, но она не заменяет отдельный smoke test production-бинарника `linux-arm64` для роутера aarch64.

Приведённые SHA-256 нельзя принимать на доверии: план не содержит URL/имя asset/источник checksum и не уточняет, относится checksum к `.gz` или распакованному бинарнику. Все значения нужно сверить с официальным release manifest и зафиксировать вместе с именами файлов.

### 3. DNS grammar в плане содержит неверное обобщение

Нельзя валидировать все URI одинаковым требованием «непустой host». У Mihomo схемы имеют разные payload contracts:

- `udp://`, `tcp://`, `tls://`, `https://`, `quic://` адресуют DNS server;
- `dhcp://en0` принимает имя интерфейса, а `dhcp://system` имеет специальную семантику;
- `rcode://success`, `rcode://format_error`, `rcode://server_failure`, `rcode://name_error`, `rcode://not_implemented`, `rcode://refused` принимают enum, не host;
- `system://` является допустимой формой наряду с `system`;
- `h3` документирован как дополнительный параметр DoH (`#h3=true`), а не как отдельная схема `h3://`.

Следовательно, `h3://` надо удалить из списка, добавить `system://`, а `validateDNSBase` сделать scheme-specific. Для `default-nameserver` может потребоваться более строгий контекстный контракт, чем для `nameserver`; универсальная функция должна принимать context/kind либо компилятор должен выполнять второй уровень проверки.

Добавить table-driven tests для каждой разрешённой схемы, IPv6, допустимых/недопустимых портов, `dhcp://interface`, всех rcode enum, `system://`, неизвестной схемы и URL с userinfo/fragment. Окончательный corpus желательно прогнать через pinned `mihomo -t`.

### 4. `Enabled *bool` нельзя ограничивать HTTP handler

План решает проблему DTO в handler, но внутренний `Store.SaveRule(Rule)` по-прежнему получает обычный `bool` и не различает omitted от explicit false. Прямые callers и batch API сохраняют двусмысленность.

Нужен единый store-level контракт. Возможные варианты:

- отдельные `CreateRule(CreateRuleInput)` и `UpdateRule(UpdateRuleInput)` с `Enabled *bool`;
- patch DTO с `Enabled *bool`, преобразуемый в Store mutation;
- default `true` выполняется только в CreateRule до нормализации, а Save/Update никогда не меняют explicit false.

Обязательно проверить `SaveRulesBatch`, импорт/восстановление snapshot и все прямые вызовы `SaveRule`. Тесты должны покрывать create omitted -> true, create explicit false -> false, update omitted -> preserve, update true/false -> exact value, batch round-trip и restart/reload from disk.

## P2 — уточнения перед реализацией

### 5. Listener registry должен учитывать реальную UDP-семантику

В плане `redir` и `http` жёстко отмечены TCP-only, хотя существующий код учитывает `l.UDP`. Перед заменой нужно сверить фактические поля `Listener` и YAML, иначе можно пропустить существующую возможность или создать ложные конфликты.

Registry должен возвращать `(socketCap, error)` и использоваться единственным путём как при валидации типов, так и при collision detection. Добавить тесты case/whitespace, пустого типа, UDP flag и конфликтов IPv4/IPv6 wildcard.

### 6. Удалять target перед rename в генераторе нельзя

Фраза «remove target before rename» создаёт окно, когда generated файл отсутствует, и при сбое приводит к потере предыдущей версии. Это не atomic replacement.

В проекте есть `internal/storage.AtomicWrite`, но его текущая реализация тоже основана на `os.Rename`, поэтому нельзя автоматически заявлять, что она решает Windows replacement. Нужен либо проверенный platform-specific replace helper, либо генерация во временный файл с безопасным Windows replacement/rollback. Для build-time генератора допустим простой checked write, если атомарность не является требованием, но удаление старого файла заранее запрещено.

Добавить Windows test: target существует, генерация заменяет содержимое, ошибка не уничтожает старый файл, temp-файл убирается.

### 7. OpenAPI и frontend regeneration должны быть частью плана

Недостаточно изменить `swagger.yaml`: нужно выполнить принятый в проекте генератор схем/клиента и проверить отсутствие drift. Не редактировать `schemas.gen.ts` вручную, если он generated. Указать точную команду и scoped diff.

UI confirmation не должен использовать browser `confirm()` без необходимости. Предпочтителен штатный modal с отображением name/type/id, revision и последствия применения. Во время POST кнопки блокируются; `409` вызывает reload и просит подтвердить уже новый snapshot.

## Корректировка verification plan

Добавить к существующему списку:

1. Чистый checkout/worktree для Stage 1 validation — текущая рабочая директория содержит множество несвязанных изменений.
2. Проверку, что acceptance test действительно был запущен и не skipped (`go test -json` или отдельная обязательная CI job).
3. Негативный acceptance test: повреждённый checksum и отсутствующий fixture завершаются ошибкой.
4. Race tests для Store/API migration (`go test -race` на поддерживаемой Linux-среде).
5. OpenAPI drift/generated client check.
6. API auth tests и точные HTTP/error codes.
7. Generator parity через вывод во временный путь и byte comparison, чтобы сама проверка не меняла tracked файл.
8. `git diff --check` и список skipped tests в итоговом walkthrough.

Команда `go run genrules && git diff --exit-code` в грязном рабочем дереве менее надёжна. Лучше:

```bash
tmp="$(mktemp)"
go run ./internal/mihomo/cmd/genrules -out "$tmp"
cmp "$tmp" frontend/src/lib/types/mihomoRuleTypes.generated.ts
rm -f "$tmp"
```

## Решение по запуску

**Статус: требуется небольшая, но обязательная ревизия v5 -> v5.1.**

Минимум до начала кодинга:

1. заменить migration ID-check на revision/snapshot contract;
2. запретить PATH/$HOME fallback в acceptance mode и указать точные asset URLs/checksum semantics;
3. переписать DNS section на scheme-specific grammar, убрать `h3://`, добавить `system://`;
4. перенести tri-state Enabled contract на Store/API boundary;
5. убрать предложение удаления target перед rename;
6. добавить OpenAPI generation/drift и clean-worktree acceptance в verification.

После этих правок план будет достаточно конкретным и безопасным для передачи агенту.
