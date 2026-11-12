//go:build unix && !linux && !darwin

package menu

import (
	"errors"
	"os"
	"testing"
)

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	t.Skip("PTY harness supports Linux and macOS")
	return nil, nil
}
func ptsName(*os.File) (string, error) {
	return "", errors.New("PTY name is unsupported on this platform")
}
