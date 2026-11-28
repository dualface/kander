package svcinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeManager records every service manager command and answers from a script.
type fakeManager struct {
	mu    sync.Mutex
	calls []string
	// answer returns the output and error of a command; nil answers every command with success.
	answer func(line string) ([]byte, error)
}

func (f *fakeManager) run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.calls = append(f.calls, line)
	f.mu.Unlock()
	if f.answer == nil {
		return nil, nil
	}
	return f.answer(line)
}

func (f *fakeManager) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeManager) reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func useSystemd(t *testing.T, f *fakeManager) string {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	UseForTest(t, TestPlatform{GOOS: "linux", Dir: dir, Run: f.run, User: "alice"})
	return dir
}

func TestSystemdInstallIsIdempotent(t *testing.T) {
	f := &fakeManager{}
	dir := useSystemd(t, f)
	ctx := context.Background()
	prepared, err := Prepare(ctx, linuxWorker)
	if err != nil || prepared.Found || !prepared.Changed {
		t.Fatalf("first prepare: %+v %v", prepared, err)
	}
	if err := prepared.Write(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"systemctl --user daemon-reload", "systemctl --user enable kander-worker.service"}; !slices.Equal(f.commands(), want) {
		t.Fatalf("commands %q, want %q", f.commands(), want)
	}
	written, err := os.ReadFile(filepath.Join(dir, "kander-worker.service"))
	if err != nil || !strings.Contains(string(written), "KillMode=process") {
		t.Fatalf("unit: %v\n%s", err, written)
	}
	again, err := Prepare(ctx, linuxWorker)
	if err != nil || !again.Found || again.Changed {
		t.Fatalf("an unchanged definition must not be rewritten: %+v %v", again, err)
	}
	changed := linuxWorker
	changed.ClientHome = ""
	if p, err := Prepare(ctx, changed); err != nil || !p.Changed {
		t.Fatalf("a changed definition: %+v %v", p, err)
	}
	inst, found, err := Lookup(ctx, KindWorker)
	if err != nil || !found || !inst.Managed || inst.Spec.ClientHome != linuxWorker.ClientHome || inst.Location != filepath.Join(dir, "kander-worker.service") {
		t.Fatalf("lookup: %+v %v %v", inst, found, err)
	}
}

func TestForeignDefinitionIsNeverChanged(t *testing.T) {
	f := &fakeManager{}
	dir := useSystemd(t, f)
	path := filepath.Join(dir, "kander-worker.service")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	own := "[Service]\nExecStart=/usr/bin/my-worker\n"
	if err := os.WriteFile(path, []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(context.Background(), linuxWorker)
	var foreign *ForeignError
	if !errors.As(err, &foreign) || foreign.Location != path {
		t.Fatalf("a hand-written unit must be refused with its path: %v", err)
	}
	inst, found, err := Lookup(context.Background(), KindWorker)
	if err != nil || !found || inst.Managed {
		t.Fatalf("lookup of a foreign unit: %+v %v %v", inst, found, err)
	}
	if data, _ := os.ReadFile(path); string(data) != own || len(f.commands()) != 0 {
		t.Fatalf("the unit changed or a command ran: %q %q", data, f.commands())
	}
	// A link a dotfile manager placed is not followed and counts as not Kander's.
	link := filepath.Join(dir, "kander-server.service")
	if err := os.Symlink(path, link); err != nil {
		t.Skip("symlinks are not available:", err)
	}
	if _, err := Prepare(context.Background(), linuxServer); !errors.As(err, &foreign) {
		t.Fatalf("a linked unit must be refused: %v", err)
	}
}

func TestSystemdUnavailableRefusesInstall(t *testing.T) {
	f := &fakeManager{answer: func(line string) ([]byte, error) {
		return []byte("Failed to connect to bus: No medium found"), errors.New("exit status 1")
	}}
	useSystemd(t, f)
	err := Available(context.Background())
	if err == nil || !strings.Contains(err.Error(), "No medium found") {
		t.Fatalf("no user instance must refuse: %v", err)
	}
	if f.commands()[0] != "systemctl --user show-environment" {
		t.Fatalf("commands %q", f.commands())
	}
}

func TestSystemdControlStateRemoveAndLinger(t *testing.T) {
	f := &fakeManager{answer: func(line string) ([]byte, error) {
		switch {
		case strings.HasPrefix(line, "systemctl --user show kander-worker.service"):
			return []byte("LoadState=loaded\nActiveState=active\n"), nil
		case strings.HasPrefix(line, "loginctl show-user alice"):
			return []byte("Linger=no\n"), nil
		}
		return nil, nil
	}}
	dir := useSystemd(t, f)
	ctx := context.Background()
	prepared, _ := Prepare(ctx, linuxWorker)
	if err := prepared.Write(ctx); err != nil {
		t.Fatal(err)
	}
	f.reset()
	for _, op := range []func(context.Context, Kind) error{Start, Stop, Restart} {
		if err := op(ctx, KindWorker); err != nil {
			t.Fatal(err)
		}
	}
	st, err := StateOf(ctx, KindWorker)
	if err != nil || !st.Loaded || !st.Running || st.Active != "active" {
		t.Fatalf("state %+v %v", st, err)
	}
	enabled, known, applies := Linger(ctx)
	if enabled || !known || !applies {
		t.Fatalf("linger %v %v %v", enabled, known, applies)
	}
	if err := Remove(ctx, KindWorker); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"systemctl --user start kander-worker.service", "systemctl --user stop kander-worker.service", "systemctl --user restart kander-worker.service",
		"systemctl --user show kander-worker.service --property=LoadState --property=ActiveState",
		"loginctl show-user alice --property=Linger",
		"systemctl --user disable --now kander-worker.service", "systemctl --user daemon-reload",
	}
	if !slices.Equal(f.commands(), want) {
		t.Fatalf("commands\n%q\nwant\n%q", f.commands(), want)
	}
	if _, err := os.Stat(filepath.Join(dir, "kander-worker.service")); !os.IsNotExist(err) {
		t.Fatalf("the unit is still there: %v", err)
	}
	if !ManagerStops() {
		t.Fatal("systemd stops its services itself")
	}
}

