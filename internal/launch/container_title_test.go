package launch

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

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

func wantPlacement(t *testing.T, probe *titleBackend, title string) {
	t.Helper()
	if len(probe.created) != 1 || len(probe.placed) != 1 || probe.created[0] != probe.placed[0] {
		t.Fatalf("created=%+v placed=%+v", probe.created, probe.placed)
	}
	if probe.created[0].title != title {
		t.Fatalf("title=%q, want %q", probe.created[0].title, title)
	}
}

func TestTaskTabTitle(t *testing.T) {
	long := strings.Repeat("长", 60)
	for _, tc := range []struct{ name, text, want string }{
		{"card title", "# Open each agent in a tab\n", "Open each agent in a tab"},
		{"control characters", "# a\x00b\x1b[0m\tc\n", "ab[0mc"},
		{"at the limit", "# " + strings.Repeat("x", tabTitleLimit) + "\n", strings.Repeat("x", tabTitleLimit)},
		{"over the limit", "# " + long + "\n", strings.Repeat("长", tabTitleLimit-1) + "…"},
		{"no title", "no heading here\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetLang(t)
			got := taskTabTitle(tc.text)
			if got != tc.want || utf8.RuneCountInString(got) > tabTitleLimit {
				t.Fatalf("title=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestOrchestratorTabTitle(t *testing.T) {
	for _, tc := range []struct {
		references []string
		want       string
	}{
		{nil, ""},
		{[]string{"20261116-a-task"}, "20261116-a-task"},
		{[]string{"20261116-a-group", "b"}, "20261116-a-group +1"},
		{[]string{"A", "B", "C"}, "A +2"},
	} {
		if got := orchestratorTabTitle(tc.references); got != tc.want {
			t.Fatalf("%v: title=%q, want %q", tc.references, got, tc.want)
		}
	}
	long := strings.Repeat("a", 60)
	if got := orchestratorTabTitle([]string{long, "b"}); utf8.RuneCountInString(got) > tabTitleLimit || !strings.HasSuffix(got, "…") {
		t.Fatalf("title=%q", got)
	}
}

// start gives the container the card title and lets the backend warn.
func TestStartPlacesContainerWithCardTitle(t *testing.T) {
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
	wantPlacement(t, probe, "任务 tab-title")
	if label := probe.created[0].label; !strings.HasPrefix(label, "kb-") {
		t.Fatalf("label=%q", label)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "tab not named") || !strings.HasPrefix(result.Warnings[0], "警告") {
		t.Fatalf("warnings=%q", result.Warnings)
	}
}

// resume takeover and the notify recovery process name the tab after the card
// as well, and print the placement warning when the caller collects none.
func TestResumeAndNotifyRecoveryPlaceContainerWithCardTitle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	freezeClock(t)
	id, path := makeTodo(t, root, "tab-resume")
	startThenReview(t, root, "claude", id, path)
	probe := useTitleBackend(t, "tab not named")

	_, stderr, err := capture(t, func() error { return commandResume(root, nil, "title-test", id, "go on", "", true, 61) })
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "任务 tab-resume")
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
	wantPlacement(t, probe, "任务 tab-recovery")
	if !strings.Contains(stderr, "tab not named") {
		t.Fatalf("recovery warning missing from stderr: %q", stderr)
	}
}

func TestOrchestratorPlacesContainerWithReferenceTitle(t *testing.T) {
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
	wantPlacement(t, probe, second+" +2")
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings=%q", result.Warnings)
	}
}

// chat and issue triage set no title, so the backend falls back to the label.
func TestChatAndTriagePlaceContainerWithoutTitle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	useChatConfig(t, "claude", "title-test")
	probe := useTitleBackend(t, "")
	if _, err := StartChat(ChatRequest{Root: root, Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "")
	if !strings.HasPrefix(probe.created[0].label, "chat-") {
		t.Fatalf("label=%q", probe.created[0].label)
	}

	probe = useTitleBackend(t, "")
	request := triageRequest(t, root)
	request.Agent, request.Launcher = "claude", "title-test"
	if _, err := StartTriage(request); err != nil {
		t.Fatal(err)
	}
	wantPlacement(t, probe, "")
	if !strings.HasPrefix(probe.created[0].label, "issue-dualface-kander-42") {
		t.Fatalf("label=%q", probe.created[0].label)
	}
}
