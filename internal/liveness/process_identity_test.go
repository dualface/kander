package liveness

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/process"
)

const (
	identityHelperEnv = "KANDER_LIVENESS_IDENTITY_HELPER"
	identityOwner     = "codex"
)

// TestProcessIdentityHelper is the fake agent of the regression tests. As "parent"
// it stands for a foreground Kander: it starts a child, prints the child's ID and
// exits when its stdin closes.
func TestProcessIdentityHelper(t *testing.T) {
	switch os.Getenv(identityHelperEnv) {
	case "sleep":
		time.Sleep(time.Minute)
	case "parent":
		child := helperCommand("sleep")
		if err := child.Start(); err != nil {
			fmt.Println("0")
			return
		}
		fmt.Println(child.Process.Pid)
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
}

func helperCommand(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessIdentityHelper$")
	cmd.Env = append(os.Environ(), identityHelperEnv+"="+mode)
	return cmd
}

func startFakeAgent(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := helperCommand("sleep")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func observed(t *testing.T, pid int) process.ProcessFacts {
	t.Helper()
	facts, err := process.ObserveProcess(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

// startedForeground puts a card through the same start entrance as kander start.
func startedForeground(t *testing.T, root, slug, window string) (board.Entry, string) {
	t.Helper()
	id := todayID(slug)
	if _, err := board.NewTask(root, "chore", slug, slug, "en", false); err != nil {
		t.Fatal(err)
	}
	makeReady(t, filepath.Join(root, "backlog", id, "spec.md"))
	todo, err := board.MoveEntry(currentEntry(t, root, id), root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(todo, root, board.TaskStartInput{Owner: identityOwner, Session: identityOwner + " session-1", Window: window})
	if err != nil {
		t.Fatal(err)
	}
	s, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return moved, s.Text
}

func recordIdentity(t *testing.T, root string, entry board.Entry, identity board.ExecutionProcess) {
	t.Helper()
	if _, err := board.RecordExecutionProcess(root, entry, identity); err != nil {
		t.Fatal(err)
	}
}

// foregroundIdentity is what a launcher records for agent, started by this test.
func foregroundIdentity(t *testing.T, pid int) board.ExecutionProcess {
	t.Helper()
	boot, err := process.BootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	agent, self := observed(t, pid), observed(t, os.Getpid())
	return board.ExecutionProcess{Launcher: "foreground", KanderPID: self.PID, KanderStart: self.Start, AgentPID: agent.PID, AgentStart: agent.Start, BootID: boot, Executable: agent.Executable}
}

func requireIdentityPlatform(t *testing.T) {
	t.Helper()
	if !process.ProcessIdentitySupported {
		t.Skip("no process identity on this platform")
	}
}

func classified(t *testing.T, root string, entry board.Entry) Report {
	t.Helper()
	s, err := board.ReadSnapshot(root, entry.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return ClassifyTaskContext(context.Background(), s.Entry, s.Text)
}

func TestForegroundProcessLifecycle(t *testing.T) {
	requireIdentityPlatform(t)
	root := tempBoard(t)
	agent := startFakeAgent(t)
	entry, _ := startedForeground(t, root, "identity-alive", "foreground")
	recordIdentity(t, root, entry, foregroundIdentity(t, agent.Process.Pid))
	got := classified(t, root, entry)
	if got.Status != Alive || !got.ObservationValid || got.Channel != "foreground" {
		t.Fatalf("report=%+v", got)
	}
	_ = agent.Process.Kill()
	_ = agent.Wait()
	if got = classified(t, root, entry); got.Status != Stopped || !got.ObservationValid {
		t.Fatalf("report=%+v", got)
	}
}

// A live process that holds the recorded ID is not the executor.
func TestAnotherProcessInTheRecordedIdentityIsNotAlive(t *testing.T) {
	requireIdentityPlatform(t)
	for _, c := range []struct {
		name   string
		damage func(*board.ExecutionProcess)
		want   string
	}{
		{"start time", func(p *board.ExecutionProcess) { p.AgentStart = "1" }, Stopped},
		{"executable", func(p *board.ExecutionProcess) { p.Executable = "other" }, IdentityMismatch},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := tempBoard(t)
			agent := startFakeAgent(t)
			entry, _ := startedForeground(t, root, "identity-occupied", "foreground")
			identity := foregroundIdentity(t, agent.Process.Pid)
			c.damage(&identity)
			recordIdentity(t, root, entry, identity)
			if got := classified(t, root, entry); got.Status != c.want {
				t.Fatalf("report=%+v", got)
			}
		})
	}
}

func TestForegroundStarterExitLeavesTheAgentOrphaned(t *testing.T) {
	requireIdentityPlatform(t)
	root := tempBoard(t)
	starter := helperCommand("parent")
	stdin, err := starter.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := starter.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = starter.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = starter.Process.Kill(); _ = starter.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid == 0 {
		t.Fatalf("child %q err=%v", line, err)
	}
	t.Cleanup(func() {
		if child, err := os.FindProcess(pid); err == nil {
			_ = child.Kill()
		}
	})
	entry, _ := startedForeground(t, root, "identity-orphan", "foreground")
	identity := foregroundIdentity(t, pid)
	boss := observed(t, starter.Process.Pid)
	identity.KanderPID, identity.KanderStart = boss.PID, boss.Start
	recordIdentity(t, root, entry, identity)
	if got := classified(t, root, entry); got.Status != Alive {
		t.Fatalf("report=%+v", got)
	}
	_ = stdin.Close()
	if err = starter.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := classified(t, root, entry); got.Status != AliveOrphaned || !got.ObservationValid {
		t.Fatalf("report=%+v", got)
	}
}

// fakeProcessProbe answers for a fixed process table. A missing ID is gone.
func fakeProcessProbe(t *testing.T, table map[int]process.ProcessFacts, failures map[int]error, boot string, bootErr error, supported bool) {
	t.Helper()
	saved := processProbe
	t.Cleanup(func() { processProbe = saved })
	processProbe.supported = func() bool { return supported }
	processProbe.boot = func() (string, error) { return boot, bootErr }
	processProbe.observe = func(_ context.Context, pid int) (process.ProcessFacts, error) {
		if err := failures[pid]; err != nil {
			return process.ProcessFacts{}, err
		}
		facts, ok := table[pid]
		if !ok {
			return process.ProcessFacts{}, process.ErrProcessGone
		}
		return facts, nil
	}
}

// Every step of the check, in order: the first failed step decides.
func TestProcessIdentitySteps(t *testing.T) {
	const kander, agent = 100, 200
	foreground := board.ExecutionProcess{Launcher: "foreground", KanderPID: kander, KanderStart: "k1", AgentPID: agent, AgentStart: "a1", BootID: "boot-1", Executable: "node"}
	console := board.ExecutionProcess{Launcher: "console", AgentPID: agent, AgentStart: "a1", BootID: "boot-1", Executable: "node"}
	healthy := func() map[int]process.ProcessFacts {
		return map[int]process.ProcessFacts{
			kander: {PID: kander, ParentPID: 1, Start: "k1", Executable: "kander"},
			agent:  {PID: agent, ParentPID: kander, Start: "a1", Executable: "node"},
		}
	}
	denied := fmt.Errorf("%w: test", process.ErrProcessDenied)
	timedOut := errors.New("probe timed out")
	cases := []struct {
		name      string
		identity  board.ExecutionProcess
		edit      func(table map[int]process.ProcessFacts)
		failures  map[int]error
		boot      string
		bootErr   error
		unsupport bool
		owner     string
		cycle     string
		status    string
		detail    string
		valid     bool
	}{
		{name: "unsupported platform", identity: foreground, unsupport: true, status: Unprobeable, detail: "不支持"},
		{name: "reboot", identity: foreground, boot: "boot-2", status: Stopped, detail: "系统已重启", valid: true},
		{name: "boot read failure", identity: foreground, bootErr: timedOut, status: Unknown, detail: "timed out"},
		{name: "boot read denied", identity: foreground, bootErr: denied, status: Unprobeable, detail: "拒绝"},
		{name: "agent gone", identity: foreground, edit: func(m map[int]process.ProcessFacts) { delete(m, agent) }, status: Stopped, detail: "已退出", valid: true},
		{name: "agent denied", identity: foreground, failures: map[int]error{agent: denied}, status: Unprobeable, detail: "拒绝"},
		{name: "agent timeout", identity: foreground, failures: map[int]error{agent: timedOut}, status: Unknown, detail: "timed out"},
		{name: "pid reused", identity: foreground, edit: func(m map[int]process.ProcessFacts) {
			m[agent] = process.ProcessFacts{PID: agent, ParentPID: kander, Start: "a2", Executable: "node"}
		}, status: Stopped, detail: "已复用", valid: true},
		{name: "executable changed", identity: foreground, edit: func(m map[int]process.ProcessFacts) {
			m[agent] = process.ProcessFacts{PID: agent, ParentPID: kander, Start: "a1", Executable: "python"}
		}, status: IdentityMismatch, detail: "python", valid: true},
		{name: "owner changed", identity: foreground, owner: "claude", status: IdentityMismatch, detail: "其他执行", valid: true},
		{name: "cycle changed", identity: foreground, cycle: "2020-01-01 00:00", status: IdentityMismatch, detail: "其他执行", valid: true},
		{name: "starter gone", identity: foreground, edit: func(m map[int]process.ProcessFacts) { delete(m, kander) }, status: AliveOrphaned, detail: "100", valid: true},
		{name: "starter pid reused", identity: foreground, edit: func(m map[int]process.ProcessFacts) {
			m[kander] = process.ProcessFacts{PID: kander, Start: "k2", Executable: "other"}
		}, status: AliveOrphaned, valid: true},
		{name: "starter is no parent", identity: foreground, edit: func(m map[int]process.ProcessFacts) {
			m[agent] = process.ProcessFacts{PID: agent, ParentPID: 1, Start: "a1", Executable: "node"}
		}, status: AliveOrphaned, valid: true},
		{name: "starter denied", identity: foreground, failures: map[int]error{kander: denied}, status: Unprobeable, detail: "拒绝"},
		{name: "starter timeout", identity: foreground, failures: map[int]error{kander: timedOut}, status: Unknown, detail: "timed out"},
		{name: "foreground match", identity: foreground, status: Alive, detail: "200", valid: true},
		{name: "console match", identity: console, edit: func(m map[int]process.ProcessFacts) { delete(m, kander) }, status: Alive, valid: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := tempBoard(t)
			window := c.identity.Launcher
			entry, text := startedForeground(t, root, "identity-steps", window)
			recordIdentity(t, root, entry, c.identity)
			table := healthy()
			if c.edit != nil {
				c.edit(table)
			}
			boot := c.boot
			if boot == "" {
				boot = "boot-1"
			}
			fakeProcessProbe(t, table, c.failures, boot, c.bootErr, !c.unsupport)
			if c.owner != "" {
				text = strings.Replace(text, "- OWNER: "+identityOwner, "- OWNER: "+c.owner, 1)
			}
			if c.cycle != "" {
				text = strings.Replace(text, "- STARTED_AT: "+board.MetadataFrom(text, board.FieldStartedAt), "- STARTED_AT: "+c.cycle, 1)
			}
			got := ClassifyTaskContext(context.Background(), entry, text)
			if got.Status != c.status || !strings.Contains(got.Detail, c.detail) || got.ObservationValid != c.valid {
				t.Fatalf("report=%+v", got)
			}
		})
	}
}

func TestMissingProcessIdentityIsUnknown(t *testing.T) {
	root := tempBoard(t)
	t.Run("old card", func(t *testing.T) {
		id, path := makeWorking(t, "identity-legacy", "legacy")
		setLocation(t, path, "codex session-1", "foreground")
		// A binary from before the ledger left no record of this card.
		if err := os.RemoveAll(filepath.Join(root, ".kander", "groups", "00000000-start-group", id)); err != nil {
			t.Fatal(err)
		}
		s, err := board.ReadSnapshot(root, id)
		if err != nil {
			t.Fatal(err)
		}
		got := ClassifyTaskContext(context.Background(), s.Entry, s.Text)
		if got.Status != Unknown || !strings.Contains(got.Detail, "旧卡没有进程身份") {
			t.Fatalf("report=%+v", got)
		}
	})
	t.Run("launcher got no identity", func(t *testing.T) {
		entry, _ := startedForeground(t, root, "identity-unrecorded", "console")
		got := classified(t, root, entry)
		if got.Status != Unknown || !strings.Contains(got.Detail, "启动者没有记录") {
			t.Fatalf("report=%+v", got)
		}
	})
}

// A probe cut short never reads as alive.
func TestAProbeThatEndsEarlyIsNeverAlive(t *testing.T) {
	requireIdentityPlatform(t)
	root := tempBoard(t)
	agent := startFakeAgent(t)
	entry, text := startedForeground(t, root, "identity-canceled", "foreground")
	recordIdentity(t, root, entry, foregroundIdentity(t, agent.Process.Pid))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := ClassifyTaskContext(ctx, entry, text); got.Status != Unknown || got.ObservationValid {
		t.Fatalf("report=%+v", got)
	}
}

func TestCheckPrintsTheProcessResults(t *testing.T) {
	requireIdentityPlatform(t)
	root := tempBoard(t)
	agent := startFakeAgent(t)
	entry, _ := startedForeground(t, root, "identity-check", "foreground")
	identity := foregroundIdentity(t, agent.Process.Pid)
	identity.Executable = "other"
	recordIdentity(t, root, entry, identity)
	code, out, _ := capture(t, func() int { return RunCheck([]string{"--all"}) })
	if code != 0 || !strings.Contains(out, "状态="+IdentityMismatch) || !strings.Contains(out, "人工核对") {
		t.Fatalf("code=%d out=%s", code, out)
	}
	lines := formatReport(Report{TaskID: entry.TaskID, Status: AliveOrphaned, ObservedAt: time.Now()})
	if !strings.Contains(lines, "--adopt") {
		t.Fatal(lines)
	}
}
