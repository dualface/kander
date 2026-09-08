//go:build linux

package tui

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"golang.org/x/sys/unix"
)

func TestPrepareTaskStartOpensOnlySelectedCard(t *testing.T) {
	root := t.TempDir()
	t.Setenv(board.EnvBoardDir, root)
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	selected, err := board.NewTask(root, "feature", "preview-selected", "Selected", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := board.NewTask(root, "feature", "preview-other", "Other", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete, cfg.Launcher = true, "herdr"
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, path)
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	selectedWatch, err := unix.InotifyAddWatch(fd, filepath.Join(selected, "spec.md"), unix.IN_OPEN)
	if err != nil {
		t.Fatal(err)
	}
	otherWatch, err := unix.InotifyAddWatch(fd, filepath.Join(other, "spec.md"), unix.IN_OPEN)
	if err != nil {
		t.Fatal(err)
	}
	request, err := prepareTaskStart(filepath.Base(selected))
	if err != nil || request.TaskID != filepath.Base(selected) {
		t.Fatalf("preview=%+v err=%v", request, err)
	}
	buffer := make([]byte, 4096)
	n, err := unix.Read(fd, buffer)
	if err != nil {
		t.Fatal(err)
	}
	openedSelected := false
	for offset := 0; offset+unix.SizeofInotifyEvent <= n; {
		watch := int(binary.NativeEndian.Uint32(buffer[offset:]))
		if watch == otherWatch {
			t.Fatal("preview opened unrelated card body")
		}
		openedSelected = openedSelected || watch == selectedWatch
		offset += unix.SizeofInotifyEvent + int(binary.NativeEndian.Uint32(buffer[offset+12:]))
	}
	if !openedSelected {
		t.Fatal("missing selected-card open evidence")
	}
}
