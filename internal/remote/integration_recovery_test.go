package remote_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

func integrationDropAction(t *testing.T, action string, enabled *atomic.Bool) func(http.ResponseWriter, *http.Request, http.Handler) bool {
	return func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		if !enabled.Load() || r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, api.IntegrationID("refs/heads/v2")) {
			return false
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		var command api.IntegrationCommand
		if json.Unmarshal(data, &command) != nil || command.Action != action {
			return false
		}
		response := httptest.NewRecorder()
		next.ServeHTTP(response, r)
		if response.Code != http.StatusOK {
			t.Errorf("action %s before dropped response: %d %s", action, response.Code, response.Body.String())
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	}
}
func TestIntegrationRecoversLostReleaseResponseWithoutStrandingTarget(t *testing.T) {
	f := newIntegrationFixture(t)
	var enabled atomic.Bool
	enabled.Store(true)
	f.intercept = integrationDropAction(t, "pushing", &enabled)
	options := f.options("lost-release-response")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("push intent response was not hidden")
	}
	credentials, err := remote.Credentials(f.a.Home)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, c := range credentials {
		if c.ResourceKind == "integration" {
			if err := remote.RemoveCredential(f.a.Home, c); err != nil {
				t.Fatal(err)
			}
			removed = true
		}
	}
	if !removed {
		t.Fatal("no integration credential before recovery")
	}
	f.intercept = integrationDropAction(t, "release", &enabled)
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("release response was not hidden")
	}
	stored, err := f.b.Project.Integration(t.Context(), api.IntegrationID(options.TargetRef))
	if err != nil || stored.Status != api.ClaimReleased || stored.Phase != "uncertain" {
		t.Fatalf("uncertain release: %+v %v", stored, err)
	}
	enabled.Store(false)
	reopened := *f.a
	result, err := reopened.Integrate(t.Context(), f.first, options)
	if err != nil || result.Phase != "integrated" || result.Generation != stored.Generation+1 {
		t.Fatalf("release receipt recovery: %+v %v", result, err)
	}
}
func TestIntegrationInspectsRemoteWhenSuccessfulPushReturnsFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX Git wrapper fixture")
	}
	f := newIntegrationFixture(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrappers := filepath.Join(f.root, "bin")
	if err := os.Mkdir(wrappers, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
integration_push=0
integration_target=0
for integration_arg do
 case "$integration_arg" in push) integration_push=1;; *:refs/heads/v2) integration_target=1;; esac
done
"$KANDER_TEST_REAL_GIT" "$@"
integration_status=$?
if [ "$integration_push" = 1 ] && [ "$integration_target" = 1 ] && [ "$integration_status" = 0 ]; then
 printf 'pushed\n' >> "$KANDER_TEST_PUSH_COUNT"
 exit 1
fi
exit "$integration_status"
`
	if err := os.WriteFile(filepath.Join(wrappers, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	count := filepath.Join(f.root, "push-count")
	t.Setenv("KANDER_TEST_REAL_GIT", git)
	t.Setenv("KANDER_TEST_PUSH_COUNT", count)
	t.Setenv("PATH", wrappers+string(os.PathListSeparator)+os.Getenv("PATH"))
	options := f.options("push-returned-failure")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil || result.Phase != "integrated" {
		t.Fatalf("actual remote success was not reconciled: %+v %v", result, err)
	}
	if _, err := f.a.Integrate(t.Context(), f.first, options); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(count)
	if err != nil || string(data) != "pushed\n" {
		t.Fatalf("unexpected physical push count: %q %v", data, err)
	}
}
