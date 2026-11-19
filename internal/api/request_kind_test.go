package api

import (
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestRequestKindFollowsTheDispatch(t *testing.T) {
	for dispatch, want := range map[string]string{"fix": RequestFix, "sync": RequestSync, "wrap-up": RequestWrapUp} {
		if got := RequestKindFor(&board.DispatchInput{Kind: dispatch}); got != want {
			t.Errorf("dispatch %s: request kind %s, want %s", dispatch, got, want)
		}
	}
	if got := RequestKindFor(nil); got != RequestNote {
		t.Errorf("no dispatch: request kind %s, want %s", got, RequestNote)
	}
}
