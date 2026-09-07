package fs

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedLockChild(t *testing.T) {
	root := os.Getenv("KANDER_TEST_LOCK_ROOT")
	if root == "" {
		return
	}
	f, err := OpenLockFile(root, filepath.Join(root, "access.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Println("ready")
	var lock *ExclusiveLock
	if os.Getenv("KANDER_TEST_LOCK_SHARED") == "1" {
		lock, err = LockShared(f)
	} else {
		lock, err = LockExclusive(f)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	fmt.Println("locked")
}
func TestSharedLocksExcludeWriterAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "access.lock")
	first, err := OpenLockFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenLockFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	one, err := LockShared(first)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Unlock()
	two, err := LockShared(second)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Unlock()
	reader := exec.Command(os.Args[0], "-test.run=^TestSharedLockChild$")
	reader.Env = append(os.Environ(), "KANDER_TEST_LOCK_ROOT="+root, "KANDER_TEST_LOCK_SHARED=1")
	if out, err := reader.CombinedOutput(); err != nil {
		t.Fatalf("concurrent reader failed: %v %s", err, out)
	}
	writer := exec.Command(os.Args[0], "-test.run=^TestSharedLockChild$")
	writer.Env = append(os.Environ(), "KANDER_TEST_LOCK_ROOT="+root)
	stdout, err := writer.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	writer.Stderr = os.Stderr
	if err = writer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if writer.ProcessState == nil {
			_ = writer.Process.Kill()
			_ = writer.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatal("writer not ready")
	}
	entered := make(chan bool, 1)
	go func() { entered <- scanner.Scan() && scanner.Text() == "locked" }()
	select {
	case <-entered:
		t.Fatal("writer entered while readers hold locks")
	case <-time.After(100 * time.Millisecond):
	}
	if err = one.Unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
		t.Fatal("writer entered while second reader holds lock")
	case <-time.After(100 * time.Millisecond):
	}
	if err = two.Unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-entered:
		if !ok {
			t.Fatal("writer did not acquire lock")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("writer lock not released")
	}
	if err = writer.Wait(); err != nil {
		t.Fatal(err)
	}
}
