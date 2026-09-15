package cli

import "github.com/dualface/kander/internal/board"

func init() {
	Commands["init"] = board.RunInit
	Commands["list"] = board.RunList
	Commands["show"] = board.RunShow
	Commands["update"] = board.RunUpdate
	Commands["dispatch"] = board.RunDispatch
	Commands["new"] = board.RunNew
	Commands["move"] = board.RunMove
	Commands["pick"] = board.RunPick
	// check is registered by internal/check (blank-imported from cmd/kander).
	// The no-mode form delegates to liveness.RunCheck. board.RunCheck remains
	// the structural-only helper used by board package tests.
	Commands["guard-write"] = board.RunGuardWrite
}
