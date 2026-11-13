package board

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

const modelChildEnv = "KANDER_MODEL_CHILD"

// modelSubprocess keeps the search's millions of file reads out of the testlog
// that go test reads into memory for its cache. It returns true in the parent.
func modelSubprocess(t *testing.T, name string) bool {
	t.Helper()
	if os.Getenv(modelChildEnv) == name {
		return false
	}
	// Child reads are invisible to the cache: register the search mode here.
	_ = os.Getenv(modelDepthEnv)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^" + name + "$", "-test.timeout=0"}
	ctx := t.Context()
	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("model subprocess: parent deadline already expired")
		}
		// Leave time for the child's timeout dump and the parent's failure report.
		// Cap the reserve so long CI searches keep almost their entire budget.
		reserve := min(time.Second, remaining/10)
		args[1] = "-test.timeout=" + (remaining - reserve).String()
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-reserve/2))
		defer cancel()
	}
	if testing.Verbose() {
		args = append(args, "-test.v=true")
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = append(os.Environ(), modelChildEnv+"="+name)
	// Direct forwarding preserves scenario logs even before the child exits.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("model subprocess failed: %v (output above)", err)
	}
	return true
}
