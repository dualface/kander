package liveness

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

// clockEvents records delivery and changes only the temporary board from the output worker.
type clockEvents struct {
	events  []groupEvent
	onEvent func(groupEvent) error
}

func (w *clockEvents) Write(p []byte) (int, error) {
	var event groupEvent
	if err := json.Unmarshal(p, &event); err != nil {
		return 0, err
	}
	w.events = append(w.events, event)
	if err := w.onEvent(event); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *clockEvents) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return w.Write(p)
}

// This reverses the audit's assertion that repeated changes suppress liveness.
func TestAuditChangesSuppressLiveness(t *testing.T) {
	for _, watched := range []bool{false, true} {
		for _, working := range []bool{false, true} {
			name := "watched=" + strconv.FormatBool(watched) + "/working=" + strconv.FormatBool(working)
			t.Run(name, func(t *testing.T) {
				root := tempBoard(t)
				group := time.Now().Format("20060102") + "-clock-group"
				if _, err := board.NewTask(root, "chore", "clock-moving", "Moving task", "en", false); err != nil {
					t.Fatal(err)
				}
				movingID := todayID("clock-moving")
				movingPath := currentEntry(t, root, movingID).Document
				makeReady(t, movingPath)
				idleID, idlePath := makeWorking(t, "clock-idle", "Unchanged task")
				setTaskGroup(t, idlePath, group)
				setLocation(t, idlePath, "codex", "foreground")
				if !working {
					if _, err := board.MoveEntry(currentEntry(t, root, idleID), root, "review"); err != nil {
						t.Fatal(err)
					}
				}
				opts := subscribeOptions{Group: group, Members: []string{idleID}, Refresh: .000001, Heartbeat: .1}
				if watched {
					opts.Watch = []string{movingID}
				} else {
					setTaskGroup(t, movingPath, group)
					opts.Members = append(opts.Members, movingID)
				}
				movingEntry := currentEntry(t, root, movingID)
				state := movingEntry.State
				// Advance the clock on each delivered change, avoiding wall-clock races.
				oldNow := nowFn
				clock := time.Now()
				var clockMu sync.Mutex
				nowFn = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock }
				t.Cleanup(func() { nowFn = oldNow })
				stop := make(chan struct{})
				changes, heartbeats := 0, 0
				writer := &clockEvents{}
				writer.onEvent = func(event groupEvent) error {
					switch event.Event {
					case "heartbeat":
						heartbeats++
						if changes < 3*heartbeats {
							t.Errorf("heartbeat %d after %d changes; want %d", heartbeats, changes, 3*heartbeats)
						}
						if working {
							if live, ok := event.Liveness[idleID]; !ok || live.Status != Unknown || len(event.Liveness) != 1 {
								t.Errorf("missing unchanged task liveness: %+v", event)
							}
						} else if event.Liveness != nil {
							t.Errorf("unexpected liveness without working tasks: %+v", event)
						}
						if heartbeats == 2 {
							close(stop)
						}
						return nil
					case "state-change":
						changes++
						clockMu.Lock()
						clock = clock.Add(40 * time.Millisecond)
						clockMu.Unlock()
						if len(event.Changed) != 1 || event.Changed[0].TaskID != movingID || event.Tasks[movingID] != state {
							t.Errorf("incorrect state change: %+v", event)
						}
						if changes == 12 {
							close(stop)
							return nil
						}
					}
					if event.Liveness != nil {
						t.Errorf("non-heartbeat contains liveness: %+v", event)
					}
					next := "todo"
					if state == "todo" {
						next = "backlog"
					}
					moved, err := board.MoveEntry(movingEntry, root, next)
					if err != nil {
						return err
					}
					movingEntry, state = moved, moved.State
					return nil
				}
				if err := Subscribe(root, opts, writer, stop); err != nil {
					t.Fatal(err)
				}
				if heartbeats != 2 || changes < 6 {
					t.Fatalf("heartbeats=%d changes=%d; want 2 and 6", heartbeats, changes)
				}
			})
		}
	}
}

