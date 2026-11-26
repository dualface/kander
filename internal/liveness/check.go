package liveness

import (
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

func formatReport(rep Report) string {
	fields := []string{
		t("liveness.liveness", rep.TaskID),
		"Agent=" + rep.Agent,
		t("liveness.status", rep.Status),
		t("liveness.channel", rep.Channel),
		t("liveness.container", rep.Container),
		t("liveness.observation", rep.ObservedAt.Format(time.RFC3339Nano), rep.ObservationValid, rep.RuntimeState),
	}
	if rep.NewWindow != "" {
		fields = append(fields, t("liveness.new_address", rep.NewWindow))
	}
	if rep.Detail != "" {
		fields = append(fields, t("liveness.reason", rep.Detail))
	}
	switch rep.Status {
	case Stopped:
		fields = append(fields, t(
			"liveness.suggestion_kander_resume_agent_other_message_status", rep.TaskID,
		))
	case Drifted:
		fields = append(fields, t(
			"liveness.suggestion_kander_notify_message_status", rep.TaskID,
		))
	case AliveOrphaned:
		fields = append(fields, t("liveness.suggestion_orphaned", rep.TaskID))
	case IdentityMismatch, Unprobeable:
		fields = append(fields, t("liveness.suggestion_verify_by_hand"))
	}
	return strings.Join(fields, "\t")
}

// RunCheckRemote implements kander check against the server: the structural validation plus a read-only liveness section.
func RunCheckRemote(tasks []string, all bool) int {
	cfg, err := config.Load(false)
	if err != nil {
		return command.Fail(err)
	}
	for _, warning := range config.AgentWarnings(cfg) {
		command.Notify("agent_warning", warning)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return command.Fail(err)
	}
	return runRemoteCheck(cfg, tasks, all)
}
