package probe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// HerdrPaneProbe 是 herdr pane get 的事实: pane 存在或已消失.
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

// PublicID 校验 herdr 公开 id: 非空且不含 NUL.
func PublicID(value any) string {
	s, ok := value.(string)
	if !ok || s == "" || strings.ContainsRune(s, 0) {
		return ""
	}
	return s
}

// ProbeHerdrPane 采集 herdr pane get 事实. pane_not_found 记为消失, 其它失败返回 Error.
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

// HerdrProbePane 返回 pane 对象或 nil (已消失).
func HerdrProbePane(herdr, paneID string, timeout time.Duration) (map[string]any, error) {
	probe, err := ProbeHerdrPane(herdr, paneID, timeout)
	if err != nil {
		return nil, err
	}
	return probe.Pane, nil
}

// HerdrResult 解析 herdr JSON 成功响应的 result 对象.
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

// Capture 运行外部程序, 供 liveness 反查复用同一采集口.
func Capture(program string, args []string, timeout time.Duration) (Result, error) {
	return runCommand(program, args, timeout)
}