func TestSubscribePureHeartbeatBeforeRefresh(t *testing.T) {
	root := tempBoard(t)
	group := time.Now().Format("20060102") + "-pure-clock-group"
	id, path := makeWorking(t, "pure-clock", "Idle task")
	setTaskGroup(t, path, group)
	if _, err := board.MoveEntry(currentEntry(t, root, id), root, "review"); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(stop) }) }
	timeout := time.AfterFunc(time.Second, finish)
	defer timeout.Stop()
	writer := &clockEvents{onEvent: func(event groupEvent) error {
		if event.Event == "heartbeat" {
			finish()
		}
		return nil
	}}
	if err := Subscribe(root, subscribeOptions{Group: group, Members: []string{id}, Refresh: 3600, Heartbeat: .002}, writer, stop); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 2 || writer.events[1].Event != "heartbeat" || writer.events[1].Liveness != nil {
		t.Fatalf("events: %+v", writer.events)
	}
}

// This reverses the audit's acceptance of an overflowing positive interval.
func TestAuditRefreshDurationOverflow(t *testing.T) {
	for _, flag := range []string{"--refresh", "--heartbeat"} {
		for _, raw := range []string{"1e300", "9223372036.854776", "1e-300", "0.0000000009", "0", "-1", "NaN", "Inf", "-Inf", "invalid"} {
			t.Run(flag+"/"+raw, func(t *testing.T) {
				_, errText := parseSubscribeArgs([]string{flag, raw, "20260907-clock-group", "20260907-clock-task"})
				if errText == "" || errText == "usage" {
					t.Fatalf("invalid interval %s %s accepted: %q", flag, raw, errText)
				}
			})
		}
	}
}

func TestSubscribeIntervalBoundariesAndDefaults(t *testing.T) {
	args := []string{"20260907-clock-group", "20260907-clock-task"}
	opts, errText := parseSubscribeArgs(args)
	if errText != "" || opts.Refresh != 1 || opts.Heartbeat != 900 {
		t.Fatalf("defaults: %+v %q", opts, errText)
	}
	upper := math.Nextafter(float64(math.MaxInt64)/float64(time.Second), 0)
	for _, raw := range []string{"1e-9", "1.9e-9", ".25", strconv.FormatFloat(upper, 'g', -1, 64)} {
		for _, flag := range []string{"--refresh=", "--heartbeat="} {
			opts, errText := parseSubscribeArgs(append([]string{flag + raw}, args...))
			if errText != "" {
				t.Fatalf("valid %s%s rejected: %s", flag, raw, errText)
			}
			for _, value := range []float64{opts.Refresh, opts.Heartbeat} {
				if duration, valid := subscriptionInterval(value); !valid || duration <= 0 {
					t.Fatalf("unsafe duration for %g: %s", value, duration)
				}
			}
		}
	}
	if duration, valid := subscriptionInterval(1.9e-9); !valid || duration != time.Nanosecond {
		t.Fatalf("fractional nanosecond truncation: %s, %t", duration, valid)
	}
}

func TestSubscribeRejectsInvalidIntervalsBeforeBoardAccess(test *testing.T) {
	for _, value := range []float64{0, -1, 1e-300, 1e300, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, refresh := range []bool{false, true} {
			opts := subscribeOptions{Refresh: defaultRefresh, Heartbeat: defaultHeartbeat}
			if refresh {
				opts.Refresh = value
			} else {
				opts.Heartbeat = value
			}
			var output bytes.Buffer
			err := Subscribe(filepath.Join(test.TempDir(), "missing"), opts, &output, make(chan struct{}))
			id := "liveness.heartbeat_interval_must_be_greater_than_0"
			if refresh {
				id = "liveness.refresh_interval_must_be_greater_than_0"
			}
			if err == nil || err.Error() != t(id) || output.Len() != 0 {
				test.Fatalf("interval %g: err=%v output=%q", value, err, output.String())
			}
		}
	}
}
