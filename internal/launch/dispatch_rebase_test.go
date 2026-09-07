package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestDispatchIntegrationVerifiesRebasedPatchWithoutIgnoringWhitespace(t *testing.T) {
	cwd, _ := integrationGit(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, data, err)
		}
		return strings.TrimSpace(string(data))
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(message string) string {
		t.Helper()
		git("add", ".")
		git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", message)
		return git("rev-parse", "HEAD")
	}
	write("feature.txt", "value = 1\n")
	base := commit("base")
	git("checkout", "-b", "group")
	write("feature.txt", "value = 2\n")
	reviewed := commit("reviewed change")
	git("checkout", "develop")
	write("other.txt", "unrelated delivery\n")
	rebasedBase := commit("parallel delivery")
	git("checkout", "group")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "rebase", "develop")
	source := git("rev-parse", "HEAD")
	git("checkout", "develop")
	git("merge", "--ff-only", "group")
	evidence := board.DispatchIntegration{CWD: cwd, ReviewBase: base, ReviewTarget: reviewed, SourceCommit: source, TargetCommit: source, TargetRef: "refs/heads/develop"}
	if err := verifyDispatchIntegration(context.Background(), evidence); err == nil {
		t.Fatal("rewritten SHA passed unqualified ancestry")
	}
	evidence.RebasedBase = rebasedBase
	if err := verifyDispatchIntegration(context.Background(), evidence); err != nil {
		t.Fatal("identical reviewed patch rejected", err)
	}
	write("feature.txt", "value = 2 \n")
	changed := commit("unreviewed whitespace")
	evidence.SourceCommit, evidence.TargetCommit = changed, changed
	if err := verifyDispatchIntegration(context.Background(), evidence); err == nil {
		t.Fatal("meaningful whitespace difference ignored")
	}
}
