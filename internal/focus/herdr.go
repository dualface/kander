package focus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

func focusHerdrPane(ctx context.Context, socket, pane string) error {
	if socket == "" {
		return errors.New(config.Text("focus.socket_missing"))
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	return sendPaneFocus(ctx, conn, pane)
}

func sendPaneFocus(parent context.Context, conn net.Conn, pane string) error {
	ctx, cancel := probe.WithDefaultTimeout(parent)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	const requestID = "kander-focus"
	request := struct {
		ID     string            `json:"id"`
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}{requestID, "pane.focus", map[string]string{"pane_id": pane}}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	var response struct {
		ID     string `json:"id"`
		Result struct {
			Type string `json:"type"`
			Pane struct {
				ID string `json:"pane_id"`
			} `json:"pane"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(conn, 64*1024)).Decode(&response); err != nil {
		return err
	}
	if response.ID != requestID || response.Result.Type != "pane_info" || response.Result.Pane.ID != pane {
		return errors.New(config.Text("focus.pane_rejected"))
	}
	return nil
}
