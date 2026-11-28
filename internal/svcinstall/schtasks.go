package svcinstall

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"os"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/dualface/kander/v2/internal/config"
)

// schtasks manages scheduled tasks of the user who installs them. A task runs at the logon of
// that user, so a service on Windows runs only after the user logged in.
type schtasks struct {
	run  Runner
	user string
	// temp holds the task XML while schtasks reads it.
	temp string
}

func (schtasks) name() string { return "schtasks" }

func (schtasks) task(k Kind) string { return "kander-" + string(k) }

func (s schtasks) location(k Kind) string { return `\` + s.task(k) }

func (s schtasks) schtasks(ctx context.Context, args ...string) ([]byte, error) {
	out, err := s.run(ctx, "schtasks", args...)
	if err != nil {
		return out, commandError("schtasks", args, out, err)
	}
	return out, nil
}

func (s schtasks) available(ctx context.Context) error {
	if _, err := s.schtasks(ctx, "/Query", "/FO", "CSV", "/NH"); err != nil {
		return errors.New(config.Text("service.schtasks_unavailable", err))
	}
	return nil
}

// cmdQuote quotes one word of the cmd.exe line. validate already refused " and %, which cmd.exe
// cannot carry inside quotes; a trailing backslash would escape the closing quote of a program
// that parses its command line the C way.
func cmdQuote(value string) string { return `"` + value + `"` }

// commandLine is the cmd.exe command the task runs: it sets the variables, runs kander, and
// appends its output to the log, like the detached start does.
func commandLine(spec Spec) string {
	var parts []string
	for _, kv := range environment(spec) {
		parts = append(parts, `set "`+kv[0]+"="+kv[1]+`"`)
	}
	words := []string{cmdQuote(spec.Executable)}
	for _, arg := range spec.Args {
		words = append(words, cmdQuote(arg))
	}
	parts = append(parts, strings.Join(words, " ")+" >> "+cmdQuote(spec.LogPath)+" 2>&1")
	return strings.Join(parts, " && ")
}

func (s schtasks) render(spec Spec) ([]byte, error) {
	if err := validate(spec, `"%`); err != nil {
		return nil, err
	}
	for _, value := range append([]string{spec.Executable, spec.ClientHome, spec.WorkDir, spec.LogPath}, spec.Args...) {
		if strings.HasSuffix(value, `\`) {
			return nil, errors.New(config.Text("service.invalid_value", value))
		}
	}
	mark, err := encodeMarker(spec)
	if err != nil {
		return nil, err
	}
	user := xmlText(s.user)
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-16\"?>\n")
	b.WriteString("<Task version=\"1.2\" xmlns=\"http://schemas.microsoft.com/windows/2004/02/mit/task\">\n")
	b.WriteString("  <RegistrationInfo>\n")
	b.WriteString("    <Description>Kander " + string(spec.Kind) + ", managed by Kander: written by kander service install " + string(spec.Kind) + "; do not edit. " + xmlText(mark) + "</Description>\n")
	b.WriteString("    <URI>" + xmlText(s.location(spec.Kind)) + "</URI>\n")
	b.WriteString("  </RegistrationInfo>\n")
	b.WriteString("  <Triggers>\n    <LogonTrigger>\n      <Enabled>true</Enabled>\n      <UserId>" + user + "</UserId>\n    </LogonTrigger>\n  </Triggers>\n")
	b.WriteString("  <Principals>\n    <Principal id=\"Author\">\n      <UserId>" + user + "</UserId>\n")
	b.WriteString("      <LogonType>InteractiveToken</LogonType>\n      <RunLevel>LeastPrivilege</RunLevel>\n    </Principal>\n  </Principals>\n")
	b.WriteString("  <Settings>\n")
	b.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\n")
	b.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\n")
	b.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\n")
	b.WriteString("    <StartWhenAvailable>true</StartWhenAvailable>\n")
	b.WriteString("    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>\n")
	b.WriteString("    <IdleSettings>\n      <StopOnIdleEnd>false</StopOnIdleEnd>\n      <RestartOnIdle>false</RestartOnIdle>\n    </IdleSettings>\n")
	b.WriteString("    <AllowStartOnDemand>true</AllowStartOnDemand>\n")
	b.WriteString("    <Enabled>true</Enabled>\n")
	b.WriteString("    <Hidden>true</Hidden>\n")
	b.WriteString("    <RunOnlyIfIdle>false</RunOnlyIfIdle>\n")
	b.WriteString("    <WakeToRun>false</WakeToRun>\n")
	// PT0S: no execution time limit; the default of three days would end the service.
	b.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\n")
	b.WriteString("    <Priority>7</Priority>\n")
	b.WriteString("    <RestartOnFailure>\n      <Interval>PT1M</Interval>\n      <Count>999</Count>\n    </RestartOnFailure>\n")
	b.WriteString("  </Settings>\n")
	b.WriteString("  <Actions Context=\"Author\">\n    <Exec>\n")
	// conhost --headless runs cmd.exe without a console window on the desktop of the user.
	b.WriteString("      <Command>%SystemRoot%\\System32\\conhost.exe</Command>\n")
	b.WriteString("      <Arguments>" + xmlText("--headless cmd.exe /d /v:off /s /c \""+commandLine(spec)+"\"") + "</Arguments>\n")
	b.WriteString("      <WorkingDirectory>" + xmlText(spec.WorkDir) + "</WorkingDirectory>\n")
	b.WriteString("    </Exec>\n  </Actions>\n</Task>\n")
	return []byte(b.String()), nil
}

// decodeTaskOutput turns the output of schtasks /XML into text: the XML is UTF-16 when it
// carries a byte order mark or NUL bytes, and the marker itself is plain ASCII either way.
func decodeTaskOutput(out []byte) string {
	if bytes.HasPrefix(out, []byte{0xff, 0xfe}) || (len(out) > 1 && bytes.IndexByte(out, 0) >= 0) {
		out = bytes.TrimPrefix(out, []byte{0xff, 0xfe})
		units := make([]uint16, len(out)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(out[2*i:])
		}
		return string(utf16.Decode(units))
	}
	return string(out)
}

var xmlDeclaration = regexp.MustCompile(`^\s*<\?xml[^>]*\?>`)

// taskDescription returns the description of a task XML, with its entities resolved.
func taskDescription(text string) (string, bool) {
	var task struct {
		Description string `xml:"RegistrationInfo>Description"`
	}
	if xml.Unmarshal([]byte(xmlDeclaration.ReplaceAllString(text, "")), &task) != nil {
		return "", false
	}
	return task.Description, true
}

// read returns the description of the task, which holds the marker. A failed query means no
// task: schtasks answers a missing task and a failed query with the same exit status.
func (s schtasks) read(ctx context.Context, k Kind) ([]byte, bool, error) {
	out, err := s.run(ctx, "schtasks", "/Query", "/TN", s.task(k), "/XML")
	if err != nil {
		return nil, false, nil
	}
	description, ok := taskDescription(decodeTaskOutput(out))
	if !ok {
		return nil, true, nil
	}
	return []byte(description), true, nil
}

// same compares the markers: Task Scheduler stores its own form of the XML.
func (schtasks) same(installed, rendered []byte) bool {
	a, ok := decodeMarker(installed)
	if !ok {
		return false
	}
	b, ok := decodeMarker(rendered)
	return ok && sameMarker(a, b)
}

// encodeUTF16 is the task XML as schtasks /Create /XML reads it: UTF-16LE with a byte order mark.
func encodeUTF16(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2+2*len(units))
	out[0], out[1] = 0xff, 0xfe
	for i, unit := range units {
		binary.LittleEndian.PutUint16(out[2+2*i:], unit)
	}
	return out
}

func (s schtasks) write(ctx context.Context, k Kind, data []byte) error {
	file, err := os.CreateTemp(s.temp, "kander-task-*.xml")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	_, err = file.Write(encodeUTF16(string(data)))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	_, err = s.schtasks(ctx, "/Create", "/TN", s.task(k), "/XML", path, "/F")
	return err
}

func (s schtasks) start(ctx context.Context, k Kind) error {
	_, err := s.schtasks(ctx, "/Run", "/TN", s.task(k))
	return err
}

// stop and restart are left to the Kander protocol: /End would terminate the process tree of
// the task, and a worker that handed over runs outside the task anyway.
func (schtasks) stop(context.Context, Kind) error    { return ErrProtocolStop }
func (schtasks) restart(context.Context, Kind) error { return ErrProtocolStop }

// remove deletes the task; the caller stops the process through the Kander protocol.
func (s schtasks) remove(ctx context.Context, k Kind) error {
	_, err := s.schtasks(ctx, "/Delete", "/TN", s.task(k), "/F")
	return err
}

func (s schtasks) state(ctx context.Context, k Kind) (State, error) {
	if _, err := s.run(ctx, "schtasks", "/Query", "/TN", s.task(k)); err != nil {
		return State{Active: "not registered"}, nil
	}
	return State{Loaded: true, Active: "registered"}, nil
}
