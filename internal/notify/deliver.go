package notify

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/probe"
)

func herdrFailureDetail(res probe.Result) string {
	if s := strings.TrimSpace(res.Stderr); s != "" {
		return s
	}
	return fmt.Sprintf("exit %d", res.Code)
}

// HerdrAgentPrompt 向 pane 内已运行的 Agent TUI 投递正文, 不用 pane run.
func HerdrAgentPrompt(herdr, paneID, text string) error {
	res, err := probe.Capture(herdr, []string{"agent", "prompt", paneID, text}, 0)
	if err != nil {
		return notifyError("launch.herdr_invocation_failed", err.Error())
	}
	if res.Code != 0 {
		return notifyError(
			"notify.herdr_agent_prompt_failed", herdrFailureDetail(res),
		)
	}
	return nil
}

func herdrDirectNotify(herdr, paneID, instruction, marker string, timeout float64) (bool, string, error) {
	if err := HerdrAgentPrompt(herdr, paneID, instruction); err != nil {
		return false, "", err
	}
	fmt.Println(t("notify.delivered_waiting_for_acknowledgement_channel_herdr_direct"))
	flushStdout()
	ms := int(timeout * 1000)
	if ms < 1 {
		ms = 1
	}
	res, err := probe.Capture(herdr, []string{
		"pane", "wait-output", paneID, "--match", marker, "--source", "recent",
		"--timeout", fmt.Sprintf("%d", ms),
	}, 0)
	if err != nil {
		return false, err.Error(), nil
	}
	if res.Code != 0 {
		return false, herdrFailureDetail(res), nil
	}
	return true, "", nil
}

func tmuxDirectNotify(tmux, paneID, instruction, marker string, timeout float64) (bool, string, error) {
	for _, args := range [][]string{
		{"send-keys", "-t", paneID, "-l", instruction},
		{"send-keys", "-t", paneID, "Enter"},
	} {
		res, err := probe.Capture(tmux, args, 0)
		if err != nil {
			return false, "", notifyError("notify.tmux_direct_notification_failed", err.Error())
		}
		if res.Code != 0 {
			detail := strings.TrimSpace(res.Stderr)
			if detail == "" {
				detail = fmt.Sprintf("exit %d", res.Code)
			}
			return false, "", notifyError("notify.tmux_direct_notification_failed", detail)
		}
	}
	fmt.Println(t("notify.delivered_waiting_for_acknowledgement_channel_tmux_direct"))
	flushStdout()
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	for {
		res, err := probe.Capture(tmux, []string{"capture-pane", "-p", "-t", paneID}, 0)
		if err != nil {
			return false, err.Error(), nil
		}
		if res.Code != 0 {
			detail := strings.TrimSpace(res.Stderr)
			if detail == "" {
				detail = fmt.Sprintf("exit %d", res.Code)
			}
			return false, detail, nil
		}
		if strings.Contains(res.Stdout, marker) {
			return true, "", nil
		}
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return false, t("notify.acknowledgement_timed_out", marker), nil
		}
		d := pollInterval
		if remaining < d {
			d = remaining
		}
		sleepFn(d)
	}
}

func flushStdout() {
	_ = os.Stdout.Sync()
}
