package svcinstall

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

// launchd manages LaunchAgents below dir (~/Library/LaunchAgents) in the gui domain of uid.
type launchd struct {
	dir string
	uid string
	run Runner
}

// launchdLabelPrefix starts the label of each service; the label also names the plist file.
const launchdLabelPrefix = "io.github.dualface.kander."

func (launchd) name() string { return "launchd" }

func (launchd) label(k Kind) string { return launchdLabelPrefix + string(k) }

func (l launchd) location(k Kind) string { return filepath.Join(l.dir, l.label(k)+".plist") }

func (l launchd) domain() string { return "gui/" + l.uid }

func (l launchd) target(k Kind) string { return l.domain() + "/" + l.label(k) }

func (l launchd) launchctl(ctx context.Context, args ...string) ([]byte, error) {
	out, err := l.run(ctx, "launchctl", args...)
	if err != nil {
		return out, commandError("launchctl", args, out, err)
	}
	return out, nil
}

func (l launchd) available(ctx context.Context) error {
	if _, err := l.launchctl(ctx, "print", l.domain()); err != nil {
		return errors.New(config.Text("service.launchd_unavailable", err))
	}
	return nil
}

// xmlText escapes the text of an element. Quotes need no escape there, which keeps the marker
// and the cmd.exe line of a task readable.
func xmlText(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

func (l launchd) render(spec Spec) ([]byte, error) {
	if err := validate(spec, ""); err != nil {
		return nil, err
	}
	mark, err := encodeMarker(spec)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<!-- " + mark + " -->\n")
	b.WriteString("<!-- Managed by Kander: written by kander service install " + string(spec.Kind) + "; do not edit. -->\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	key := func(name string) { b.WriteString("\t<key>" + name + "</key>\n") }
	str := func(indent, value string) { b.WriteString(indent + "<string>" + xmlText(value) + "</string>\n") }
	key("Label")
	str("\t", l.label(spec.Kind))
	key("ProgramArguments")
	b.WriteString("\t<array>\n")
	str("\t\t", spec.Executable)
	for _, arg := range spec.Args {
		str("\t\t", arg)
	}
	b.WriteString("\t</array>\n")
	key("EnvironmentVariables")
	b.WriteString("\t<dict>\n")
	for _, kv := range environment(spec) {
		b.WriteString("\t\t<key>" + xmlText(kv[0]) + "</key>\n")
		str("\t\t", kv[1])
	}
	b.WriteString("\t</dict>\n")
	key("WorkingDirectory")
	str("\t", spec.WorkDir)
	key("RunAtLoad")
	b.WriteString("\t<true/>\n")
	key("KeepAlive")
	b.WriteString("\t<true/>\n")
	// launchd kills the whole process group of a job that stops; the agents and terminal
	// servers a worker starts belong to that group and must outlive a worker restart.
	key("AbandonProcessGroup")
	b.WriteString("\t<true/>\n")
	key("StandardOutPath")
	str("\t", spec.LogPath)
	key("StandardErrorPath")
	str("\t", spec.LogPath)
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String()), nil
}

func (l launchd) read(_ context.Context, k Kind) ([]byte, bool, error) {
	return readDefinition(l.dir, l.location(k))
}

func (launchd) same(installed, rendered []byte) bool { return bytes.Equal(installed, rendered) }

// write only publishes the plist: loading it into the domain is the start.
func (l launchd) write(_ context.Context, k Kind, data []byte) error {
	return writeDefinition(l.dir, l.location(k), data)
}

func (l launchd) loaded(ctx context.Context, k Kind) bool {
	_, err := l.run(ctx, "launchctl", "print", l.target(k))
	return err == nil
}

func (l launchd) bootstrap(ctx context.Context, k Kind) error {
	// A label the user disabled refuses to load; installing or starting it enables it again.
	_, _ = l.run(ctx, "launchctl", "enable", l.target(k))
	_, err := l.launchctl(ctx, "bootstrap", l.domain(), l.location(k))
	return err
}

func (l launchd) start(ctx context.Context, k Kind) error {
	if !l.loaded(ctx, k) {
		return l.bootstrap(ctx, k)
	}
	_, err := l.launchctl(ctx, "kickstart", l.target(k))
	return err
}

// stop unloads the job: with KeepAlive, a job that is only killed comes back at once. bootout
// returns before the job has left the domain, so the wait polls until it has.
func (l launchd) stop(ctx context.Context, k Kind) error {
	if !l.loaded(ctx, k) {
		return nil
	}
	if _, err := l.launchctl(ctx, "bootout", l.target(k)); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for l.loaded(ctx, k) {
		if time.Now().After(deadline) {
			return errors.New(config.Text("service.launchd_still_loaded", l.target(k)))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

// restart unloads and loads the job again, which also reads a changed plist.
func (l launchd) restart(ctx context.Context, k Kind) error {
	if err := l.stop(ctx, k); err != nil {
		return err
	}
	return l.bootstrap(ctx, k)
}

func (l launchd) remove(ctx context.Context, k Kind) error {
	if err := l.stop(ctx, k); err != nil {
		return err
	}
	return removeDefinition(l.dir, l.location(k))
}

func (l launchd) state(ctx context.Context, k Kind) (State, error) {
	out, err := l.run(ctx, "launchctl", "print", l.target(k))
	if err != nil {
		return State{Active: "not loaded"}, nil
	}
	st := State{Loaded: true, Active: "loaded"}
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "state = "); ok {
			st.Active = value
			break
		}
	}
	st.Running = st.Active == "running"
	return st, nil
}