func TestLaunchdLoadsIntoTheGuiDomain(t *testing.T) {
	loaded := false
	f := &fakeManager{}
	f.answer = func(line string) ([]byte, error) {
		switch {
		case strings.HasPrefix(line, "launchctl print gui/501/"):
			if !loaded {
				return nil, errors.New("exit status 113")
			}
			return []byte("\tstate = running\n\tpid = 77\n"), nil
		case strings.HasPrefix(line, "launchctl bootstrap"):
			loaded = true
		case strings.HasPrefix(line, "launchctl bootout"):
			loaded = false
		}
		return nil, nil
	}
	dir := filepath.Join(t.TempDir(), "LaunchAgents")
	UseForTest(t, TestPlatform{GOOS: "darwin", Dir: dir, UID: "501", Run: f.run})
	ctx := context.Background()
	prepared, err := Prepare(ctx, macWorker)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Write(ctx); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dir, "io.github.dualface.kander.worker.plist")
	if err := Start(ctx, KindWorker); err != nil {
		t.Fatal(err)
	}
	if err := Start(ctx, KindWorker); err != nil {
		t.Fatal(err)
	}
	st, _ := StateOf(ctx, KindWorker)
	if !st.Running {
		t.Fatalf("state %+v", st)
	}
	if err := Remove(ctx, KindWorker); err != nil {
		t.Fatal(err)
	}
	target := "gui/501/io.github.dualface.kander.worker"
	want := []string{
		"launchctl print " + target, "launchctl enable " + target, "launchctl bootstrap gui/501 " + plist,
		"launchctl print " + target, "launchctl kickstart " + target,
		"launchctl print " + target,
		"launchctl print " + target, "launchctl bootout " + target, "launchctl print " + target,
	}
	if !slices.Equal(f.commands(), want) {
		t.Fatalf("commands\n%q\nwant\n%q", f.commands(), want)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatalf("the plist is still there: %v", err)
	}
}

