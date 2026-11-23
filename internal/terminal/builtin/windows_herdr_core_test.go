package builtin

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func TestWindowsHerdrCoreAuto(t *testing.T) {
	// Only the embedded definitions take part, never a user's share directory.
	saved := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir { return nil }
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = saved
		terminal.ReloadDefinitions()
	})
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"herdr-core", map[string]string{"HC_TERMINAL_ID": "t3"}, HerdrCore},
		{"herdr-core over herdr", map[string]string{"HC_TERMINAL_ID": "t3", "HERDR_ENV": "1"}, HerdrCore},
		{"herdr", map[string]string{"HERDR_ENV": "1"}, Herdr},
		{"never luvus", map[string]string{"LUVUS_ENV": "1"}, ""},
		{"never tmux", map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}, ""},
		{"none", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ""
			if backend, ok := terminal.ResolveAuto(true, envOf(tc.env)); ok {
				got = backend.Name()
			}
			if got != tc.want {
				t.Fatalf("auto=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestWindowsHerdrCoreDefinition(t *testing.T) {
	const program = `C:\Programs\herdr-core\hc.exe`
	env := map[string]string{"PATH": `C:\bin`, "KANBAN_DIR": "C:\\work\\O\u2019Brien", "KANDER_LANG": ""}
	backend := herdrCoreBackendForTest(t, env)
	target, err := backend.Prepare(terminal.PrepareRequest{
		Project:  `C:\work`,
		Windows:  true,
		LookPath: func(string) (string, error) { return program, nil },
		Getenv:   envOf(env),
	})
	if err != nil || !target.Windows || target.Program != program {
		t.Fatalf("prepare=%+v err=%v", target, err)
	}
	var calls [][]string
	conn := terminal.Conn{Program: program, Run: func(_ context.Context, _ string, args []string) (probe.Result, error) {
		calls = append(calls, args)
		return probe.Result{Stdout: `{"address":"t8"}`}, nil
	}}
	address, err := backend.CreateContainer(conn, target, `C:\work dir`, "kb-task")
	if err != nil || address.Pane != "t8" || len(calls) != 1 {
		t.Fatalf("address=%+v err=%v calls=%q", address, err, calls)
	}
	run := calls[0]
	if at := slices.Index(run, "--cwd"); at < 0 || run[at+1] != `C:\work dir` {
		t.Errorf("run lacks the startup directory: %q", run)
	}
	if !slices.Contains(run, "--rm") {
		t.Errorf("run lacks --rm: %q", run)
	}
	if at := slices.Index(run, "--"); at < 0 || !slices.Equal(run[at+1:], []string{"powershell.exe", "-NoLogo", "-NoProfile"}) {
		t.Errorf("container shell: %q", run)
	}
	calls = nil
	if err := backend.RunCommand(conn, "t8", "", "& 'agent'", false); err != nil {
		t.Fatal(err)
	}
	want := []string{"send", "--json", "t8", "try { " +
		"$env:KANBAN_DIR='C:\\work\\O\u2019\u2019Brien'; " + // set: assigned
		"$env:KANDER_CONFIG=$null; " + // unset: removed
		"$env:KANDER_LANG=$null; " + // empty: removed
		"& 'agent' } finally { if ($null -eq $LASTEXITCODE) { exit 1 }; exit $LASTEXITCODE }"}
	if len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("send=%q\nwant %q", calls, want)
	}

	calls = nil
	if _, err := backend.CreateContainer(conn, terminal.Target{}, "/work", "kb-task"); err != nil {
		t.Fatal(err)
	}
	if err := backend.RunCommand(conn, "t8", "'/work'", "agent", true); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !slices.Contains(calls[0], "/usr/bin/env") || !slices.Contains(calls[0], "/bin/sh") ||
		strings.Join(calls[1], " ") != "send --json t8 cd -- '/work' && exec agent" {
		t.Fatalf("a target without a platform is POSIX: %q", calls)
	}
}
