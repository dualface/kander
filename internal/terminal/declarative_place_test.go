package terminal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

func placeBackend(t *testing.T, hook string, change func(root map[string]any)) *DeclarativeBackend {
	t.Helper()
	data := mutateFixture(t, func(root map[string]any) {
		if hook != "" {
			root["hooks"] = map[string]any{"place_container": hook}
		}
		firstStep(root, "create_container")["argv"] = []any{"new", "--cwd", "{cwd}", "--label", "{label}", "--title", "{title}"}
		if change != nil {
			change(root)
		}
	})
	def, err := DecodeDefinition("faketerm.json", data)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewDeclarativeBackend(def, "faketerm", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

// create_container sees the display title as {title}; an empty title is the
// container label.
func TestCreateContainerTitleVariable(t *testing.T) {
	resetLanguage(t)
	backend := placeBackend(t, "", nil)
	fake := terminaltest.New(t,
		reply(`{"result":{"id":"w1/p1"}}`, "new"),
		reply(`{"result":{"id":"w1/p1"}}`, "new"),
	)
	spawn := Conn{Program: fake.Program, Run: SpawnRunner}
	for _, target := range []Target{{Title: "Card title"}, {}} {
		if _, err := backend.CreateContainer(spawn, target, "/w", "kb-label"); err != nil {
			t.Fatal(err)
		}
	}
	wantCalls(t, fake,
		[]string{"new", "--cwd", "/w", "--label", "kb-label", "--title", "Card title"},
		[]string{"new", "--cwd", "/w", "--label", "kb-label", "--title", "kb-label"},
	)
}

// PlaceContainer hands the hook the container, the pane, the title and the
// label, turns a degraded result into the note and a failed one into its
// error, and does nothing without a hook.
func TestPlaceContainerHook(t *testing.T) {
	resetLanguage(t)
	var seen []HookCall
	var outcome HookResult
	RegisterHook("place-test-hook", func(_ context.Context, call HookCall) HookResult {
		seen = append(seen, call)
		return outcome
	})
	backend := placeBackend(t, "place-test-hook", nil)
	address := Address{Container: "w1", Pane: "p1"}
	conn := Conn{Program: "faketerm"}
	for _, tc := range []struct {
		outcome HookResult
		target  Target
		want    string
		title   string
	}{
		{HookResult{Status: HookOK}, Target{Title: "Card"}, "", "Card"},
		{HookResult{Status: HookOK}, Target{}, "", "kb-label"},
		{HookResult{Status: HookDegraded, Note: "tab not named"}, Target{Title: "Card"}, "tab not named", "Card"},
		{HookResult{Status: HookFailed, Err: errors.New("hook broke")}, Target{}, "hook broke", "kb-label"},
	} {
		outcome, seen = tc.outcome, nil
		if got := backend.PlaceContainer(conn, address, tc.target, "kb-label"); got != tc.want {
			t.Fatalf("warning=%q, want %q", got, tc.want)
		}
		if len(seen) != 1 || seen[0].Point != HookPointPlaceContainer || seen[0].Launcher != "faketerm" {
			t.Fatalf("calls=%+v", seen)
		}
		want := map[string]string{"container": "w1", "pane": "p1", "title": tc.title, "label": "kb-label"}
		if !reflect.DeepEqual(seen[0].Values, want) {
			t.Fatalf("values=%v, want %v", seen[0].Values, want)
		}
	}
	outcome, seen = HookResult{}, nil
	if got := backend.PlaceContainer(conn, address, Target{}, "kb-label"); got == "" {
		t.Fatal("a hook without a status must not read as success")
	}

	plain := placeBackend(t, "", nil)
	if got := plain.PlaceContainer(conn, address, Target{Title: "x"}, "l"); got != "" || len(seen) != 1 {
		t.Fatalf("a definition without the hook placed a container: %q", got)
	}
	var _ ContainerPlacer = plain
}

// place_container is a valid mount point; every definition has the container
// capability it needs, so no separate capability check exists.
func TestPlaceContainerHookValidation(t *testing.T) {
	resetLanguage(t)
	RegisterHook("place-validate-hook", func(context.Context, HookCall) HookResult { return HookResult{Status: HookOK} })
	placeBackend(t, "place-validate-hook", nil)
	data := mutateFixture(t, func(root map[string]any) { root["hooks"] = map[string]any{"place_container": "no-such-hook"} })
	if _, err := DecodeDefinition("faketerm.json", data); err == nil || !strings.Contains(err.Error(), "field hooks.place_container") {
		t.Fatalf("err=%v", err)
	}
}