func TestSchtasksRegistersFromXMLAndLeavesStopsToKander(t *testing.T) {
	temp := t.TempDir()
	var registered []byte
	f := &fakeManager{}
	f.answer = func(line string) ([]byte, error) {
		switch {
		case strings.HasPrefix(line, "schtasks /Create"):
			fields := strings.Fields(line)
			data, err := os.ReadFile(fields[5])
			if err != nil {
				return nil, err
			}
			registered = data
		case strings.HasPrefix(line, "schtasks /Query /TN kander-worker /XML"):
			if registered == nil {
				return []byte("ERROR: The system cannot find the file specified."), errors.New("exit status 1")
			}
			// schtasks prints the XML it stores; the marker survives its encoding.
			return registered, nil
		}
		return nil, nil
	}
	UseForTest(t, TestPlatform{GOOS: "windows", Run: f.run, User: `HOST\alice`, Temp: temp})
	ctx := context.Background()
	if _, found, err := Lookup(ctx, KindWorker); found || err != nil {
		t.Fatalf("no task yet: %v %v", found, err)
	}
	prepared, err := Prepare(ctx, windowsWorker)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Write(ctx); err != nil {
		t.Fatal(err)
	}
	if len(registered) < 2 || registered[0] != 0xff || registered[1] != 0xfe {
		t.Fatalf("the task XML is not UTF-16LE with a byte order mark: % x", registered[:min(4, len(registered))])
	}
	if text := decodeTaskOutput(registered); !strings.Contains(text, "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>") {
		t.Fatalf("decoded task: %s", text)
	}
	if entries, _ := os.ReadDir(temp); len(entries) != 0 {
		t.Fatalf("the task XML was left behind: %v", entries)
	}
	inst, found, err := Lookup(ctx, KindWorker)
	if err != nil || !found || !inst.Managed || inst.Spec.ClientHome != windowsWorker.ClientHome {
		t.Fatalf("lookup %+v %v %v", inst, found, err)
	}
	if again, err := Prepare(ctx, windowsWorker); err != nil || again.Changed {
		t.Fatalf("an unchanged task must not be registered again: %+v %v", again, err)
	}
	if !errors.Is(Stop(ctx, KindWorker), ErrProtocolStop) || !errors.Is(Restart(ctx, KindWorker), ErrProtocolStop) || ManagerStops() {
		t.Fatal("Windows leaves the stop to the Kander protocol")
	}
	f.reset()
	if err := Start(ctx, KindWorker); err != nil {
		t.Fatal(err)
	}
	if err := Remove(ctx, KindWorker); err != nil {
		t.Fatal(err)
	}
	if want := []string{"schtasks /Run /TN kander-worker", "schtasks /Delete /TN kander-worker /F"}; !slices.Equal(f.commands(), want) {
		t.Fatalf("commands %q", f.commands())
	}
	for _, c := range f.commands() {
		if strings.Contains(c, "/End") {
			t.Fatal("a task is never ended with /End")
		}
	}
}

func TestManagedForMatchesTheClientHome(t *testing.T) {
	f := &fakeManager{}
	useSystemd(t, f)
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	prepared, _ := Prepare(ctx, linuxWorker)
	if err := prepared.Write(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := ManagedForHome(ctx, KindWorker, linuxWorker.ClientHome); !ok || err != nil {
		t.Fatalf("the home the definition names: %v %v", ok, err)
	}
	if _, ok, _ := ManagedForHome(ctx, KindWorker, "/elsewhere"); ok {
		t.Fatal("another client home must not use the service")
	}
	t.Setenv("KANDER_MANAGED", "1")
	if _, ok, _ := ManagedForHome(ctx, KindWorker, linuxWorker.ClientHome); ok {
		t.Fatal("a managed process never goes through the service manager")
	}
	t.Setenv("KANDER_MANAGED", "")
	// Both unset: the definition names no client home and the process uses the default one.
	server := linuxServer
	prepared, _ = Prepare(ctx, server)
	if err := prepared.Write(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CLIENT_HOME", "")
	if _, ok, err := ManagedFor(ctx, KindServer); !ok || err != nil {
		t.Fatalf("both client homes unset count as the same: %v %v", ok, err)
	}
	t.Setenv("KANDER_CLIENT_HOME", filepath.Join(t.TempDir(), "other"))
	if _, ok, _ := ManagedFor(ctx, KindServer); ok {
		t.Fatal("a process with its own client home must not use the service")
	}
}

func TestATestBinaryWithoutAPlatformFindsNothing(t *testing.T) {
	if _, found, err := Lookup(context.Background(), KindWorker); found || err != nil {
		t.Fatalf("found %v %v", found, err)
	}
	if err := Start(context.Background(), KindWorker); err == nil {
		t.Fatal("a test binary must not run a service manager")
	}
}
