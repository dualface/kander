package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

type policyFixture struct {
	t       *testing.T
	data    string
	hooks   Hooks
	srv     *Server
	http    *httptest.Server
	client  *client.Client
	project *client.Project
	info    api.Project
}

func newPolicyFixture(t *testing.T, hooks Hooks) *policyFixture {
	t.Helper()
	f := &policyFixture{t: t, data: t.TempDir(), hooks: hooks}
	f.open()
	t.Cleanup(f.close)
	return f
}

func (f *policyFixture) open() {
	f.t.Helper()
	srv, err := Open(Options{DataDir: f.data, Hooks: f.hooks, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		f.t.Fatal(err)
	}
	f.srv = srv
	f.http = httptest.NewUnstartedServer(srv.Handler())
	f.http.Config.ErrorLog = log.New(io.Discard, "", 0)
	f.http.Config.SetKeepAlivesEnabled(false)
	f.http.Start()
	c, err := client.New(f.http.URL, "policy-client")
	if err != nil {
		f.t.Fatal(err)
	}
	c.Retries = 0
	f.client = c
	f.info, err = c.Register(f.t.Context(), "golf", "")
	if err != nil {
		f.t.Fatal(err)
	}
	f.project = c.Project(f.info.ID)
}

func (f *policyFixture) close() {
	if f.http != nil {
		f.http.Close()
		f.http = nil
	}
	if f.srv != nil {
		f.srv.Close()
		f.srv = nil
	}
}

func (f *policyFixture) restart() {
	f.t.Helper()
	f.close()
	f.open()
}

func (f *policyFixture) root() string { return projectRoot(f.data, "golf") }

func (f *policyFixture) policyProject() *project {
	f.t.Helper()
	p, err := f.srv.project(f.info.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *policyFixture) files() (policy, audit []byte) {
	f.t.Helper()
	policy, _ = os.ReadFile(filepath.Join(f.root(), policyFile))
	audit, _ = os.ReadFile(filepath.Join(f.root(), policyAuditFile))
	return policy, audit
}

func (f *policyFixture) auditRecords() []policyAuditRecord {
	f.t.Helper()
	_, audit := f.files()
	var records []policyAuditRecord
	scanner := bufio.NewScanner(bytes.NewReader(audit))
	for scanner.Scan() {
		var r policyAuditRecord
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			f.t.Fatalf("audit line %q: %v", scanner.Text(), err)
		}
		records = append(records, r)
	}
	return records
}

func policyValues(mode string) json.RawMessage {
	return json.RawMessage(`{"git":{"branch_mode":"` + mode + `"},"review_stages":{"large":{"PMQA":"required","Security":"auto"},"small":{"PMQA":"auto","Security":"skip"}},` +
		`"rules":{"code":true,"collaboration":true,"git":true,"reporting":true,"review":true,"task_groups":true,"task_intake":true},"agent_language":"zh-CN","issue":{"result_on_done":"ask"}}`)
}

func (f *policyFixture) put(key string, expected uint64, values json.RawMessage) (api.ProjectPolicy, error) {
	return f.project.PutPolicy(f.t.Context(), key, api.PolicyWrite{ExpectedRevision: expected, Values: values, Submitter: "Dev One"})
}

func requireAPICode(t *testing.T, err error, code string, status int) *api.Error {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) || e.Code != code || e.Status != status {
		t.Fatalf("error %v, want %s (%d)", err, code, status)
	}
	return e
}

func TestPolicyStartsUnsetInEveryProjectAnswer(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	policy, err := f.project.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Revision != 0 || policy.Epoch != "" || policy.Values != nil || !policy.UpdatedAt.IsZero() || policy.UpdatedBy != "" {
		t.Fatalf("unset policy %+v", policy)
	}
	byName, err := f.client.ProjectByName(t.Context(), "golf")
	if err != nil {
		t.Fatal(err)
	}
	byID, err := f.project.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range []api.Project{f.info, byName, byID} {
		if info.Policy == nil || info.Policy.Revision != 0 {
			t.Fatalf("project answer without the unset policy: %+v", info)
		}
	}
	registry, err := os.ReadFile(filepath.Join(f.data, "projects.json"))
	if err != nil || strings.Contains(string(registry), "policy") {
		t.Fatalf("the registry stores the policy: %s %v", registry, err)
	}
}

