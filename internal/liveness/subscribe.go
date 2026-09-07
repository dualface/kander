package liveness

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
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
	SchemaVersion          int                     `json:"schema_version"`
	SubscriptionID         string                  `json:"subscription_id"`
	Seq                    uint64                  `json:"seq"`
	ObservedAt             time.Time               `json:"observed_at"`
	TaskRevisions          map[string]uint64       `json:"task_revisions"`
	Updated                []string                `json:"updated,omitempty"`
	WatchReferences        []string                `json:"watch_references,omitempty"`
	MembershipVersions     map[string]string       `json:"membership_versions,omitempty"`
	Memberships            map[string][]string     `json:"memberships,omitempty"`
	MembershipComplete     bool                    `json:"membership_complete"`
	ReconciliationRequired bool                    `json:"reconciliation_required"`
	ReadStatus             string                  `json:"read_status"`
	Detail                 string                  `json:"detail,omitempty"`
	Removed                []string                `json:"removed,omitempty"`
	Event                  string                  `json:"event"`
	GroupID                string                  `json:"group_id"`
	Tasks                  map[string]string       `json:"tasks"`
	Changed                []changeEvent           `json:"changed,omitempty"`
	Watched                []string                `json:"watched,omitempty"`
	Liveness               map[string]livenessJSON `json:"liveness,omitempty"`
}

var nowFn = time.Now

func parsePositiveFloat(raw, id string, args ...any) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if _, valid := subscriptionInterval(value); err != nil || !valid {
		return 0, fmt.Errorf("%s", t(id, args...))
	}
	return value, nil
}

// subscriptionInterval rejects values that would overflow or truncate to zero.
func subscriptionInterval(seconds float64) (time.Duration, bool) {
	nanoseconds := seconds * float64(time.Second)
	// float64(MaxInt64) rounds up to 2^63, so the upper bound is exclusive.
	if math.IsNaN(nanoseconds) || nanoseconds < 1 || nanoseconds >= float64(math.MaxInt64) {
		return 0, false
	}
	return time.Duration(nanoseconds), true
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
		text, err := scanned.Document(taskID)
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

// Subscribe writes JSON Lines to w until stop is closed.
func Subscribe(root string, opts subscribeOptions, w io.Writer, stop <-chan struct{}) error {
	refresh, valid := subscriptionInterval(opts.Refresh)
	if !valid {
		return fmt.Errorf("%s", t("liveness.refresh_interval_must_be_greater_than_0"))
	}
	heartbeat, valid := subscriptionInterval(opts.Heartbeat)
	if !valid {
		return fmt.Errorf("%s", t("liveness.heartbeat_interval_must_be_greater_than_0"))
	}
	session, err := newSubscription(opts)
	if err != nil {
		return err
	}
	snapshot, err := session.read(root)
	if err != nil {
		return session.unavailable(w, snapshot, err)
	}
	if err := session.emit(w, "snapshot", snapshot, nil, nil, nil); err != nil {
		return err
	}
	heartbeatDue := nowFn().Add(heartbeat)
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		heartbeatRemaining := heartbeatDue.Sub(nowFn())
		wait := refresh
		if heartbeatRemaining < wait {
			if heartbeatRemaining < 0 {
				wait = 0
			} else {
				wait = heartbeatRemaining
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-stop:
			timer.Stop()
			return nil
		case <-timer.C:
		}
		current, err := session.read(root)
		if err != nil {
			return session.unavailable(w, current, err)
		}
		var changed []changeEvent
		var updated []string
		for _, taskID := range current.monitored {
			if previous, ok := snapshot.states[taskID]; ok {
				if current.states[taskID] != previous {
					changed = append(changed, changeEvent{From: previous, TaskID: taskID, To: current.states[taskID]})
				}
				if current.revisions[taskID] != snapshot.revisions[taskID] {
					updated = append(updated, taskID)
				}
			}
		}
		if membershipChanged(snapshot, current) {
			removed := removedMembers(snapshot, current)
			if len(removed) > 0 {
				session.reconciliation = true
			}
			if err := session.emit(w, "membership-change", current, nil, nil, removed); err != nil {
				return err
			}
		}
		if len(changed) > 0 {
			if err := session.emit(w, "state-change", current, changed, updated, nil); err != nil {
				return err
			}
		} else if len(updated) > 0 {
			if err := session.emit(w, "task-update", current, nil, updated, nil); err != nil {
				return err
			}
		}
		snapshot = current
		if !nowFn().Before(heartbeatDue) {
			if err := session.emit(w, "heartbeat", current, nil, nil, nil); err != nil {
				return err
			}
			heartbeatDue = nowFn().Add(heartbeat)
		}
	}
}

func usageSubscribe(w io.Writer) {
	fmt.Fprintln(w, t(
		"liveness.usage_kander_subscribe_refresh_seconds_heartbeat_seconds_task_group",
	))
}

// RunSubscribe implements kander subscribe.
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
