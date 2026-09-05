//go:build unix

package menu

import (
	"strings"
	"testing"
)

func TestDoctorAutoLauncherWithOnlyHerdr(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	h.fakeCommand("herdr", "")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "auto"}))
	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "launcher=auto 在启动时按当前环境选择") {
		t.Fatalf("%s", err)
	}
}

func TestDoctorRejectsHerdrWithoutWorkspaceID(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("herdr", "")
	h.setenv("HERDR_ENV", "1")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "herdr"}))
	code, _, err := h.run("doctor")
	if code != 1 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "缺少 HERDR_WORKSPACE_ID") {
		t.Fatalf("%s", err)
	}
}
