package stream

import (
	"testing"
	"time"
)

func TestFmtLimit(t *testing.T) {
	cases := map[int64]string{
		0:    "-",
		-1:   "-",
		1000: "1000",
	}

	for in, want := range cases {
		if got := fmtLimit(in); got != want {
			t.Errorf("fmtLimit(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	if got := fmtDuration(0); got != "-" {
		t.Errorf("fmtDuration(0) = %q, want -", got)
	}

	if got := fmtDuration(time.Hour); got != "1h0m0s" {
		t.Errorf("fmtDuration(1h) = %q, want 1h0m0s", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[uint64]string{
		512:               "512 B",
		1024:              "1.0 KiB",
		1024 * 1024:       "1.0 MiB",
		3 * 1024 * 1024:   "3.0 MiB",
		1024 * 1024 * 1024: "1.0 GiB",
	}

	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
