package launch

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/process"
)

// kander start takes the order of a worker's start out of its own environment
// before it starts anything, so no agent or terminal server inherits it.
func TestTakeStartOrderRemovesTheVariable(t *testing.T) {
	t.Setenv(process.EnvStartOrder, "so-0123456789abcdef")
	if got := takeStartOrder(); got != "so-0123456789abcdef" {
		t.Fatalf("order = %q", got)
	}
	if value, ok := os.LookupEnv(process.EnvStartOrder); ok {
		t.Fatalf("the order stayed in the environment: %q", value)
	}
	t.Setenv(process.EnvStartOrder, "not an identity")
	if got := takeStartOrder(); got != "" {
		t.Fatalf("a malformed order was taken: %q", got)
	}
	if got := takeStartOrder(); got != "" {
		t.Fatalf("no order = %q", got)
	}
}
