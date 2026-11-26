package launch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

const agyTestPrompt = "Read the UTF-8 task file /tmp/kander-task-start-123.md and follow it."

// agyBrainDir is the brain directory below the current test home.
func agyBrainDir(t *testing.T) string {
	t.Helper()
	root, err := agyBrainRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// agyHome points the user home at a temporary directory and returns the brain
// directory below it.
func agyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return agyBrainDir(t)
}

// agyTranscriptLine is the first transcript line agy writes for a user request,
// followed by the newline that ends it.
func agyTranscriptLine(request string) string {
	return agyTranscriptRecord("USER_EXPLICIT", "USER_INPUT", "<USER_REQUEST>\n"+request+"\n</USER_REQUEST>") + "\n"
}

func agyTranscriptRecord(source, kind, content string) string {
	data, err := json.Marshal(map[string]any{"source": source, "type": kind, "content": content, "step_index": 0})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func agyTranscriptPath(root, id string) string {
	return filepath.Join(root, id, ".system_generated", "logs", "transcript.jsonl")
}

func writeAgyTranscript(t *testing.T, root, id, firstLine string) string {
	t.Helper()
	path := agyTranscriptPath(root, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(firstLine), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newAgyScan(root string, previous map[string]struct{}, prompt string) *agyTranscriptScan {
	return &agyTranscriptScan{root: root, previous: previous, prompt: prompt, settled: map[string]struct{}{}}
}

// discoverNewAgySession runs Antigravity conversation discovery for a launch
// that sent prompt, with the given wait.
func discoverNewAgySession(previous map[string]struct{}, prompt string, wait time.Duration) (string, error) {
	def := &config.AgentSessionDefinition{Mode: "hook:agy-transcript"}
	l := sessionLaunch{TaskID: "task", Prompt: prompt, Definition: def, Budget: fixedBudget(wait)}
	return discoverLaunchSession(mustSessionProvider(def), l, previous)
}

// frozenDiscoveryClock makes discovery waits take no real time.
func frozenDiscoveryClock(t *testing.T) {
	t.Helper()
	previousNow, previousSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = previousNow, previousSleep })
	clock := time.Unix(1_800_000_000, 0)
	nowFn = func() time.Time { return clock }
	sleepFn = func(d time.Duration) { clock = clock.Add(d) }
}

func TestAgyProviderDeclaresOnlyDiscovery(t *testing.T) {
	provider := mustSessionProvider(&config.AgentSessionDefinition{Mode: "hook:agy-transcript"})
	if !provider.Resumable() {
		t.Fatal("an agy conversation can be resumed")
	}
	if _, ok := provider.(sessionDiscoverer); !ok {
		t.Fatal("agy sessions are discovered after start")
	}
	if _, ok := provider.(sessionAllocator); ok {
		t.Fatal("agy has no id to give before launch")
	}
	discoverer := provider.(sessionDiscoverer)
	if !discoverer.Persists() || discoverer.Wait(sessionLaunch{}) != 30*time.Second {
		t.Fatalf("persists=%v wait=%v", discoverer.Persists(), discoverer.Wait(sessionLaunch{}))
	}
	extended := sessionLaunch{Budget: func(d time.Duration) time.Duration { return max(d, 120*time.Second) }}
	if got := discoverer.Wait(extended); got != 120*time.Second {
		t.Fatalf("extended launch timeouts must raise the wait: %v", got)
	}
}

func TestAgySnapshotToleratesMissingRoot(t *testing.T) {
	agyHome(t)
	def := &config.AgentSessionDefinition{Mode: "hook:agy-transcript"}
	previous, err := sessionDiscoverSnapshot(def, "task", true, nil, "", fixedBudget(time.Second))
	if err != nil || len(previous) != 0 {
		t.Fatalf("previous=%v err=%v", previous, err)
	}
}

func TestAgyDiscoveryPicksTheNewConversationOfTheLaunchInstruction(t *testing.T) {
	root := agyHome(t)
	def := &config.AgentSessionDefinition{Mode: "hook:agy-transcript"}
	// An earlier launch of the same task used the same instruction text.
	writeAgyTranscript(t, root, "0d1c-old", agyTranscriptLine(agyTestPrompt))
	previous, err := sessionDiscoverSnapshot(def, "task", true, nil, "", fixedBudget(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]struct{}{"0d1c-old": {}}; !reflect.DeepEqual(previous, want) {
		t.Fatalf("previous=%v", previous)
	}
	writeAgyTranscript(t, root, "1a2b-mine", agyTranscriptLine(agyTestPrompt))
	writeAgyTranscript(t, root, "3c4d-other-task", agyTranscriptLine("Read the UTF-8 task file /tmp/kander-other-start-456.md and follow it."))
	writeAgyTranscript(t, root, "5e6f-assistant", agyTranscriptRecord("MODEL", "PLANNER_RESPONSE", "<USER_REQUEST>\n"+agyTestPrompt+"\n</USER_REQUEST>")+"\n")
	writeAgyTranscript(t, root, "7a8b-garbage", "not json\n")
	if err := os.WriteFile(filepath.Join(root, "stray-file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := sessionLaunch{TaskID: "task", Prompt: agyTestPrompt, Definition: def, Budget: fixedBudget(time.Second)}
	got, err := discoverLaunchSession(mustSessionProvider(def), l, previous)
	if err != nil || got != "1a2b-mine" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestAgyScanMatchesOnlyTheExactRequest(t *testing.T) {
	root := agyHome(t)
	for id, line := range map[string]string{
		"exact":         agyTranscriptLine(agyTestPrompt),
		"extra-text":    agyTranscriptLine(agyTestPrompt + " Also do something else."),
		"leading-text":  agyTranscriptLine("Please " + agyTestPrompt),
		"plan-prefix":   agyTranscriptLine("/plan " + agyTestPrompt),
		"no-open-tag":   agyTranscriptRecord("USER_EXPLICIT", "USER_INPUT", agyTestPrompt+"\n</USER_REQUEST>") + "\n",
		"unclosed":      agyTranscriptRecord("USER_EXPLICIT", "USER_INPUT", "<USER_REQUEST>\n"+agyTestPrompt) + "\n",
		"model-source":  agyTranscriptRecord("MODEL", "USER_INPUT", "<USER_REQUEST>\n"+agyTestPrompt+"\n</USER_REQUEST>") + "\n",
		"wrong-type":    agyTranscriptRecord("USER_EXPLICIT", "PLANNER_RESPONSE", "<USER_REQUEST>\n"+agyTestPrompt+"\n</USER_REQUEST>") + "\n",
		"empty-content": agyTranscriptRecord("USER_EXPLICIT", "USER_INPUT", "") + "\n",
	} {
		writeAgyTranscript(t, root, id, line)
	}
	scan := newAgyScan(root, map[string]struct{}{}, agyTestPrompt)
	got, err := scan.Candidates(t.Context())
	if err != nil || !reflect.DeepEqual(got, []string{"exact"}) {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, settled := scan.settled["plan-prefix"]; !settled {
		t.Fatal("a conversation that can never match is not read again")
	}
}

func TestAgyScanAcceptsClientMetadataAfterTheRequest(t *testing.T) {
	root := agyHome(t)
	line := agyTranscriptRecord("USER_EXPLICIT", "USER_INPUT", "<USER_REQUEST>\n"+agyTestPrompt+"\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nworkspace: /tmp/w\n</ADDITIONAL_METADATA>") + "\n"
	writeAgyTranscript(t, root, "with-metadata", line)
	got, err := newAgyScan(root, map[string]struct{}{}, agyTestPrompt).Candidates(t.Context())
	if err != nil || !reflect.DeepEqual(got, []string{"with-metadata"}) {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestAgyScanRetriesAFirstLineStillBeingWritten(t *testing.T) {
	root := agyHome(t)
	line := agyTranscriptLine(agyTestPrompt)
	path := writeAgyTranscript(t, root, "cccc", strings.TrimSuffix(line, "\n"))
	scan := newAgyScan(root, map[string]struct{}{}, agyTestPrompt)
	got, err := scan.Candidates(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("partial first line: got=%q err=%v", got, err)
	}
	if _, settled := scan.settled["cccc"]; settled {
		t.Fatal("a partial first line must be read again")
	}
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = scan.Candidates(t.Context())
	if err != nil || !reflect.DeepEqual(got, []string{"cccc"}) {
		t.Fatalf("complete first line: got=%q err=%v", got, err)
	}
}

func TestAgyScanRetriesAConversationWithoutATranscriptYet(t *testing.T) {
	root := agyHome(t)
	if err := os.MkdirAll(filepath.Join(root, "dddd"), 0o700); err != nil {
		t.Fatal(err)
	}
	scan := newAgyScan(root, map[string]struct{}{}, agyTestPrompt)
	got, err := scan.Candidates(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, settled := scan.settled["dddd"]; settled {
		t.Fatal("a missing transcript must be looked for again")
	}
	writeAgyTranscript(t, root, "dddd", agyTranscriptLine(agyTestPrompt))
	if got, err = scan.Candidates(t.Context()); err != nil || !reflect.DeepEqual(got, []string{"dddd"}) {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestAgyScanSettlesAnOverlongFirstLine(t *testing.T) {
	root := agyHome(t)
	writeAgyTranscript(t, root, "long", strings.Repeat("x", agyTranscriptHeaderLimit+1))
	scan := newAgyScan(root, map[string]struct{}{}, agyTestPrompt)
	got, err := scan.Candidates(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, settled := scan.settled["long"]; !settled {
		t.Fatal("a first line past the limit can never be the launch instruction")
	}
}

func TestAgyScanDoesNotFollowASymbolicLinkTranscript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symbolic links needs a privilege on Windows")
	}
	root := agyHome(t)
	target := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	if err := os.WriteFile(target, []byte(agyTranscriptLine(agyTestPrompt)), 0o600); err != nil {
		t.Fatal(err)
	}
	link := agyTranscriptPath(root, "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, err := newAgyScan(root, map[string]struct{}{}, agyTestPrompt).Candidates(t.Context())
	if len(got) != 0 || err == nil {
		t.Fatalf("a linked transcript must be refused: got=%q err=%v", got, err)
	}
}

func TestAgyScanIgnoresASymbolicLinkConversationDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symbolic links needs a privilege on Windows")
	}
	root := agyHome(t)
	real := filepath.Join(t.TempDir(), "real")
	writeAgyTranscript(t, real, "inner", agyTranscriptLine(agyTestPrompt))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "inner"), filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	got, err := newAgyScan(root, map[string]struct{}{}, agyTestPrompt).Candidates(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("a linked conversation directory must be ignored: got=%q err=%v", got, err)
	}
}

func TestAgyDiscoveryRejectsSeveralMatchingConversations(t *testing.T) {
	root := agyHome(t)
	writeAgyTranscript(t, root, "aaaa", agyTranscriptLine(agyTestPrompt))
	writeAgyTranscript(t, root, "bbbb", agyTranscriptLine(agyTestPrompt))
	_, err := discoverNewAgySession(map[string]struct{}{}, agyTestPrompt, time.Second)
	if err == nil || err.Error() != launchError("launch.multiple_new_sessions_appeared_during_launch", "2").Error() {
		t.Fatalf("err=%v", err)
	}
}

func TestAgyDiscoveryTimesOutWithoutAMatchingConversation(t *testing.T) {
	root := agyHome(t)
	writeAgyTranscript(t, root, "aaaa", agyTranscriptLine("Read the UTF-8 task file /tmp/other.md and follow it."))
	frozenDiscoveryClock(t)
	_, err := discoverNewAgySession(map[string]struct{}{}, agyTestPrompt, time.Second)
	if err == nil || err.Error() != launchError("launch.the_newly_started_session_has_not_appeared_yet").Error() {
		t.Fatalf("err=%v", err)
	}
}

func TestAgyDiscoveryBindsNothingWithoutALaunchInstruction(t *testing.T) {
	root := agyHome(t)
	writeAgyTranscript(t, root, "aaaa", agyTranscriptLine(""))
	frozenDiscoveryClock(t)
	if got, err := discoverNewAgySession(map[string]struct{}{}, "", time.Second); err == nil {
		t.Fatalf("an empty instruction bound %q", got)
	}
}

// writeUnreadableAgyTranscript writes a transcript of the launch that this user
// cannot open.
func writeUnreadableAgyTranscript(t *testing.T, root, id string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file mode 0 does not deny reads on Windows or to root")
	}
	if err := os.Chmod(writeAgyTranscript(t, root, id, agyTranscriptLine(agyTestPrompt)), 0); err != nil {
		t.Fatal(err)
	}
}

func TestAgyDiscoveryReadsPastAnUnreadableTranscript(t *testing.T) {
	root := agyHome(t)
	writeUnreadableAgyTranscript(t, root, "eeee")
	writeAgyTranscript(t, root, "ffff", agyTranscriptLine(agyTestPrompt))
	got, err := discoverNewAgySession(map[string]struct{}{}, agyTestPrompt, time.Second)
	if err != nil || got != "ffff" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestAgyDiscoveryReportsTheReadErrorWithoutAMatch(t *testing.T) {
	root := agyHome(t)
	writeUnreadableAgyTranscript(t, root, "eeee")
	frozenDiscoveryClock(t)
	_, err := discoverNewAgySession(map[string]struct{}{}, agyTestPrompt, time.Second)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err=%v", err)
	}
}
