package launch

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dualface/kander/internal/probe"
)

var herdrReportSeq atomic.Int64

func herdrCapture(herdr string, args []string, timeout time.Duration) (cmdResult, error) {
	res, err := runCaptured(herdr, args, timeout)
	if err != nil {
		return res, launchError("launch.herdr_invocation_failed", err.Error())
	}
	return res, nil
}

func herdrFailureDetail(res cmdResult) string {
	if s := strings.TrimSpace(res.Stderr); s != "" {
		return s
	}
	return "exit " + itoa(res.Code)
}

func herdrJSONResult(res cmdResult, action string) (map[string]any, error) {
	if res.Code != 0 {
		return nil, launchError("launch.herdr_failed", action, herdrFailureDetail(res))
	}
	var payload any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		return nil, launchError("launch.herdr_failed_response_is_not_json", action, err.Error())
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil, launchError("launch.herdr_failed_response_is_not_a_json_object", action)
	}
	data, ok := obj["result"].(map[string]any)
	if !ok {
		return nil, launchError("launch.herdr_failed_response_is_missing_result", action)
	}
	return data, nil
}

func herdrPublicID(value any) string {
	s, ok := value.(string)
	if !ok || s == "" || strings.ContainsRune(s, 0) {
		return ""
	}
	return s
}

func herdrCloseTab(herdr, tabID string) string {
	res, err := herdrCapture(herdr, []string{"tab", "close", tabID}, probe.DefaultCommandTimeout)
	if err != nil {
		return t("launch.failed_to_close_tab", tabID, err.Error())
	}
	if res.Code == 0 {
		return ""
	}
	return t("launch.failed_to_close_tab", tabID, herdrFailureDetail(res))
}

func herdrCreateTab(herdr, workspace, cwd, label string) (string, string, error) {
	res, err := herdrCapture(herdr, []string{"tab", "create", "--workspace", workspace, "--cwd", cwd, "--label", label, "--no-focus"}, 0)
	if err != nil {
		return "", "", err
	}
	data, err := herdrJSONResult(res, "tab create")
	if err != nil {
		return "", "", err
	}
	tab, _ := data["tab"].(map[string]any)
	pane, _ := data["root_pane"].(map[string]any)
	tabID := herdrPublicID(nil)
	paneID := herdrPublicID(nil)
	if tab != nil {
		tabID = herdrPublicID(tab["tab_id"])
	}
	if pane != nil {
		paneID = herdrPublicID(pane["pane_id"])
	}
	if tabID == "" || paneID == "" {
		if tabID != "" {
			if closeErr := herdrCloseTab(herdr, tabID); closeErr != "" {
				return "", "", launchError(
					"launch.herdr_tab_create_failed_response_is_missing_tab_or", closeErr,
				)
			}
		}
		return "", "", launchError(
			"launch.herdr_tab_create_failed_response_is_missing_tab_or_2",
		)
	}
	return tabID, paneID, nil
}

func herdrWaitPaneReady(herdr, paneID string) error {
	res, err := herdrCapture(herdr, []string{
		"pane", "wait-output", paneID, "--regex", `\S`, "--source", "visible",
		"--timeout", itoa(herdrReadyTimeoutMS),
	}, 0)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return launchError("launch.herdr_pane_is_not_ready", herdrFailureDetail(res))
	}
	return nil
}

func herdrPaneRun(herdr, paneID, command string) error {
	res, err := herdrCapture(herdr, []string{"pane", "run", paneID, command}, 0)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return launchError("launch.herdr_pane_run_failed", herdrFailureDetail(res))
	}
	return nil
}

func herdrSessionReference(pane map[string]any) string {
	identity, _ := pane["agent_session"].(map[string]any)
	if identity == nil {
		return ""
	}
	ref, _ := identity["value"].(string)
	return ref
}

func herdrPaneInfo(herdr, paneID string, timeout time.Duration) (map[string]any, error) {
	pane, err := probe.HerdrProbePane(herdr, paneID, timeout)
	if err != nil {
		return nil, err
	}
	if pane == nil {
		return nil, launchError("launch.pane_does_not_exist_2", paneID)
	}
	return pane, nil
}

func reportHerdrAgentSession(herdr, paneID string, session AgentSession) {
	if session.Reference == "" {
		return
	}
	socketPath := strings.TrimSpace(os.Getenv("HERDR_SOCKET_PATH"))
	if socketPath == "" {
		os.Stderr.WriteString(t(
			"launch.warning_failed_to_report_the_herdr_session_identity_herdr",
		))
		return
	}
	deadline := nowFn().Add(herdrReportBudget)
	var last error = launchError("launch.herdr_session_identity_report_budget_exhausted")
	for nowFn().Before(deadline) {
		err := sendAndReadHerdrSession(herdr, socketPath, paneID, session, deadline)
		if err == nil {
			return
		}
		last = err
		remaining := deadline.Sub(nowFn())
		if remaining > 0 {
			d := notifyPollInterval
			if remaining < d {
				d = remaining
			}
			sleepFn(d)
		}
	}
	os.Stderr.WriteString(t(
		"launch.warning_failed_to_report_the_herdr_session_identity", last.Error(),
	))
}

func sendAndReadHerdrSession(herdr, socketPath, paneID string, session AgentSession, deadline time.Time) error {
	seq := herdrReportSeq.Add(1)
	if n := time.Now().UnixNano(); n > seq {
		herdrReportSeq.Store(n)
		seq = n
	}
	requestID := "kander:" + session.Agent + ":" + itoa(int(seq))
	req := map[string]any{
		"id":     requestID,
		"method": "pane.report_agent_session",
		"params": map[string]any{
			"pane_id":          paneID,
			"source":           "herdr:" + session.Agent,
			"agent":            session.Agent,
			"seq":              seq,
			"agent_session_id": session.Reference,
		},
	}
	payload, _ := json.Marshal(req)
	payload = append(payload, '\n')
	conn, err := net.DialTimeout("unix", socketPath, deadline.Sub(nowFn()))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return launchError("launch.herdr_socket_returned_no_response")
	}
	var response map[string]any
	if err := json.Unmarshal(bytesTrim(line), &response); err != nil {
		return launchError("launch.herdr_socket_response_is_not_valid_json", err.Error())
	}
	result, _ := response["result"].(map[string]any)
	id, _ := response["id"].(string)
	typ, _ := result["type"].(string)
	if id != requestID || typ != "ok" {
		return launchError("launch.herdr_socket_response_is_not_ok")
	}
	remaining := deadline.Sub(nowFn())
	if remaining <= 0 {
		return launchError("launch.timed_out_reading_back_the_herdr_session_identity")
	}
	pane, err := herdrPaneInfo(herdr, paneID, remaining)
	if err != nil {
		return err
	}
	ref := herdrSessionReference(pane)
	if ref == session.Reference {
		return nil
	}
	return launchError(
		"launch.herdr_session_identity_read_back_mismatch_reported_pane", session.Reference, orNA(ref),
	)
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