func TestPolicyWritesCompareAndSwapAndKeepTheEpoch(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	first, err := f.put("k-first", 0, policyValues("main-only"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || len(first.Epoch) != 32 || first.UpdatedBy != "Dev One" || first.UpdatedAt.IsZero() {
		t.Fatalf("first write %+v", first)
	}
	second, err := f.put("k-second", 1, policyValues("main-develop"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 || second.Epoch != first.Epoch {
		t.Fatalf("second write %+v after %+v", second, first)
	}
	got, err := f.project.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.Epoch != first.Epoch || !strings.Contains(string(got.Values), `"main-develop"`) || got.UpdatedBy != "Dev One" {
		t.Fatalf("GET %+v", got)
	}
	info, err := f.client.Register(t.Context(), "golf", "")
	if err != nil || info.Policy == nil || info.Policy.Revision != 2 {
		t.Fatalf("registration answer %+v %v", info.Policy, err)
	}
	records := f.auditRecords()
	if len(records) != 2 {
		t.Fatalf("audit %+v", records)
	}
	r := records[1]
	if r.Epoch != first.Epoch || r.Revision != 2 || r.UpdatedBy != "Dev One" || r.ClientID != "policy-client" || r.IdempotencyKey != "k-second" ||
		!strings.Contains(string(r.OldValues), "main-only") || !strings.Contains(string(r.NewValues), "main-develop") || r.At.IsZero() {
		t.Fatalf("audit record %+v", r)
	}
	if records[0].OldValues != nil {
		t.Fatalf("the first record has old values: %s", records[0].OldValues)
	}

	policyBefore, auditBefore := f.files()
	_, err = f.put("k-stale", 1, policyValues("main-only"))
	requireAPICode(t, err, api.CodePolicyRevisionConflict, 409)
	if policyAfter, auditAfter := f.files(); !bytes.Equal(policyBefore, policyAfter) || !bytes.Equal(auditBefore, auditAfter) {
		t.Fatal("a conflicting write changed the policy or the audit log")
	}
}

func TestPolicyWriteRefusesBadValuesWithoutWriting(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	cases := map[string]string{
		"missing": `{"git":{"branch_mode":"main-only"},"review_stages":{"PMQA":"auto","Security":"auto"},"rules":{},"agent_language":"en"}`,
		"invalid": strings.Replace(string(policyValues("main-only")), `"ask"`, `"later"`, 1),
		"unknown": strings.Replace(string(policyValues("main-only")), `"agent_language"`, `"launcher":"tmux","agent_language"`, 1),
		"absent":  ``,
	}
	for name, values := range cases {
		var raw json.RawMessage
		if values != "" {
			raw = json.RawMessage(values)
		}
		_, err := f.put("k-"+name, 0, raw)
		e := requireAPICode(t, err, api.CodeInvalidRequest, 400)
		if name == "unknown" && !strings.Contains(e.Message, "launcher") {
			t.Errorf("unknown key not named: %s", e.Message)
		}
		if name == "missing" && !strings.Contains(e.Message, "issue") {
			t.Errorf("missing value not named: %s", e.Message)
		}
	}
	if policy, audit := f.files(); policy != nil || audit != nil {
		t.Fatalf("a refused write left files: %s %s", policy, audit)
	}
}

func TestPolicyRoutesPassTheAuthorizeHook(t *testing.T) {
	var seen []string
	f := newPolicyFixture(t, Hooks{Authorize: func(_ context.Context, r Request) error {
		seen = append(seen, r.Pattern)
		if strings.HasSuffix(r.Pattern, "/policy") && r.Method == "PUT" {
			return errors.New("no policy writes here")
		}
		return nil
	}})
	_, err := f.put("k-denied", 0, policyValues("main-only"))
	requireAPICode(t, err, api.CodeForbidden, 403)
	if policy, audit := f.files(); policy != nil || audit != nil {
		t.Fatal("a denied write left files")
	}
	if _, err := f.project.Policy(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(seen, "\n")
	if !strings.Contains(joined, "PUT /api/v1/projects/{project}/policy") || !strings.Contains(joined, "GET /api/v1/projects/{project}/policy") {
		t.Fatalf("authorized patterns %v", seen)
	}
}

func TestPolicyRetryAnswersTheFirstResultAfterACrashBeforeTheReceipt(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	f.policyProject().policyFaultHook = func(stage string) {
		if stage == "policy-replaced" {
			panic("injected crash after the policy was replaced")
		}
	}
	if _, err := f.put("k-crash", 0, policyValues("main-only")); err == nil {
		t.Fatal("the interrupted write answered")
	}
	f.restart()
	if got, _ := f.project.Policy(t.Context()); got.Revision != 1 {
		t.Fatalf("after restart the policy is %+v", got)
	}
	if _, err := f.put("k-later", 1, policyValues("main-develop")); err != nil {
		t.Fatal(err)
	}
	retry, err := f.put("k-crash", 0, policyValues("main-only"))
	if err != nil {
		t.Fatalf("the retry failed: %v", err)
	}
	if retry.Revision != 1 || !strings.Contains(string(retry.Values), "main-only") {
		t.Fatalf("the retry answered %+v, not the first result", retry)
	}
	if records := f.auditRecords(); len(records) != 2 {
		t.Fatalf("audit %+v", records)
	}
	if got, _ := f.project.Policy(t.Context()); got.Revision != 2 {
		t.Fatalf("the retry changed the policy: %+v", got)
	}
}

func TestPolicyCrashBeforeTheReplacementIsVoidedAndRunsAgain(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	first, err := f.put("k-one", 0, policyValues("main-only"))
	if err != nil {
		t.Fatal(err)
	}
	policyBefore, _ := f.files()
	f.policyProject().policyFaultHook = func(stage string) {
		if stage == "audit-appended" {
			panic("injected crash after the audit append")
		}
	}
	if _, err := f.put("k-two", 1, policyValues("main-develop")); err == nil {
		t.Fatal("the interrupted write answered")
	}
	f.restart()
	if policyAfter, _ := f.files(); !bytes.Equal(policyBefore, policyAfter) {
		t.Fatal("the interrupted write changed policy.json")
	}
	records := f.auditRecords()
	if len(records) != 3 || records[2].Void == nil || *records[2].Void != (policyVoid{Epoch: first.Epoch, Revision: 2, IdempotencyKey: "k-two"}) || records[2].Reason != policyVoidReason {
		t.Fatalf("audit %+v", records)
	}
	// Opening the project again voids nothing twice.
	f.restart()
	if again := f.auditRecords(); len(again) != 3 {
		t.Fatalf("a second restart changed the audit log: %+v", again)
	}
	// The voided write never took effect, so its retry runs and takes revision 2.
	retry, err := f.put("k-two", 1, policyValues("main-develop"))
	if err != nil || retry.Revision != 2 {
		t.Fatalf("retry %+v %v", retry, err)
	}
	if records := f.auditRecords(); len(records) != 4 || records[3].IdempotencyKey != "k-two" || records[3].Void != nil {
		t.Fatalf("audit %+v", records)
	}
}

func TestPolicyTornAuditTailIsEndedAtStartup(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	if _, err := f.put("k-one", 0, policyValues("main-only")); err != nil {
		t.Fatal(err)
	}
	f.close()
	path := filepath.Join(f.root(), policyAuditFile)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"epoch":"x","revision":2,"idemp`); err != nil {
		t.Fatal(err)
	}
	file.Close()
	f.open()
	data, _ := os.ReadFile(path)
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatal("the torn tail was not ended")
	}
	if _, err := f.put("k-two", 1, policyValues("main-develop")); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.project.Policy(t.Context()); got.Revision != 2 {
		t.Fatalf("policy %+v", got)
	}
}

func TestPolicySurvivesARestart(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	written, err := f.put("k-one", 0, policyValues("main-only"))
	if err != nil {
		t.Fatal(err)
	}
	f.restart()
	got, err := f.project.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Epoch != written.Epoch || got.Revision != written.Revision || !bytes.Equal(got.Values, written.Values) || got.UpdatedBy != written.UpdatedBy {
		t.Fatalf("after restart %+v, want %+v", got, written)
	}
}

func TestPolicyImportAndMaintainLeaveThePolicyAlone(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	if _, err := f.put("k-one", 0, policyValues("main-only")); err != nil {
		t.Fatal(err)
	}
	policyBefore, auditBefore := f.files()
	source := t.TempDir()
	if err := board.InitStorage(source); err != nil {
		t.Fatal(err)
	}
	if _, err := board.NewTask(source, "chore", "imported", "Imported card", "en", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.Import("golf", "", source); err != nil {
		t.Fatal(err)
	}
	if policyAfter, auditAfter := f.files(); !bytes.Equal(policyBefore, policyAfter) || !bytes.Equal(auditBefore, auditAfter) {
		t.Fatal("an import into an existing project changed its policy")
	}
	if _, err := f.srv.Import("chess", "", source); err != nil {
		t.Fatal(err)
	}
	chess, err := f.srv.project(api.ProjectID("chess"))
	if err != nil {
		t.Fatal(err)
	}
	if got := chess.policyView(); got.Revision != 0 {
		t.Fatalf("an imported new project has a policy: %+v", got)
	}
	f.close()
	if _, err := Maintain(maintenanceOptions(f.data), "golf", true); err != nil {
		t.Fatal(err)
	}
	if policyAfter, auditAfter := f.files(); !bytes.Equal(policyBefore, policyAfter) || !bytes.Equal(auditBefore, auditAfter) {
		t.Fatal("maintain changed the policy files")
	}
}

// A voided write whose revision another write took later is still not applied:
// its retry is judged as a new write, never answered with the other result.
func TestPolicyVoidedWriteIsNotAppliedWhenAnotherWriteTookItsRevision(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	if _, err := f.put("k-one", 0, policyValues("main-only")); err != nil {
		t.Fatal(err)
	}
	f.policyProject().policyFaultHook = func(stage string) {
		if stage == "audit-appended" {
			panic("injected crash after the audit append")
		}
	}
	if _, err := f.put("k-two", 1, policyValues("main-develop")); err == nil {
		t.Fatal("the interrupted write answered")
	}
	f.restart()
	if other, err := f.put("k-three", 1, policyValues("main-develop")); err != nil || other.Revision != 2 {
		t.Fatalf("k-three %+v %v", other, err)
	}
	_, err := f.put("k-two", 1, policyValues("main-develop"))
	requireAPICode(t, err, api.CodePolicyRevisionConflict, 409)
}
