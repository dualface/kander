package menu

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// serverPolicy is a set policy in the shape the panel receives it: main-only,
// result_on_done ask, and keys this version does not know.
func serverPolicy(revision uint64) config.PolicySnapshot {
	values := strings.Replace(cachedPolicyValues, `"task_intake":true}`, `"task_intake":true,"future_module":true}`, 1)
	values = strings.Replace(values, `{"git":`, `{"future":{"knob":"x"},"git":`, 1)
	return config.PolicySnapshot{Epoch: "e1", Revision: revision, Values: json.RawMessage(values)}
}

func requireFileValues(t *testing.T, want config.PolicyValues) {
	t.Helper()
	saved, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.PolicyValuesOf(saved); !jsonEqual(t, got, want) {
		t.Fatalf("config.json holds the policy values %+v, want the file values %+v", got, want)
	}
}

func TestOptionsShowTheServerPolicyAndSaveHostValuesOnly(t *testing.T) {
	session, _ := tempOverlaySession(t)
	fileValues := config.PolicyValuesOf(session.Config)
	session.AttachPolicy(serverPolicy(3))
	if session.Config.Git.BranchMode != "main-only" || session.Config.Issue.ResultOnDone != "ask" || session.Config.AgentLanguage != "zh-CN" {
		t.Fatalf("the panel shows %+v, not the server policy", config.PolicyValuesOf(session.Config))
	}
	if label := session.PolicyLabel("git", "branch_mode"); !strings.Contains(label, "3") {
		t.Fatalf("policy label %q does not name revision 3", label)
	}
	if label := session.PolicyLabel("launcher"); label != "" {
		t.Fatalf("a host value carries the policy label %q", label)
	}
	if err := session.SetIssueResultOnDone("off"); err != nil {
		t.Fatal(err)
	}
	if !session.PolicyDirty() || session.ScopeDirty || !session.HasUnsaved() {
		t.Fatalf("a policy edit: policy dirty %v, scope dirty %v", session.PolicyDirty(), session.ScopeDirty)
	}
	session.SetExtendedLaunchTimeouts(true)
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	requireFileValues(t, fileValues)
	saved, _ := config.LoadScope(true)
	if !saved.ExtendedLaunchTimeouts {
		t.Fatal("the host value of the same save was not written")
	}
	if session.Config.Issue.ResultOnDone != "off" || !session.PolicyDirty() {
		t.Fatalf("after the file save the panel shows %q, dirty %v", session.Config.Issue.ResultOnDone, session.PolicyDirty())
	}
	expected, values, err := session.PolicyWrite()
	if err != nil || expected != 3 {
		t.Fatalf("policy write expects revision %d: %v", expected, err)
	}
	for _, want := range []string{`"result_on_done":"off"`, `"branch_mode":"main-only"`, `"future_module":true`, `"future":{"knob":"x"}`} {
		if !strings.Contains(string(values), want) {
			t.Fatalf("policy write %s lacks %s", values, want)
		}
	}
	session.AcceptPolicyWrite(config.PolicySnapshot{Epoch: "e1", Revision: 4, Values: values})
	if session.PolicyDirty() || session.PolicyRevision() != 4 || session.HasUnsaved() {
		t.Fatalf("after the write: dirty %v, revision %d", session.PolicyDirty(), session.PolicyRevision())
	}
}

func TestOptionsProjectTabKeepsPolicyEditsOutOfTheOverlay(t *testing.T) {
	session, overlay := tempOverlaySession(t)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.AttachPolicy(serverPolicy(2))
	if err := session.SetGitBranchMode("main-develop"); err != nil {
		t.Fatal(err)
	}
	if session.OverlayDirty {
		t.Fatal("a policy edit marked the overlay dirty")
	}
	// A host edit rebuilds the merged buffer; the policy stays on display.
	session.SetExtendedLaunchTimeouts(true)
	if session.Config.Git.BranchMode != "main-develop" || session.Config.Issue.ResultOnDone != "ask" {
		t.Fatalf("after a host edit the panel shows %+v", config.PolicyValuesOf(session.Config))
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range config.PolicyKeys {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("the overlay %s holds the policy key %s", data, key)
		}
	}
	// The Global tab shows the same server values.
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	if session.Config.Git.BranchMode != "main-develop" || session.Config.AgentLanguage != "zh-CN" {
		t.Fatalf("the Global tab shows %+v", config.PolicyValuesOf(session.Config))
	}
}

func TestOptionsKeepPolicyEditsWhenTheServerChanges(t *testing.T) {
	session, _ := tempOverlaySession(t)
	session.AttachPolicy(serverPolicy(3))
	// Without edits a newer policy replaces the shown one and the expected revision.
	if session.AttachPolicy(serverPolicy(4)) || session.PolicyRevision() != 4 {
		t.Fatalf("a refresh without edits left revision %d", session.PolicyRevision())
	}
	if err := session.SetGitBranchMode("main-develop"); err != nil {
		t.Fatal(err)
	}
	if !session.AttachPolicy(serverPolicy(5)) {
		t.Fatal("a newer policy during an edit was not reported")
	}
	if session.PolicyRevision() != 4 || session.PolicyChangedOnServer() != 5 || session.Config.Git.BranchMode != "main-develop" {
		t.Fatalf("the edit moved: revision %d, newer %d, branch %s", session.PolicyRevision(), session.PolicyChangedOnServer(), session.Config.Git.BranchMode)
	}
	// After a refused write the panel drops the edit and shows the server values.
	session.ResetPolicy(serverPolicy(5))
	if session.PolicyDirty() || session.PolicyRevision() != 5 || session.Config.Git.BranchMode != "main-only" || session.PolicyChangedOnServer() != 0 {
		t.Fatalf("after the reset: dirty %v, revision %d, branch %s", session.PolicyDirty(), session.PolicyRevision(), session.Config.Git.BranchMode)
	}
}

func TestOptionsWithoutAPolicyKeepTheFileBehavior(t *testing.T) {
	session, _ := tempOverlaySession(t)
	session.AttachPolicy(config.PolicySnapshot{})
	if session.PolicyBacked() {
		t.Fatal("an unset policy backs the panel")
	}
	if label := session.PolicyLabel("rules", config.RuleReview); !strings.Contains(label, "kander policy init") {
		t.Fatalf("unset policy label %q", label)
	}
	if err := session.SetGitBranchMode("main-only"); err != nil {
		t.Fatal(err)
	}
	if !session.ScopeDirty || session.PolicyDirty() {
		t.Fatal("an edit without a policy did not go to the file")
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	if saved, _ := config.LoadScope(true); saved.Git.BranchMode != "main-only" {
		t.Fatalf("config.json holds %q", saved.Git.BranchMode)
	}
	// A session of no server project shows no policy label at all.
	plain, _ := tempOverlaySession(t)
	if label := plain.PolicyLabel("git", "branch_mode"); label != "" {
		t.Fatalf("an unbound session labels %q", label)
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(left) == string(right)
}
