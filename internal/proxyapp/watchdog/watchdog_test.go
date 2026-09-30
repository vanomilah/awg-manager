package watchdog

import (
	"context"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/awgmproto"
	"github.com/hoaxisr/awg-manager/internal/proxyrt/instancestore"
	"github.com/hoaxisr/awg-manager/internal/proxyrt/roles"
)

type fakeManager struct {
	recs     []instancestore.Record
	restarts []struct {
		Key    string
		Reason string
	}
}

func (f *fakeManager) Records() []instancestore.Record { return f.recs }
func (f *fakeManager) Restart(_ context.Context, key, reason string) error {
	f.restarts = append(f.restarts, struct {
		Key    string
		Reason string
	}{Key: key, Reason: reason})
	return nil
}

type fakeJournal struct {
	infos []string
	warns []string
}

func (j *fakeJournal) Info(action, target, message string) {
	j.infos = append(j.infos, action+":"+target+":"+message)
}
func (j *fakeJournal) Warn(action, target, message string) {
	j.warns = append(j.warns, action+":"+target+":"+message)
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"on_failure", 0},
		{"30m", 30 * time.Minute},
		{"1h", 1 * time.Hour},
		{"", 1 * time.Hour},
		{"2h", 2 * time.Hour},
		{"4h", 4 * time.Hour},
		{"12h", 12 * time.Hour},
		{"15m", 15 * time.Minute},
	}
	for _, tc := range tests {
		got := ParseInterval(tc.in)
		if got != tc.want {
			t.Errorf("ParseInterval(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDetectFailure(t *testing.T) {
	logWith401 := `
2026/09/28 10:00:00 [STREAM 200] Auth error (cache=20, count=3/3)
2026/09/28 10:00:01 TURN Allocate: Allocate error response (error 401: Unauthorized)
2026/09/28 10:00:02 some other line
`
	sig, found := DetectFailure(logWith401)
	if !found {
		t.Fatal("DetectFailure: expected failure signature found")
	}
	if sig != "TURN Allocate: Allocate error" && sig != "error 401: Unauthorized" {
		t.Fatalf("unexpected signature: %s", sig)
	}

	cleanLog := `
2026/09/28 10:00:00 Started listening on 127.0.0.1:9000
2026/09/28 10:00:01 Traffic flowing normally
`
	if _, foundClean := DetectFailure(cleanLog); foundClean {
		t.Fatal("DetectFailure: expected no failure in clean log")
	}

	freeTurnAckErrorLog := `
2026/09/30 06:10:00 [session=5cb368ec] [STREAM 5] DTLS: failed to write client ID: clientsdb: server did not acknowledge client ID (server outdated?) - повтор через 17s
`
	sig, foundAck := DetectFailure(freeTurnAckErrorLog)
	if !foundAck || sig != "server did not acknowledge client ID" {
		t.Fatalf("DetectFailure: expected 'server did not acknowledge client ID', got %v (found=%v)", sig, foundAck)
	}

	// Проверяем, что спам статистики WDTT ([СТАТИСТИКА]) не скрывает ошибку, случившуюся ранее
	wdttSpamLog := "2026/09/30 06:00:00 [WARN] all streams down\n"
	for i := 0; i < 40; i++ {
		wdttSpamLog += "2026/09/30 06:01:00 [СТАТИСТИКА] Активных: 0 | Трафик: 0.00 МБ\n"
	}
	sig, foundSpam := DetectFailure(wdttSpamLog)
	if !foundSpam || sig != "all streams down" {
		t.Fatalf("DetectFailure with stats spam: expected 'all streams down', got %v (found=%v)", sig, foundSpam)
	}
}

func TestWatchdogCheck_LogFailure(t *testing.T) {
	mgr := &fakeManager{
		recs: []instancestore.Record{
			{
				ID:   "c1",
				Kind: instancestore.KindWdttClient,
				Name: "Client 1",
				Enabled: true,
				WdttClient: &roles.WdttClientConfig{
					AutoReconnect:         true,
					AutoReconnectInterval: "on_failure",
				},
			},
		},
	}
	jrnl := &fakeJournal{}
	now := time.Now()

	wd := New(Deps{
		Manager: mgr,
		Snapshot: func(key string) (awgmproto.State, bool) {
			return awgmproto.State{PID: 1234, UptimeS: 100}, true
		},
		LogTail: func(key string) string {
			return "TURN Allocate: Allocate error response (error 401: Unauthorized)"
		},
		Journal: jrnl,
		Now:     func() time.Time { return now },
	})

	wd.Check(context.Background())

	if len(mgr.restarts) != 1 {
		t.Fatalf("expected 1 restart, got %d", len(mgr.restarts))
	}
	if mgr.restarts[0].Key != "wdtt-client:c1" {
		t.Errorf("wrong key: %s", mgr.restarts[0].Key)
	}
}

func TestWatchdogCheck_Interval(t *testing.T) {
	mgr := &fakeManager{
		recs: []instancestore.Record{
			{
				ID:   "ft1",
				Kind: instancestore.KindFreeTurnClient,
				Name: "FT Client",
				Enabled: true,
				FreeTurnClient: &roles.FreeTurnClientConfig{
					AutoReconnect:         true,
					AutoReconnectInterval: "30m",
				},
			},
		},
	}
	jrnl := &fakeJournal{}
	now := time.Now()

	wd := New(Deps{
		Manager: mgr,
		Snapshot: func(key string) (awgmproto.State, bool) {
			// UptimeS 1900 > 30m (1800s)
			return awgmproto.State{PID: 5678, UptimeS: 1900}, true
		},
		LogTail: func(key string) string { return "everything ok" },
		Journal: jrnl,
		Now:     func() time.Time { return now },
	})

	wd.Check(context.Background())

	if len(mgr.restarts) != 1 {
		t.Fatalf("expected 1 restart, got %d", len(mgr.restarts))
	}
	if mgr.restarts[0].Key != "freeturn-client:ft1" {
		t.Errorf("wrong key: %s", mgr.restarts[0].Key)
	}
}

func TestWatchdogCheck_CooldownAndStartupGrace(t *testing.T) {
	mgr := &fakeManager{
		recs: []instancestore.Record{
			{
				ID:   "c1",
				Kind: instancestore.KindWdttClient,
				Name: "Client 1",
				Enabled: true,
				WdttClient: &roles.WdttClientConfig{
					AutoReconnect: true,
				},
			},
		},
	}
	now := time.Now()
	uptime := int64(10) // less than 20s grace period

	wd := New(Deps{
		Manager: mgr,
		Snapshot: func(key string) (awgmproto.State, bool) {
			return awgmproto.State{PID: 1234, UptimeS: uptime}, true
		},
		LogTail: func(key string) string {
			return "TURN Allocate: Allocate error response (error 401: Unauthorized)"
		},
		Now: func() time.Time { return now },
	})

	// 1. Startup grace: uptime < 20s -> should not restart
	wd.Check(context.Background())
	if len(mgr.restarts) != 0 {
		t.Fatalf("expected 0 restarts during startup grace, got %d", len(mgr.restarts))
	}

	// 2. Now uptime is 30s -> should restart
	uptime = 30
	wd.Check(context.Background())
	if len(mgr.restarts) != 1 {
		t.Fatalf("expected 1 restart, got %d", len(mgr.restarts))
	}

	// 3. Immediately check again -> cooldown 60s should prevent another restart
	uptime = 35
	wd.Check(context.Background())
	if len(mgr.restarts) != 1 {
		t.Fatalf("expected cooldown to prevent second restart, got %d", len(mgr.restarts))
	}

	// 4. Advance time past cooldown -> should restart again
	now = now.Add(65 * time.Second)
	wd.Check(context.Background())
	if len(mgr.restarts) != 2 {
		t.Fatalf("expected restart after cooldown expired, got %d", len(mgr.restarts))
	}
}
