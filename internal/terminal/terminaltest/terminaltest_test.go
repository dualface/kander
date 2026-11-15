package terminaltest

import (
	"os"
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {
	Main()
	os.Exit(m.Run())
}

func TestReplyEnvSetMatchesOnlyWhileVariableIsSet(t *testing.T) {
	fake := New(t,
		Reply{Args: []string{"send"}, EnvSet: "KANDER_FAKE_SENDER", Code: 7},
		Reply{Args: []string{"send"}},
	)
	for _, tc := range []struct {
		name string
		env  string
		want int
	}{
		{"variable set", "KANDER_FAKE_SENDER=t2b", 7},
		{"variable empty", "KANDER_FAKE_SENDER=", 0},
		{"variable unset", "KANDER_FAKE_OTHER=x", 0},
	} {
		cmd := exec.Command(fake.Program, "send")
		cmd.Env = append(os.Environ(), ActivationEnv+"="+fake.dir, tc.env)
		err := cmd.Run()
		got := 0
		if exit, ok := err.(*exec.ExitError); ok {
			got = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: exit=%d want %d", tc.name, got, tc.want)
		}
	}
}
