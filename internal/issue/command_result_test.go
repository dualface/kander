package issue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type resultCommandProvider struct {
	*stubResolver
	*resultFake
}

func TestResultCommandRoundTripAndLaunchIsolation(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	provider := &resultCommandProvider{stubResolver: &stubResolver{repository: repo}, resultFake: fake}
	factory := func() IssueProvider { return provider }
	base := []string{"result", "42", "--card", id, "--repo", "github.com/dualface/kander"}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := runWith(factory, append(append([]string{}, base...), args...), &out, &stderr)
		if code != 0 {
			return code, stderr.String()
		}
		return code, out.String()
	}
	code, text := run("--action", "inspect")
	if code != 0 {
		t.Fatal(text)
	}
	var inspection ResultInspection
	if err := json.Unmarshal([]byte(text), &inspection); err != nil {
		t.Fatal(err)
	}
	proposal := fixtureProposal(inspection)
	data, _ := json.Marshal(proposal)
	path := filepath.Join(t.TempDir(), "proposal.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if code, text := run("--action=apply", "--file", path); code != 0 {
		t.Fatal(text)
	}
	if fake.posts != 1 {
		t.Fatal("controlled apply did not publish")
	}
	old := triageStarter
	t.Cleanup(func() { triageStarter = old })
	paths := []string{}
	triageStarter = func(request TriageLaunch) (TriageOutcome, error) {
		if !request.ResultSync || request.CardID != id || request.Root != root {
			t.Fatal("wrong result launch")
		}
		paths = append(paths, request.JSONPath)
		if _, err := os.Stat(request.JSONPath); err != nil {
			t.Fatal(err)
		}
		return TriageOutcome{}, errors.New("simulated terminal failure")
	}
	before, _ := os.ReadFile(filepath.Join(root, "done", id, "spec.md"))
	for i := 0; i < 2; i++ {
		if code, _ := run(); code != 1 {
			t.Fatal("failed launcher reported success")
		}
	}
	if len(paths) != 2 || paths[0] == paths[1] {
		t.Fatal("sessions overwrite evidence")
	}
	after, _ := os.ReadFile(filepath.Join(root, "done", id, "spec.md"))
	if !bytes.Equal(before, after) {
		t.Fatal("result session changed card")
	}
	if _, err := StartResult(context.Background(), fake, root, repo, 42, TriageOptions{CardID: "wrong"}); err == nil {
		t.Fatal("unbound result session accepted")
	}
}
