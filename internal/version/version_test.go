package version

import "testing"

func TestStringUsesVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() {
		Version = old
	})
	Version = "0.5.0"
	if got := String(); got != "0.5.0" {
		t.Fatalf("version=%q", got)
	}
}

func TestStringFallsBackForEmptyVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() {
		Version = old
	})
	Version = " "
	if got := String(); got != "dev" {
		t.Fatalf("version=%q", got)
	}
}
