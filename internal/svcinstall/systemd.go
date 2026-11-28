package svcinstall

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
)

// systemd manages systemd user units below dir (the systemd user directory of
// $XDG_CONFIG_HOME). Every command addresses the user instance with --user.
type systemd struct {
	dir  string
	run  Runner
	user string
}

func (systemd) name() string { return "systemd" }

func (systemd) unit(k Kind) string { return "kander-" + string(k) + ".service" }

func (s systemd) location(k Kind) string { return filepath.Join(s.dir, s.unit(k)) }

func (s systemd) systemctl(ctx context.Context, args ...string) ([]byte, error) {
	args = append([]string{"--user"}, args...)
	out, err := s.run(ctx, "systemctl", args...)
	if err != nil {
		return out, commandError("systemctl", args, out, err)
	}
	return out, nil
}

// available requires systemctl and a user instance that answers. Without it no unit can run,
// and the self-start of the server and the worker stays in charge. A missing systemctl fails
// the same command.
func (s systemd) available(ctx context.Context) error {
	if _, err := s.systemctl(ctx, "show-environment"); err != nil {
		return errors.New(config.Text("service.systemd_unavailable", err))
	}
	return nil
}

// systemdQuote quotes one word of ExecStart or one Environment assignment: inside double quotes
// a backslash and a quote are escaped, % starts a specifier and $ a variable, both doubled.
func systemdQuote(value string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")
	return `"` + r.Replace(value) + `"`
}

// systemdEnvQuote quotes an Environment assignment, where $ is not expanded.
func systemdEnvQuote(value string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%")
	return `"` + r.Replace(value) + `"`
}

// systemdPath escapes the specifiers of a path setting, which takes no quotes.
func systemdPath(value string) string { return strings.ReplaceAll(value, "%", "%%") }

func (s systemd) render(spec Spec) ([]byte, error) {
	if err := validate(spec, ""); err != nil {
		return nil, err
	}
	mark, err := encodeMarker(spec)
	if err != nil {
		return nil, err
	}
	words := []string{systemdQuote(spec.Executable)}
	for _, arg := range spec.Args {
		words = append(words, systemdQuote(arg))
	}
	var b strings.Builder
	b.WriteString("# " + mark + "\n")
	b.WriteString("# Managed by Kander: written by \"kander service install " + string(spec.Kind) + "\". Kander rewrites\n")
	b.WriteString("# or removes this file, so do not edit it; \"kander service uninstall " + string(spec.Kind) + "\" removes it.\n")
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Kander " + string(spec.Kind) + "\n")
	// A worker that exits for a replaced binary, or one that finds another worker holding the
	// lock, is started again whatever the rate.
	b.WriteString("StartLimitIntervalSec=0\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString("ExecStart=" + strings.Join(words, " ") + "\n")
	b.WriteString("WorkingDirectory=" + systemdPath(spec.WorkDir) + "\n")
	for _, kv := range environment(spec) {
		b.WriteString("Environment=" + systemdEnvQuote(kv[0]+"="+kv[1]) + "\n")
	}
	b.WriteString("Restart=always\n")
	b.WriteString("RestartSec=5\n")
	if spec.Kind == KindWorker {
		// A stop or restart of the worker ends the worker process only: the agents and terminal
		// servers it started keep running.
		b.WriteString("KillMode=process\n")
	}
	b.WriteString("StandardOutput=append:" + systemdPath(spec.LogPath) + "\n")
	b.WriteString("StandardError=append:" + systemdPath(spec.LogPath) + "\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return []byte(b.String()), nil
}

func (s systemd) read(_ context.Context, k Kind) ([]byte, bool, error) {
	return readDefinition(s.dir, s.location(k))
}

func (systemd) same(installed, rendered []byte) bool { return bytes.Equal(installed, rendered) }

func (s systemd) write(ctx context.Context, k Kind, data []byte) error {
	if err := writeDefinition(s.dir, s.location(k), data); err != nil {
		return err
	}
	if _, err := s.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	_, err := s.systemctl(ctx, "enable", s.unit(k))
	return err
}

func (s systemd) start(ctx context.Context, k Kind) error {
	_, err := s.systemctl(ctx, "start", s.unit(k))
	return err
}

func (s systemd) stop(ctx context.Context, k Kind) error {
	_, err := s.systemctl(ctx, "stop", s.unit(k))
	return err
}

func (s systemd) restart(ctx context.Context, k Kind) error {
	_, err := s.systemctl(ctx, "restart", s.unit(k))
	return err
}

func (s systemd) remove(ctx context.Context, k Kind) error {
	if _, err := s.systemctl(ctx, "disable", "--now", s.unit(k)); err != nil {
		return err
	}
	if err := removeDefinition(s.dir, s.location(k)); err != nil {
		return err
	}
	_, err := s.systemctl(ctx, "daemon-reload")
	return err
}

func (s systemd) state(ctx context.Context, k Kind) (State, error) {
	out, err := s.systemctl(ctx, "show", s.unit(k), "--property=LoadState", "--property=ActiveState")
	if err != nil {
		return State{}, err
	}
	var st State
	for _, line := range strings.Split(string(out), "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch key {
		case "LoadState":
			st.Loaded = value == "loaded"
		case "ActiveState":
			st.Active = value
		}
	}
	st.Running = st.Active == "active"
	return st, nil
}

// linger reports whether the user manager of this user runs without a login session, which a
// worker needs to come back after a reboot before anyone logs in. known is false when loginctl
// cannot tell.
func (s systemd) linger(ctx context.Context) (enabled, known bool) {
	if s.user == "" {
		return false, false
	}
	out, err := s.run(ctx, "loginctl", "show-user", s.user, "--property=Linger")
	if err != nil {
		return false, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "Linger="); ok {
			return value == "yes", true
		}
	}
	return false, false
}
