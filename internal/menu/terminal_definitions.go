package menu

import (
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

// definitionLauncher returns the backend of a launcher provided by a loaded
// terminal definition other than the built-in names this package already
// presents with tool-specific wording.
func definitionLauncher(name string) (terminal.Backend, bool) {
	for _, builtin := range config.DefaultLauncherNames() {
		if name == builtin {
			return nil, false
		}
	}
	backend, ok := terminal.Lookup(name)
	if !ok {
		return nil, false
	}
	if _, declarative := backend.(*terminal.DeclarativeBackend); !declarative {
		return nil, false
	}
	return backend, true
}

// definitionLauncherChoices lists the launchers of loaded user definitions
// usable on this platform.
func definitionLauncherChoices() []Choice {
	var choices []Choice
	for _, name := range config.LauncherNames() {
		backend, ok := definitionLauncher(name)
		if !ok || (isWindowsOS() && backend.Capabilities().POSIXOnly) {
			continue
		}
		choices = append(choices, Choice{Value: name, Label: config.Text("terminal.launcher_choice", name)})
	}
	return choices
}

// definitionLauncherAvailable reports whether a definition launcher can be
// kept by doctor repair: its binary is on PATH and its platform matches.
func definitionLauncherAvailable(name string) bool {
	backend, ok := definitionLauncher(name)
	if !ok || (isWindowsOS() && backend.Capabilities().POSIXOnly) {
		return false
	}
	return lookPath(backend.Executable()) != ""
}

// reportTerminalDefinitions prints the load result of every terminal
// definition; an invalid file is unhealthy but leaves the built-in launchers
// untouched.
func reportTerminalDefinitions() bool {
	healthy := true
	for _, report := range terminal.DefinitionInventory() {
		switch {
		case report.Err != nil:
			healthy = false
			warning(config.Text("terminal.definition_invalid", report.Err.Error()))
		case report.Active:
			success(config.Text("terminal.definition_loaded", report.Path, strings.Join(report.Launchers, ", ")))
		default:
			hint(config.Text("terminal.definition_overridden", report.Path, report.Name))
		}
	}
	return healthy
}

// checkDefinitionLauncher warns when the configured launcher comes from a
// definition whose binary is not on PATH.
func checkDefinitionLauncher(name string) bool {
	backend, ok := definitionLauncher(name)
	if !ok {
		return true
	}
	if isWindowsOS() && backend.Capabilities().POSIXOnly {
		warning(config.Text("menu.the_configured_launcher_is_but_native_windows_does_not", name))
		return false
	}
	if lookPath(backend.Executable()) == "" {
		warning(config.Text("terminal.configured_launcher_binary_missing", name, backend.Executable()))
		return false
	}
	return true
}
