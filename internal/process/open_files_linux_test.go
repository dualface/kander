//go:build linux

package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestObserveOpenFilesBindsProcessAndDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "open.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	before, err := ObserveOpenFiles(t.Context(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, observed := range before.Files {
		if observed.Path == path && os.SameFile(info, observed.Info) {
			found = true
		}
	}
	if !found || before.Identity == "" {
		t.Fatalf("observation=%+v", before)
	}
	after, err := ObserveOpenFiles(t.Context(), os.Getpid())
	if err != nil || before.Identity != after.Identity {
		t.Fatalf("process changed: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ObserveOpenFiles(ctx, os.Getpid()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := ObserveOpenFiles(t.Context(), -1); err == nil {
		t.Fatal("invalid PID accepted")
	}
}
