package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
)

const (
	dismissGroup       = "20261118-dismiss-group"
	dismissMember      = "20261118-dismiss-member-task"
	dismissMemberExec  = "member-exec"
	dismissCoordinator = "coordinator-exec"
	dismissToken       = "private-group-token"
)

type dismissFixture struct {
	session *Session
	task    api.Task
}

// bindProcess sets both execution variables explicitly so the test never
// inherits the bindings of the agent that runs it; an empty value is unset.
func bindProcess(t *testing.T, execution, coordinator string) {
	t.Helper()
	for name, value := range map[string]string{EnvExecutionID: execution, EnvCoordinatorExecutionID: coordinator} {
		t.Setenv(name, value)
		if value == "" {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// useLanguage selects the language config.Text renders in: en, cn or ja.
func useLanguage(t *testing.T, lang string) {
	t.Helper()
	t.Setenv(config.EnvLang, lang)
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
}

// wantRefusal renders the whole refusal the way LocalExecutionRefused must,
// from the catalog of lang alone.
func wantRefusal(lang, condition string, args ...any) string {
	return i18n.Text(lang, "remote.local_execution_refused", i18n.Text(lang, "remote.local_execution_required"), i18n.Text(lang, "remote.local_execution."+condition, args...))
}

func newDismissFixture(t *testing.T) dismissFixture {
	t.Helper()
	useLanguage(t, "en")
	connection, err := client.New("http://example.invalid", "host-a")
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{Client: connection, ServerURL: "http://example.invalid", Info: api.Project{ID: "dismiss-project", Name: "dismiss"}, Home: t.TempDir()}
	task := api.Task{TaskID: dismissMember, State: "done", Text: "# Member\n\n- TASK_GROUP: " + dismissGroup + "\n- OWNER: claude\n- SESSION: claude session-1\n- WINDOW: herdr:w1:t8:w1:p8\n", Claim: &api.ClaimView{ClientID: "host-a", ExecutionID: dismissMemberExec, Generation: 3, Status: api.ClaimReleased}}
	member := Credential{ServerURL: session.ServerURL, ClientID: "host-a", ProjectID: session.Info.ID, TaskID: dismissMember, ExecutionID: dismissMemberExec, Generation: 3, Token: "member-token", Session: "claude session-1", Window: "herdr:w1:t8:w1:p8"}
	if err := session.SaveCredential(member); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCredential(session.Home, member); err != nil {
		t.Fatal(err)
	}
	return dismissFixture{session: session, task: task}
}

func (f dismissFixture) groupCredential() Credential {
	return Credential{ResourceKind: "group", ServerURL: f.session.ServerURL, ClientID: "host-a", ProjectID: f.session.Info.ID, TaskID: dismissGroup, ExecutionID: dismissCoordinator, Generation: 1, Token: dismissToken}
}

func (f dismissFixture) saveGroup(t *testing.T, change func(*Credential)) Credential {
	t.Helper()
	c := f.groupCredential()
	if change != nil {
		change(&c)
	}
	if err := f.session.SaveCredential(c); err != nil {
		t.Fatal(err)
	}
	return c
}

func (f dismissFixture) groupPath() string {
	return filepath.Join(credentialDir(f.session.Home, f.session.ServerURL, f.session.Info.ID, dismissGroup), dismissCoordinator+".json")
}

func requireRefusal(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("the resolution was accepted")
	}
	text := err.Error()
	if !strings.HasPrefix(text, config.Text("remote.local_execution_required")) {
		t.Fatalf("refusal does not start with the common sentence: %s", text)
	}
	for _, fragment := range want {
		if !strings.Contains(text, fragment) {
			t.Fatalf("refusal lacks %q: %s", fragment, text)
		}
	}
	if strings.Contains(text, dismissToken) || strings.Contains(text, "member-token") {
		t.Fatalf("refusal leaks a token: %s", text)
	}
}

func TestRetainedForDismissAcceptsTheCoordinatorOfTheMembersGroup(t *testing.T) {
	f := newDismissFixture(t)
	f.saveGroup(t, nil)
	bindProcess(t, dismissCoordinator, dismissCoordinator)
	got, err := f.session.RetainedForDismiss(f.task)
	if err != nil || got.ExecutionID != dismissMemberExec || got.Generation != 3 || got.Window != "herdr:w1:t8:w1:p8" || got.Session != "claude session-1" {
		t.Fatalf("coordinator was refused: %+v %v", got, err)
	}
	if _, err := f.session.Retained(f.task); err == nil {
		t.Fatal("Retained accepted a coordinator process")
	}
	if f.session.SceneIsLocal(f.task.TaskID, f.task.Claim, got.Window, got.Session) {
		t.Fatal("a coordinator process sees the member scene as local")
	}
}

func TestRetainedForDismissRefusesBindingsThatAreNotTheMembersCoordinator(t *testing.T) {
	f := newDismissFixture(t)
	f.saveGroup(t, nil)
	empty := config.Text("remote.local_execution.value_empty")
	cases := []struct {
		name                   string
		execution, coordinator string
		want                   []string
	}{
		{"task executor", "predecessor-exec", "", []string{"predecessor-exec", dismissMemberExec, empty}},
		{"differing variables", dismissCoordinator, "other-coordinator", []string{dismissCoordinator, dismissMemberExec, "other-coordinator"}},
		{"claim execution with another coordinator", dismissMemberExec, "other-coordinator", []string{dismissMemberExec, "other-coordinator"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bindProcess(t, tc.execution, tc.coordinator)
			_, err := f.session.RetainedForDismiss(f.task)
			requireRefusal(t, err, tc.want...)
		})
	}
	t.Run("coordinator variable alone", func(t *testing.T) {
		// Without KANDER_EXECUTION_ID the process is unbound and keeps the
		// unchanged lookup of the retained record.
		bindProcess(t, "", dismissCoordinator)
		if _, err := f.session.RetainedForDismiss(f.task); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Retained", func(t *testing.T) {
		bindProcess(t, dismissCoordinator, dismissCoordinator)
		_, err := f.session.Retained(f.task)
		requireRefusal(t, err, dismissCoordinator, dismissMemberExec)
	})
}

func TestRetainedForDismissRefusesAMemberWithoutAGroup(t *testing.T) {
	f := newDismissFixture(t)
	f.saveGroup(t, nil)
	f.task.Text = strings.Replace(f.task.Text, "- TASK_GROUP: "+dismissGroup, "- TASK_GROUP:", 1)
	bindProcess(t, dismissCoordinator, dismissCoordinator)
	_, err := f.session.RetainedForDismiss(f.task)
	requireRefusal(t, err, dismissCoordinator, dismissMember)
}

func TestRetainedForDismissRefusesAnInvalidGroupCredential(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, f dismissFixture)
		reason  string
		absent  string
	}{
		{"missing", func(*testing.T, dismissFixture) {}, "missing or was released", ""},
		{"only under another execution", func(t *testing.T, f dismissFixture) {
			f.saveGroup(t, func(c *Credential) { c.ExecutionID = "other-coordinator" })
		}, "missing or was released", ""},
		{"another server", func(t *testing.T, f dismissFixture) {
			f.saveGroup(t, func(c *Credential) { c.ServerURL = "http://other.invalid" })
		}, "missing or was released", ""},
		{"another project", func(t *testing.T, f dismissFixture) {
			f.saveGroup(t, func(c *Credential) { c.ProjectID = "other-project" })
		}, "missing or was released", ""},
		{"released", func(t *testing.T, f dismissFixture) {
			RemoveCredentialOrFail(t, f, f.saveGroup(t, nil))
		}, "missing or was released", ""},
		{"tombstone beside a valid credential", func(t *testing.T, f dismissFixture) {
			f.saveGroup(t, nil)
			if err := os.WriteFile(strings.TrimSuffix(f.groupPath(), ".json")+".released", []byte("released\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "missing or was released", ""},
		{"unreadable", func(t *testing.T, f dismissFixture) {
			if err := os.MkdirAll(f.groupPath(), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "could not be read", "is a directory"},
		{"lost", func(t *testing.T, f dismissFixture) {
			f.saveGroup(t, func(c *Credential) { c.Lost, c.LostReason = true, "taken_over" })
		}, "lost: taken_over", ""},
		{"not a group credential", func(t *testing.T, f dismissFixture) {
			f.saveGroup(t, func(c *Credential) { c.ResourceKind = "" })
		}, "not a group credential", ""},
		{"execution differs from path", func(t *testing.T, f dismissFixture) {
			writeGroupFile(t, f, `{"resource_kind":"group","server_url":"http://example.invalid","client_id":"host-a","project_id":"dismiss-project","task_id":"`+dismissGroup+`","execution_id":"someone-else","generation":1,"token":"`+dismissToken+`"}`)
		}, "does not match its private location", "identity does not match"},
		{"client differs", func(t *testing.T, f dismissFixture) {
			writeGroupFile(t, f, `{"resource_kind":"group","server_url":"http://example.invalid","client_id":"host-b","project_id":"dismiss-project","task_id":"`+dismissGroup+`","execution_id":"`+dismissCoordinator+`","generation":1,"token":"`+dismissToken+`"}`)
		}, "does not match its private location", "identity does not match"},
		{"damaged", func(t *testing.T, f dismissFixture) {
			writeGroupFile(t, f, "not json "+dismissToken)
		}, "damaged", "execution credential of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDismissFixture(t)
			tc.arrange(t, f)
			bindProcess(t, dismissCoordinator, dismissCoordinator)
			_, err := f.session.RetainedForDismiss(f.task)
			requireRefusal(t, err, tc.reason, dismissCoordinator)
			if tc.absent != "" && strings.Contains(err.Error(), tc.absent) {
				t.Fatalf("raw error leaked: %v", err)
			}
			if tc.name == "unreadable" {
				// Nothing but the common sentence, the identities and the
				// localized reason: no path and no file system error.
				if want := wantRefusal("en", RefusalCoordinatorHolder, dismissGroup, dismissCoordinator, i18n.Text("en", "remote.local_execution.reason_unreadable")); err.Error() != want {
					t.Fatalf("refusal %q, want %q", err, want)
				}
			}
		})
	}
}

func RemoveCredentialOrFail(t *testing.T, f dismissFixture, c Credential) {
	t.Helper()
	if err := RemoveCredential(f.session.Home, c); err != nil {
		t.Fatal(err)
	}
}

func writeGroupFile(t *testing.T, f dismissFixture, content string) {
	t.Helper()
	path := f.groupPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRetainedRefusesClaimsThatAreNotThisHosts(t *testing.T) {
	f := newDismissFixture(t)
	bindProcess(t, "", "")
	noClaim := f.task
	noClaim.Claim = nil
	_, err := f.session.Retained(noClaim)
	requireRefusal(t, err, dismissMember)
	for _, lang := range []string{"en", "cn", "ja"} {
		useLanguage(t, lang)
		badTask := f.task
		badTask.TaskID = "bad task"
		_, err = f.session.Retained(badTask)
		if want := wantRefusal(lang, RefusalInvalidIdentity, i18n.Text(lang, "remote.local_execution.label_task_id"), `"bad task"`); err == nil || err.Error() != want {
			t.Fatalf("%s: refusal %v, want %q", lang, err, want)
		}
		badExecution := f.task
		badExecution.Claim = &api.ClaimView{ClientID: "host-a", ExecutionID: "bad execution", Generation: 3}
		_, err = f.session.Retained(badExecution)
		if want := wantRefusal(lang, RefusalInvalidIdentity, i18n.Text(lang, "remote.local_execution.label_claim_execution_id"), `"bad execution"`); err == nil || err.Error() != want {
			t.Fatalf("%s: refusal %v, want %q", lang, err, want)
		}
	}
	useLanguage(t, "en")
	foreign := f.task
	foreign.Claim = &api.ClaimView{ClientID: "host-z", ExecutionID: dismissMemberExec, Generation: 3}
	_, err = f.session.Retained(foreign)
	requireRefusal(t, err, "host-z", "host-a")
	mismatch := f.task
	mismatch.Claim = &api.ClaimView{ClientID: "host-a", ExecutionID: dismissMemberExec, Generation: 9}
	_, err = f.session.RetainedForDismiss(mismatch)
	requireRefusal(t, err, ".json", dismissMemberExec, "generation 9")
}

// Every refusal condition has a catalog entry in all three languages that
// renders each value it is given, so no condition prints a bare key.
func TestRefusalConditionsRenderTheirValuesInEveryLanguage(t *testing.T) {
	conditions := map[string]int{
		RefusalNoClaim: 1, RefusalInvalidIdentity: 2, RefusalForeignClient: 2, RefusalBoundExecution: 3,
		RefusalCoordinatorNoGroup: 2, RefusalCoordinatorHolder: 3, RefusalRetainedMismatch: 3,
		RefusalSceneMismatch: 4, RefusalSelfClaimed: 2, RefusalRecordMismatch: 1, RefusalSessionInvalid: 1,
		RefusalTaskChanged: 2, RefusalPaneStale: 2,
	}
	if len(conditions) != 13 {
		t.Fatalf("expected 13 conditions, have %d", len(conditions))
	}
	reasons := map[string]int{reasonMissing: 0, reasonNotGroup: 0, reasonLost: 1, reasonMismatch: 0, reasonDamaged: 0, reasonUnreadable: 0}
	fragments := map[string]int{"label_task_id": 0, "label_claim_execution_id": 0, "record_owner": 2, "record_session": 2, "change_revision": 2, "change_state": 2, "change_claim_removed": 0, "change_claim_execution": 2, "change_claim_generation": 2}
	for _, lang := range []string{"en", "cn", "ja"} {
		check := func(key string, count int) {
			t.Helper()
			args := make([]any, count)
			for i := range args {
				args[i] = "sentinel-" + string(rune('a'+i))
			}
			text := i18n.Text(lang, key, args...)
			if text == key {
				t.Fatalf("%s has no %s entry", lang, key)
			}
			for _, arg := range args {
				if !strings.Contains(text, arg.(string)) {
					t.Fatalf("%s %s drops %v: %s", lang, key, arg, text)
				}
			}
		}
		for condition, count := range conditions {
			check("remote.local_execution."+condition, count)
		}
		for reason, count := range reasons {
			check("remote.local_execution.reason_"+reason, count)
		}
		for fragment, count := range fragments {
			check("remote.local_execution."+fragment, count)
		}
		check("remote.local_execution.value_empty", 0)
		check("remote.local_execution_refused", 2)
		check("remote.execution_target_gone", 2)
	}
}
