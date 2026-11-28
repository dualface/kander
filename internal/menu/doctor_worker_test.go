package menu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/svcinstall"
	"github.com/dualface/kander/v2/internal/version"
)

// workerDoctor runs the worker check against a client home in a temporary
// directory and returns the verdict and the report lines.
func workerDoctor(t *testing.T, home string) (bool, []ReportLine) {
	t.Helper()
	t.Setenv(remote.EnvClientHome, home)
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(config.EnvLang, "en")
	var healthy bool
	lines := CaptureReport(func() { healthy = reportBackgroundWorker() })
	return healthy, lines
}

func joinLines(lines []ReportLine) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line.Level + ": " + line.Text + "\n")
	}
	return b.String()
}

// holdLock stands in for a running worker until the test ends.
func holdLock(t *testing.T, home string) {
	t.Helper()
	file, err := fs.OpenLockFile(home, filepath.Join(home, remote.WorkerLockName))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := fs.LockExclusiveContext(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Unlock(); _ = file.Close() })
}

func TestDoctorWorkerCheckWithoutClientDirectoryReportsNothing(t *testing.T) {
	home := filepath.Join(t.TempDir(), "client")
	healthy, lines := workerDoctor(t, home)
	if !healthy || len(lines) != 0 {
		t.Fatalf("healthy=%v lines=%v", healthy, lines)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("doctor must not create the client directory: %v", err)
	}
}

func TestDoctorWorkerCheckNotRunning(t *testing.T) {
	home := t.TempDir()
	healthy, lines := workerDoctor(t, home)
	if !healthy || len(lines) != 1 || lines[0].Level != LevelInfo || !strings.Contains(lines[0].Text, "not running") {
		t.Fatalf("healthy=%v lines=%v", healthy, lines)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("doctor must create no file in the client directory: %v", entries)
	}
	// A lock file that nobody holds, with the record of a worker that has gone.
	if err := remote.WriteWorkerRecord(home, remote.WorkerRecord{Version: "v-gone", PID: 5}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, remote.WorkerLockName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if healthy, lines = workerDoctor(t, home); !healthy || len(lines) != 1 || !strings.Contains(lines[0].Text, "not running") {
		t.Fatalf("a record without a lock holder is ignored: healthy=%v lines=%v", healthy, lines)
	}
}

func TestDoctorWorkerCheckSameVersion(t *testing.T) {
	home := t.TempDir()
	holdLock(t, home)
	if err := remote.WriteWorkerRecord(home, remote.WorkerRecord{Version: version.String(), PID: 4321, Executable: "/x/kander", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	healthy, lines := workerDoctor(t, home)
	if !healthy || len(lines) != 1 || lines[0].Level != LevelOK {
		t.Fatalf("healthy=%v lines=%v", healthy, lines)
	}
	if !strings.Contains(lines[0].Text, version.String()) || !strings.Contains(lines[0].Text, "4321") {
		t.Fatalf("the line names the version and the pid: %s", lines[0].Text)
	}
}

func TestDoctorWorkerCheckAnotherVersionFails(t *testing.T) {
	home := t.TempDir()
	holdLock(t, home)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.WriteWorkerRecord(home, remote.WorkerRecord{Version: "v9.9.9", PID: 4711, Executable: "/old/kander", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	healthy, lines := workerDoctor(t, home)
	if healthy || len(lines) != 1 || lines[0].Level != LevelWarning {
		t.Fatalf("healthy=%v lines=%v", healthy, lines)
	}
	for _, want := range []string{"v9.9.9", version.String(), "4711", "/old/kander", self, "Stop process 4711"} {
		if !strings.Contains(lines[0].Text, want) {
			t.Errorf("the warning lacks %q:\n%s", want, lines[0].Text)
		}
	}
}

func TestDoctorWorkerCheckWithoutAUsableRecordFails(t *testing.T) {
	for name, record := range map[string]string{"no record": "", "another schema": `{"schema":2}`, "not JSON": "{"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			holdLock(t, home)
			if record != "" {
				if err := os.WriteFile(filepath.Join(home, remote.WorkerRecordName), []byte(record), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			healthy, lines := workerDoctor(t, home)
			if healthy || len(lines) != 1 || lines[0].Level != LevelWarning {
				t.Fatalf("healthy=%v lines=%v", healthy, lines)
			}
			for _, want := range []string{"without a usable version record", home, "Stop the kander worker process", "no lease of this host is renewed"} {
				if !strings.Contains(lines[0].Text, want) {
					t.Errorf("the warning lacks %q:\n%s", want, lines[0].Text)
				}
			}
		})
	}
}

// Doctor reads only: the client directory keeps its files. The record keeps its
// modification time and bytes; the lock file keeps its modification time and
// size, because its content cannot be read while the worker holds it.
func TestDoctorWorkerCheckChangesNothing(t *testing.T) {
	home := t.TempDir()
	holdLock(t, home)
	if err := remote.WriteWorkerRecord(home, remote.WorkerRecord{Version: "v9.9.9", PID: 1, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		entries, err := os.ReadDir(home)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				t.Fatal(err)
			}
			if e.Name() == remote.WorkerLockName {
				// A Windows byte-range lock refuses a read through another
				// handle while the worker holds it, so the lock file is
				// compared by metadata only.
				out[e.Name()] = fmt.Sprintf("%s|%d", info.ModTime(), info.Size())
				continue
			}
			data, err := os.ReadFile(filepath.Join(home, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = info.ModTime().String() + "|" + string(data)
		}
		return out
	}
	before := snapshot()
	workerDoctor(t, home)
	after := snapshot()
	if len(before) != len(after) {
		t.Fatalf("files before %v, after %v", before, after)
	}
	for name, content := range before {
		if after[name] != content {
			t.Fatalf("%s changed", name)
		}
	}
}

// Doctor shows both services; a definition whose executable does not exist is a problem.
func TestDoctorReportsTheServices(t *testing.T) {
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(remote.EnvClientHome, home)
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(config.EnvLang, "en")
	svcinstall.UseForTest(t, svcinstall.TestPlatform{GOOS: "linux", Dir: t.TempDir(), User: "alice",
		Run: func(context.Context, string, ...string) ([]byte, error) { return []byte("ActiveState=inactive\n"), nil }})
	var healthy bool
	lines := CaptureReport(func() { healthy = reportServices() })
	if !healthy || !strings.Contains(joinLines(lines), "server service: not installed") || !strings.Contains(joinLines(lines), "worker service: not installed") {
		t.Fatalf("healthy=%v\n%s", healthy, joinLines(lines))
	}
	missing := filepath.Join(t.TempDir(), "kander")
	spec := svcinstall.Spec{Kind: svcinstall.KindWorker, Executable: missing, Args: []string{"worker"}, ClientHome: home, WorkDir: home, LogPath: filepath.Join(home, "worker.log")}
	prepared, err := svcinstall.Prepare(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Write(t.Context()); err != nil {
		t.Fatal(err)
	}
	lines = CaptureReport(func() { healthy = reportServices() })
	if healthy || !strings.Contains(joinLines(lines), missing+", which does not exist") {
		t.Fatalf("a missing executable must be a problem: healthy=%v\n%s", healthy, joinLines(lines))
	}
}
