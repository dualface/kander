package herdrcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dualface/kander/internal/terminal"
)

var (
	errTabMissing = errors.New("herdrcore: the new tab did not appear in the layout")
	errConflict   = errors.New("herdrcore: the layout document changed")
)

// hc runs the herdr-core executable through the launch connection.
type hc struct {
	conn terminal.Conn
}

// run executes one hc command within the per-call budget and returns stdout.
func (h hc) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callBudget)
	defer cancel()
	res, err := h.conn.Run(ctx, h.conn.Program, args)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("hc %s: exit %d: %s", args[0], res.Code, firstText(res.Stderr, res.Stdout))
	}
	return res.Stdout, nil
}

// rpc sends one JSON-RPC request through `hc connect` and returns the result
// of the response with the same ID. The response stream may carry event lines
// too, so lines are matched by ID.
func (h hc) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	const requestID = 1
	request, err := encodeJSON(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callBudget)
	defer cancel()
	run := h.conn.RunInput
	if run == nil {
		run = terminal.ProbeInputRunner
	}
	res, err := run(ctx, h.conn.Program, []string{"connect", "--json"}, request+"\n")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("hc connect: exit %d: %s", res.Code, firstText(res.Stderr, res.Stdout))
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		var response struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
				Data    struct {
					Kind string `json:"kind"`
				} `json:"data"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &response) != nil || response.ID == nil || *response.ID != requestID {
			continue
		}
		if response.Error != nil {
			if response.Error.Data.Kind == "conflict" {
				return nil, errConflict
			}
			return nil, fmt.Errorf("%s: %s", method, response.Error.Message)
		}
		return response.Result, nil
	}
	return nil, fmt.Errorf("%s: no response", method)
}

// getLayout reads the layout document as a generic JSON value, with numbers
// kept as their original text, together with its version.
func (h hc) getLayout(ctx context.Context) (any, uint64, error) {
	raw, err := h.rpc(ctx, "doc.get", map[string]string{"name": layoutDocument})
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Value   json.RawMessage `json:"value"`
		Version uint64          `json:"version"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, 0, fmt.Errorf("doc.get: %w", err)
	}
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(result.Value))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return nil, 0, fmt.Errorf("doc.get: %w", err)
	}
	return doc, result.Version, nil
}

// setLayout writes the layout document back at the version it was read at.
func (h hc) setLayout(ctx context.Context, doc any, version uint64) error {
	value, err := encodeJSON(doc)
	if err != nil {
		return err
	}
	_, err = h.rpc(ctx, "doc.set", map[string]any{"name": layoutDocument, "value": json.RawMessage(value), "version": version})
	return err
}

// encodeJSON is compact JSON that leaves <, > and & as they are: the layout
// document is written back unchanged except for the one tab's name and place.
func encodeJSON(value any) (string, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// placeTab waits for Herdr App to create the tab and then, in one versioned
// write of the layout document, sets its name and moves it to the end of its
// space. Only that tab's name and position change: everything else in the
// document is written back as it was read. A version conflict rereads the
// document and redoes both changes on it, up to maxConflicts times.
func (h hc) placeTab(ctx context.Context, space, tab, title string) error {
	deadline := time.Now().Add(tabWait)
	conflicts := 0
	for {
		doc, version, err := h.getLayout(ctx)
		if err != nil {
			return err
		}
		if placeTabLast(doc, space, tab, title) {
			err := h.setLayout(ctx, doc, version)
			if !errors.Is(err, errConflict) {
				return err
			}
			if conflicts++; conflicts >= maxConflicts {
				return fmt.Errorf("doc.set: %w after %d attempts", errConflict, conflicts)
			}
			continue
		}
		if !time.Now().Before(deadline) {
			return errTabMissing
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tabPoll):
		}
	}
}

// placeTabLast sets the name of the tab with the given ID inside the given
// space and moves that tab entry, as it is, to the end of the space's tabs.
// Herdr App inserts a new tab after the active one, so the order it wrote is
// not the order Kander wants. It reports whether the tab exists.
func placeTabLast(doc any, space, tab, title string) bool {
	root, _ := doc.(map[string]any)
	spaces, _ := root["spaces"].([]any)
	for _, entry := range spaces {
		spaceEntry, _ := entry.(map[string]any)
		if spaceEntry["id"] != space {
			continue
		}
		tabs, _ := spaceEntry["tabs"].([]any)
		for i, tabValue := range tabs {
			tabEntry, _ := tabValue.(map[string]any)
			if tabEntry["id"] != tab {
				continue
			}
			tabEntry["name"] = title
			moved := make([]any, 0, len(tabs))
			moved = append(moved, tabs[:i]...)
			moved = append(moved, tabs[i+1:]...)
			spaceEntry["tabs"] = append(moved, tabValue)
			return true
		}
	}
	return false
}

func firstText(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}
