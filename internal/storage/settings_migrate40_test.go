package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// V40 включает статистику существующим установкам ОДИН раз: выключенная
// пользователем не должна возвращаться на следующей загрузке.
func TestMigrateToV40_StatsEnabled(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"схема 39 — включается", `{"schemaVersion":39,"updates":{"checkEnabled":true}}`, true},
		{"схема 40, выключено — остаётся выключенным", `{"schemaVersion":40,"updates":{"statsEnabled":false}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(tc.raw), 0o644); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ { // повторная загрузка — как рестарт демона
				s, err := NewSettingsStore(dir).Load()
				if err != nil {
					t.Fatal(err)
				}
				if s.Updates.StatsEnabled != tc.want {
					t.Fatalf("загрузка %d: StatsEnabled = %v, want %v", i+1, s.Updates.StatsEnabled, tc.want)
				}
			}
		})
	}
}
