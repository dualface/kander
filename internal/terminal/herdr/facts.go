package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// PaneFacts collects the facts of herdr pane get. pane_not_found is recorded
// as gone; other failures return a CommandError classified by kind, while
// deadline and cancellation errors are returned unchanged.
func (b *Backend) PaneFacts(ctx context.Context, conn terminal.Conn, paneID string) (terminal.PaneFacts, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	res, err := conn.Run(ctx, conn.Program, []string{"pane", "get", paneID})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return terminal.PaneFacts{}, err
		}
		return terminal.PaneFacts{}, commandError(terminal.KindExec, config.Text("launch.herdr_invocation_failed", err.Error()), res, err)
	}
	detail := failureDetail(res)
	if res.Code != 0 {
		if errorCode(res) == "pane_not_found" {
			return terminal.PaneFacts{Gone: true, GoneDetail: detail}, nil
		}
		return terminal.PaneFacts{}, commandError(terminal.KindExit, config.Text("launch.pane_does_not_exist", paneID, detail), res, nil)
	}
	var payload any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		return terminal.PaneFacts{}, commandError(terminal.KindNotJSON, config.Text("probe.herdr_pane_get_failed_response_is_not_json", err.Error()), res, err)
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return terminal.PaneFacts{}, commandError(terminal.KindNotObject, config.Text("probe.herdr_pane_get_failed_response_is_not_a_json"), res, nil)
	}
	data, ok := obj["result"].(map[string]any)
	if !ok {
		return terminal.PaneFacts{}, commandError(terminal.KindMissingResult, config.Text("probe.herdr_pane_get_failed_response_is_missing_result"), res, nil)
	}
	pane, _ := data["pane"].(map[string]any)
	actual := ""
	if pane != nil {
		actual = publicID(pane["pane_id"])
	}
	if pane == nil || actual != paneID || actual == "" {
		return terminal.PaneFacts{}, commandError(terminal.KindInvalidResponse, config.Text("launch.pane_does_not_exist_2", paneID), res, nil)
	}
	if err := ctx.Err(); err != nil {
		return terminal.PaneFacts{}, err
	}
	agent, _ := pane["agent"].(string)
	status, _ := pane["agent_status"].(string)
	return terminal.PaneFacts{
		Agent:        agent,
		AgentStatus:  status,
		AgentSession: sessionReference(pane),
		Container:    publicID(pane["tab_id"]),
	}, nil
}

// listResult extracts the result object of a successful herdr JSON response.
func listResult(res probe.Result) (map[string]any, error) {
	if res.Code != 0 {
		return nil, textError(failureDetail(res), failureDetail(res))
	}
	var payload any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		return nil, textError(err.Error(), err.Error())
	}
	obj, _ := payload.(map[string]any)
	data, _ := obj["result"].(map[string]any)
	if data == nil {
		return nil, textError("probe.herdr_response_is_missing_result")
	}
	return data, nil
}

// Topology lists the panes of the tab named by the address.
func (b *Backend) Topology(ctx context.Context, conn terminal.Conn, address terminal.Address) (terminal.Topology, error) {
	res, err := conn.Run(ctx, conn.Program, []string{"pane", "list"})
	if err != nil {
		return terminal.Topology{}, err
	}
	data, err := listResult(res)
	if err != nil {
		return terminal.Topology{}, textError("takeover.herdr_pane_list_failed", err.Error())
	}
	panes, _ := data["panes"].([]any)
	if panes == nil {
		return terminal.Topology{}, textError("liveness.herdr_pane_list_response_has_no_panes")
	}
	var matching []string
	for _, item := range panes {
		pane, _ := item.(map[string]any)
		if pane == nil {
			return terminal.Topology{}, textError("takeover.herdr_pane_list_response_contains_an_invalid_pane")
		}
		currentTab := publicID(pane["tab_id"])
		currentPane := publicID(pane["pane_id"])
		if currentTab == "" || currentPane == "" {
			return terminal.Topology{}, textError("takeover.a_pane_in_the_herdr_pane_list_response_has")
		}
		if currentTab == address.Container {
			matching = append(matching, currentPane)
		}
	}
	return terminal.Topology{Container: address.Container, Panes: matching}, nil
}

// ReverseLookup uniquely locates a pane by agent and session identity.
func (b *Backend) ReverseLookup(ctx context.Context, conn terminal.Conn, identity terminal.Identity) (terminal.Address, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	res, err := conn.Run(ctx, conn.Program, []string{"pane", "list"})
	if err != nil {
		return terminal.Address{}, err
	}
	data, err := listResult(res)
	if err != nil {
		return terminal.Address{}, err
	}
	panes, _ := data["panes"].([]any)
	if panes == nil {
		return terminal.Address{}, textError("liveness.herdr_pane_list_response_has_no_panes")
	}
	var matches []terminal.Address
	for _, item := range panes {
		if err := ctx.Err(); err != nil {
			return terminal.Address{}, err
		}
		pane, _ := item.(map[string]any)
		if pane == nil {
			return terminal.Address{}, textError("liveness.herdr_session_lookup_returned_invalid_output")
		}
		tab := publicID(pane["tab_id"])
		id := publicID(pane["pane_id"])
		agent, agentOK := pane["agent"].(string)
		if tab == "" || id == "" || !windowRe.MatchString("herdr:"+tab+":"+id) || (pane["agent"] != nil && !agentOK) {
			return terminal.Address{}, textError("liveness.herdr_session_lookup_returned_invalid_output")
		}
		sessionIdentity, _ := pane["agent_session"].(map[string]any)
		if pane["agent_session"] != nil && sessionIdentity == nil {
			return terminal.Address{}, textError("liveness.herdr_session_lookup_returned_invalid_output")
		}
		var reference string
		if sessionIdentity != nil {
			var ok bool
			reference, ok = sessionIdentity["value"].(string)
			if sessionIdentity["value"] != nil && !ok {
				return terminal.Address{}, textError("liveness.herdr_session_lookup_returned_invalid_output")
			}
		}
		if agent != identity.Agent || reference != identity.Reference {
			continue
		}
		matches = append(matches, terminal.Address{Launcher: Name, Container: tab, Pane: id})
	}
	if err := ctx.Err(); err != nil {
		return terminal.Address{}, err
	}
	if len(matches) == 0 {
		return terminal.Address{}, &terminal.MatchError{Matches: 0, Cause: textError(
			"liveness.herdr_session_lookup_found_no_match", identity.Reference,
		)}
	}
	if len(matches) != 1 {
		return terminal.Address{}, &terminal.MatchError{Matches: len(matches), Cause: textError(
			"liveness.herdr_session_lookup_is_ambiguous_panes", identity.Reference, strconv.Itoa(len(matches)),
		)}
	}
	return matches[0], nil
}
