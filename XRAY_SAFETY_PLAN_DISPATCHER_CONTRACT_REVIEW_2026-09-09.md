# Проверка контрактов dispatcher в плане Xray Safety Foundation

Дата: 2026-09-09  
Проверен файл:
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Итог

План закрыл предыдущие архитектурные замечания: введена topology-aware migration,
межкомпонентный coordinator, неразрушающий `keep_legacy` и разделение источников
бинарника. До реализации осталось согласовать реальные контракты dispatcher и сделать
межкомпонентный commit восстанавливаемым после аварийного завершения процесса AWG Manager.

## P0: фактический dispatcher не имеет маршрута `unknown -> 404`

Текущий `internal/cdndispatcher/dispatcher.go` работает так:

```text
path с Xray prefix → Xray target
любой другой path  → Telegram target
```

План требует одновременно:

- проверить Telegram prefix;
- проверить неизвестный path как `404`.

Это другой routing contract. Для Telegram Web Proxy могут быть значимы корневой путь,
query parameters и `/api/v1`; простое добавление default `404` способно нарушить рабочие
подключения.

До реализации определить и зафиксировать таблицу маршрутов:

| Условие | Target |
|---|---|
| Нормализованный Xray path prefix | Xray |
| Поддерживаемый Telegram Web Proxy path/query | Telegram worker |
| Health probe path | Внутренний ответ dispatcher |
| Остальные запросы | Явно выбранная политика: Telegram fallback или 404 |

Политика должна быть совместима с фактическим Telegram-клиентом и покрыта
интеграционными тестами. Нельзя изменить существующий fallback на `404` только ради
удобства тестирования.

## P0: `Dispatcher.Start()` сейчас сообщает успех до bind

Текущий dispatcher устанавливает `running=true`, запускает `ListenAndServe()` в goroutine
и немедленно возвращает `nil`. Ошибка занятого порта возникает позже только в логе.
Транзакционный coordinator не сможет определить, что candidate dispatcher не запустился,
и ошибочно выполнит commit.

Изменить lifecycle dispatcher:

```text
net.Listen(candidate.ListenAddr) синхронно
  → ошибка bind возвращается вызывающему коду
  → сохранить listener и подтверждённый bound address
  → запустить http.Server.Serve(listener) в единственной lifecycle goroutine
  → публиковать broadcast exit state и сохранённую ошибку
```

`running=true` выставляется только после успешного `net.Listen`. `Reconfigure` должен
возвращать структурированный результат и поддерживать rollback предыдущего listener.

## P0: межкомпонентному commit нужен persistent transaction journal

Rollback, выполняемый только в памяти, не помогает, если AWG Manager завершится между:

- остановкой legacy Xray;
- сменой dispatcher;
- запуском нового Xray;
- записью одного из settings-файлов;
- фиксацией runtime decision.

Coordinator должен вести атомарный journal `server-ingress-transaction.json` с правами
`0600`:

```text
transaction_id
phase
previous fingerprints and paths
candidate fingerprints and paths
previous active generation
components already changed
created backups
```

Journal записывается и синхронизируется перед первой мутацией и после каждой критической
фазы. При следующем старте coordinator сначала выполняет recovery незавершённой
транзакции, и только затем рассматривает новую миграцию. Journal не содержит UUID,
секреты или полные конфигурации — только ссылки на защищённые snapshots и fingerprints.

Добавить crash-point tests после каждой фазы и подтвердить восстановление после нового
создания приложения.

## P1: internal Xray listener требует явного `ListenAddress`

План требует для новой схемы:

```text
Xray слушает 127.0.0.1:Q
```

Текущая модель `xrayserver.Config` хранит только `ListenPort`, а runtime renderer жёстко
создаёт `listen: 0.0.0.0`. Одной смены порта недостаточно.

Добавить типизированное поле, например `ListenAddress`, с безопасными правилами:

- для dispatcher topology default `127.0.0.1`;
- внешний bind допускается только в явно выбранном direct-сценарии;
- адрес участвует в validation, runtime rendering, listener ownership и status;
- миграция не меняет доступность direct-сценария молча;
- wildcard bind показывается в preview как отдельное изменение области доступа.

