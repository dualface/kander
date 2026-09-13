package terminalcheck

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/terminal/builtin"
)

func TestEmbeddedHerdrConformance(t *testing.T) {
	if os.Getenv("KANDER_E2E_HERDR") != "1" {
		t.Skip("herdr: set KANDER_E2E_HERDR=1 explicitly to create a test tab")
	}
	socket := os.Getenv("HERDR_SOCKET_PATH")
	info, err := os.Stat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Skip("herdr: HERDR_SOCKET_PATH is not an available socket")
	}
	backend, err := builtin.DefinitionBackend("herdr", "herdr", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Check(backend, Options{}, &output)
	t.Log("embedded herdr definition:\n" + output.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "pass PaneFacts.gone") {
		t.Fatal("missing gone check")
	}
}
