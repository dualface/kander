package notify

import (
	"os/exec"
	"regexp"
	"time"

	"github.com/dualface/kander/internal/liveness"
)

var (
	herdrWindowRe = regexp.MustCompile(`^herdr:([^:\s]+:[^:\s]+):([^:\s]+:[^:\s]+)$`)
	tmuxWindowRe  = regexp.MustCompile(`^(tmux|tmux-session):([^:\s]+):([^:\s]+):([^:\s]+)$`)

	lookPath = exec.LookPath
	sleepFn  = time.Sleep
	nowFn    = time.Now
)

// DirectTarget is a fully validated direct-delivery address that may receive a payload.
type DirectTarget struct {
	Kind    string
	Program string
	PaneID  string
	Window  string
	Timeout float64
}

func waitForTarget(probeFn func(time.Duration) TargetProbe, timeout float64) (TargetProbe, float64, error) {
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	last := TargetProbe{State: "busy", Detail: t("notify.target_probe_timed_out")}
	for {
		remainingDuration := deadline.Sub(nowFn())
		if remainingDuration <= 0 {
			return last, 0, &BusyError{Message: t(
				"notify.target_agent_is_busy_nothing_was_delivered", last.Detail,
			)}
		}
		result := probeFn(remainingDuration)
		last = result
		if result.State != "busy" {
			remaining := deadline.Sub(nowFn()).Seconds()
			if remaining < 0 {
				remaining = 0
			}
			return result, remaining, nil
		}
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return result, 0, &BusyError{Message: t(
				"notify.target_agent_is_busy_nothing_was_delivered", result.Detail,
			)}
		}
		d := pollInterval
		if remaining < d {
			d = remaining
		}
		sleepFn(d)
	}
}

func requireReady(probe TargetProbe) error {
	if probe.State != "ready" {
		return &Error{Message: probe.Detail}
	}
	return nil
}

func staleLookup(detail string, lookup func() (DirectTarget, error)) (DirectTarget, error) {
	target, err := lookup()
	if err != nil {
		if isBusy(err) {
			return DirectTarget{}, err
		}
		return DirectTarget{}, notifyError(
			"notify.stale_address_lookup", detail, err.Error(),
		)
	}
	return target, nil
}

// ResolveTarget resolves and fully validates the direct-delivery target before any payload is created.
func ResolveTarget(window, paneOverride string, session liveness.TaskSession, timeout float64) (DirectTarget, error) {
	herdrMatch := herdrWindowRe.FindStringSubmatch(window)
	tmuxMatch := tmuxWindowRe.FindStringSubmatch(window)
	if paneOverride != "" {
		herdr, err := lookPath("herdr")
		if err != nil {
			return DirectTarget{}, notifyError("liveness.herdr_is_not_in_path")
		}
		tabID, paneID, err := HerdrExplicitTarget(herdr, paneOverride)
		if err != nil {
			return DirectTarget{}, err
		}
		probe, remaining, err := waitForTarget(func(remaining time.Duration) TargetProbe {
			return HerdrNotifyProbe(herdr, paneID, session, remaining)
		}, timeout)
		if err != nil {
			return DirectTarget{}, err
		}
		if err := requireReady(probe); err != nil {
			return DirectTarget{}, err
		}
		return DirectTarget{Kind: "herdr", Program: herdr, PaneID: paneID, Window: "herdr:" + tabID + ":" + paneID, Timeout: remaining}, nil
	}
	if herdrMatch != nil {
		paneID := herdrMatch[2]
		herdr, err := lookPath("herdr")
		if err != nil {
			return DirectTarget{}, notifyError("liveness.herdr_is_not_in_path")
		}
		probe, remaining, err := waitForTarget(func(remaining time.Duration) TargetProbe {
			return HerdrNotifyProbe(herdr, paneID, session, remaining)
		}, timeout)
		if err != nil {
			return DirectTarget{}, err
		}
		if probe.State != "stale" {
			if err := requireReady(probe); err != nil {
				return DirectTarget{}, err
			}
			return DirectTarget{Kind: "herdr", Program: herdr, PaneID: paneID, Timeout: remaining}, nil
		}
		return staleLookup(probe.Detail, func() (DirectTarget, error) {
			tabID, discovered, err := liveness.HerdrReverseLookup(herdr, session)
			if err != nil {
				return DirectTarget{}, err
			}
			found, finalTimeout, err := waitForTarget(func(remaining time.Duration) TargetProbe {
				return HerdrNotifyProbe(herdr, discovered, session, remaining)
			}, remaining)
			if err != nil {
				return DirectTarget{}, err
			}
			if err := requireReady(found); err != nil {
				return DirectTarget{}, err
			}
			return DirectTarget{
				Kind: "herdr", Program: herdr, PaneID: discovered,
				Window: "herdr:" + tabID + ":" + discovered, Timeout: finalTimeout,
			}, nil
		})
	}
	if tmuxMatch != nil {
		launcher, paneID := tmuxMatch[1], tmuxMatch[4]
		tmux, err := lookPath("tmux")
		if err != nil {
			return DirectTarget{}, notifyError("liveness.tmux_is_not_in_path")
		}
		probe, remaining, err := waitForTarget(func(remaining time.Duration) TargetProbe {
			return TmuxNotifyProbe(tmux, paneID, session, remaining)
		}, timeout)
		if err != nil {
			return DirectTarget{}, err
		}
		if probe.State != "stale" {
			if err := requireReady(probe); err != nil {
				return DirectTarget{}, err
			}
			return DirectTarget{Kind: "tmux", Program: tmux, PaneID: paneID, Timeout: remaining}, nil
		}
		return staleLookup(probe.Detail, func() (DirectTarget, error) {
			location, err := liveness.TmuxReverseLookup(tmux, session)
			if err != nil {
				return DirectTarget{}, err
			}
			found, finalTimeout, err := waitForTarget(func(remaining time.Duration) TargetProbe {
				return TmuxNotifyProbe(tmux, location.PaneID, session, remaining)
			}, remaining)
			if err != nil {
				return DirectTarget{}, err
			}
			if err := requireReady(found); err != nil {
				return DirectTarget{}, err
			}
			return DirectTarget{
				Kind: "tmux", Program: tmux, PaneID: location.PaneID,
				Window: liveness.RenderTmuxWindow(launcher, location), Timeout: finalTimeout,
			}, nil
		})
	}
	if window == "" {
		herdr, err := lookPath("herdr")
		if err != nil {
			return DirectTarget{}, notifyError(
				"notify.herdr_is_not_in_path_cannot_look_up_a",
			)
		}
		tabID, paneID, err := liveness.HerdrReverseLookup(herdr, session)
		if err != nil {
			return DirectTarget{}, err
		}
		probe, remaining, err := waitForTarget(func(remaining time.Duration) TargetProbe {
			return HerdrNotifyProbe(herdr, paneID, session, remaining)
		}, timeout)
		if err != nil {
			return DirectTarget{}, err
		}
		if err := requireReady(probe); err != nil {
			return DirectTarget{}, err
		}
		return DirectTarget{
			Kind: "herdr", Program: herdr, PaneID: paneID,
			Window: "herdr:" + tabID + ":" + paneID, Timeout: remaining,
		}, nil
	}
	return DirectTarget{}, notifyError(
		"notify.no_direct_notification_address", orNA(window),
	)
}
