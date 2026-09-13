package install

import (
	"os"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// EnvPostInstall marks a process started by a successful install handoff.
// The launched binary must unset it before spawning any further children.
const EnvPostInstall = "KANDER_POST_INSTALL"

// handoff launches or replaces the process with the installed binary.
// Tests may override it; the env slice already includes EnvPostInstall.
var handoff = defaultHandoff

// handoffEnviron builds the environment of the installed binary. The wizard
// language travels only as a plain KANDER_LANG default: a --lang argument or the
// KANDER_LANG_CLI marker the wizard set on its own environment would rank above
// the config language and hide later Options changes until a restart.
func handoffEnviron(base []string, lang string) []string {
	dropped := []string{EnvPostInstall + "=", config.EnvLangCLI + "=", config.EnvLang + "="}
	out := make([]string, 0, len(base)+2)
	for _, e := range base {
		if hasAnyPrefix(e, dropped) {
			continue
		}
		out = append(out, e)
	}
	return append(out, config.EnvLang+"="+lang, EnvPostInstall+"=1")
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func launchInstalled(dest, lang string) error {
	return handoff(dest, []string{dest}, handoffEnviron(os.Environ(), lang))
}
