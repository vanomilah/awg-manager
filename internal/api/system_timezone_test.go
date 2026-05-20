package api

import "testing"

func TestParsePOSIXTZ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		in         string
		wantName   string
		wantOffset int
		wantOK     bool
	}{
		{name: "MSK-3", in: "MSK-3", wantName: "MSK", wantOffset: 180, wantOK: true},
		{name: "UTC0", in: "UTC0", wantName: "UTC", wantOffset: 0, wantOK: true},
		{name: "GMT0", in: "GMT0", wantName: "GMT", wantOffset: 0, wantOK: true},
		{name: "EST5", in: "EST5", wantName: "EST", wantOffset: -300, wantOK: true},
		{name: "EST5EDT", in: "EST5EDT,M3.2.0/2,M11.1.0/2", wantName: "EST", wantOffset: -300, wantOK: true},
		{name: "IST-5:30", in: "IST-5:30", wantName: "IST", wantOffset: 330, wantOK: true},
		{name: "invalid empty", in: "", wantOK: false},
		{name: "invalid missing offset", in: "MSK", wantOK: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotOffset, ok := parsePOSIXTZ(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotName != tt.wantName {
				t.Fatalf("name=%q want %q", gotName, tt.wantName)
			}
			if gotOffset != tt.wantOffset {
				t.Fatalf("offset=%d want %d", gotOffset, tt.wantOffset)
			}
		})
	}
}
