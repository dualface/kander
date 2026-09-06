package cli

import "github.com/dualface/kander/internal/board"

func init() {
	Commands["init"] = board.RunInit
	Commands["list"] = board.RunList
	Commands["show"] = board.RunShow
	Commands["new"] = board.RunNew
	Commands["move"] = board.RunMove
	Commands["pick"] = board.RunPick
	// check is registered only by internal/liveness (blank-imported from cmd/kander).
	// board.RunCheck remains the structural-only helper used by board package tests.
	Commands["guard-write"] = board.RunGuardWrite
}
