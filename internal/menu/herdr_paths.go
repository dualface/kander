package menu

import (
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/terminal/builtin"
)

// herdrDefaultBinaries lists where the official herdr installer puts the binary,
// most preferred first.
func herdrDefaultBinaries(windows bool) []string {
	if windows {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return nil
		}
		return []string{filepath.Join(local, "Programs", "Herdr", "bin", builtin.HerdrExecutable+".exe")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin", builtin.HerdrExecutable)}
}
