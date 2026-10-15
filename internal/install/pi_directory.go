package install

import (
	"os"
	"path/filepath"
	"strings"
)

// globalAgentTarget resolves an embedded home-relative target. Pi's environment
// override replaces its config root for both rules and executable extensions.
func globalAgentTarget(agent, relative string) string {
	if agent == "pi" {
		if directory := os.Getenv("PI_CODING_AGENT_DIR"); directory != "" {
			if directory == "~" || strings.HasPrefix(directory, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					return ""
				}
				directory = home + directory[1:]
			}
			absolute, err := filepath.Abs(directory)
			if err != nil {
				return ""
			}
			return filepath.Join(absolute, filepath.FromSlash(strings.TrimPrefix(relative, ".pi/agent/")))
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(relative))
}
