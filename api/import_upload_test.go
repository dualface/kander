package api

import (
	"math"
	"testing"
	"time"
)

func TestDefaultImportUploadLimits(t *testing.T) {
	want := ImportUploadLimits{Files: 1_000_000, Dirs: 1_000_000, Bytes: 128 << 30, FileBytes: 256 << 20, Manifest: 256 << 20}
	if got := DefaultImportUploadLimits(); got != want {
		t.Fatalf("defaults %+v, want %+v", got, want)
	}
}

func TestImportUploadDeadlineGrowsWithTheDeclaredBytes(t *testing.T) {
	for _, tc := range []struct {
		bytes int64
		want  time.Duration
	}{
		{-1, 30 * time.Minute},
		{0, 30 * time.Minute},
		{1, 30*time.Minute + time.Second},
		{8 << 20, 30*time.Minute + time.Second},
		{8<<20 + 1, 30*time.Minute + 2*time.Second},
		{28_044_023_251, 30*time.Minute + 3344*time.Second},
		{128 << 30, 30*time.Minute + 16384*time.Second},
		{math.MaxInt64, math.MaxInt64},
	} {
		if got := ImportUploadDeadline(tc.bytes); got != tc.want {
			t.Errorf("ImportUploadDeadline(%d) = %s, want %s", tc.bytes, got, tc.want)
		}
	}
}
