# Аудит `walkthrough.md`: Xray Full Management, Phase 0/1

Дата проверки: 2026-09-11  
Репозиторий: `E:\AWGM\awg-manager`  
Проверенный отчёт: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
SHA-256 отчёта: `C328EC8D6552D4A4DFDC969C321532312201A96ADA123EA5B3D82F5BD9C984D3`

## Вердикт

Заявление отчёта «Phase 0/1 реализованы на 100%» **не подтверждается**. В коде действительно появился большой каркас: модель конфигурации, parser/compiler/validator, generation store, transaction state machine, REST API и клиентские TypeScript-типы. Но основной пользовательский сценарий — импортировать/создать полноценный Xray-профиль и безопасно применить именно его — сейчас не работает.

Реализацию **нельзя считать готовой к деплою или приёмке**. Ниже перечислены блокирующие дефекты.

## Критические дефекты

### P0. Скомпилированный профиль не применяется

`ApplyProfile` компилирует профиль в `candidateJSON`, но затем этот результат не используется (`internal/api/xray_profiles.go:555-603`). В `PrepareCandidate` передаётся старая ограниченная `xrayserver.Config`, полученная через `GetConfig`; из управляемого профиля в неё копируются только `Enabled`, адрес и порт первого inbound.

Следствия:

- outbounds, routing, balancers, несколько inbounds, transport/security, REALITY/TLS и raw overlay не попадают в runtime;
- клиентский профиль и серверный профиль фактически сводятся к одному legacy server config;
- API может вернуть `success: true`, хотя Xray запущен не с тем JSON, который пользователь импортировал или отредактировал.

Требуется отдельный транзакционный путь применения готового Xray `Document`/JSON, с `xray run -test`, атомарной заменой runtime-файла, рестартом, проверкой здоровья и откатом. Нельзя оставлять `candidateJSON` заглушкой `_ = candidateJSON`.

### P0. SecretStore не подключён к профилям, секреты сохраняются открытым текстом

`SecretRef` и `DiskSecretStore` существуют, но create/update/import профилей их не используют. `ManagedConfig` по-прежнему содержит строковые `UUID`, `Password`, `Reality.PrivateKey`; `SaveProfile` сериализует весь объект прямо в `managed.json` (`internal/xrayserver/profile_store.go:124-134`). Импорт дополнительно сохраняет исходный документ в `raw.json`.

Это противоречит заявлению walkthrough о «секретах отдельно от managed.json» и делает SecretStore практически мёртвым кодом.

Требуется:

1. определить реальные secret-bearing поля;
2. при create/import/update атомарно stage-ить их в SecretStore и заменять в профиле ссылками;
3. разрешать ссылки только при private export и runtime compile;
4. коммитить/откатывать профиль и секреты одной транзакцией;
5. мигрировать уже записанные plaintext generations.

### P0. Обычный GET профиля раскрывает исходный raw config

`GetProfile` возвращает `RawOverlay: string(stored.RawOverlay)` (`internal/api/xray_profiles.go:263-292`). Для импортированного профиля `raw.json` — это полный исходный Xray JSON, который может содержать UUID, passwords, REALITY private key, TLS key material и токены. Аналогично create/update немедленно возвращают присланный `raw_overlay` без редактирования.

Следовательно, защищённый POST `export-private` теряет смысл: секреты уже доступны через обычный GET/detail response. Redacted export редактирует только `ManagedConfig`, но не обеспечивает безопасную работу с raw overlay.

Требуется никогда не возвращать raw overlay в обычных DTO. Для просмотра — отдельный POST private-export с `no-store`, явным подтверждением и полной авторизацией; redacted-вариант должен рекурсивно редактировать именно итоговый документ.

### P0. `validateID` разрешает `.` и `..`, возможен выход за корень и рекурсивное удаление

Общий `validateID` допускает идентификатор, состоящий только из точек (`internal/xrayserver/secret_store.go:69-78`). Затем пути строятся обычным `filepath.Join`.

Особенно опасные случаи:

