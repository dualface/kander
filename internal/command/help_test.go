package command

import (
	"slices"
	"testing"
)

func TestWrapBreaksAtSpacesWithinTheWidth(t *testing.T) {
	got := wrap("aaa bbb ccc ddd", "> ", "  ", 9)
	if want := []string{"> aaa bbb", "  ccc ddd"}; !slices.Equal(got, want) {
		t.Fatalf("wrap = %q, want %q", got, want)
	}
	// A word longer than the width stays whole on a line of its own.
	if got := wrap("a bbbbbbbbbbbb c", "", "", 5); !slices.Equal(got, []string{"a", "bbbbbbbbbbbb", "c"}) {
		t.Fatalf("long word: %q", got)
	}
	if got := wrap("", "  ", "  ", 80); !slices.Equal(got, []string{"  "}) {
		t.Fatalf("empty text: %q", got)
	}
}

func TestSubcommandsGroupUsageByName(t *testing.T) {
	usage := []string{
		"kander check [--all] [task-id ...]",
		"kander check delivery --base <ref>",
		"kander check overlap --source <ref>",
		"kander check delivery --other <ref>",
	}
	got := subcommands("check", usage)
	if len(got) != 2 || got[0].Name != "delivery" || len(got[0].Usage) != 2 || got[1].Name != "overlap" {
		t.Fatalf("subcommands = %+v", got)
	}
	if len(subcommands("version", []string{"kander version"})) != 0 {
		t.Fatal("a command without subcommands lists none")
	}
}
