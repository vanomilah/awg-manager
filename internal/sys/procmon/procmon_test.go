package procmon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseProcStat(t *testing.T) {
	tmpDir := t.TempDir()
	statFile := filepath.Join(tmpDir, "stat")
	content := "1351 (awg-manager) S 1 1351 1351 0 -1 4194304 3144 0 0 0 120 45 0 0 20 0 8 0 1234567 104857600 2560 18446744073709551615 0 0 0 0 0 0 0 2147483647 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	if err := os.WriteFile(statFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := parseProcStat(statFile)
	if err != nil {
		t.Fatalf("parseProcStat failed: %v", err)
	}

	if p.comm != "awg-manager" {
		t.Errorf("expected comm 'awg-manager', got %q", p.comm)
	}
	if p.state != "S" {
		t.Errorf("expected state 'S', got %q", p.state)
	}
	if p.utime != 120 {
		t.Errorf("expected utime 120, got %d", p.utime)
	}
	if p.stime != 45 {
		t.Errorf("expected stime 45, got %d", p.stime)
	}
	if p.threads != 8 {
		t.Errorf("expected threads 8, got %d", p.threads)
	}
	if p.rssBytes != 2560*4096 {
		t.Errorf("expected rssBytes %d, got %d", 2560*4096, p.rssBytes)
	}
}

