package board

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/dualface/kander/internal/config"
)

func parseExecutionAuthorization(values map[string]string) (ExecutionAuthorization, error) {
	a := ExecutionAuthorization{DispatchID: values["--dispatch-id"]}
	if values["--execution-epoch"] != "" {
		v, err := strconv.ParseUint(values["--execution-epoch"], 10, 64)
		if err != nil {
			return a, err
		}
		a.Epoch = v
	}
	if (a.DispatchID == "") != (a.Epoch == 0) || a.DispatchID != "" && !validDispatchID(a.DispatchID) {
		return a, dispatchError(a.DispatchID)
	}
	return a, nil
}

// RunDispatch exposes producer-owned intents without permitting ordinary updates.
func RunDispatch(args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println(t("board.dispatch_usage"))
		return 0
	}
	if len(args) < 1 {
		return fail(kanbanError("board.dispatch_usage"))
	}
	if _, err := config.Load(false); err != nil {
		return fail(err)
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	var d Dispatch
	switch args[0] {
	case "prepare":
		if len(args) != 2 {
			return fail(kanbanError("board.dispatch_usage"))
		}
		b, e := os.ReadFile(args[1])
		if e != nil {
			return fail(e)
		}
		var in DispatchInput
		if e = DecodeReviewJSON(b, &in); e != nil {
			return fail(e)
		}
		if in.Kind == "wrap-up" {
			return fail(dispatchEvidenceError("Git-aware dispatch entrance required"))
		}
		d, err = PrepareDispatch(root, in)
	case "show":
		if len(args) != 3 {
			return fail(kanbanError("board.dispatch_usage"))
		}
		d, err = ReadDispatch(root, args[1], args[2])
	case "fail", "cancel":
		if len(args) != 5 {
			return fail(kanbanError("board.dispatch_usage"))
		}
		v, e := strconv.ParseUint(args[3], 10, 64)
		if e != nil {
			return fail(e)
		}
		state := DispatchFailed
		if args[0] == "cancel" {
			state = DispatchCancelled
		}
		err = EndDispatch(root, args[1], args[2], v, state, args[4])
		if err == nil {
			d, err = ReadDispatch(root, args[1], args[2])
		}
	default:
		return fail(kanbanError("board.dispatch_usage"))
	}
	if err != nil {
		return fail(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(d); err != nil {
		return fail(err)
	}
	return 0
}
