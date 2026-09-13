package herdr

import (
	"bufio"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

var reportSeq atomic.Int64

// reportSession reports the session identity once through the herdr socket
// and reads it back from the pane. It returns ErrNoReportChannel when
// HERDR_SOCKET_PATH is not set.
func reportSession(call terminal.HookCall) error {
	report := *call.Report
	socketPath := strings.TrimSpace(call.Getenv("HERDR_SOCKET_PATH"))
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
	backend, ok := terminal.Lookup(call.Launcher)
	if !ok {
		return terminal.ErrUnsupported
	}
	facts, err := backend.PaneFacts(ctx, report.Conn, report.Pane)
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
