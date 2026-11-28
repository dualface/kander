package process

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ResolveAgentProgramIn and LookPathIn search the PATH they are given, never the PATH of this
// process, and skip relative directories.
func TestResolveAgentProgramInSearchesTheGivenPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a POSIX executable")
	}
	recorded := t.TempDir()
	agent := filepath.Join(recorded, "fake-agent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if program := ResolveAgentProgram("fake-agent"); program != nil {
		t.Fatalf("the process PATH resolved %+v", program)
	}
	list := "relative" + string(os.PathListSeparator) + recorded
	if program := ResolveAgentProgramIn("fake-agent", list); program == nil || program.Path != agent {
		t.Fatalf("recorded PATH resolved %+v", program)
	}
	if found, err := LookPathIn(list, "fake-agent"); err != nil || found != agent {
		t.Fatalf("LookPathIn = %q %v", found, err)
	}
	if _, err := LookPathIn(t.TempDir(), "fake-agent"); err == nil {
		t.Fatal("LookPathIn found an agent outside the given PATH")
	}
	if found, err := LookPathIn("", agent); err != nil || found != agent {
		t.Fatalf("an explicit path = %q %v", found, err)
	}
}
