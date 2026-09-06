//go:build unix

package install

import (
	"os"

	"golang.org/x/sys/unix"
)

func handoff(dest, lang string) error {
	argv := []string{dest, "--lang", lang}
	return unix.Exec(dest, argv, os.Environ())
}
