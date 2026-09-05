package liveness

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
)

const (
	defaultRefresh   = 1.0
	defaultHeartbeat = 900.0
)

type subscribeOptions struct {
	Group     string
	Members   []string
	Watch     []string
	Refresh   float64
	Heartbeat float64
}

type changeEvent struct {
	From   string `json:"from"`
	TaskID string `json:"task_id"`
	To     string `json:"to"`
}

type livenessJSON struct {
	Agent   string `json:"agent"`
	Status  string `json:"status"`
	Channel string `json:"channel"`
	Detail  string `json:"detail"`
}

type groupEvent struct {
	Event    string                  `json:"event"`
	GroupID  string                  `json:"group_id"`
	Tasks    map[string]string       `json:"tasks"`
	Changed  []changeEvent           `json:"changed,omitempty"`
	Watched  []string                `json:"watched,omitempty"`
	Liveness map[string]livenessJSON `json:"liveness,omitempty"`
}

var nowFn = time.Now

func parsePositiveFloat(raw, id string, args ...any) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, fmt.Errorf("%s", t(id, args...))
	}
	return value, nil
}

func parseSubscribeArgs(args []string) (subscribeOptions, string) {
	opts := subscribeOptions{Refresh: defaultRefresh, Heartbeat: defaultHeartbeat}
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--refresh" || strings.HasPrefix(arg, "--refresh="):
			raw := strings.TrimPrefix(arg, "--refresh=")
			if arg == "--refresh" {
				if i+1 >= len(args) {
					return opts, t("liveness.missing_refresh_value")
				}
				i++
				raw = args[i]
			}
			value, err := parsePositiveFloat(raw, "liveness.refresh_interval_must_be_greater_than_0")
			if err != nil {
				return opts, err.Error()
			}
			opts.Refresh = value
		case arg == "--heartbeat" || strings.HasPrefix(arg, "--heartbeat="):
			raw := strings.TrimPrefix(arg, "--heartbeat=")
			if arg == "--heartbeat" {
				if i+1 >= len(args) {
					return opts, t("liveness.missing_heartbeat_value")
				}
				i++
				raw = args[i]
			}
			value, err := parsePositiveFloat(raw, "liveness.heartbeat_interval_must_be_greater_than_0")
			if err != nil {
				return opts, err.Error()
			}
			opts.Heartbeat = value
		case arg == "--watch":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return opts, t("liveness.missing_watch_value")
			}
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				opts.Watch = append(opts.Watch, args[i])
			}
		case strings.HasPrefix(arg, "--watch="):
			opts.Watch = append(opts.Watch, strings.TrimPrefix(arg, "--watch="))
		case strings.HasPrefix(arg, "-"):
			return opts, t("board.unknown_option", arg)
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) < 2 {
		return opts, "usage"
	}
	opts.Group = positionals[0]
	opts.Members = positionals[1:]
	return opts, ""
}