func TestReadMemInfo(t *testing.T) {
	tmpDir := t.TempDir()
	memFile := filepath.Join(tmpDir, "meminfo")
	content := `MemTotal:         516096 kB
MemFree:          124500 kB
MemAvailable:     345000 kB
Buffers:           15000 kB
Cached:           220000 kB
SwapTotal:        262144 kB
SwapFree:         262144 kB
`
	if err := os.WriteFile(memFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mem, err := readMemInfo(memFile)
	if err != nil {
		t.Fatalf("readMemInfo failed: %v", err)
	}

	if mem.Total != 516096*1024 {
		t.Errorf("expected total %d, got %d", 516096*1024, mem.Total)
	}
	if mem.Available != 345000*1024 {
		t.Errorf("expected available %d, got %d", 345000*1024, mem.Available)
	}
	if mem.Used != (516096-345000)*1024 {
		t.Errorf("expected used %d, got %d", (516096-345000)*1024, mem.Used)
	}
}

func TestReadCPUStat(t *testing.T) {
	tmpDir := t.TempDir()
	statFile := filepath.Join(tmpDir, "stat")
	content := `cpu  1200 50 800 15000 300 20 50 0 0 0
cpu0 600 25 400 7500 150 10 25 0 0 0
cpu1 600 25 400 7500 150 10 25 0 0 0
intr 1234567
`
	if err := os.WriteFile(statFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cpus, err := readCPUStat(statFile)
	if err != nil {
		t.Fatalf("readCPUStat failed: %v", err)
	}

	if len(cpus) != 3 {
		t.Fatalf("expected 3 cpus (total, cpu0, cpu1), got %d", len(cpus))
	}
	if cpus["total"].user != 1200 {
		t.Errorf("expected user 1200, got %d", cpus["total"].user)
	}
	if cpus["cpu0"].idle != 7500 {
		t.Errorf("expected idle 7500, got %d", cpus["cpu0"].idle)
	}
}

// Замер не чаще minSampleInterval, как кнопки интервала в панели (5с/30с):
// повторный запрос раньше получает тот же снимок, без прохода по /proc.
func TestSnapshot_NotMoreOftenThanMinInterval(t *testing.T) {
	s := NewSampler()
	s.procDir = t.TempDir()

	first, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.Snapshot()
	if again != first {
		t.Fatal("повторный замер раньше 5 с должен вернуть прошлый снимок")
	}

	s.lastSample = time.Now().Add(-minSampleInterval)
	fresh, _ := s.Snapshot()
	if fresh == first {
		t.Fatal("по истечении интервала ждали новый замер")
	}
}

// Снятие процесса сбрасывает снимок: панель обновляет список сразу после
// kill, и снятый процесс не должен висеть живым до конца интервала.
func TestKillProcess_DropsCachedSnapshot(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("нет sleep: " + err.Error())
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	s := NewSampler()
	s.procDir = t.TempDir()
	first, _ := s.Snapshot()
	if err := s.KillProcess(cmd.Process.Pid, "SIGKILL"); err != nil {
		t.Fatalf("KillProcess: %v", err)
	}
	if after, _ := s.Snapshot(); after == first {
		t.Fatal("после снятия процесса ждали новый замер, а не прошлый снимок")
	}
}

// fakeProc кладёт в каталог /proc-подобные stat (система) и stat+status одного
// процесса PID 100.
func fakeProc(t *testing.T, sysStat, pidStat, pidStatus string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(sysStat), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte("MemTotal: 262144 kB\nMemAvailable: 131072 kB\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pd := filepath.Join(dir, "100")
	if err := os.Mkdir(pd, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "stat"), []byte(pidStat), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "status"), []byte(pidStatus), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// utime/stime задаются полями 14/15 — остальное как у настоящего stat.
func pidStatLine(utime, stime int) string {
	return fmt.Sprintf("100 (sing-box) S 1 100 100 0 -1 0 0 0 0 0 %d %d 0 0 20 0 8 0 1 1000 50 0\n", utime, stime)
}

const fourCores = `cpu  %d 0 0 %d 0 0 0 0 0 0
cpu0 0 0 0 0 0 0 0 0 0 0
cpu1 0 0 0 0 0 0 0 0 0 0
cpu2 0 0 0 0 0 0 0 0 0 0
cpu3 0 0 0 0 0 0 0 0 0 0
`

// CPU % — доля всего процессора: процесс, занявший одно ядро из четырёх,
// это 25 %, а не 100 % (раньше доля умножалась на число ядер и резалась на 100).
func TestSnapshot_CPUPercentIsShareOfAllCores(t *testing.T) {
	dir := fakeProc(t,
		fmt.Sprintf(fourCores, 400, 1200), // за интервал: 400 active + 1200 idle = 1600 тиков на 4 ядра
		pidStatLine(300, 100),             // процесс: +400 тиков = одно ядро целиком
		"Uid:\t0\t0\t0\t0\n")

	s := NewSampler()
	s.procDir = dir
	s.lastSample = time.Now().Add(-5 * time.Second) // свежая база, без прогрева
	s.lastCPUs = map[string]cpuSample{"total": {}}
	s.lastProcCPUs = map[int]uint64{100: 0}

	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Processes) != 1 {
		t.Fatalf("процессов %d, ждали 1", len(snap.Processes))
	}
	p := snap.Processes[0]
	if p.CPUPercent != 25 {
		t.Errorf("CPUPercent = %v, ждали 25 (одно ядро из четырёх)", p.CPUPercent)
	}
	if p.CPUTimeSec != 4 {
		t.Errorf("CPUTimeSec = %v, ждали 4 (400 тиков по 1/100 с)", p.CPUTimeSec)
	}
	if snap.CPUCount != 4 {
		t.Errorf("CPUCount = %d, ждали 4", snap.CPUCount)
	}
}

// Без свежей базы снимок берёт базу сам и ждёт warmup: первый кадр
// показывает загрузку за паузу, а не пустоту и не среднее с включения роутера.
// Файлы меняются посреди паузы — так видно, что дельта считается от базы.
func TestSnapshot_FirstFrameMeasuresWarmup(t *testing.T) {
	dir := fakeProc(t, fmt.Sprintf(fourCores, 5000, 5000), pidStatLine(300, 100), "Uid:\t0\t0\t0\t0\n")

	s := NewSampler()
	s.procDir = dir
	s.warmup = 300 * time.Millisecond

	go func() {
		time.Sleep(100 * time.Millisecond)
		// +400 active, +1200 idle; процесс +400 тиков — одно ядро из четырёх
		_ = os.WriteFile(filepath.Join(dir, "stat"), []byte(fmt.Sprintf(fourCores, 5400, 6200)), 0644)
		_ = os.WriteFile(filepath.Join(dir, "100", "stat"), []byte(pidStatLine(700, 100)), 0644)
	}()

	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if u := snap.Cores[0].Usage; u != 25 {
		t.Errorf("total Usage = %v, ждали 25 (загрузка за паузу)", u)
	}
	if p := snap.Processes[0].CPUPercent; p != 25 {
		t.Errorf("CPUPercent = %v, ждали 25", p)
	}
}

// Память процесса делится на свою (RssAnon+RssShmem) и файловую (RssFile);
// процент считается от своей.
func TestSnapshot_MemorySplit(t *testing.T) {
	dir := fakeProc(t, fmt.Sprintf(fourCores, 0, 100), pidStatLine(0, 0),
		"Name:\tsing-box\nUid:\t65534\t65534\t65534\t65534\nVmRSS:\t   27364 kB\nRssAnon:\t    4324 kB\nRssFile:\t   23028 kB\nRssShmem:\t      12 kB\n")

	s := NewSampler()
	s.procDir = dir
	s.warmup = 0

	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	p := snap.Processes[0]
	if p.MemoryOwn != (4324+12)*1024 {
		t.Errorf("MemoryOwn = %d, ждали %d", p.MemoryOwn, (4324+12)*1024)
	}
	if p.MemoryFile != 23028*1024 {
		t.Errorf("MemoryFile = %d, ждали %d", p.MemoryFile, 23028*1024)
	}
	if p.MemoryPercent != 1.6 { // 4336 КБ от 262144 КБ = 1.65 %, отсечка до десятых
		t.Errorf("MemoryPercent = %v, ждали 1.6", p.MemoryPercent)
	}
	if p.User != "nobody" {
		t.Errorf("User = %q, ждали nobody", p.User)
	}
}
