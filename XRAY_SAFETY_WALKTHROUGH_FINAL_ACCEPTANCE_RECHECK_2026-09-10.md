# Финальная повторная приемка Xray / Server Ingress walkthrough

Дата: 2026-09-10  
Документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Предыдущий отчёт: `XRAY_SAFETY_WALKTHROUGH_ACCEPTANCE_REVIEW_2026-09-10.md`

## Вердикт

Все пять замечаний предыдущего отчёта были адресованы в реализации и тестах. Тем не менее окончательная приемка **условно отклонена из-за одного оставшегося fail-open случая P0** в procfs-проверке. После его исправления и добавления негативного теста раздел можно закрывать без нового архитектурного пересмотра.

## Что исправлено корректно

- `ECONNREFUSED` при полностью недоступном procfs теперь возвращает `LegacyProbeConflict`.
- `Coordinator` получил внедряемые `procDir` и `dialTimeout`, поэтому production-ветка dial действительно тестируется.
- Timeout-error проходит через реальную ветку `probeLegacyState`, а не через готовую заглушку результата.
- Ошибка чтения `/proc/<pid>/exe` теперь приводит к `LegacyProbeConflict`.
- Удалено общее строковое совпадение по слову `refused`; оставлено только `connection refused` и проверка error chain через `syscall.ECONNREFUSED`.
- `shutdownIngressRuntime` возвращает объединённую ошибку через `errors.Join`, а shutdown hook пишет её в boot log.
- Проверка точного аргумента конфигурации и строгая обработка `DiscoverLegacy` сохранены.

## Оставшееся замечание

### P0 — найденный listening socket теряется, если inode невозможно сопоставить с PID

Файлы:

- `internal/serveringress/migration_saga.go:562-573`;
- `internal/serveringress/migration_saga.go:721-789`.

`findListeningPIDForAddressPort` возвращает только `int`. Значение `0` одновременно означает разные состояния:

1. в socket tables действительно нет подходящего listening socket;
2. socket найден, но `os.ReadDir(procDir)` завершился ошибкой;
3. socket найден, но каталог `/proc/<pid>/fd` недоступен;
4. socket найден, но inode не удалось сопоставить с PID из-за гонки или ограничений доступа.

В ветке `ECONNREFUSED` проверяется только `pid > 0`. Во всех остальных случаях код возвращает `LegacyProbeStopped`. Следовательно, если строка listening socket уже присутствует в `/proc/net/tcp`, но PID не найден, состояние ошибочно считается доказанно остановленным. Это нарушает заявленное правило «verified absent from procfs socket tables».

### Как исправить

Нужно перестать кодировать три состояния одним `int`. Например:

```go
type listenerLookup struct {
    SocketFound bool
    PID         int
}

func findListeningProcess(...) (listenerLookup, error)
```

Контракт:

- таблица не прочитана или обход procfs завершился существенной ошибкой — `error`, следовательно `LegacyProbeConflict`;
- socket отсутствует в успешно прочитанных релевантных таблицах — `SocketFound=false`, допускается `LegacyProbeStopped` при `ECONNREFUSED`;
- socket найден, PID не установлен — `SocketFound=true, PID=0`, обязательно `LegacyProbeConflict`;
- socket и PID найдены — продолжить проверку cmdline, config argv и executable.

Для IPv4 и IPv6 следует читать релевантную таблицу согласно адресу назначения. Наличие только `/proc/net/tcp` не должно считаться достаточной проверкой IPv6-слушателя, и наоборот.

### Обязательный тест

Создать fake procfs со следующими данными:

- `net/tcp` содержит LISTEN-строку для `127.0.0.1:443` и inode;
- соответствующего `/proc/<pid>/fd` symlink нет либо каталог PID недоступен;
- injected dialer возвращает `syscall.ECONNREFUSED`.

Ожидание: `LegacyProbeConflict`, а не `LegacyProbeStopped`.

Дополнительно нужен тест ошибки чтения socket table: путь существует, но прочитать его нельзя/это каталог. Результат также должен быть `LegacyProbeConflict`.

## Незначительное замечание к документации

Фраза walkthrough «all issues ... completely resolved» пока не соответствует фактическому fail-closed поведению из-за указанного P0. До исправления лучше заменить её на «previously reported issues were addressed; final acceptance pending».

## Выполненные проверки

### Go race detector

```text
go test -count=1 -race ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/api/... ./cmd/awg-manager
```

Результат: exit code 0; все перечисленные пакеты прошли.

### Linux ARM64 build

```text
GOOS=linux GOARCH=arm64 go build -o /tmp/awg-manager-audit2 ./cmd/awg-manager
```

Результат: exit code 0.

### Frontend

```text
npm exec -- svelte-check --threshold error
```

Результат: 0 ошибок, 118 предупреждений.

### Git diff check

```text
git diff --check
```

Результат: exit code 0. Выведены только предупреждения о будущей нормализации CRLF/LF; whitespace errors и conflict markers не найдены.

IPK не собирался, установка на роутеры не выполнялась.
