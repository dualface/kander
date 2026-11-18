package liveness

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
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

func takeFlag(args []string, name string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == name {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return out, found
}

func usageCheck(w *os.File) {
	fmt.Fprintln(w, config.CommandUsage("check"))
}

// RunCheckRemote implements kander check against the server: the structural validation plus a read-only liveness section.
func RunCheckRemote(args []string) int {
	args, all := takeFlag(args, "--all")
	var tasks []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			usageCheck(os.Stderr)
			fmt.Fprintln(os.Stderr, t("board.unknown_option", arg))
			return 2
		}
		tasks = append(tasks, arg)
	}
	cfg, err := config.Load(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, warning := range config.AgentWarnings(cfg) {
		fmt.Fprintln(os.Stderr, warning)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return runRemoteCheck(cfg, tasks, all)
}
