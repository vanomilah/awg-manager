# Повторное ревью Revised Gate 1 Plan

Дата: 2026-09-16  
Файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План значительно улучшен и теперь охватывает почти все необходимые подсистемы. Однако **реализацию пока следует оставить Gemini Pro**. До начала кодинга нужно устранить несколько архитектурных неоднозначностей, одна из которых критична: описанный read/compare/write не является настоящим CAS при конкурирующих процессах без механизма сериализации владельца.

Статус: **почти одобрено; требуется короткая финальная редакция Phase A, C и E**.

## Что теперь принято

- Descriptor-relative Linux `SecureDir` описан правильно на уровне архитектуры.
- Присутствуют immutable current/next и ambiguous outcome resolver.
- Candidate bundle публикуется до destructive swap.
- Учитывается post-mutation snapshot и `RuntimeOff` без config.
- Bridge operations и durable cleanup journal возвращены в scope.
- Protected set для GC стал достаточно полным.
- Linux failpoint/crash/restart matrix включен в приемку.
- Сборка IPK и деплой корректно запрещены до прохождения тестов.

## Что обязательно уточнить до кодинга

### 1. Межпроцессная сериализация CAS

Последовательность `read current -> compare -> atomic rename` не предотвращает одновременную запись двумя процессами: оба могут прочитать один Sequence и затем по очереди успешно заменить файл.

План должен выбрать и закрепить один механизм владения транзакцией:

- advisory lock на отдельном lock-файле с удержанием fd на всю критическую секцию; либо
- единственный coordinator process плюс доказуемый запрет остальных writers; либо
- другой Linux-механизм, обеспечивающий эксклюзивного writer.

Lock должен иметь определенную семантику после crash, не удаляться как признак освобождения и покрывать initial manifest creation, CAS, pointer update и recovery decision. Внутрипроцессного `sync.Mutex` недостаточно.

### 2. Полная таблица переходов, а не только список состояний

Нужно добавить явную таблицу `from -> allowed to` для:

- `idle` и первого создания manifest;
- нормального forward path;
- cancel до commit boundary;
- failure до commit boundary;
- failure после commit boundary;
- restart/recovery из каждого intent/applied состояния;
- rollback success/failure;
- terminal cleanup и возврата в `idle`.

Запись вида `snapshot_secured / prepared` неоднозначна: это два последовательных состояния или альтернативные имена. Необходимо оставить одно точное имя и порядок.

### 3. Bridge state не следует моделировать одним глобальным состоянием на операцию

При нескольких bridges глобальная цепочка `bridge_create_intent -> applied -> verified` теряет прогресс отдельных операций. Основной manifest может находиться в общей фазе `bridges_reconciling`, а каждый `BridgeOperation` должен иметь собственные:

- operation ID;
- action;
- target/spec digest;
- state;
- attempts/last error;
- observed result.

Recovery обязан продолжать незавершенные операции, не повторяя уже verified операции с побочными эффектами.

### 4. Отсутствует точный финальный commit protocol

После bridge lifecycle план должен явно задать порядок:

1. убедиться, что candidate generation, runtime и bridges verified;
2. durable `commit_intent`;
3. обновить verified-active record;
4. обновить active/current generation pointer;
5. перечитать и проверить оба durable объекта;
6. durable `committed`;
7. создать/обновить cleanup journal;
8. выполнить cleanup;
9. удалить terminal manifest только после подтвержденного cleanup;
10. перейти в `idle`.

Нужно определить canonical source of truth при crash между пунктами 3–6. Обновление двух файлов не атомарно, поэтому recovery rule обязателен.

### 5. `RuntimeOff` должен быть отдельным алгоритмом в плане

Фраза в скобках недостаточна. Нужно перечислить его полный порядок, включая candidate bundle, config deletion intent, stop verification, bridge withdrawal, commit и recovery после каждого шага. Это предотвратит возврат текущей ошибочной сокращенной ветки `applyRuntimeOffLocked`.

### 6. Initial creation и damaged manifest

CAS-раздел должен определить:

- отсутствие manifest при первой транзакции;
- существующий terminal manifest;
- битый JSON;
- неизвестную schema version;
- валидный manifest другого TxID;
- manifest с большей Sequence;
- stale lock holder после crash.

Fail-open поведение запрещено: неоднозначность должна приводить к `recovery_required`, а не к новой транзакции поверх старой.

### 7. Acceptance дополнить конкурентными тестами

Добавить:

- два отдельных writer/process на одном manifest;
- simultaneous initial create;
- stale Sequence;
- crash владельца lock;
- retry одного и того же `next`;
- crash между verified-active и pointer update;
- несколько bridge operations с partial success;
- поврежденный manifest/pointer/generation bundle.

## Разделение между Pro и Flash

### Оставить на Pro

- финальную таблицу state machine;
- lock/ownership и настоящий CAS;
- schema и commit protocol;
- recovery/rollback rules;
- Linux strictfs;
- RuntimeOn/RuntimeOff transaction flow;
- bridge reconciliation architecture.

### Можно будет передать Flash позже

Только после того, как Pro реализует перечисленное и базовые тесты станут зелеными:

- добавление однотипных failpoint test cases по готовой таблице;
- расширение fixture/builders;
- механическое устранение `_ =` по заранее определенной политике ошибок;
- документация;
- форматирование и простые локальные исправления;
- UI после полной backend-приемки.

## Указание агенту

Внести семь уточнений выше в `implementation_plan.md`, затем выполнять Phases A–F на Pro. Не переключаться на Flash во время архитектурной реализации. После зеленых базовых Linux-тестов показать `walkthrough.md` и фактический diff для отдельного решения о переключении.
