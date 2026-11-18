package launch

import "github.com/dualface/kander/internal/command/commandtest"

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