- `DeleteProfile(".")` может вызвать `RemoveAll` для корня profile store;
- `DeleteProfile("..")` указывает на родительский каталог;
- `RollbackTx("..")` строит `staging/..` и может удалить корень SecretStore;
- аналогичный риск есть у generation/profile/transaction path helpers.

Тест с `../escape` недостаточен: он проверяет только запрещённый slash, но не `.`/`..` и не каноническое нахождение итогового пути внутри root.

Требуется запретить `.`/`..`, начинать ID с alphanumeric, применять строгую regexp и после `filepath.Abs/Clean` проверять containment через `filepath.Rel`. Для destructive операций нужны отдельные отрицательные тесты.

## Высокий приоритет

### P1. «Транзакционный» SecretStore допускает частичный commit

`CommitTx` переносит секреты по одному. При ошибке посередине часть уже оказывается в active, часть остаётся staging; автоматического обратного отката и durable manifest нет (`internal/xrayserver/secret_store.go:179-229`). Это не атомарная транзакция и может рассинхронизировать профиль и секреты.

Нужен generation directory + один атомарно переключаемый указатель/manifest либо полноценный журнал с recovery.

### P1. Raw overlay сохраняется, но не участвует в компиляции

Импорт кладёт оригинал в `raw.json`, однако `Compile(stored.Managed)` вызывается без base document/raw overlay и для preview, и для export, и для apply. Поэтому обещанное сохранение неизвестных полей не гарантируется. `CompileWithBase` существует, но рабочий API его не вызывает.

Нужно формально определить семантику raw: base document или overlay; парсить его, применять детерминированный merge, валидировать конфликты и использовать одинаковый pipeline для preview/export/apply.

### P1. Rollback меняет только pointer, но не активный runtime

`RollbackProfile` вызывает лишь `RollbackToGeneration` и сразу возвращает success (`internal/api/xray_profiles.go:627-653`). Запущенный Xray и `config.json` при этом не меняются.

Если rollback означает только выбор версии в редакторе, API/UI должны так и называться. Если это operational rollback, после pointer switch нужен тот же безопасный apply pipeline; при неудаче pointer также должен быть восстановлен.

### P1. Определение capabilities выдаёт поддержку при отсутствии доказательств

При неизвестном формате `xray version` inspector выставляет XHTTP и REALITY в `true`; endpoint делает то же при любой ошибке запуска бинарника (`internal/xrayserver/xraybin/inspector.go:45-53`, `internal/api/xray_profiles.go:656-675`). Это fail-open поведение: UI разрешит функции отсутствующего/несовместимого бинарника.

Нужно возвращать `known:false`, ошибку discovery и capabilities `false/unknown`. `SupportsMuxCool` также нельзя безусловно ставить `true`.

### P1. В UI отсутствует заявленное полноценное управление профилями

Добавлены `frontend/src/lib/types/xray.ts`, `frontend/src/lib/api/clientXray.ts` и методы общего API-клиента, но `XrayServerCard.svelte` не использует profile CRUD/import/export/preview/apply/rollback API. Поиск по Svelte-компонентам не обнаружил рабочего profile manager.

То есть frontend-часть — транспортный клиент, а не конечный интерфейс «полного управления Xray». Пользователь после деплоя не увидит заявленную массу настроек.

### P1. Возможная взаимная блокировка в `ListProfiles`

`ListProfiles` удерживает `RLock`, затем вызывает `GetActiveProfile`, который повторно берёт `RLock` (`internal/xrayserver/profile_store.go:291-315`). Повторный read-lock может зависнуть, если между ними writer уже ожидает lock: Go `RWMutex` блокирует новых readers ради writer, а внешний read-lock не будет освобождён.

Нужно вынести внутренний `getActiveProfileLocked` без повторного lock либо собрать список ID под lock и читать их после освобождения.

## Средний приоритет

### P2. Ошибочная классификация HTTP-ошибок и слабая защита API-контрактов

