package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
)

// StopAgent is provided by the terminal layer. It verifies the locally recorded
// session, stops only that agent and preserves its pane, worktree and branch.
var StopAgent func(context.Context, string, string) error

func (w *loop) stopLost(ctx context.Context, log *slog.Logger, c remote.Credential) {
	if c.AgentPID == 0 && (c.Window == "" || c.Session == "") {
		return
	}
	window, session := c.Window, c.Session
	if c.AgentPID > 0 {
		window, session = "", ""
	}
	material, _ := json.Marshal(struct {
		PID                          int
		Start, Boot, Window, Session string
	}{c.AgentPID, c.AgentStart, c.BootID, window, session})
	hash := sha256.Sum256(material)
	target := hex.EncodeToString(hash[:])
	if slices.Contains(c.StopAttempts, target) {
		return
	}
	// Save intent before the external effect. A worker restart never blindly
	// repeats an uncertain stop against a possibly replaced pane or PID.
	c.StopAttempts = append(c.StopAttempts, target)
	c.StopResult = "pending: " + target
	if err := remote.WriteCredential(w.o.Home, c); err != nil {
		log.Error("agent stop intent could not be saved", "error", err)
		return
	}
	var err error
	switch {
	case c.AgentPID > 0 && w.o.Stop != nil:
		err = w.o.Stop(c.AgentPID)
	case c.AgentPID > 0:
		if c.AgentStart == "" || c.BootID == "" {
			err = errors.New("managed process has no recorded incarnation")
		} else {
			err = process.TerminateObserved(ctx, process.ProcessFacts{PID: c.AgentPID, Start: c.AgentStart}, c.BootID)
		}
	case StopAgent != nil:
		err = StopAgent(ctx, c.Window, c.Session)
	default:
		err = errors.New("terminal agent stop is unavailable")
	}
	c.StopResult = "stopped: " + target
	if err != nil {
		c.StopResult = "unresolved: " + target + ": " + err.Error()
		log.Error("agent stop unresolved; later submissions remain fenced by the service", "execution", c.ExecutionID, "error", err)
	} else {
		log.Info("lost execution agent stopped", "execution", c.ExecutionID)
	}
	if err := remote.WriteCredential(w.o.Home, c); err != nil {
		log.Error("agent stop receipt could not be saved", "error", err)
	}
}
