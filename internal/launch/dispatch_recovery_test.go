package launch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestAcceptedResumeRequiresStoppedAndRenewsExpiredDeadline(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "stopped"}[stopped], func(t *testing.T) {
			root, task, d := resumeDispatchFixture(t)
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
				t.Fatal(err)
			}
			d, err = board.ReadDispatch(root, task, d.Input.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Backdate only the test fixture to represent long-running accepted work.
			d.Input.CreatedAt = time.Now().UTC().Add(-12 * time.Minute)
			d.Input.ConfirmBy = d.Input.CreatedAt.Add(120 * time.Second)
			err = board.WithTransaction(root, board.LockScope{Tasks: []string{task}, Groups: []string{"00000000-dispatch-group"}}, func(tx *board.Transaction) error {
				data, _ := json.Marshal(d)
				for _, name := range []string{"intent", "state"} {
					if err := tx.Put(task, "dispatches/"+d.Input.ID+"/"+name+".json", string(data)+"\n"); err != nil {
						return err
					}
				}
				original, _ := json.Marshal(d.Input)
				return tx.PutGroup("00000000-dispatch-group", d.Input.ID+".json", string(original)+"\n")
			})
			if err != nil {
				t.Fatal(err)
			}
			if stopped {
				s, err = board.ReadSnapshot(root, task)
				if err != nil {
					t.Fatal(err)
				}
				text := strings.Replace(s.Text, "- WINDOW: foreground", "- WINDOW: tmux:$42:@8:%8", 1)
				if err = board.WriteManagedDocument(root, s.Entry, text); err != nil {
					t.Fatal(err)
				}
				program, err := exec.LookPath("tmux")
				if err != nil {
					t.Fatal(err)
				}
				script, err := os.ReadFile(program)
				if err != nil {
					t.Fatal(err)
				}
				prefix := `#!/bin/sh
if [ "$1" = "display-message" ]; then
  case "$*" in *%8*) printf 'claude\t0\t1\n'; exit 0;; esac
fi
if [ "$1" = "list-panes" ]; then
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' '%8' '$42' 'other' '@8' 'sh' '0' 'other-session' ''
  exit 0
fi
`
				if err = os.WriteFile(program, append([]byte(prefix), script...), 0700); err != nil {
					t.Fatal(err)
				}
			}
			agent := "claude"
			_, _, err = capture(t, func() error {
				return commandResume(root, &agent, "tmux", task, d.Input.Message, "", true, 61, DispatchOptions{ID: d.Input.ID})
			})
			current, readErr := board.ReadDispatch(root, task, d.Input.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !stopped {
				if err == nil || current.Authorization != d.Authorization || !strings.Contains(err.Error(), d.Input.ID) || strings.Contains(err.Error(), "fresh confirmed exit required") {
					t.Fatalf("unknown takeover: %+v %v", current, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resume: %v; current: %+v", err, current)
			}
			if current.Authorization.Epoch != 2 || current.State != board.DispatchUnknown || !current.AcceptBefore().After(time.Now()) || !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
				t.Fatalf("new epoch did not deliver: %+v", current)
			}
			command, err := os.ReadFile(filepath.Join(root, "tmux.log.command"))
			if err != nil {
				t.Fatal(err)
			}
			prompt, err := os.ReadFile(taskFileFromCommand(t, string(command)))
			if err != nil || !strings.Contains(string(prompt), "--execution-epoch 2") || !strings.Contains(string(prompt), d.Input.Message) {
				t.Fatalf("recovery payload: %s %v", prompt, err)
			}
			s, err = board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: current.Authorization}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