- invalid profile ID и повреждённое хранилище часто превращаются в 404/500 без стабильных error codes;
- delete/update не различают отсутствие профиля и успех;
- private export test проверяет метод и заголовки, но не доказывает отсутствие тех же секретов в остальных ответах;
- отсутствуют acceptance-тесты «import -> preview -> apply -> runtime JSON byte/semantic equality -> restart -> rollback».

### P2. Контексты почти не используются хранилищами

Методы принимают `context.Context`, но файловые циклы и операции его не проверяют. Для небольших файлов это терпимо, однако контракт вводит в заблуждение и не даёт отменять долгие list/recovery/import операции.

### P2. Текст redactor повреждён кодировкой

`MaskSecret` возвращает строку вида `вЂўвЂў...` вместо корректного символа маски. Это заметный UI/API-дефект и признак смешения UTF-8/Windows encoding.

## Что действительно реализовано

- Файлы и основные типы Phase 0/1 присутствуют.
- REST routes `/api/servers/xray/profiles...`, capabilities и recovery зарегистрированы в сервере.
- Generation store пишет файлы во временно сформированное поколение и атомарно обновляет JSON pointer `active.json`; это именно JSON-указатель, не symlink.
- Legacy `xrayserver.Config` имеет развитую transaction state machine и recovery.
- POST private-export отклоняет GET через router и выставляет `Cache-Control: no-store, private` и `Pragma: no-cache`.
- Targeted package tests `internal/xrayconfig`, `internal/xrayserver`, `internal/xrayserver/xraybin` проходят.

Эти элементы полезны как основа, но пока не соединены в корректный end-to-end продукт.

## Независимая проверка тестов

Команда:

```powershell
$env:GOCACHE='E:\AWGM\awg-manager\.gocache-audit'
go test ./internal/xrayconfig ./internal/xrayserver/... ./internal/api
```

Результат:

- `internal/xrayconfig` — PASS;
- `internal/xrayserver` — PASS;
- `internal/xrayserver/xraybin` — PASS;
- `internal/api` — не собран на Windows из-за существующих Linux-only зависимостей (`awgmproto.FrameConn`, `unix.UnixRights`, `syscall.Kill`, `SysProcAttr.Setsid`). Это не доказательство дефекта Xray API, но утверждение «все internal Go tests проходят» в текущей среде независимо не подтверждено.

`git diff --check` также не чист: обнаружена лишняя пустая строка в конце `frontend/src/lib/api/clientServers.ts:695`. Полный frontend suite в этом аудите не перезапускался; наличие 1919 passing tests в walkthrough принято только как заявление автора отчёта, не как независимое подтверждение.

## Минимальный порядок исправления

1. Немедленно закрыть path traversal/destructive ID cases и добавить regression tests для `.`, `..`, encoded variants и containment.
2. Убрать `raw_overlay` из обычных API-ответов и проверить canary-секретом все GET/redacted endpoints.
3. Спроектировать единый canonical compile pipeline: managed + raw/base + secret resolution -> validate -> `xray run -test` -> transaction apply.
4. Переделать ApplyProfile так, чтобы runtime получал именно скомпилированный документ; добавить end-to-end semantic equality test.
5. Реально интегрировать SecretStore либо удалить ложную абстракцию до корректной реализации. Сделать атомарный commit generations профиля и секретов.
6. Исправить rollback runtime и fail-closed capabilities.
7. Исправить nested `RLock` и добавить конкурентный тест с ожидающим writer.
8. Только после backend acceptance tests делать UI профилей и редактор.
9. После исправлений прогнать Linux/WSL Go suite, frontend `npm test`, `npm run check`, `git diff --check`; IPK и роутеры не трогать без отдельного решения.

## Критерий повторной приёмки

Phase 0/1 можно признать завершёнными только когда автоматический тест докажет весь сценарий:

`import full Xray JSON with canary secrets and unknown fields -> no secret in normal API/storage managed snapshot -> preview matches candidate -> apply candidate -> runtime config semantically equals private export -> restart survives -> rollback changes runtime -> injected failure restores prior runtime/profile/secrets`.

До этого статус корректнее обозначать как **backend prototype / incomplete integration**, а не 100% завершение.
