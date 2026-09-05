package cli

import "github.com/dualface/kander/internal/board"

func init() {
	Commands["init"] = board.RunInit
	Commands["list"] = board.RunList
	Commands["show"] = board.RunShow
	Commands["new"] = board.RunNew
	Commands["move"] = board.RunMove
	Commands["pick"] = board.RunPick
	Commands["check"] = board.RunCheck
	Commands["guard-write"] = board.RunGuardWrite
}
