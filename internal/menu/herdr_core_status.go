package menu

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

// herdrCoreStatusTimeout bounds one `hc server status` of doctor.
var herdrCoreStatusTimeout = 5 * time.Second

// probeHerdrCoreStatus asks hc whether the core runs. Only native Windows uses
// it: there `hc server status` never starts a core, while on Unix it does. At
// most one result is set; neither means the core runs.
func probeHerdrCoreStatus(program string) (down, unknown string) {
	ctx, cancel := context.WithTimeout(context.Background(), herdrCoreStatusTimeout)
	defer cancel()
	result, err := probe.CaptureContext(ctx, program, []string{"server", "status", "--json"})
	if err != nil {
		return "", config.Text("herdr_core.doctor_status_undecidable", probe.FailureDetail(err))
	}
	if result.Code == 0 {
		var running struct {
			PID *int64 `json:"pid"`
		}
		if json.Unmarshal([]byte(result.Stdout), &running) == nil && running.PID != nil {
			return "", ""
		}
	} else {
		for _, output := range []string{result.Stderr, result.Stdout} {
			var failed struct {
				Error struct {
					Kind    string `json:"kind"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(output), &failed) == nil && failed.Error.Kind == "unavailable" {
				return config.Text("herdr_core.doctor_status_unavailable", failed.Error.Message), ""
			}
		}
	}
	detail := strings.TrimSpace(result.Stderr + result.Stdout)
	return "", config.Text("herdr_core.doctor_status_undecidable", detail)
}
