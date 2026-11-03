package menu

import (
	"errors"
	"os"
	"testing"
)

func TestHerdrCoreSocketResolution(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		config string
		runDir bool
		want   string
		fails  bool
	}{
		{"explicit socket", map[string]string{"HC_SOCKET": "/s/core.sock", "HOME": "/h", "XDG_RUNTIME_DIR": "/run/x"}, `socket = "/cfg.sock"`, true, "/s/core.sock", false},
		{"config socket", map[string]string{"HOME": "/h", "XDG_RUNTIME_DIR": "/run/x"}, "# hc\nsocket = \"/cfg.sock\" # mine\n[machines]\nsocket = \"/other\"", true, "/cfg.sock", false},
		{"literal config socket", map[string]string{"HOME": "/h"}, "socket = '/lit.sock'", false, "/lit.sock", false},
		{"unreadable config socket", map[string]string{"HOME": "/h"}, `socket = "/a\"b"`, false, "", true},
		{"table socket ignored", map[string]string{"HOME": "/h", "XDG_RUNTIME_DIR": "/run/x"}, "[machine]\nsocket = \"/m.sock\"", false, "/run/x/herdr-core/default.sock", false},
		{"runtime dir", map[string]string{"HOME": "/h", "XDG_RUNTIME_DIR": "/run/x", "HC_SESSION": " work "}, "", false, "/run/x/herdr-core/work.sock", false},
		{"uid runtime dir", map[string]string{"HOME": "/h"}, "", true, "/run/user/1000/herdr-core/default.sock", false},
		{"state dir", map[string]string{"HOME": "/h"}, "", false, "/h/.local/state/herdr-core/default/default.sock", false},
		{"xdg state dir", map[string]string{"HOME": "/h", "XDG_STATE_HOME": "/st"}, "", false, "/st/herdr-core/default/default.sock", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := herdrCoreSocketEnv{
				getenv: func(name string) string { return tc.env[name] },
				readFile: func(path string) ([]byte, error) {
					if tc.config == "" || path != "/h/.config/herdr-core/config.toml" {
						return nil, os.ErrNotExist
					}
					return []byte(tc.config), nil
				},
				isDir: func(path string) bool { return tc.runDir && path == "/run/user/1000" },
				uid:   1000,
			}
			got, err := herdrCoreSocket(env)
			if (err != nil) != tc.fails || got != tc.want {
				t.Fatalf("socket=%q err=%v; want %q fails=%v", got, err, tc.want, tc.fails)
			}
		})
	}
	failing := herdrCoreSocketEnv{
		getenv:   func(string) string { return "" },
		readFile: func(string) ([]byte, error) { return nil, errors.New("permission denied") },
		isDir:    func(string) bool { return false },
	}
	if _, err := herdrCoreSocket(failing); err == nil {
		t.Fatal("unreadable config file accepted")
	}
}
