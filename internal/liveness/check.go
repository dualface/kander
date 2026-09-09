package liveness

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
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
	}
	return strings.Join(fields, "\t")
}

func workingEntries(root string, taskIDs []string) ([]board.Entry, error) {
	var scanned board.Board
	var err error
	if len(taskIDs) > 0 {
		scanned, err = board.ScanTargets(root, taskIDs)
	} else {
		scanned, err = board.Scan(root)
	}
	if err != nil {
		return nil, err
	}
	var working []board.Entry
	for _, entry := range scanned.Entries {
		if entry.State == "working" {
			working = append(working, entry)
		}
	}
	sort.Slice(working, func(i, j int) bool { return working[i].TaskID < working[j].TaskID })
	return working, nil
}

func livenessLines(root string, taskIDs []string) ([]string, error) {
	entries, err := workingEntries(root, taskIDs)
	if err != nil {
		return nil, err
	}
	inputs := make([]TaskInput, 0, len(entries))
	for _, entry := range entries {
		text, err := board.ReadDocument(entry)
		inputs = append(inputs, TaskInput{Entry: entry, Text: text, ReadError: err})
	}
	var lines []string
	for _, rep := range ClassifyTasksContext(context.Background(), inputs, BatchOptions{}) {
		lines = append(lines, formatReport(rep))
	}
	return lines, nil
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
	fmt.Fprintln(w, t("liveness.usage_kander_check_all_task"))
}

// RunCheck implements kander check, reusing the board structural validation and appending a read-only liveness section.
func RunCheck(args []string) int {
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
	root, err := board.BoardRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
	code, stdout, stderr, err := board.CheckBoard(root, tasks, all)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
	for _, line := range stderr {
		fmt.Fprintln(os.Stderr, line)
	}
	lines, liveErr := livenessLines(root, tasks)
	if liveErr != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", liveErr)
		return 1
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	os.Stdout.WriteString(stdout)
	return code
}
