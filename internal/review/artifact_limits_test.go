package review

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func TestEvidenceOversizedPromptSuggestsShorterContext(t *testing.T) {
	root := t.TempDir()
	setupLang(t, filepath.Join(root, "config.json"))
	size := artifactRequestLimit / 4 * 3
	if err := os.WriteFile(filepath.Join(root, "prompt.txt"), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	// An accidental upload would try to use the uninitialized session.
	archive := archiveExecution{store: &remoteReviewStore{}}
	err := archive.snapshotRuntime(root, "")
	var gate *gateError
	if !errors.As(err, &gate) || gate.code != 2 {
		t.Fatalf("prompt limit did not fail as a gate error: %v", err)
	}
	message := gate.Error()
	for _, want := range []string{"prompt.txt", "Shorten supplemental review context", "new run ID"} {
		if !strings.Contains(message, want) {
			t.Errorf("prompt limit message does not include %q: %s", want, message)
		}
	}
	if strings.Contains(message, ".gitattributes") {
		t.Errorf("prompt limit message gives unrelated evidence guidance: %s", message)
	}
	// review-contract.md is the other non-evidence artifact. Its advice also
	// permits shortening custom instructions rather than changing Git config.
	contract := config.Text("review.artifact_request_too_large", config.ReviewContractFilename, size, artifactRequestLimit+1, artifactRequestLimit, "")
	if !strings.Contains(contract, "custom review instructions") || strings.Contains(contract, ".gitattributes") {
		t.Errorf("wrong contract limit guidance: %s", contract)
	}
}
