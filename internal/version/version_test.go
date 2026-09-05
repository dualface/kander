package version

import "testing"

func TestStringUsesBuildTimestampAndGitHash(t *testing.T) {
	oldTimestamp, oldHash := BuildTimestamp, GitHash
	t.Cleanup(func() {
		BuildTimestamp, GitHash = oldTimestamp, oldHash
	})
	BuildTimestamp = "20260906T123456Z"
	GitHash = "0123456789ab"
	if got := String(); got != "20260906T123456Z-0123456789ab" {
		t.Fatalf("version=%q", got)
	}
}

func TestStringFallsBackForEmptyComponents(t *testing.T) {
	oldTimestamp, oldHash := BuildTimestamp, GitHash
	t.Cleanup(func() {
		BuildTimestamp, GitHash = oldTimestamp, oldHash
	})
	BuildTimestamp = " "
	GitHash = ""
	if got := String(); got != "dev-unknown" {
		t.Fatalf("version=%q", got)
	}
}