func uniqueStrings(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func groupMembers(root string) (map[string][]string, error) {
	scanned, err := board.Scan(root)
	if err != nil {
		return nil, err
	}
	members := map[string][]string{}
	for taskID, entry := range scanned.Entries {
		text, err := board.ReadDocument(entry)
		if err != nil {
			continue
		}
		if group := taskGroupFrom(text); group != "" {
			members[group] = append(members[group], taskID)
		}
	}
	for group, ids := range members {
		sort.Strings(ids)
		members[group] = ids
	}
	return members, nil
}

func watchedTaskIDs(root string, values, memberIDs []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	memberSet := map[string]struct{}{}
	for _, id := range memberIDs {
		memberSet[id] = struct{}{}
	}
	var groups []string
	for _, value := range values {
		if taskGroupRe.MatchString(value) {
			groups = append(groups, value)
		}
	}
	var groupMap map[string][]string
	if len(groups) > 0 {
		var err error
		groupMap, err = groupMembers(root)
		if err != nil {
			return nil, err
		}
	}
	var watched []string
	watchedSet := map[string]struct{}{}
	for _, value := range values {
		var expanded []string
		if taskGroupRe.MatchString(value) {
			expanded = groupMap[value]
			if len(expanded) == 0 {
				return nil, fmt.Errorf("%s", t(
					"liveness.watched_task_group_has_no_members", value,
				))
			}
		} else {
			id, err := board.NormalizeTaskID(value)
			if err != nil {
				return nil, err
			}
			expanded = []string{id}
		}
		for _, taskID := range expanded {
			if _, ok := memberSet[taskID]; ok {
				return nil, fmt.Errorf("%s", t(
					"liveness.watched_target_duplicates_a_member_task", taskID,
				))
			}
			if _, ok := watchedSet[taskID]; ok {
				return nil, fmt.Errorf("%s", t(
					"liveness.watched_task_ids_must_not_be_repeated", taskID,
				))
			}
			watched = append(watched, taskID)
			watchedSet[taskID] = struct{}{}
		}
	}
	scanned, err := board.ScanTargets(root, watched)
	if err != nil {
		return nil, err
	}
	if len(scanned.Problems) > 0 {
		return nil, fmt.Errorf("%s", scanned.Problems[0].Message)
	}
	return watched, nil
}

func groupStateSnapshot(root string, taskIDs []string) (board.Board, map[string]string, error) {
	scanned, err := board.ScanTargets(root, taskIDs)
	if err != nil {
		return board.Board{}, nil, err
	}
	if len(scanned.Problems) > 0 {
		return board.Board{}, nil, fmt.Errorf("%s", scanned.Problems[0].Message)
	}
	states := map[string]string{}
	for _, taskID := range taskIDs {
		states[taskID] = scanned.Entries[taskID].State
	}
	return scanned, states, nil
}

func emitEvent(w io.Writer, payload groupEvent) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func subscriptionLiveness(scanned board.Board, states map[string]string) map[string]livenessJSON {
	reports := map[string]livenessJSON{}
	for taskID, state := range states {
		if state != "working" {
			continue
		}
		entry := scanned.Entries[taskID]
		text, err := board.ReadDocument(entry)
		var rep Report
		if err != nil {
			rep = Report{Agent: "N/A", Status: Unknown, Channel: "unknown", Detail: err.Error()}
		} else {
			rep = ClassifyTask(entry, text)
		}
		reports[taskID] = livenessJSON{Agent: rep.Agent, Status: rep.Status, Channel: rep.Channel, Detail: rep.Detail}
	}
	if len(reports) == 0 {
		return nil
	}
	return reports
}

func makeEvent(event, groupID string, tasks map[string]string, watched []string, changed []changeEvent, live map[string]livenessJSON) groupEvent {
	payload := groupEvent{Event: event, GroupID: groupID, Tasks: tasks}
	if changed != nil {
		payload.Changed = changed
	}
	if len(watched) > 0 {
		payload.Watched = watched
	}
	if len(live) > 0 {
		payload.Liveness = live
	}
	return payload
}

func validateSubscribe(root string, opts subscribeOptions) ([]string, []string, error) {
	if !taskGroupRe.MatchString(opts.Group) {
		return nil, nil, fmt.Errorf("%s", t("liveness.invalid_task_group_id", opts.Group))
	}
	if !uniqueStrings(opts.Members) {
		return nil, nil, fmt.Errorf("%s", t("liveness.member_task_ids_must_not_be_repeated"))
	}
	var members []string
	for _, value := range opts.Members {
		id, err := board.NormalizeTaskID(value)
		if err != nil {
			return nil, nil, err
		}
		members = append(members, id)
	}
	if !uniqueStrings(members) {
		return nil, nil, fmt.Errorf("%s", t("liveness.member_task_ids_must_not_be_repeated"))
	}
	watched, err := watchedTaskIDs(root, opts.Watch, members)
	if err != nil {
		return nil, nil, err
	}
	monitored := append(append([]string{}, members...), watched...)
	scanned, _, err := groupStateSnapshot(root, monitored)
	if err != nil {
		return nil, nil, err
	}
	for _, taskID := range members {
		text, err := board.ReadDocument(scanned.Entries[taskID])
		if err != nil {
			return nil, nil, err
		}
		actual := taskGroupFrom(text)
		if actual != opts.Group {
			shown := actual
			if shown == "" {
				shown = "N/A"
			}
			return nil, nil, fmt.Errorf("%s", t(
				"liveness.task_does_not_belong_to_the_specified_group_actual", taskID, shown,
			))
		}
	}
	return members, watched, nil
}

// Subscribe 向 w 输出 JSON Lines, 直到 stop 关闭.
func Subscribe(root string, opts subscribeOptions, w io.Writer, stop <-chan struct{}) error {
	members, watched, err := validateSubscribe(root, opts)
	if err != nil {
		return err
	}
	monitored := append(append([]string{}, members...), watched...)
	_, snapshot, err := groupStateSnapshot(root, monitored)
	if err != nil {
		return err
	}
	if err := emitEvent(w, makeEvent("snapshot", opts.Group, snapshot, watched, nil, nil)); err != nil {
		return err
	}
	lastEvent := nowFn()
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		heartbeatRemaining := opts.Heartbeat - nowFn().Sub(lastEvent).Seconds()
		wait := opts.Refresh
		if heartbeatRemaining < wait {
			if heartbeatRemaining < 0 {
				wait = 0
			} else {
				wait = heartbeatRemaining
			}
		}
		timer := time.NewTimer(time.Duration(wait * float64(time.Second)))
		select {
		case <-stop:
			timer.Stop()
			return nil
		case <-timer.C:
		}
		currentBoard, current, err := groupStateSnapshot(root, monitored)
		if err != nil {
			return err
		}
		var changed []changeEvent
		for _, taskID := range monitored {
			if current[taskID] != snapshot[taskID] {
				changed = append(changed, changeEvent{From: snapshot[taskID], TaskID: taskID, To: current[taskID]})
			}
		}
		if len(changed) > 0 {
			if err := emitEvent(w, makeEvent("state-change", opts.Group, current, watched, changed, nil)); err != nil {
				return err
			}
			snapshot = current
			lastEvent = nowFn()
			continue
		}
		if nowFn().Sub(lastEvent).Seconds() >= opts.Heartbeat {
			if err := emitEvent(w, makeEvent("heartbeat", opts.Group, current, watched, nil, subscriptionLiveness(currentBoard, current))); err != nil {
				return err
			}
			lastEvent = nowFn()
		}
	}
}

func usageSubscribe(w io.Writer) {
	fmt.Fprintln(w, t(
		"liveness.usage_kander_subscribe_refresh_seconds_heartbeat_seconds_task_group",
	))
}

// RunSubscribe 实现 kander subscribe.
func RunSubscribe(args []string) int {
	opts, parseErr := parseSubscribeArgs(args)
	if parseErr == "usage" {
		usageSubscribe(os.Stderr)
		return 2
	}
	if parseErr != "" {
		if strings.HasPrefix(parseErr, t("liveness.unknown_option")) || strings.HasPrefix(parseErr, t("liveness.missing_prefix")) {
			usageSubscribe(os.Stderr)
			fmt.Fprintln(os.Stderr, parseErr)
			return 2
		}
		fmt.Fprintf(os.Stderr, "kander: %s\n", parseErr)
		return 1
	}
	root, err := board.BoardRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		select {
		case <-sig:
			close(stop)
		case <-stop:
		}
	}()
	if err := Subscribe(root, opts, os.Stdout, stop); err != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
	return 0
}
