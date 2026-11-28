package svcinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
)

// markerPrefix starts the Kander marker every definition carries: a comment of the unit or the
// plist, or the description of the scheduled task. A definition without it is not Kander's and
// is never changed or deleted.
const markerPrefix = "kander-service: "

// markerSchema is the revision of the marker and of the definition template. A template change
// raises it, so an install rewrites a definition an earlier release wrote.
const markerSchema = 1

type marker struct {
	Schema int `json:"schema"`
	Spec
}

// encodeMarker is the marker line without a line end. The JSON is plain ASCII without a hyphen,
// so it survives an XML comment (which cannot hold "--") and the code page of schtasks output.
func encodeMarker(s Spec) (string, error) {
	data, err := json.Marshal(marker{Schema: markerSchema, Spec: s})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString(markerPrefix)
	for _, r := range string(data) {
		switch {
		case r == '-':
			out.WriteString("\\u002d")
		case r < 0x80:
			out.WriteRune(r)
		default:
			for _, unit := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&out, `\u%04x`, unit)
			}
		}
	}
	return out.String(), nil
}

// decodeMarker finds the marker in a definition. ok is false when there is none or it does not
// describe a known schema.
func decodeMarker(data []byte) (marker, bool) {
	at := bytes.Index(data, []byte(markerPrefix))
	if at < 0 {
		return marker{}, false
	}
	var m marker
	if json.NewDecoder(bytes.NewReader(data[at+len(markerPrefix):])).Decode(&m) != nil || m.Schema < 1 {
		return marker{}, false
	}
	if _, ok := ParseKind(string(m.Kind)); !ok {
		return marker{}, false
	}
	return m, true
}

func sameMarker(a, b marker) bool {
	return a.Schema == b.Schema && a.Kind == b.Kind && a.Executable == b.Executable && slices.Equal(a.Args, b.Args) &&
		a.ClientHome == b.ClientHome && a.WorkDir == b.WorkDir && a.LogPath == b.LogPath
}

// environment is the variables a definition sets, in a fixed order.
func environment(s Spec) [][2]string {
	env := [][2]string{{process.EnvManaged, "1"}}
	if s.ClientHome != "" {
		env = append(env, [2]string{config.EnvClientHome, s.ClientHome})
	}
	return env
}

// validate refuses a spec a definition cannot carry safely. forbidden lists the characters the
// manager's own quoting cannot express.
func validate(s Spec, forbidden string) error {
	if _, ok := ParseKind(string(s.Kind)); !ok {
		return fmt.Errorf("unknown service %q", s.Kind)
	}
	values := append([]string{s.Executable, s.ClientHome, s.WorkDir, s.LogPath}, s.Args...)
	for _, value := range values {
		if strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) || strings.ContainsAny(value, forbidden) {
			return errors.New(config.Text("service.invalid_value", value))
		}
	}
	for _, path := range []string{s.Executable, s.WorkDir, s.LogPath} {
		if !isAbs(path) {
			return errors.New(config.Text("service.invalid_value", path))
		}
	}
	if s.ClientHome != "" && !isAbs(s.ClientHome) {
		return errors.New(config.Text("service.invalid_value", s.ClientHome))
	}
	return nil
}

// isAbs accepts an absolute path of either platform, so the definitions of every manager render
// in tests on any host.
func isAbs(path string) bool {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return true
	}
	return len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

// currentUserName is the account the services run as.
func currentUserName() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.Username
}
