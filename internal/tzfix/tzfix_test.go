package tzfix

import "testing"

func TestParsePosixOffset(t *testing.T) {
	cases := []struct {
		name    string
		tz      string
		wantNm  string
		wantSec int
		wantOK  bool
	}{
		{"msk", "MSK-3", "MSK", 10800, true},
		{"angle-bracket", "<+03>-3", "+03", 10800, true},
		{"est", "EST5", "EST", -18000, true},
		{"utc0", "UTC0", "UTC", 0, true},
		{"gmt0", "GMT0", "GMT", 0, true},
		{"dst-form-takes-first", "MSK-3MSD,M3.5.0/2,M10.5.0/3", "MSK", 10800, true},
		{"colon-minutes", "IST-5:30", "IST", 19800, true},
		{"iana-name", "Europe/Moscow", "", 0, false},
		{"empty", "", "", 0, false},
		{"name-only-no-offset", "MSK", "", 0, false},
		{"unterminated-bracket", "<+03", "", 0, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotNm, gotSec, gotOK := parsePosixOffset(c.tz)
			if gotOK != c.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, c.wantOK)
			}
			if !c.wantOK {
				return
			}
			if gotNm != c.wantNm {
				t.Errorf("name = %q, want %q", gotNm, c.wantNm)
			}
			if gotSec != c.wantSec {
				t.Errorf("offsetSec = %d, want %d", gotSec, c.wantSec)
			}
		})
	}
}
