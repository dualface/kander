package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

var reportSeq atomic.Int64

// run executes a herdr command and reports a run failure as an invocation
// failure of herdr itself.
func run(ctx context.Context, conn terminal.Conn, args []string) (probe.Result, error) {
	res, err := conn.Run(ctx, conn.Program, args)
	if err != nil {
		return res, commandError(terminal.KindExec, config.Text("launch.herdr_invocation_failed", err.Error()), res, err)
	}
	return res, nil
}

func jsonResult(res probe.Result, action string) (map[string]any, error) {
	if res.Code != 0 {
		return nil, textError("launch.herdr_failed", action, failureDetail(res))
	}
	var payload any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		return nil, textError("launch.herdr_failed_response_is_not_json", action, err.Error())
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil, textError("launch.herdr_failed_response_is_not_a_json_object", action)
	}
	data, ok := obj["result"].(map[string]any)
	if !ok {
		return nil, textError("launch.herdr_failed_response_is_missing_result", action)
	}
	return data, nil
}

// CreateContainer creates a background tab in the workspace and returns its
// root pane. A tab created without a usable pane id is closed again.
func (b *Backend) CreateContainer(conn terminal.Conn, target terminal.Target, cwd, label string) (terminal.Address, error) {
	res, err := run(context.Background(), conn, []string{"tab", "create", "--workspace", target.Workspace, "--cwd", cwd, "--label", label, "--no-focus"})
	if err != nil {
		return terminal.Address{}, err
	}
	data, err := jsonResult(res, "tab create")
	if err != nil {
		return terminal.Address{}, err
	}
	tab, _ := data["tab"].(map[string]any)
	pane, _ := data["root_pane"].(map[string]any)
	tabID, paneID := "", ""
	if tab != nil {
		tabID = publicID(tab["tab_id"])
	}
	if pane != nil {
		paneID = publicID(pane["pane_id"])
	}
	if tabID == "" || paneID == "" {
		if tabID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), probe.DefaultCommandTimeout)
			defer cancel()
			if closeErr := b.CloseContainer(ctx, conn, terminal.Address{Container: tabID}); closeErr != nil {
				return terminal.Address{}, textError("launch.herdr_tab_create_failed_response_is_missing_tab_or", closeErr.Error())
			}
		}
		return terminal.Address{}, textError("launch.herdr_tab_create_failed_response_is_missing_tab_or_2")
	}
	return terminal.Address{Container: tabID, Pane: paneID}, nil
}

// WaitReady waits for the new pane to render its first output: text sent
// before the shell owns the terminal is discarded.
func (b *Backend) WaitReady(conn terminal.Conn, pane string) error {
	res, err := run(context.Background(), conn, []string{
		"pane", "wait-output", pane, "--regex", `\S`, "--source", "visible",
		"--timeout", strconv.Itoa(readyTimeoutMS),
	})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return textError("launch.herdr_pane_is_not_ready", failureDetail(res))
	}
	return nil
}

func (b *Backend) RunCommand(conn terminal.Conn, pane, command string, _ bool) error {
	res, err := run(context.Background(), conn, []string{"pane", "run", pane, command})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return textError("launch.herdr_pane_run_failed", failureDetail(res))
	}
	return nil
}

// ReportSession reports the session identity once through the herdr socket
// and reads it back from the pane. It returns ErrNoReportChannel when
// HERDR_SOCKET_PATH is not set.
func (b *Backend) ReportSession(report terminal.SessionReport) error {
	socketPath := strings.TrimSpace(b.getenv("HERDR_SOCKET_PATH"))
	if socketPath == "" {
		return terminal.ErrNoReportChannel
	}
	seq := reportSeq.Add(1)
	if n := time.Now().UnixNano(); n > seq {
		reportSeq.Store(n)
		seq = n
	}
	requestID := "kander:" + report.Agent + ":" + strconv.Itoa(int(seq))
	req := map[string]any{
		"id":     requestID,
		"method": "pane.report_agent_session",
		"params": map[string]any{
			"pane_id":          report.Pane,
			"source":           "herdr:" + report.Agent,
			"agent":            report.Agent,
			"seq":              seq,
			"agent_session_id": report.Reference,
		},
	}
	payload, _ := json.Marshal(req)
	payload = append(payload, '\n')
	conn, err := net.DialTimeout("unix", socketPath, report.Deadline.Sub(report.Now()))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(report.Deadline)
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return textError("launch.herdr_socket_returned_no_response")
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(line))), &response); err != nil {
		return textError("launch.herdr_socket_response_is_not_valid_json", err.Error())
	}
	result, _ := response["result"].(map[string]any)
	id, _ := response["id"].(string)
	typ, _ := result["type"].(string)
	if id != requestID || typ != "ok" {
		return textError("launch.herdr_socket_response_is_not_ok")
	}
	remaining := report.Deadline.Sub(report.Now())
	if remaining <= 0 {
		return textError("launch.timed_out_reading_back_the_herdr_session_identity")
	}
	ctx, cancel := probe.TimeoutContext(remaining)
	defer cancel()
	facts, err := b.PaneFacts(ctx, report.Conn, report.Pane)
	if err != nil {
		return err
	}
	if facts.Gone {
		return textError("launch.pane_does_not_exist_2", report.Pane)
	}
	if facts.AgentSession == report.Reference {
		return nil
	}
	return textError("launch.herdr_session_identity_read_back_mismatch_reported_pane", report.Reference, orNA(facts.AgentSession))
}
