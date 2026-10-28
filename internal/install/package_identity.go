package install

import (
	"errors"

	"github.com/dualface/kander/internal/config"
)

// packaged reports MSIX package identity; tests replace it to exercise the
// Microsoft Store paths on any platform.
var packaged = detectPackaged

// Packaged reports whether this process runs with MSIX package identity, that
// is, it was installed from Microsoft Store. The package directory is
// read-only and versioned and the Store owns updates, so a packaged kander
// never replaces its own binary or copies itself onto PATH.
func Packaged() bool { return packaged() }

func errStoreManaged() error {
	return errors.New(config.Text("install.update_store_managed"))
}
