package launch

import (
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

// titleBackend is the tmux backend under another launcher name. It records the
// display title and label of every container created and answers every
// placement with a fixed warning.
type titleBackend struct {
	terminal.Backend
	mu      sync.Mutex
	created []createdContainer
	placed  []createdContainer
	warning string
}

type createdContainer struct{ title, label string }

func (b *titleBackend) Name() string { return "title-test" }

// The registry is global, so the backend claims only its own addresses and
// never takes part in auto resolution.
func (b *titleBackend) AutoDetect(func(string) string) bool { return false }

func (b *titleBackend) ParseAddress(value string) (terminal.Address, bool) {
	rest, ok := strings.CutPrefix(value, "title-test:")
	if !ok {
		return terminal.Address{}, false
	}
	return b.Backend.ParseAddress("tmux:" + rest)
}

func (b *titleBackend) ParseFocusAddress(fields []string) (terminal.Address, bool) {
	if len(fields) == 0 || fields[0] != "title-test" {
		return terminal.Address{}, false
	}
	return b.Backend.ParseFocusAddress(append([]string{"tmux"}, fields[1:]...))
}

func (b *titleBackend) CreateContainer(conn terminal.Conn, target terminal.Target, cwd, label string) (terminal.Address, error) {
	b.mu.Lock()
	b.created = append(b.created, createdContainer{target.Title, label})
	b.mu.Unlock()
	return b.Backend.CreateContainer(conn, target, cwd, label)
}

func (b *titleBackend) PlaceContainer(_ terminal.Conn, _ terminal.Address, target terminal.Target, label string) string {
	b.mu.Lock()
	b.placed = append(b.placed, createdContainer{target.Title, label})
	b.mu.Unlock()
	return b.warning
}

var titleProbe = &titleBackend{}

// useTitleBackend registers the title backend once (the registry keeps the
// first backend of a name) and selects it as the launcher.
func useTitleBackend(t *testing.T, warning string) *titleBackend {
	t.Helper()
	tmux, ok := terminal.Lookup("tmux")
	if !ok {
		t.Fatal("tmux backend missing")
	}
	titleProbe.mu.Lock()
	titleProbe.Backend, titleProbe.created, titleProbe.placed, titleProbe.warning = tmux, nil, nil, warning
	titleProbe.mu.Unlock()
	terminal.Register(titleProbe)
	return titleProbe
}

// wantPlacement checks that the one container was created and placed with no
// display title, so the backend names its tab by the label, and that the label
// starts with prefix.
func wantPlacement(t *testing.T, probe *titleBackend, prefix string) {
	t.Helper()
	if len(probe.created) != 1 || len(probe.placed) != 1 || probe.created[0] != probe.placed[0] {
		t.Fatalf("created=%+v placed=%+v", probe.created, probe.placed)
	}
	if title := probe.created[0].title; title != "" {
		t.Fatalf("title=%q, want none", title)
	}
	if label := probe.created[0].label; !strings.HasPrefix(label, prefix) {
		t.Fatalf("label=%q, want prefix %q", label, prefix)
	}
}

// start sets no display title, so the tab is named by the kb- label, and lets
// the backend warn.
func TestStartPlacesContainerWithLabel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	probe := useTitleBackend(t, "tab not named")
	id, _ := makeTodo(t, root, "tab-title")
	result, err := Start(root, "claude", "title-test", id)
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "kb-")
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "tab not named") || !strings.HasPrefix(result.Warnings[0], "警告") {
		t.Fatalf("warnings=%q", result.Warnings)
	}
}

// resume takeover and the notify recovery process name the tab by the kb-
// label as well, and print the placement warning when the caller collects none.
func TestResumeAndNotifyRecoveryPlaceContainerWithLabel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	freezeClock(t)
	id, path := makeTodo(t, root, "tab-resume")
	startThenReview(t, root, "claude", id, path)
	probe := useTitleBackend(t, "tab not named")
	cleanups := recordCleanup(t)

	// A named agent with a decision makes this a takeover, not a plain resume.
	agent := "claude"
	_, stderr, err := capture(t, func() error {
		return commandResume(root, &agent, "title-test", id, "go on", "", true, 61, DispatchOptions{Decision: "user approved"})
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "kb-")
	if len(*cleanups) != 1 {
		t.Fatalf("takeover cleanups=%+v, want one", *cleanups)
	}
	if !strings.Contains(stderr, "tab not named") {
		t.Fatalf("takeover warning missing from stderr: %q", stderr)
	}

	// The takeover above replaced the pane session marker the fake tmux keeps,
	// so the recovery card starts after it.
	recoveryID, recoveryPath := makeTodo(t, root, "tab-recovery")
	startThenReview(t, root, "claude", recoveryID, recoveryPath)
	probe = useTitleBackend(t, "tab not named")
	loadEffective = func() (*config.Config, error) { return envConfig("claude", "title-test", nil), nil }
	snapshot, err := board.ReadSnapshot(root, recoveryID)
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, err = capture(t, func() error {
		_, err := NotifyViaResume(root, snapshot.Entry, snapshot.Text, "go on", 61)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "kb-")
	if !strings.Contains(stderr, "tab not named") {
		t.Fatalf("recovery warning missing from stderr: %q", stderr)
	}
}

// An orchestrator sets no display title either; its tab is named by the
// orchestrate- label.
func TestOrchestratorPlacesContainerWithLabel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	probe := useTitleBackend(t, "")
	first, _ := makeTodo(t, root, "tab-first")
	second, _ := makeBacklog(t, root, "tab-second", true)
	third, _ := makeBacklog(t, root, "tab-third", true)
	result, err := StartOrchestrator(OrchestrateRequest{
		Root: root, References: []string{second, first, third}, Handover: "Run them.", Agent: "claude", Launcher: "title-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "orchestrate-"+second)
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings=%q", result.Warnings)
	}
}

// chat and issue triage set no display title, so their tabs are named by the
// chat- and issue- labels.
func TestChatAndTriagePlaceContainerWithLabel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	useChatConfig(t, "claude", "title-test")
	probe := useTitleBackend(t, "")
	if _, err := StartChat(ChatRequest{Root: root, Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "chat-")

	probe = useTitleBackend(t, "")
	request := triageRequest(t, root)
	request.Agent, request.Launcher = "claude", "title-test"
	if _, err := StartTriage(request); err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "issue-dualface-kander-42")
}
