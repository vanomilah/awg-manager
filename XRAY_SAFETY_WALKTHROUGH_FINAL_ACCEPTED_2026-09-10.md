# Финальная приемка и закрытие аудита Xray / Server Ingress Walkthrough

Дата: 2026-09-10  
Документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Предыдущий отчёт: `XRAY_SAFETY_WALKTHROUGH_CLOSURE_RECHECK_2026-09-10.md`

---

## 1. Итоговый вердикт: ПРИНЯТО (ACCEPTED)

Все замечания аудита (P0, P1, P2), включая финальное замечание P1 по точной идентификации executable и `argv[0]`, полностью устранены, покрыты строгими негативными тестами и верифицированы полным набором проверок.

---

## 2. Устранение финального замечания P1

### Проблема
Ранее в `probeLegacyState` имя исполняемого файла и командная строка проверялись подстрокой:
```go
if !strings.Contains(cmdline, "xray") { ... }
if !strings.Contains(exeBase, "xray") { ... }
```
Это приводило к двум уязвимостям:
1. Исполняемые файлы с именами `notxray`, `xray-wrapper` или `fake-xray-daemon` ошибочно принимались за Xray.
2. Проверка всей строки `cmdline` подстрокой `"xray"` срабатывала для любого стороннего процесса (например, Python или shell), если ему передавался конфигурационный файл `/opt/etc/xray-cdn/config.json`, так как путь к конфигу сам содержит `"xray"`.

### Реализация
1. В пакет `internal/xrayserver/xraybin/resolver.go` добавлен единый exported helper:
   ```go
   // IsXrayExecutableName reports whether base is an exact supported Xray binary filename ("xray" or "xray.exe").
   func IsXrayExecutableName(base string) bool {
       lower := strings.ToLower(strings.TrimSpace(base))
       return lower == "xray" || lower == "xray.exe"
   }
   ```
   Этот же хелпер теперь используется и в resolver'е (`findOpkgBinary`), устраняя расхождения между поиском бинарников и проверкой миграции.

2. В `probeLegacyState` (`internal/serveringress/migration_saga.go`):
   - Проверка подстроки `strings.Contains(cmdline, "xray")` удалена.
   - Добавлена строгая проверка `argv[0]`:
     ```go
     rawArgs := bytes.Split(cmdlineData, []byte{0})
     if len(rawArgs) == 0 || len(rawArgs[0]) == 0 {
         return LegacyProbeConflict, fmt.Errorf("port %s pid %d cmdline is empty", target, pid)
     }
     argv0 := string(rawArgs[0])
     argv0Base := strings.ToLower(filepath.Base(argv0))
     if !xraybin.IsXrayExecutableName(argv0Base) {
         return LegacyProbeConflict, fmt.Errorf("port %s occupied by alien non-xray process (pid %d argv[0]: %q)", target, pid, argv0)
     }
     ```
   - Проверка симлинка `/proc/<pid>/exe` переведена на точное соответствие:
     ```go
     exeBase := strings.ToLower(filepath.Base(exe))
     if !xraybin.IsXrayExecutableName(exeBase) {
         return LegacyProbeConflict, fmt.Errorf("port %s pid %d executable %q does not match xray", target, pid, exe)
     }
     ```

---

## 3. Новые модульные и негативные тесты

1. **`internal/xrayserver/xraybin/resolver_test.go` — `TestIsXrayExecutableName`:**
   - Валидные имена: `"xray"`, `"xray.exe"`, `"XRAY"`, `"Xray.Exe"`, `"  xray  "` $\to$ `true`.
   - Невалидные имена: `"notxray"`, `"xray-wrapper"`, `"fake-xray-daemon"`, `"other"`, `"xray_bin"`, `"xray1"`, `""` $\to$ `false`.

2. **`internal/serveringress/saga_fault_test.go` — `TestMigrationSaga_ExecutableVerificationFailsClosed`:**
   - `/opt/bin/notxray` $\to$ `LegacyProbeConflict`, ошибка `"does not match xray"`.
   - `/opt/bin/xray-wrapper` $\to$ `LegacyProbeConflict`, ошибка `"does not match xray"`.
   - Сторонний процесс с cmdline `/usr/bin/python /opt/etc/xray-cdn/config.json` и exe `/opt/bin/other` $\to$ `LegacyProbeConflict`, ошибка `"alien non-xray process"`.
   - `/opt/bin/xray` $\to$ `LegacyProbeRunning`, ошибок нет.

---

## 4. Результаты воспроизводимых проверок

### 1. Go Race Detector
```bash
wsl -d Ubuntu bash -c "cd /mnt/e/AWGM/awg-manager && go test -count=1 -race ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/api/... ./cmd/awg-manager"
```
- `internal/tgwebproxy`: `ok` (22.698s)
- `internal/xrayserver`: `ok` (1.328s)
- `internal/xrayserver/xraybin`: `ok` (1.047s)
- `internal/cdndispatcher`: `ok` (1.080s)
- `internal/serveringress`: `ok` (1.646s) — все тесты пройдены
- `internal/api`: `ok` (4.172s)
- `cmd/awg-manager`: `ok` (1.328s)
- **Exit code: 0** (0 ошибок, 0 race warnings).

### 2. Linux ARM64 Cross-Compilation
```bash
wsl -d Ubuntu bash -c "cd /mnt/e/AWGM/awg-manager && GOOS=linux GOARCH=arm64 go build -o /tmp/awg-manager-audit4 ./cmd/awg-manager"
```
- **Exit code: 0** (чистая компиляция).

### 3. Frontend Typecheck
```bash
cmd.exe /c "npm exec -- svelte-check --threshold error"
```
- **Результат:** `0 errors and 118 warnings in 25 files` (**0 ошибок**).

### 4. Git Diff Check
```bash
git diff --check
```
- **Exit code: 0** (whitespace errors и conflict markers отсутствуют).

---

## 5. Заключение

Все требования по отказоустойчивости, целостности процессов и безопасной миграции Xray Server Ingress выполнены и проверены в полном объёме.
Аудит окончательно закрыт.
