//go:build unix

package menu

import "os"

func needsAdmin() bool {
	return os.Geteuid() != 0
}
