package main

import (
	"testing"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

func TestAwgKernelIface(t *testing.T) {
	tests := []struct {
		name string
		tun  *storage.AWGTunnel
		want string
	}{
		{
			name: "nativewg backend uses nwgIndex",
			tun: &storage.AWGTunnel{
				ID:       "awg0",
				Backend:  "nativewg",
				NWGIndex: 2,
			},
			want: "nwg2",
		},
		{
			name: "wdtt-raw backend uses rawKernelIface",
			tun: &storage.AWGTunnel{
				ID:             "wdttraw-default",
				Backend:        "wdtt-raw",
				RawKernelIface: "opkgtun17",
			},
			want: "opkgtun17",
		},
		{
			name: "wdtt-raw backend with whitespace is trimmed",
			tun: &storage.AWGTunnel{
				ID:             "wdttraw-de",
				Backend:        "wdtt-raw",
				RawKernelIface: "  opkgtun18 \n",
			},
			want: "opkgtun18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awgKernelIface(tt.tun)
			if got != tt.want {
				t.Errorf("awgKernelIface() = %q, want %q", got, tt.want)
			}
		})
	}
}
