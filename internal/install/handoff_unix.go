//go:build unix

package install

import "golang.org/x/sys/unix"

func defaultHandoff(dest string, argv, env []string) error {
	return unix.Exec(dest, argv, env)
}
