package main

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/remote"
)

// The usage error of an unknown request kind lists every kind the command accepts, in every
// language, and none it does not: the text is a catalog string, so it is compared with the list.
func TestRequestKindErrorListsEveryAcceptedKind(t *testing.T) {
	unboundCheckout(t)
	for _, language := range []string{"en", "cn", "ja"} {
		result := runCLI(t, "--lang", language, "request", sampleTask, "bogus", "text")
		if result.code != 2 {
			t.Fatalf("%s: exit=%d stderr=%s", language, result.code, result.stderr)
		}
		message, _, _ := strings.Cut(result.stderr, "\n")
		for _, kind := range remote.RequestKinds() {
			if !strings.Contains(message, kind) {
				t.Errorf("%s: the error does not list %q: %s", language, kind, message)
			}
		}
		if strings.Contains(message, "wrap-up") {
			t.Errorf("%s: the error lists wrap-up, which request does not accept: %s", language, message)
		}
	}
}
