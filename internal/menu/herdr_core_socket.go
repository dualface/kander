package menu

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
)

// Every hc command, `hc server status` included, starts a missing core on
// Unix (hc 0.1.17), so doctor never asks hc whether the core runs. It finds the
// core's socket the way hc resolves it and only tries to connect.

// herdrCoreSocketEnv is what the socket resolution reads, injectable for tests.
type herdrCoreSocketEnv struct {
	getenv   func(string) string
	readFile func(string) ([]byte, error)
	isDir    func(string) bool
	uid      int
}

func systemHerdrCoreSocketEnv() herdrCoreSocketEnv {
	return herdrCoreSocketEnv{
		getenv:   os.Getenv,
		readFile: os.ReadFile,
		isDir: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && info.IsDir()
		},
		uid: os.Getuid(),
	}
}

// configSocketKey matches the socket key of the hc config file, bare or
// quoted, and captures its value text; configSocketValue reads a basic string
// without escapes or a literal string from that text.
var (
	configSocketKey   = regexp.MustCompile(`^(?:socket|"socket"|'socket')\s*=(.*)$`)
	configSocketValue = regexp.MustCompile(`^\s*(?:"([^"\\]*)"|'([^']*)')\s*(?:#.*)?$`)
)

// herdrCoreSocket resolves the socket of the herdr-core session the hc CLI
// talks to: HC_SOCKET, else `socket` in the hc config file, else
// <runtime dir>/herdr-core/<session>.sock, where the runtime dir is
// XDG_RUNTIME_DIR or an existing /run/user/<uid>, else
// <state>/herdr-core/<session>/<session>.sock. The session is HC_SESSION or
// "default".
func herdrCoreSocket(env herdrCoreSocketEnv) (string, error) {
	if socket := env.getenv("HC_SOCKET"); socket != "" {
		return socket, nil
	}
	session := strings.TrimSpace(env.getenv("HC_SESSION"))
	if session == "" {
		session = "default"
	}
	home := env.getenv("HOME")
	if home == "" {
		home = "/"
	}
	xdg := func(name, fallback string) string {
		if value := env.getenv(name); value != "" {
			return value
		}
		return filepath.Join(home, fallback)
	}
	socket, err := configuredSocket(env, filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "herdr-core", "config.toml"))
	if err != nil || socket != "" {
		return socket, err
	}
	run := env.getenv("XDG_RUNTIME_DIR")
	if candidate := "/run/user/" + strconv.Itoa(env.uid); run == "" && env.isDir(candidate) {
		run = candidate
	}
	if run != "" {
		return filepath.Join(run, "herdr-core", session+".sock"), nil
	}
	return filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), "herdr-core", session, session+".sock"), nil
}

// configuredSocket reads the top-level socket key of the hc config file; a
// missing file means none. A socket key this reader cannot parse is an error,
// so doctor reports that it cannot tell rather than probing another path.
func configuredSocket(env herdrCoreSocketEnv, path string) (string, error) {
	data, err := env.readFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			break
		}
		key := configSocketKey.FindStringSubmatch(line)
		if key == nil {
			continue
		}
		value := configSocketValue.FindStringSubmatch(key[1])
		if value == nil {
			return "", errors.New(config.Text("herdr_core.doctor_socket_unreadable", path))
		}
		return value[1] + value[2], nil
	}
	return "", scanner.Err()
}

// probeHerdrCoreServer reports why herdr-core is not running, or "" when its
// socket accepts a connection. It never starts the core.
func probeHerdrCoreServer(env herdrCoreSocketEnv) string {
	socket, err := herdrCoreSocket(env)
	if err != nil {
		return err.Error()
	}
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return config.Text("herdr_core.doctor_no_core", socket, err.Error())
	}
	_ = conn.Close()
	return ""
}
