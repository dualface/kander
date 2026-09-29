package process

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentPathUsesForwardSlashesOnlyOnWindows(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`C:\Users\x\proj\.kander\bin\kander`, "C:/Users/x/proj/.kander/bin/kander"},
		{`C:\Users\x\AppData\Local\Temp\kander-task-1.md`, "C:/Users/x/AppData/Local/Temp/kander-task-1.md"},
		{`\\server\share\proj\AGENTS.md`, "//server/share/proj/AGENTS.md"},
		{"C:/already/forward", "C:/already/forward"},
		{`C:\Program Files\kander\kander`, "C:/Program Files/kander/kander"},
	} {
		if got := agentPath(tc.in, true); got != tc.want {
			t.Errorf("agentPath(%q, windows) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, posix := range []string{"/home/x/proj/.kander/bin/kander", `/tmp/odd\name.md`, "relative/path"} {
		if got := agentPath(posix, false); got != posix {
			t.Errorf("agentPath(%q, posix) = %q, want it unchanged", posix, got)
		}
	}
}

// The task file and its one-line pointer show the agent-facing path, while the file itself is still
// created and read at its native path.
func TestTaskFileShowsAgentPath(t *testing.T) {
	path, err := CreateTaskFile("body", "kander-agent-path-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	if !filepath.IsAbs(path) {
		t.Fatalf("task file path %q is not native absolute", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	instruction := TaskFileInstruction("Execute this task.", path)
	for _, text := range []string{string(content), instruction} {
		if !strings.Contains(text, AgentPath(path)) {
			t.Fatalf("missing agent path in %q", text)
		}
		if runtime.GOOS == "windows" && strings.Contains(text, `\`) {
			t.Fatalf("Windows task text still has a backslash: %q", text)
		}
		if runtime.GOOS != "windows" && !strings.Contains(text, path) {
			t.Fatalf("POSIX task text changed the path: %q", text)
		}
	}
}
