package process

import (
	"slices"
	"testing"
)

func TestDaemonEnvironmentDropsOnlyTheExecutionAndManagedVariables(t *testing.T) {
	env := []string{"PATH=/bin", EnvExecutionID + "=exec-1", "KANDER_CLIENT_HOME=/home/c", EnvCoordinatorExecutionID + "=exec-2", EnvStartOrder + "=so-1", EnvManaged + "=1", "EMPTY=", "kander_execution_id=lower"}
	got := daemonEnvironment(env, false, []string{"KANDER_CLIENT_HOME=/other"})
	want := []string{"PATH=/bin", "KANDER_CLIENT_HOME=/home/c", "EMPTY=", "kander_execution_id=lower", "KANDER_CLIENT_HOME=/other"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	folded := daemonEnvironment(env, true, nil)
	if slices.Contains(folded, "kander_execution_id=lower") || len(folded) != 3 {
		t.Fatalf("Windows ignores the case of a name: %v", folded)
	}
}

func TestDaemonEnvironmentReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv(EnvExecutionID, "exec-1")
	t.Setenv(EnvCoordinatorExecutionID, "exec-2")
	t.Setenv("KANDER_KEEP_ME", "yes")
	got := DaemonEnvironment("EXTRA=1")
	for _, entry := range got {
		if entry == EnvExecutionID+"=exec-1" || entry == EnvCoordinatorExecutionID+"=exec-2" {
			t.Fatalf("an execution variable leaked: %q", entry)
		}
	}
	if !slices.Contains(got, "KANDER_KEEP_ME=yes") || got[len(got)-1] != "EXTRA=1" {
		t.Fatalf("other variables stay and extra comes last: %v", got)
	}
}
