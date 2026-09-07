package board

import (
	"testing"

	"github.com/dualface/kander/internal/fs"
	"golang.org/x/sys/windows"
)

func holdJournalTemporary(t *testing.T, root string) func() {
	t.Helper()
	path := control(root, "operations", ".publication.tmp")
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Match the DELETE access retained by the atomic publisher's temporary
	// handle. An uncoordinated ListDirectory leaf open rejects this sharing.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
		if _, err := fs.RemoveRegularFileIfExists(root, path); err != nil {
			t.Error(err)
		}
	}
}
