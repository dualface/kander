// Package builtin registers the built-in terminal backends: the Go backends
// (foreground, console) and the definitions embedded under
// definitions/. Import it for its side effect wherever a backend is looked up
// by launcher name.
package builtin

import (
	"embed"
	"path"

	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/direct"
)

// Launcher names and tool constants of the embedded definitions, for the
// product UI (install hints and launcher choices).
const (
	// Herdr is the launcher provided by the embedded herdr definition.
	Herdr = "herdr"
	// HerdrExecutable is the herdr command resolved on PATH.
	HerdrExecutable = "herdr"
	// Tmux is the launcher that opens a window in the current tmux session.
	Tmux = "tmux"
	// TmuxSession is the launcher that opens a window in a per-project session.
	TmuxSession = "tmux-session"
	// TmuxExecutable is the tmux command resolved on PATH.
	TmuxExecutable = "tmux"
	// TmuxSessionHint is the command that enters a tmux session.
	TmuxSessionHint = "tmux new -A -s kander"
)

//go:embed definitions/*.json
var definitions embed.FS

func init() {
	registerHooks()
	terminal.Register(direct.New(direct.Foreground))
	terminal.Register(direct.New(direct.Console))
	entries, err := definitions.ReadDir("definitions")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		name := path.Join("definitions", entry.Name())
		data, err := definitions.ReadFile(name)
		if err != nil {
			panic(err)
		}
		terminal.RegisterDefinition(name, data)
	}
}

// Definition decodes an embedded definition by name, for tests and tools that
// build a backend with their own environment.
func Definition(name string) (*terminal.Definition, error) {
	source := path.Join("definitions", name+".json")
	data, err := definitions.ReadFile(source)
	if err != nil {
		return nil, err
	}
	return terminal.DecodeDefinition(source, data)
}

// DefinitionBackend builds one launcher of an embedded definition with the
// given environment, for callers that inject their own getenv.
func DefinitionBackend(name, launcher string, getenv func(string) string) (terminal.Backend, error) {
	def, err := Definition(name)
	if err != nil {
		return nil, err
	}
	return terminal.NewDeclarativeBackend(def, launcher, getenv)
}
