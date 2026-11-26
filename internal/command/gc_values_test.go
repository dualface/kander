package command

import (
	"errors"
	"testing"
	"time"
)

func TestParseRetention(t *testing.T) {
	for raw, want := range map[string]time.Duration{"7d": 7 * 24 * time.Hour, "1d": 24 * time.Hour, "1h": time.Hour, "36h": 36 * time.Hour, "90m": 90 * time.Minute, "1h30m": 90 * time.Minute} {
		got, err := ParseRetention("older-than", raw)
		if err != nil || got != want {
			t.Errorf("%s = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "d", "0d", "59m", "30s", "-7d", "7", "7days", "1.5d", "abc", "-1h", "99999999999999999d"} {
		_, err := ParseRetention("older-than", raw)
		var usage *UsageError
		if !errors.As(err, &usage) {
			t.Errorf("%q: want an argument error, got %v", raw, err)
		}
	}
}

func TestParseByteSize(t *testing.T) {
	for raw, want := range map[string]int64{"64MiB": 64 << 20, "1GiB": 1 << 30, "1MiB": 1 << 20, "1024KiB": 1 << 20, "1048576": 1 << 20, "67108864": 64 << 20} {
		got, err := ParseByteSize("log-keep", raw)
		if err != nil || got != want {
			t.Errorf("%s = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "MiB", "1023KiB", "1048575", "0", "1MB", "1 MiB", "-1MiB", "1.5GiB", "64mib", "99999999999GiB"} {
		_, err := ParseByteSize("log-keep", raw)
		var usage *UsageError
		if !errors.As(err, &usage) {
			t.Errorf("%q: want an argument error, got %v", raw, err)
		}
	}
}
