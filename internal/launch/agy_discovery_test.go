package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeAgyTranscript(t *testing.T, id, prompt string) {
	t.Helper()
	root, err := agyBrainRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, id, ".system_generated", "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	step := map[string]string{"source": "USER_EXPLICIT", "type": "USER_INPUT", "content": "<USER_REQUEST>\n" + prompt + "\n</USER_REQUEST>\n<ADDITIONAL_METADATA>"}
	line, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAgyDiscoveryBindsOnlyTheNewConversationWithTheLaunchInstruction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const prompt = "Read the task file at /tmp/unique-1."
	writeAgyTranscript(t, "existing", prompt)
	previous, err := agySessionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	writeAgyTranscript(t, "concurrent", prompt+" Other instructions")
	writeAgyTranscript(t, "mine", "/plan "+prompt)
	got, err := discoverAgySession(previous, prompt, time.Second)
	if err != nil || got != "mine" {
		t.Fatalf("session=%q err=%v", got, err)
	}
	writeAgyTranscript(t, "duplicate", prompt)
	if _, err := discoverAgySession(previous, prompt, time.Second); err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("ambiguous session: %v", err)
	}
}
