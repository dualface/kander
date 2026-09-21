package launch

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// herdrReportListener fakes the herdr session-report socket: every accepted
// connection's request line is decoded into the reports channel and answered
// with a success result.
func herdrReportListener(t *testing.T, root string) (socketPath string, reports chan map[string]any) {
	t.Helper()
	socketPath = filepath.Join(root, "herdr-report.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	reports = make(chan map[string]any, 8)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var request map[string]any
				if err := json.Unmarshal(line, &request); err != nil {
					return
				}
				reports <- request
				id, _ := request["id"].(string)
				response, _ := json.Marshal(map[string]any{
					"id":     id,
					"result": map[string]any{"type": "ok"},
				})
				_, _ = conn.Write(append(response, '\n'))
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return socketPath, reports
}

// An agent declaring session.file reports its path-kind identity through its
// own integration, so a herdr launch must not send the id-kind
// pane.report_agent_session call or warn about it. An agent without the
// declaration keeps the proactive report, its read-back, and real warnings.
func TestHerdrSessionReportSkipsSessionFileAgents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	installHerdr(t, root, fakeBin)
	socketPath, reports := herdrReportListener(t, root)
	t.Setenv("HERDR_SOCKET_PATH", socketPath)

	piID, piPath := makeTodo(t, root, "herdr-report-pi-skip")
	out, errb, err := capture(t, func() error { return commandStart(root, "pi", "herdr", piID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已启动: "+piID) {
		t.Fatalf("stdout=%s", out)
	}
	if strings.Contains(errb, "警告") {
		t.Fatalf("session.file agent must not warn: stderr=%s", errb)
	}
	select {
	case request := <-reports:
		t.Fatalf("session.file agent must not send an id report: %v", request)
	case <-time.After(300 * time.Millisecond):
	}
	spec, err := os.ReadFile(filepath.Join(root, "working", filepath.Base(filepath.Dir(piPath)), "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "- SESSION: pi ") || !strings.Contains(string(spec), "- WINDOW: herdr:w1:t9:w1:p9") {
		t.Fatalf("spec=%s", spec)
	}

	// A missing socket must not warn for a session.file agent either: there
	// is no report attempt to fail.
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(root, "missing-herdr.sock"))
	missID, _ := makeTodo(t, root, "herdr-report-pi-missing-socket")
	_, errb, err = capture(t, func() error { return commandStart(root, "pi", "herdr", missID) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errb, "警告") {
		t.Fatalf("session.file agent must not warn on a missing socket: stderr=%s", errb)
	}

	// An id-type agent without session.file keeps the report and read-back:
	// the request carries this invocation's pane, source, and reference, and
	// a matching read-back produces no warning.
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	t.Setenv("KANBAN_HERDR_SESSION", "chat-fake-0001")
	cursorID, _ := makeTodo(t, root, "herdr-report-cursor-id")
	_, errb, err = capture(t, func() error { return commandStart(root, "cursor", "herdr", cursorID) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errb, "警告") {
		t.Fatalf("matching read-back must not warn: stderr=%s", errb)
	}
	select {
	case request := <-reports:
		if method, _ := request["method"].(string); method != "pane.report_agent_session" {
			t.Fatalf("method=%v", request["method"])
		}
		params, _ := request["params"].(map[string]any)
		if params["pane_id"] != "w1:p9" || params["source"] != "herdr:cursor" ||
			params["agent"] != "cursor" || params["agent_session_id"] != "chat-fake-0001" {
			t.Fatalf("params=%v", params)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("id-type agent kept no report call")
	}
}

// The plan flag follows the agent definition: only a declared session.file
// suppresses the proactive report.
func TestApplyAgentDeliveryMarksSessionFileDeclaration(t *testing.T) {
	cfg := envConfig("pi", "herdr", nil)
	var plan LaunchPlan
	if err := applyAgentDelivery(&plan, cfg, "pi"); err != nil {
		t.Fatal(err)
	}
	if !plan.sessionFileDeclared {
		t.Fatal("pi declares session.file")
	}
	plan = LaunchPlan{}
	if err := applyAgentDelivery(&plan, cfg, "cursor"); err != nil {
		t.Fatal(err)
	}
	if plan.sessionFileDeclared {
		t.Fatal("cursor declares no session.file")
	}
}
