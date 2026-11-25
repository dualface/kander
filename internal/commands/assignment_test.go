package commands

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/version"
)

func boundCardCLI(t *testing.T) (runGit func(...string), run func(...string) string, project *client.Project) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is required to test the submitter default")
	}
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_DIR", "GIT_WORK_TREE"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	runGit = func(args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(t.Context(), git, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	store, err := server.Open(server.Options{DataDir: t.TempDir(), Version: version.String(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	httpServer := httptest.NewServer(store.Handler())
	t.Cleanup(httpServer.Close)
	c, err := client.New(httpServer.URL, "test-host")
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Register(t.Context(), "test", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Server.URL, cfg.Project.Name = httpServer.URL, info.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	run = func(args ...string) string {
		t.Helper()
		code, stdout, stderr := commandtest.Capture(func() int {
			return cli.Run(append([]string{"kander", "--lang", "en", "--json"}, args...))
		})
		if code != 0 {
			t.Fatalf("kander %v: exit %d, stdout=%q, stderr=%q", args, code, stdout, stderr)
		}
		return stdout
	}
	return runGit, run, c.Project(info.ID)
}

func TestPeopleFieldsThroughCLI(t *testing.T) {
	runGit, run, p := boundCardCLI(t)
	newCard := func(slug string) (id string, revision uint64) {
		t.Helper()
		var item struct {
			TaskID   string `json:"task_id"`
			Revision uint64 `json:"revision"`
		}
		raw := run("new", "chore", slug, "Check people fields")
		if err := json.Unmarshal([]byte(raw), &item); err != nil || item.TaskID == "" {
			t.Fatalf("incorrect creation result: %s, %v", raw, err)
		}
		return item.TaskID, item.Revision
	}
	empty, _ := newCard("people-blank")
	runGit("config", "--local", "user.name", "Local Name")
	id, revision := newCard("people-local")
	created, err := p.Show(t.Context(), id)
	if err != nil || !strings.Contains(board.CardHeader(created.Text), "\n- SUBMITTER: Local Name\n- ASSIGNEE:\n") {
		t.Fatalf("CLI did not write the configured Git name: %+v, %v", created, err)
	}
	raw := run("assign", id, "New Assignee", "--expect-revision", strconv.FormatUint(revision, 10))
	var assigned struct {
		TaskID   string `json:"task_id"`
		Assignee string `json:"assignee"`
		Revision uint64 `json:"revision"`
	}
	if err := json.Unmarshal([]byte(raw), &assigned); err != nil || assigned.TaskID != id || assigned.Assignee != "New Assignee" || assigned.Revision != revision+1 {
		t.Fatalf("incorrect assignment result: %s, %v", raw, err)
	}
	checkList := func(wantAssignee string) {
		t.Helper()
		var output struct {
			Tasks []map[string]json.RawMessage `json:"tasks"`
		}
		raw := run("list")
		if err := json.Unmarshal([]byte(raw), &output); err != nil || len(output.Tasks) != 2 {
			t.Fatalf("incorrect list result: %s, %v", raw, err)
		}
		for _, item := range output.Tasks {
			var taskID string
			if err := json.Unmarshal(item["task_id"], &taskID); err != nil {
				t.Fatal(err)
			}
			wantSubmitter, wantPerson := "", ""
			switch taskID {
			case id:
				wantSubmitter, wantPerson = "Local Name", wantAssignee
			case empty:
			default:
				t.Fatalf("unexpected card %q", taskID)
			}
			for key, want := range map[string]string{"owner": "", "submitter": wantSubmitter, "assignee": wantPerson} {
				var got string
				if err := json.Unmarshal(item[key], &got); err != nil || got != want {
					t.Errorf("card %s field %s = %q, want %q, error = %v", taskID, key, got, want, err)
				}
			}
		}
	}
	checkList("New Assignee")
	run("assign", id)
	checkList("")
}
