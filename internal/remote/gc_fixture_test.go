package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	kfs "github.com/dualface/kander/v2/internal/fs"
)

// gcCard is one card of the fake service.
type gcCard struct {
	task     api.Task
	finished string
	group    string
}

// gcService is the REST surface the client gc reads, backed by maps.
type gcService struct {
	ProjectService
	mu     sync.Mutex
	cards  map[string]*gcCard
	runs   map[string]any
	runErr error
	// onRun, when set, runs at each read of a review run; onShow at each
	// read of a card, without the service lock.
	onShow func(id string)
	// onRun, when set, runs at each read of a review run.
	onRun       func(id string)
	taskScopes  map[string]api.EvidenceCleanup
	groupScopes map[string]api.EvidenceCleanup
}

func (s *gcService) List(context.Context, string) (api.TaskList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list api.TaskList
	for id, card := range s.cards {
		list.Tasks = append(list.Tasks, api.TaskSummary{TaskID: id, State: card.task.State, CompletedAt: card.finished, TaskGroup: card.group})
	}
	return list, nil
}

func (s *gcService) Show(_ context.Context, id string) (api.Task, error) {
	if s.onShow != nil {
		s.onShow(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	card, found := s.cards[id]
	if !found {
		return api.Task{}, &api.Error{Status: 404, Code: api.CodeNotFound, Message: id}
	}
	return card.task, nil
}

func (s *gcService) ReviewResource(_ context.Context, _, id string) (json.RawMessage, error) {
	if s.onRun != nil {
		s.onRun(id)
	}
	if s.runErr != nil {
		return nil, s.runErr
	}
	run, found := s.runs[id]
	if !found {
		return nil, &api.Error{Status: 404, Code: api.CodeNotFound, Message: id}
	}
	return json.Marshal(run)
}

func (s *gcService) TaskEvidenceCleanup(_ context.Context, task string) (api.EvidenceCleanup, error) {
	return s.taskScopes[task], nil
}

func (s *gcService) GroupEvidenceCleanup(_ context.Context, group string) (api.EvidenceCleanup, error) {
	return s.groupScopes[group], nil
}

// gcFixture is a client home with one project, a bare origin and a clone.
type gcFixture struct {
	t                *testing.T
	s                *Session
	svc              *gcService
	bare, repo, base string
	root             string
	// checkout is the clone the command runs in; the repository by default.
	checkout string
	probe    []string
}

const gcOld = "2026-01-02 03:04"

func gcGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, output, err)
	}
	return strings.TrimSpace(string(output))
}

func newGCFixture(t *testing.T) *gcFixture {
	t.Helper()
	isolateGitConfig(t)
	dir := t.TempDir()
	f := &gcFixture{t: t, bare: filepath.Join(dir, "origin.git"), repo: filepath.Join(dir, "repo")}
	gcGit(t, dir, "init", "--bare", f.bare)
	gcGit(t, dir, "clone", f.bare, f.repo)
	f.write(f.repo, "work.txt", "base\n")
	gcGit(t, f.repo, "add", "work.txt")
	gcGit(t, f.repo, "commit", "-m", "Base")
	f.base = gcGit(t, f.repo, "rev-parse", "HEAD")
	gcGit(t, f.repo, "push", "origin", "HEAD:refs/heads/v2")
	cl, err := client.New("http://127.0.0.1:1", "c-gc-host")
	if err != nil {
		t.Fatal(err)
	}
	f.svc = &gcService{cards: map[string]*gcCard{}, runs: map[string]any{}, taskScopes: map[string]api.EvidenceCleanup{}, groupScopes: map[string]api.EvidenceCleanup{}}
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	f.s = &Session{ServerURL: "http://127.0.0.1:1", Client: cl, Project: f.svc, Info: api.Project{ID: "p-gctest", Name: "demo", Remote: f.bare}, Home: home}
	if f.root, err = f.s.LocalDataRoot(); err != nil {
		t.Fatal(err)
	}
	f.probe = []string{"stopped"}
	f.checkout = f.repo
	return f
}

func (f *gcFixture) write(dir, name, text string) {
	f.t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// card adds a card in state that finished long ago.
func (f *gcFixture) card(id, state string) *gcCard {
	card := &gcCard{task: api.Task{TaskID: id, State: state, Text: "# Card\n\n- TYPE: Feature\n- INTEGRATION_TARGET: v2\n"}, finished: gcOld}
	f.svc.cards[id] = card
	return card
}

// worktree creates the worktree of one generation from base, with its intent.
func (f *gcFixture) worktree(task string, generation uint64) ExecutionWorktree {
	f.t.Helper()
	return f.worktreeOn(task, generation, api.ExecutionBranch(f.s.Info.ID, task, generation))
}

func (f *gcFixture) worktreeOn(task string, generation uint64, branch string) ExecutionWorktree {
	f.t.Helper()
	value := ExecutionWorktree{
		Path:        filepath.Join(f.root, "worktrees", task, fmt.Sprintf("g%d", generation)),
		Branch:      branch,
		Base:        f.base,
		ExecutionID: fmt.Sprintf("x-gc-%d", generation),
		Generation:  generation,
		Phase:       "ready",
	}
	if err := kfs.EnsurePrivateDirectory(f.s.Home, filepath.Dir(value.Path), true); err != nil {
		f.t.Fatal(err)
	}
	gcGit(f.t, f.repo, "worktree", "add", "-b", branch, value.Path, f.base)
	data, _ := json.Marshal(value)
	f.write(filepath.Join(f.root, "worktree-intents", task), fmt.Sprintf("g%d.json", generation), string(data))
	return value
}

func (f *gcFixture) options(dryRun bool) GCOptions {
	probe := f.probe
	return GCOptions{DryRun: dryRun, OlderThan: 7 * 24 * time.Hour, Now: time.Now(), Mainline: "v2", Checkout: f.checkout, Probe: func(_ context.Context, executions []RetainedExecution) []string {
		out := make([]string, len(executions))
		for i := range out {
			out[i] = probe[min(i, len(probe)-1)]
		}
		return out
	}}
}

func (f *gcFixture) run(dryRun bool) GCProject {
	f.t.Helper()
	return f.s.CollectGarbage(f.t.Context(), f.options(dryRun))
}

// registered reports whether Git still lists path as a worktree.
func (f *gcFixture) registered(path string) bool {
	return strings.Contains(gcGit(f.t, f.repo, "worktree", "list", "--porcelain"), "worktree "+path+"\n")
}

func (f *gcFixture) branchExists(branch string) bool {
	return gcGit(f.t, f.repo, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch) != ""
}

// commit adds a commit on the worktree's branch.
func (f *gcFixture) commit(w ExecutionWorktree, text string) string {
	f.t.Helper()
	f.write(w.Path, "work.txt", text)
	gcGit(f.t, w.Path, "commit", "-am", "Change")
	return gcGit(f.t, w.Path, "rev-parse", "HEAD")
}

// snapshot hashes every file below dir except lock files.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(path, ".lock") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[path] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func keptReason(c GCCategory, id string) string {
	for _, kept := range c.Kept {
		if kept.ID == id {
			return kept.Reason
		}
	}
	return ""
}
