package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	home = filepath.Clean(home)

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{home, "~"},
		{home + string(filepath.Separator), "~"},
		{filepath.Join(home, "works"), "~/works"},
		{filepath.Join(home, "works", "kander"), "~/works/kander"},
		{filepath.Join(string(filepath.Separator), "tmp", "elsewhere"), filepath.Join(string(filepath.Separator), "tmp", "elsewhere")},
	}
	for _, tc := range cases {
		if got := homePath(tc.in); got != tc.want {
			t.Fatalf("homePath(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
