//go:build linux

package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

func TestNotifyRepairsIdentityBeforeDelivery(t *testing.T) {
	for _, mode := range []string{"legacy", "dispatch", "blocked", "unbound", "partial"} {
		t.Run(mode, func(t *testing.T) {
			root, _ := setupBoard(t)
			task, path := makeReview(t, root, "repair-"+mode)
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text = []byte(strings.Replace(string(text), "- SESSION: claude session-1", "- SESSION: codex", 1))
			if err := os.WriteFile(path, text, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("KANBAN_HERDR_AGENT", "codex")
			identityPath := filepath.Join(root, "identity")
			if mode == "partial" {
				if err := os.WriteFile(identityPath, []byte("target-session"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("KANBAN_HERDR_SESSION_FILE", identityPath)
			t.Setenv("KANBAN_HERDR_PROCESS_JSON", fmt.Sprintf(`{"result":{"process_info":{"pane_id":"w1:p9","foreground_processes":[{"pid":%d,"name":"codex"}]}}}`, os.Getpid()))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
			sessions := filepath.Join(root, "codex", "sessions")
			if err := os.MkdirAll(sessions, 0o700); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(sessions, "rollout-open.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			prompt := "执行 Kanban 任务 " + task + "; full instructions are in the UTF-8 task file at /tmp/task.md; read the complete file first and follow it exactly."
			if mode == "unbound" {
				prompt = "orchestrate " + task
			}
			for _, record := range []any{
				map[string]any{"type": "session_meta", "payload": map[string]any{"id": "target-session", "cwd": filepath.Dir(root)}},
				map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": prompt}},
			} {
				if err := json.NewEncoder(file).Encode(record); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "partial" {
				other, err := os.Create(filepath.Join(sessions, "rollout-newer-but-not-open.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				for _, record := range []any{
					map[string]any{"type": "session_meta", "payload": map[string]any{"id": "newer-session", "cwd": filepath.Dir(root)}},
					map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": prompt}},
				} {
					if err := json.NewEncoder(other).Encode(record); err != nil {
						t.Fatal(err)
					}
				}
				if err := other.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(filepath.Join(sessions, "rollout-newer-but-not-open.jsonl"), time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "blocked" {
				t.Setenv("KANBAN_HERDR_STATUS", "blocked")
			}
			listener, err := net.Listen("unix", filepath.Join(root, "repair.sock"))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			t.Setenv("HERDR_SOCKET_PATH", listener.Addr().String())
			reports := make(chan map[string]any, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				var report map[string]any
				if err := json.NewDecoder(conn).Decode(&report); err != nil {
					return
				}
				params, _ := report["params"].(map[string]any)
				if err := os.WriteFile(identityPath, []byte(fmt.Sprint(params["agent_session_id"])), 0o600); err != nil {
					return
				}
				reports <- report
				_ = json.NewEncoder(conn).Encode(map[string]any{"id": report["id"], "result": map[string]any{"type": "ok"}})
			}()
			var d board.Dispatch
			var receipts chan error
			if mode != "legacy" && mode != "partial" {
				created := time.Now().UTC()
				d, err = board.PrepareDispatch(root, board.DispatchInput{ID: "repair-notify", TaskID: task, Kind: "sync", Message: "修复问题", Base: strings.Repeat("a", 40), CreatedAt: created, ConfirmBy: created.Add(5 * time.Second)})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "dispatch" {
					receipts = make(chan error, 1)
					go func() { receipts <- acceptDelivered(root, task, d, true) }()
				}
			}
			out, _, err := capture(t, func() error {
				if mode == "dispatch" {
					return commandNotify(root, task, d.Input.Message, "", "", true, 61, launch.DispatchOptions{ID: d.Input.ID})
				}
				if mode != "legacy" && mode != "partial" {
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					return deliverDispatchContext(ctx, root, task, d.Input.ID, "")
				}
				return commandNotifyLegacy(root, task, "修复问题", "", "", true, 61)
			})
			if (err == nil) != (mode == "legacy" || mode == "dispatch" || mode == "partial") {
				t.Fatalf("out=%s err=%v", out, err)
			}
			_, sent := os.Stat(filepath.Join(root, "herdr.log.prompt"))
			if (sent == nil) != (mode == "legacy" || mode == "dispatch" || mode == "partial") {
				t.Fatalf("delivery gate bypassed: %v", sent)
			}
			if mode == "unbound" || mode == "partial" {
				select {
				case report := <-reports:
					t.Fatalf("unbound report=%v", report)
				default:
				}
				if mode == "partial" {
					snapshot, err := board.ReadSnapshot(root, task)
					if err != nil || board.MetadataFrom(snapshot.Text, board.FieldSession) != "codex target-session" {
						t.Fatalf("partial repair: %s %v", snapshot.Text, err)
					}
				}
			} else {
				select {
				case report := <-reports:
					params := report["params"].(map[string]any)
					if params["pane_id"] != "w1:p9" || params["agent_session_id"] != "target-session" {
						t.Fatalf("report=%v", report)
					}
				case <-time.After(time.Second):
					t.Fatal("missing report")
				}
				snapshot, err := board.ReadSnapshot(root, task)
				if err != nil || board.MetadataFrom(snapshot.Text, board.FieldSession) != "codex target-session" {
					t.Fatalf("snapshot=%+v err=%v", snapshot, err)
				}
			}
			if mode == "dispatch" {
				if err := <-receipts; err != nil {
					t.Fatal(err)
				}
				current, err := board.ReadDispatch(root, task, d.Input.ID)
				if err != nil || current.State != board.DispatchCompleted || current.Authorization != d.Authorization || !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) || current.Input.Message != d.Input.Message {
					t.Fatalf("dispatch=%+v err=%v", current, err)
				}
				before, _ := os.ReadFile(filepath.Join(root, "herdr.log.order"))
				_, _, err = capture(t, func() error {
					return commandNotify(root, task, d.Input.Message, "", "", true, 61, launch.DispatchOptions{ID: d.Input.ID})
				})
				after, _ := os.ReadFile(filepath.Join(root, "herdr.log.order"))
				if err != nil || string(before) != string(after) {
					t.Fatalf("retry resent: %v", err)
				}
			}
			if mode == "blocked" || mode == "unbound" {
				current, err := board.ReadDispatch(root, task, d.Input.ID)
				if err != nil || current.State != board.DispatchPrepared || current.Accepted != nil || current.Attempts != 0 {
					t.Fatalf("failed repair fabricated delivery: %+v %v", current, err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "herdr.log.run")); !os.IsNotExist(err) {
				t.Fatal("repair started another executor")
			}
		})
	}
}