Проверки ownership должны учитывать пару `address:port`, а не только port.

## P1: route probe должен быть служебным и не имитировать XHTTP без credentials

Обычный HTTP-запрос на Xray path не обязательно доказывает работоспособность VLESS/XHTTP:
корректный протокол может отвергнуть запрос без валидной сессии. Нельзя считать любой
HTTP status ни гарантированным успехом, ни гарантированной ошибкой Xray.

Разделить readiness:

1. PID/start-time/binary/config identity Xray;
2. ownership внутреннего `address:port` процессом Xray;
3. проверка routing decision dispatcher через отдельный внутренний matcher/test API;
4. опциональный end-to-end protocol probe тестовым одноразовым credential только в
   изолированной проверке, без журналирования и сохранения;
5. проверка Telegram route собственным безопасным health contract worker.

Служебный health path dispatcher не должен пересекаться с пользовательскими Xray и
Telegram paths и не должен быть доступен с WAN без необходимости.

## P1: coordinator не должен считать Telegram частью каждой Xray-транзакции

В плане snapshot включает Telegram ingress, хотя обычное изменение клиента Xray не должно
перезапускать Telegram worker. Нужна таблица affected components:

- изменение Xray client UUID без topology change → только Xray runtime;
- изменение внутреннего Xray target → Xray + dispatcher;
- изменение публичного dispatcher port → dispatcher и route checks, Telegram worker не
  перезапускается;
- изменение Telegram target/config → Telegram worker + dispatcher только при изменении
  target;
- legacy topology migration → coordinator управляет всеми фактически затронутыми
  компонентами.

Snapshots можно снимать шире, но останавливать и восстанавливать следует только изменённые
компоненты. Это уменьшает разрыв Telegram Proxy и риск каскадного rollback.

## P1: нейтральная терминология нарушена в самом плане

В разделе topology A осталось название конкретного облачного сервиса. Заменить его на:

```text
чтобы не требовалось менять origin выбранного CDN
```

Провести lint пользовательских строк, comments, defaults и migration reports. Также
текущий dispatcher содержит персональный hostname default; его необходимо заменить
пустым безопасным значением и требовать настройку через typed config.

## P2: источник `SourceManaged` нельзя определять только по расположению файла

Наличие бинарника в `/opt/bin` или `/opt/sbin` не доказывает, что им владеет AWG Manager.
`SourceManaged` допустим только при наличии валидного AWG-owned manifest с:

- canonical path;
- checksum;
- architecture;
- installed version;
- installer generation/source;
- timestamp.

Без manifest такой бинарник классифицируется как `SourceExternal`. Это исключает удаление
пользовательского файла, случайно найденного в стандартном каталоге.

## Дополнительные тесты

Добавить:

1. occupied dispatcher port заставляет `Start()` синхронно вернуть ошибку;
2. `running` не становится true при bind failure;
3. Telegram root/query route сохраняет фактическую совместимость;
4. unknown route соответствует явно выбранной политике;
5. internal health path не доступен извне либо защищён;
6. crash после каждой фазы coordinator transaction восстанавливается по journal;
7. Xray в dispatcher topology слушает только `127.0.0.1:Q`;
8. direct topology сохраняет ожидаемую область bind;
9. изменение только Xray clients не перезапускает dispatcher/Telegram;
10. смена dispatcher port не перезапускает Telegram worker;
11. отсутствие managed manifest классифицирует бинарник как external;
12. repository terminology lint не находит запрещённые пользовательские формулировки.

## Критерий допуска

План можно запускать после добавления:

1. формальной таблицы маршрутов dispatcher без нарушения Telegram compatibility;
2. синхронного bind и наблюдаемого lifecycle dispatcher;
3. persistent coordinator transaction journal и startup recovery;
4. поля `ListenAddress` и проверки ownership по адресу и порту;
5. корректной многоуровневой readiness без ложного XHTTP probe;
6. affected-components orchestration;
7. нейтральной терминологии и удаления персонального hostname default;
8. manifest-only классификации `SourceManaged`.

После этих уточнений план будет готов к реализации без дополнительных архитектурных
изменений Stage 1.
