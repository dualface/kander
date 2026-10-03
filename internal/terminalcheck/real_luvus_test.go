//go:build unix

package terminalcheck

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/terminal/builtin"
)

// TestEmbeddedLuvusConformance runs the embedded luvus definition against the
// luvus server of the calling pane. It is opt-in because it opens a real tab.
func TestEmbeddedLuvusConformance(t *testing.T) {
	if os.Getenv("KANDER_E2E_LUVUS") != "1" {
		t.Skip("luvus: set KANDER_E2E_LUVUS=1 explicitly to create a test tab")
	}
	if os.Getenv("LUVUS_ENV") != "1" || strings.TrimSpace(os.Getenv("LUVUS_PANE_ID")) == "" {
		t.Skip("luvus: run inside a luvus pane (LUVUS_ENV=1 and LUVUS_PANE_ID)")
	}
	backend, err := builtin.DefinitionBackend("luvus", builtin.Luvus, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Check(backend, Options{}, &output)
	t.Log("embedded luvus definition:\n" + output.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "pass PaneFacts.gone") {
		t.Fatal("missing gone check")
	}
}
