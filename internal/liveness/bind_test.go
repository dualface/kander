package liveness

import (
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
)

func TestCheckBindsLivenessNotBoard(t *testing.T) {
	got := cli.Commands["check"]
	if got == nil {
		t.Fatal("check is not registered")
	}
	gotPtr := reflect.ValueOf(got).Pointer()
	livePtr := reflect.ValueOf(RunCheck).Pointer()
	boardPtr := reflect.ValueOf(board.RunCheck).Pointer()
	if gotPtr == boardPtr {
		t.Fatal("check bound to board.RunCheck; expected sole liveness.RunCheck binding")
	}
	if gotPtr != livePtr {
		t.Fatal("check must bind liveness.RunCheck when this package is linked")
	}
}
