//go:build unix

package launch

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"

	"golang.org/x/sys/unix"
)

// The native wrapper waits for an explicit gate, then execs a native reader.
// Only the final image reads the task file; the test never manufactures that
// evidence by polling the same process observer it is trying to verify.
func TestLateExecOfANativeWrapperIsNeverAMismatch(t *testing.T) {
	if os.Getenv("KANDER_IDENTITY_EXEC_CHILD") == "1" {
		var gate [1]byte
		if _, err := io.ReadFull(os.Stdin, gate[:]); err != nil {
			t.Fatal(err)
		}
		program := os.Getenv("KANDER_IDENTITY_EXEC_PROGRAM")
		if err := unix.Exec(program, []string{program, "-f", os.Getenv("KANDER_IDENTITY_EXEC_TASK")}, os.Environ()); err != nil {
			t.Fatal(err)
		}
		return
	}
	requireAccessTimes(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "identity-wrapper")
	path := filepath.Join(t.TempDir(), "task")
	if err := os.WriteFile(path, []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := exec.LookPath("tail")
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	wrapper := exec.Command(os.Args[0], "-test.run=^TestLateExecOfANativeWrapperIsNeverAMismatch$")
	wrapper.Env = append(os.Environ(), "KANDER_IDENTITY_EXEC_CHILD=1", "KANDER_IDENTITY_EXEC_PROGRAM="+program, "KANDER_IDENTITY_EXEC_TASK="+path)
	wrapper.Stdin, wrapper.Stderr = reader, os.Stderr
	moved := beginDirect(t, root, task, "foreground")
	record := processRecorder(root, moved, LaunchPlan{Launcher: "foreground"}, path)
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	t.Cleanup(func() { _ = wrapper.Process.Kill(); _ = wrapper.Wait() })
	var exited atomic.Bool
	done := record(wrapper.Process.Pid, func() bool { return !exited.Load() })
	t.Cleanup(func() { exited.Store(true); <-done })
	time.Sleep(time.Second)
	if current, _, err := board.CurrentExecution(root, task); err != nil || current.Process != nil {
		t.Fatalf("recorded before the exec: %+v err=%v", current, err)
	}
	if got := observeCard(t, root, task); got.Status != liveness.Unknown {
		t.Fatalf("report before exec=%+v", got)
	}
	if _, err := writer.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the identity step did not end after the native reader exec")
	}
	if identity := requireIdentity(t, root, task); identity.Executable != filepath.Base(program) {
		t.Fatalf("identity=%+v", identity)
	}
	if got := observeCard(t, root, task); got.Status != liveness.Alive {
		t.Fatalf("report=%+v", got)
	}
}
