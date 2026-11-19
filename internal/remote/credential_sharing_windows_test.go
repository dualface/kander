//go:build windows

package remote

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

const sharingTask = "20261119-credential-sharing-task"

// sharingFixture saves one execution record below a temporary home.
func sharingFixture(t *testing.T) (session *Session, credential Credential, record string) {
	t.Helper()
	t.Setenv(EnvExecutionID, "")
	session = &Session{Home: t.TempDir(), Info: api.Project{ID: "project"}}
	credential = Credential{ProjectID: "project", TaskID: sharingTask, ExecutionID: "exec-a", Generation: 1, Token: "secret", RenewedAt: time.Now()}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	dir := credentialDir(session.Home, session.ServerURL, session.Info.ID, sharingTask)
	return session, credential, filepath.Join(dir, "exec-a.json")
}

// holdLikeAtomicWriter opens path the way the temporary handle of an atomic
// replacement stays open after its rename: DELETE access, no DELETE sharing.
func holdLikeAtomicWriter(t *testing.T, path string) (release func()) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE|windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release = func() { once.Do(func() { windows.CloseHandle(handle) }) }
	t.Cleanup(release)
	return release
}

func releaseAfter(d time.Duration, release func()) {
	go func() {
		time.Sleep(d)
		release()
	}()
}

// The first test proves the fixture reproduces the conflict. It prints the
// error text only on failure, so a passing run leaves no sharing text in the log.
func TestWindowsCredentialSharingFixtureBlocksReaders(t *testing.T) {
	session, _, record := sharingFixture(t)
	holdLikeAtomicWriter(t, record)
	if _, err := fs.ReadRegularFile(session.Home, record); !fs.IsSharingViolation(err) {
		t.Fatalf("a held record must give a sharing violation, got %v", err)
	}
}

func TestWindowsCredentialSharingCredentialsWaitsForTheHolder(t *testing.T) {
	session, credential, record := sharingFixture(t)
	releaseAfter(100*time.Millisecond, holdLikeAtomicWriter(t, record))
	got, err := Credentials(session.Home)
	if err != nil || len(got) != 1 || got[0].ExecutionID != credential.ExecutionID {
		t.Fatalf("got %d credentials, err %v", len(got), err)
	}
}

func TestWindowsCredentialSharingCredentialsReportsBusyAfterTheBound(t *testing.T) {
	session, _, record := sharingFixture(t)
	holdLikeAtomicWriter(t, record)
	start := time.Now()
	_, err := Credentials(session.Home)
	elapsed := time.Since(start)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || !fs.IsBusyFile(err) || !strings.HasPrefix(err.Error(), config.Text("remote.credential_busy")) {
		t.Fatalf("want the busy error around the sharing violation, got a different error (%T)", err)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("the bounded retry took %v", elapsed)
	}
}

func TestWindowsCredentialSharingSessionCredentialWaitsForTheHolder(t *testing.T) {
	session, credential, record := sharingFixture(t)
	releaseAfter(100*time.Millisecond, holdLikeAtomicWriter(t, record))
	got, found, err := session.Credential(sharingTask)
	if err != nil || !found || got.ExecutionID != credential.ExecutionID {
		t.Fatalf("got %+v found=%v err=%v", got, found, err)
	}
}

func TestWindowsCredentialSharingSessionCredentialWaitsForThePointers(t *testing.T) {
	for _, pointer := range []string{"latest-generation", "latest"} {
		t.Run(pointer, func(t *testing.T) {
			session, credential, record := sharingFixture(t)
			if pointer == "latest" {
				// Only a pre-generation install has no head, so the legacy pointer is the one read.
				if _, err := fs.RemoveNonDirectoryIfExists(session.Home, filepath.Join(filepath.Dir(record), "latest-generation")); err != nil {
					t.Fatal(err)
				}
			}
			releaseAfter(100*time.Millisecond, holdLikeAtomicWriter(t, filepath.Join(filepath.Dir(record), pointer)))
			got, found, err := session.Credential(sharingTask)
			if err != nil || !found || got.ExecutionID != credential.ExecutionID {
				t.Fatalf("got %+v found=%v err=%v", got, found, err)
			}
		})
	}
}

func TestWindowsCredentialSharingReadersSurviveRealRenewals(t *testing.T) {
	session, credential, _ := sharingFixture(t)
	const rounds = 200
	var (
		wg        sync.WaitGroup
		busy      int
		busyErrno syscall.Errno
		stop      = make(chan struct{})
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 1; i <= rounds; i++ {
			next := credential
			next.RenewedAt = credential.RenewedAt.Add(time.Duration(i) * time.Second)
			err := WriteCredential(session.Home, next)
			switch {
			case err == nil:
			case fs.IsBusyFile(err):
				// A reader handle can block the rename; only the count and errno are
				// logged, because the error text may carry the sharing message.
				busy++
				errors.As(err, &busyErrno)
			default:
				t.Errorf("renewal %d: %v", i, err)
				return
			}
		}
	}()
	reads := 0
	for done := false; !done || reads < rounds; reads++ {
		select {
		case <-stop:
			done = true
		default:
		}
		got, err := Credentials(session.Home)
		if err != nil || len(got) != 1 {
			t.Fatalf("read %d: %d credentials, err %v", reads, len(got), err)
		}
	}
	wg.Wait()
	t.Logf("writer busy: %d times, errno %d", busy, busyErrno)
}
