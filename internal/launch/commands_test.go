package launch

import (
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/remote"
)

// The tests drive each command the way the binary does, with the arguments after its name.

func RunStartRemote(args []string) int    { return commandtest.Run(NewStartCommand(), args...) }
func RunResumeRemote(args []string) int   { return commandtest.Run(NewResumeCommand(), args...) }
func RunDispatchRemote(args []string) int { return commandtest.Run(NewDispatchCommand(), args...) }
func RunCoordinatorRemote(args []string) int {
	return commandtest.Run(NewCoordinatorCommand(), args...)
}
func RunOrchestrateRemote(args []string) int {
	return commandtest.Run(NewOrchestrateCommand(), args...)
}
func runRequest(args []string) int { return commandtest.Run(remote.NewRequestCommand(), args...) }
