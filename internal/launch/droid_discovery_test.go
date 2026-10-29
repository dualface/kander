package launch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

// droidHome points the user home at a temporary directory and returns the
// Droid sessions root below it.
func droidHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".factory", "sessions")
}

func writeDroidSession(t *testing.T, root, dir, id, header string) string {
	t.Helper()
	path := filepath.Join(root, dir, id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(header), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func droidHeader(id, cwd string) string {
	return `{"type":"session_start","id":"` + id + `","title":"New Session","version":2,"cwd":"` + jsonPath(cwd) + `"}` + "\n"
}

func TestDroidSessionIsDiscoveredAfterStart(t *testing.T) {
	cfg := config.DefaultConfig()
	session, err := newAgentSession("droid", &process.AgentProgram{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if session.Agent != "droid" || session.Reference != "" {
		t.Fatalf("session=%+v", session)
	}
	def := config.AgentFor(cfg, "droid").Session
	if def.Mode != "hook:droid-sessions" || !config.SessionDiscoversAfterStart(def) || !config.SessionPersistsAfterStart(def) ||
		config.SessionResolvesEmptyReference(def) || config.SessionAllocatesBeforeStart(def) {
		t.Fatalf("mode=%q", def.Mode)
	}
}

func TestDroidSnapshotToleratesMissingRoot(t *testing.T) {
	droidHome(t)
	previous, err := droidSessionSnapshot(time.Second)
	if err != nil || len(previous) != 0 {
		t.Fatalf("previous=%v err=%v", previous, err)
	}
}

func TestDroidDiscoveryPicksTheNewSessionOfTheLaunchDirectory(t *testing.T) {
	root := droidHome(t)
	cwd, other := t.TempDir(), t.TempDir()
	writeDroidSession(t, root, "-project", "0d1c-old", droidHeader("0d1c-old", cwd))
	def := config.AgentFor(config.DefaultConfig(), "droid").Session
	budget := func(d time.Duration) time.Duration { return d }
	previous, err := sessionDiscoverSnapshot(def, "task", true, nil, cwd, budget)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]struct{}{"0d1c-old": {}}; !reflect.DeepEqual(previous, want) {
		t.Fatalf("previous=%v", previous)
	}
	writeDroidSession(t, root, "-project", "1a2b-mine", droidHeader("1a2b-mine", cwd))
	writeDroidSession(t, root, "-other", "3c4d-other", droidHeader("3c4d-other", other))
	writeDroidSession(t, root, "-project", "5e6f-renamed", droidHeader("not-the-file-name", cwd))
	writeDroidSession(t, root, "-project", "7a8b-message", `{"type":"message","id":"7a8b-message"}`+"\n")
	writeDroidSession(t, root, "-project", "9c0d-garbage", "not json\n")
	if err := os.WriteFile(filepath.Join(root, "-project", "1a2b-mine.settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := runSessionDiscoverHook(def, "task", previous, time.Now(), nil, cwd, budget)
	if err != nil || got != "1a2b-mine" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestDroidDiscoveryRejectsSeveralNewSessions(t *testing.T) {
	root := droidHome(t)
	cwd := t.TempDir()
	writeDroidSession(t, root, "-project", "aaaa", droidHeader("aaaa", cwd))
	writeDroidSession(t, root, "-project", "bbbb", droidHeader("bbbb", cwd))
	_, err := discoverNewDroidSession(map[string]struct{}{}, cwd, time.Second)
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("err=%v", err)
	}
}

func TestDroidDiscoveryTimesOutWithoutANewSession(t *testing.T) {
	root := droidHome(t)
	cwd := t.TempDir()
	writeDroidSession(t, root, "-project", "aaaa", droidHeader("aaaa", cwd))
	previousNow, previousSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = previousNow, previousSleep })
	clock := time.Unix(1_800_000_000, 0)
	nowFn = func() time.Time { return clock }
	sleepFn = func(d time.Duration) { clock = clock.Add(d) }
	_, err := discoverNewDroidSession(map[string]struct{}{"aaaa": {}}, cwd, time.Second)
	if err == nil {
		t.Fatal("discovery bound an existing session")
	}
}

func TestDroidScanRetriesAHeaderStillBeingWritten(t *testing.T) {
	root := droidHome(t)
	cwd := t.TempDir()
	header := droidHeader("cccc", cwd)
	path := writeDroidSession(t, root, "-project", "cccc", strings.TrimSuffix(header, "\n"))
	scan := &droidSessionScan{root: root, previous: map[string]struct{}{}, cwd: cwd, settled: map[string]struct{}{}}
	got, err := scan.candidates(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("partial header: got=%q err=%v", got, err)
	}
	if _, settled := scan.settled["cccc"]; settled {
		t.Fatal("a partial header must be read again")
	}
	if err := os.WriteFile(path, []byte(header), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = scan.candidates(t.Context())
	if err != nil || !reflect.DeepEqual(got, []string{"cccc"}) {
		t.Fatalf("complete header: got=%q err=%v", got, err)
	}
}

// writeUnreadableDroidSession writes a session file of cwd that this user
// cannot open.
func writeUnreadableDroidSession(t *testing.T, root, dir, id, cwd string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file mode 0 does not deny reads on Windows or to root")
	}
	path := writeDroidSession(t, root, dir, id, droidHeader(id, cwd))
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
}

func TestDroidDiscoveryReadsPastAnUnreadableSession(t *testing.T) {
	root := droidHome(t)
	cwd := t.TempDir()
	writeUnreadableDroidSession(t, root, "-project", "eeee", cwd)
	writeDroidSession(t, root, "-project", "ffff", droidHeader("ffff", cwd))
	got, err := discoverNewDroidSession(map[string]struct{}{}, cwd, time.Second)
	if err != nil || got != "ffff" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestDroidDiscoveryReportsTheReadErrorWithoutASession(t *testing.T) {
	root := droidHome(t)
	cwd := t.TempDir()
	writeUnreadableDroidSession(t, root, "-project", "eeee", cwd)
	previousNow, previousSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = previousNow, previousSleep })
	clock := time.Unix(1_800_000_000, 0)
	nowFn = func() time.Time { return clock }
	sleepFn = func(d time.Duration) { clock = clock.Add(d) }
	_, err := discoverNewDroidSession(map[string]struct{}{}, cwd, time.Second)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err=%v", err)
	}
}

func TestDroidScanSettlesSessionsOfOtherDirectories(t *testing.T) {
	root := droidHome(t)
	cwd := t.TempDir()
	writeDroidSession(t, root, "-other", "dddd", droidHeader("dddd", t.TempDir()))
	scan := &droidSessionScan{root: root, previous: map[string]struct{}{}, cwd: cwd, settled: map[string]struct{}{}}
	got, err := scan.candidates(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, settled := scan.settled["dddd"]; !settled {
		t.Fatal("a session of another directory is never read again")
	}
}
