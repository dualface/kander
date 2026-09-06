package install

import (
	"os"
	"strings"
)

// EnvPostInstall marks a process started by a successful install handoff.
// The launched binary must unset it before spawning any further children.
const EnvPostInstall = "KANDER_POST_INSTALL"

// handoff launches or replaces the process with the installed binary.
// Tests may override it; the env slice already includes EnvPostInstall.
var handoff = defaultHandoff

func environWithPostInstall(base []string) []string {
	prefix := EnvPostInstall + "="
	out := make([]string, 0, len(base)+1)
	for _, e := range base {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return append(out, prefix+"1")
}

func launchInstalled(dest, lang string) error {
	return handoff(dest, []string{dest, "--lang", lang}, environWithPostInstall(os.Environ()))
}
