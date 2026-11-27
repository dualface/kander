package remote

import (
	"os"
	"os/exec"
	"testing"

	"github.com/dualface/kander/v2/internal/process"
)

func TestMain(m *testing.M) {
	clearExecutionEnv()
	os.Exit(m.Run())
}

// clearExecutionEnv drops the execution identity an executing agent session
// carries. Session picks only that execution's credential when the variable
// is set, so the tests would depend on the runner's session; a test that
// needs a value sets it with t.Setenv. os.Unsetenv rather than an empty value,
// because serverInteractive tells a set variable from an absent one.
func clearExecutionEnv() {
	for _, name := range []string{process.EnvExecutionID, process.EnvCoordinatorExecutionID} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
}

// TestMainClearsInheritedExecutionEnv re-runs this test binary with both
// execution identity variables set, so the regression shows up in an ordinary
// run where the runner's session carries neither.
func TestMainClearsInheritedExecutionEnv(t *testing.T) {
	const childMarker = "KANDER_REMOTE_TEST_EXECUTION_ENV_CHILD"
	names := []string{process.EnvExecutionID, process.EnvCoordinatorExecutionID}
	if os.Getenv(childMarker) == "1" {
		for _, name := range names {
			if value, ok := os.LookupEnv(name); ok {
				t.Fatalf("%s survived TestMain: %q", name, value)
			}
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainClearsInheritedExecutionEnv$", "-test.count=1")
	cmd.Env = append(os.Environ(), childMarker+"=1")
	for _, name := range names {
		cmd.Env = append(cmd.Env, name+"=inherited-"+name)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child test failed: %v\n%s", err, out)
	}
}
