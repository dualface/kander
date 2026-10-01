package launch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

type repairBackend struct {
	terminal.Backend
	name                  string
	facts                 terminal.PaneFacts
	processes             []terminal.ForegroundProcess
	reports, observations int
	beforeObserve         func(int)
	afterReport           func()
	reportErr             error
	readBack              bool
}

func (b *repairBackend) Name() string       { return b.name }
func (b *repairBackend) Executable() string { return os.Args[0] }
func (b *repairBackend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{Container: true, AgentIdentity: true, SessionReport: true}
}
func (b *repairBackend) ParseAddress(value string) (terminal.Address, bool) {
	pane, ok := strings.CutPrefix(value, b.name+":")
	return terminal.Address{Pane: pane, Container: "tab"}, ok
}
func (b *repairBackend) PaneFacts(context.Context, terminal.Conn, string) (terminal.PaneFacts, error) {
	return b.facts, nil
}
func (b *repairBackend) ProcessFacts(_ context.Context, _ terminal.Conn, _ string) ([]terminal.ForegroundProcess, error) {
	b.observations++
	if b.beforeObserve != nil {
		b.beforeObserve(b.observations)
	}
	return b.processes, nil
}
func (b *repairBackend) ReportSession(r terminal.SessionReport) error {
	b.reports++
	if b.readBack {
		b.facts.AgentSession = r.Reference
		b.facts.AgentSessionKind = "id"
	}
	if b.afterReport != nil {
		b.afterReport()
	}
	return b.reportErr
}

func repairRollout(t *testing.T, task, name, id, prompt string) process.OpenFile {
	t.Helper()
	root := filepath.Join(codexSessionsRoot(), "fake")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "rollout-"+name+".jsonl")
	if prompt == "" {
		prompt = codexPromptPrefixes(task)[0] + " full instructions"
	}
	records := []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": id}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}},
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := json.NewEncoder(file).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return process.OpenFile{Path: path, Info: info}
}

func TestRepairMissingSessionEvidenceAndDrift(t *testing.T) {
	for _, scenario := range []string{"legacy", "recorded", "missing-file", "other-task", "mismatch", "multiple-files", "multiple-processes", "process-change", "file-change", "board-change", "native-arrival", "post-report-change", "report-failure", "readback-failure", "existing-conflict", "native-path", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root, _, _ := setupBoard(t)
			task, _ := makeTodo(t, root, "repair-"+scenario)
			snapshot, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := board.MoveWithOptions(snapshot.Entry, root, "working", board.MoveOptions{Owner: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			backend := &repairBackend{name: "repair-" + scenario, facts: terminal.PaneFacts{Agent: "codex", AgentStatus: "idle", Container: "tab"}, processes: []terminal.ForegroundProcess{{PID: 42, Name: "codex"}}, readBack: true}
			terminal.Register(backend)
			text, err := board.ReadDocument(entry)
			if err != nil {
				t.Fatal(err)
			}
			text = strings.Replace(text, "- SESSION:\n", "- SESSION: codex\n", 1)
			text = strings.Replace(text, "- WINDOW:\n", "- WINDOW: "+backend.name+":pane\n", 1)
			if scenario == "recorded" {
				text = strings.Replace(text, "SESSION: codex\n", "SESSION: codex target-session\n", 1)
			}
			if scenario == "mismatch" {
				text = strings.Replace(text, "SESSION: codex\n", "SESSION: codex different-session\n", 1)
			}
			if err := board.WriteManagedDocument(root, entry, text); err != nil {
				t.Fatal(err)
			}
			snapshot, err = board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			files := process.ProcessFiles{Identity: "42:start", Files: []process.OpenFile{repairRollout(t, task, "target", "target-session", "")}}
			switch scenario {
			case "missing-file":
				files.Files = nil
			case "other-task":
				files.Files = []process.OpenFile{repairRollout(t, task, "target", "target-session", "orchestrate "+task)}
			case "multiple-files":
				files.Files = append(files.Files, repairRollout(t, task, "second", "second-session", ""))
			case "multiple-processes":
				backend.processes = append(backend.processes, terminal.ForegroundProcess{PID: 43, Name: "codex"})
			case "process-change":
				backend.beforeObserve = func(n int) {
					if n == 2 {
						files.Identity = "42:restart"
					}
				}
			case "file-change":
				backend.beforeObserve = func(n int) {
					if n == 2 {
						if err := os.Rename(files.Files[0].Path, files.Files[0].Path+".old"); err != nil {
							t.Fatal(err)
						}
						files.Files[0] = repairRollout(t, task, "target", "target-session", "")
					}
				}
			case "native-arrival":
				backend.beforeObserve = func(n int) {
					if n == 2 {
						backend.facts.AgentSession = "different-native-session"
					}
				}
			case "board-change":
				backend.beforeObserve = func(n int) {
					if n == 2 {
						if err := board.WriteManagedDocument(root, entry, text+"\nConcurrent change\n"); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "post-report-change":
				backend.afterReport = func() { files.Identity = "42:restart" }
			case "report-failure":
				backend.reportErr = errors.New("socket failed")
			case "readback-failure":
				backend.readBack = false
			case "existing-conflict":
				backend.facts.AgentSession = "different-session"
			case "native-path":
				backend.facts.AgentSessionKind = "path"
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			got, err := repairMissingSession(ctx, root, snapshot, "", func(ctx context.Context, _ int) (process.ProcessFiles, error) { return files, ctx.Err() })
			pass := scenario == "legacy" || scenario == "recorded" || scenario == "existing-conflict"
			if (err == nil) != pass {
				t.Fatalf("error=%v reports=%d", err, backend.reports)
			}
			if scenario == "legacy" || scenario == "recorded" {
				if board.MetadataFrom(got.Text, board.FieldSession) != "codex target-session" || backend.reports != 1 {
					t.Fatalf("session=%s reports=%d", got.Text, backend.reports)
				}
				if _, err := board.ReadDocument(got.Entry); err != nil {
					t.Fatalf("returned cursor stale: %v", err)
				}
			} else if scenario != "post-report-change" && scenario != "report-failure" && scenario != "readback-failure" && backend.reports != 0 {
				t.Fatalf("reported stale/unbound identity: %d", backend.reports)
			}
			if !pass {
				current, err := board.ReadSnapshot(root, task)
				if err != nil {
					t.Fatal(err)
				}
				if board.MetadataFrom(current.Text, board.FieldSession) != board.MetadataFrom(snapshot.Text, board.FieldSession) || current.Entry.State != "working" {
					t.Fatal("failed repair changed executor")
				}
			}
		})
	}
}
