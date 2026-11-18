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

func TestVerifyDispatchRebaseUsesClosedFinalTargetAfterAdvance(t *testing.T) {
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
	write("feature.txt", "value = 1\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base content")
	base := git("rev-parse", "HEAD")
	git("checkout", "-b", "group")
	write("feature.txt", "value = 1.5\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "registered")
	registered := git("rev-parse", "HEAD")
	write("feature.txt", "value = 2\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "closed final after advance")
	final := git("rev-parse", "HEAD")
	git("checkout", "develop")
	write("other.txt", "unrelated\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "parallel")
	rebasedBase := git("rev-parse", "HEAD")
	git("checkout", "group")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "rebase", "develop")
	source := git("rev-parse", "HEAD")
	git("checkout", "develop")
	git("merge", "--ff-only", "group")
	full := board.DispatchIntegration{CWD: cwd, ReviewBase: base, ReviewTarget: final, SourceCommit: source, TargetCommit: source, TargetRef: "refs/heads/develop", RebasedBase: rebasedBase}
	if err := verifyDispatchIntegration(context.Background(), full); err != nil {
		t.Fatal(err)
	}
	staleRange := full
	staleRange.ReviewTarget = registered
	if err := verifyDispatchIntegration(context.Background(), staleRange); err == nil {
		t.Fatal("rebase against registered-only range accepted")
	}
}
