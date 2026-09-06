package probe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// HerdrPaneProbe is the fact reported by herdr pane get: the pane exists, or it is gone.
type HerdrPaneProbe struct {
	Pane       map[string]any
	GoneDetail string
}

func herdrErrorCode(res Result) string {
	for _, output := range []string{res.Stderr, res.Stdout} {
		var payload any
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			continue
		}
		obj, _ := payload.(map[string]any)
		if obj == nil {
			continue
		}
		errObj, _ := obj["error"].(map[string]any)
		if errObj == nil {
			continue
		}
		code, _ := errObj["code"].(string)
		if code != "" {
			return code
		}
	}
	return ""
}

// PublicID validates a public herdr id: non-empty and free of NUL.
func PublicID(value any) string {
	s, ok := value.(string)
	if !ok || s == "" || strings.ContainsRune(s, 0) {
		return ""
	}
	return s
}

// ProbeHerdrPane collects the facts of herdr pane get. pane_not_found is recorded as gone; other failures return an Error.
func ProbeHerdrPane(herdr, paneID string, timeout time.Duration) (HerdrPaneProbe, error) {
	res, err := runCommand(herdr, []string{"pane", "get", paneID}, timeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return HerdrPaneProbe{}, err
		}
		return HerdrPaneProbe{}, probeError("launch.herdr_invocation_failed", err.Error())
	}
	detail := failureDetail(res)
	if res.Code != 0 {
		if herdrErrorCode(res) == "pane_not_found" {
			return HerdrPaneProbe{GoneDetail: detail}, nil
		}
		return HerdrPaneProbe{}, probeError(
			"launch.pane_does_not_exist", paneID, detail,
		)
	}
	var payload any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		return HerdrPaneProbe{}, probeError(
			"probe.herdr_pane_get_failed_response_is_not_json", err.Error(),
		)
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return HerdrPaneProbe{}, probeError(
			"probe.herdr_pane_get_failed_response_is_not_a_json",
		)
	}
	data, ok := obj["result"].(map[string]any)
	if !ok {
		return HerdrPaneProbe{}, probeError(
			"probe.herdr_pane_get_failed_response_is_missing_result",
		)
	}
	pane, _ := data["pane"].(map[string]any)
	actual := ""
	if pane != nil {
		actual = PublicID(pane["pane_id"])
	}
	if pane == nil || actual != paneID || actual == "" {
		return HerdrPaneProbe{}, probeError("launch.pane_does_not_exist_2", paneID)
	}
	return HerdrPaneProbe{Pane: pane}, nil
}

// HerdrProbePane returns the pane object, or nil when it is gone.
func HerdrProbePane(herdr, paneID string, timeout time.Duration) (map[string]any, error) {
	probe, err := ProbeHerdrPane(herdr, paneID, timeout)
	if err != nil {
		return nil, err
	}
	return probe.Pane, nil
}

// HerdrResult extracts the result object of a successful herdr JSON response.
func HerdrResult(res Result) (map[string]any, error) {
	if res.Code != 0 {
		return nil, probeError(failureDetail(res), failureDetail(res))
	}
	var payload any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		return nil, probeError(err.Error(), err.Error())
	}
	obj, _ := payload.(map[string]any)
	data, _ := obj["result"].(map[string]any)
	if data == nil {
		return nil, probeError("probe.herdr_response_is_missing_result")
	}
	return data, nil
}

// Capture runs an external program, so liveness reverse lookup reuses the same collection point.
func Capture(program string, args []string, timeout time.Duration) (Result, error) {
	return runCommand(program, args, timeout)
}
