package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/probe"
)

// MatchAgentSession decides whether the session identity a pane reports
// (kind plus value) names the same session as the recorded card reference.
// The verdict is tri-state: only a resolved identity may prove or disprove a
// match, and every resolution failure returns SessionUncertain with a
// diagnosable detail, never a fabricated mismatch.
//
//   - kind "" or "id" compares the value verbatim (missing kind is a legacy
//     direct id); an empty value is an incomplete identity and stays
//     undecidable.
//   - kind "path" resolves the session file through the agent definition's
//     session.file declaration; agents without one cannot prove a path
//     identity.
//   - any other kind is an unsupported representation and stays undecidable.
func MatchAgentSession(ctx context.Context, agent, kind, value, reference string) (SessionMatch, string) {
	if value == "" {
		return SessionUncertain, config.Text("terminal.session_identity_missing")
	}
	switch kind {
	case "", "id":
		if value == reference {
			return SessionMatches, ""
		}
		return SessionDiffers, ""
	case "path":
		resolved, detail, ok := resolveSessionFile(ctx, agent, value)
		if !ok {
			return SessionUncertain, detail
		}
		if resolved == reference {
			return SessionMatches, ""
		}
		return SessionDiffers, ""
	default:
		return SessionUncertain, config.Text("terminal.session_identity_kind_unknown", kind, agent)
	}
}

// resolveSessionFile reads the real session id out of a path-kind reported
// identity. The path is untrusted terminal-reported data: it is opened only
// through the internal/fs safe boundary, read within the declared byte bound,
// and parsed as data, never executed.
func resolveSessionFile(ctx context.Context, agent, path string) (resolved, detail string, ok bool) {
	if err := ctx.Err(); err != nil {
		return "", probe.FailureDetail(err), false
	}
	definition, err := config.LoadAgent(agent)
	if err != nil {
		return "", config.Text("terminal.session_file_agent_error", err.Error()), false
	}
	var spec *config.SessionFile
	if definition.Session != nil {
		spec = definition.Session.File
	}
	if spec == nil || spec.Format != "jsonl_header" {
		return "", config.Text("terminal.session_file_format_unknown", agent), false
	}
	if !filepath.IsAbs(path) {
		return "", config.Text("terminal.session_file_unreadable", path, config.Text("terminal.session_file_not_absolute")), false
	}
	anchor, err := sessionFileAnchor(path)
	if err != nil {
		return "", config.Text("terminal.session_file_unreadable", path, err.Error()), false
	}
	if err := ctx.Err(); err != nil {
		return "", probe.FailureDetail(err), false
	}
	file, err := fs.OpenRegularFileIfExists(anchor, path)
	if err != nil {
		return "", config.Text("terminal.session_file_unreadable", path, err.Error()), false
	}
	if file == nil {
		return "", config.Text("terminal.session_file_unreadable", path, config.Text("terminal.session_file_missing")), false
	}
	defer file.Close()
	maxBytes := spec.MaxBytes
	if maxBytes <= 0 {
		maxBytes = config.DefaultSessionFileMaxBytes
	}
	header, detail, ok := readSessionHeaderLine(file, maxBytes)
	if !ok {
		return "", detail, false
	}
	if err := ctx.Err(); err != nil {
		return "", probe.FailureDetail(err), false
	}
	var record map[string]any
	if err := json.Unmarshal(header, &record); err != nil {
		return "", config.Text("terminal.session_file_header_invalid", path), false
	}
	if spec.TypeField != "" {
		if typeValue, _ := record[spec.TypeField].(string); typeValue != spec.TypeValue {
			return "", config.Text("terminal.session_file_header_type", path), false
		}
	}
	id, _ := record[spec.IDField].(string)
	if id == "" {
		return "", config.Text("terminal.session_file_id_missing", path), false
	}
	return id, "", true
}

// readSessionHeaderLine returns the first line of a session file bounded by
// maxBytes; a longer header or a read failure is a decidable rejection.
func readSessionHeaderLine(file *os.File, maxBytes int) (line []byte, detail string, ok bool) {
	buf := make([]byte, maxBytes+1)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, config.Text("terminal.session_file_unreadable", file.Name(), err.Error()), false
	}
	data := buf[:n]
	if index := bytes.IndexByte(data, '\n'); index >= 0 {
		return data[:index], "", true
	}
	if n > maxBytes {
		return nil, config.Text("terminal.session_file_header_too_long", maxBytes), false
	}
	// A file that is one header line without a trailing newline is complete.
	return data, "", true
}

// sessionFileAnchor returns the volume anchor the fs safe boundary requires:
// the filesystem root on POSIX, the volume root on Windows.
func sessionFileAnchor(abs string) (string, error) {
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(abs)
		if volume == "" {
			return "", &probe.Error{Message: config.Text("terminal.session_file_not_absolute")}
		}
		return volume + `\`, nil
	}
	return string(os.PathSeparator), nil
}
