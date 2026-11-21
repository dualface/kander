package launch

// allocatedSessionProvider asks the agent's own command for a session id
// before the launch (session.allocate).
type allocatedSessionProvider struct{}

func init() { registerSessionProvider(allocatedSessionProvider{}) }

func (allocatedSessionProvider) Mode() string    { return "allocated" }
func (allocatedSessionProvider) Resumable() bool { return true }

func (allocatedSessionProvider) Allocate(l sessionLaunch) (string, error) {
	return allocateAgentSession(l.Definition)
}

// RequireAllocator resolves the allocator without running it. Config loading
// accepts an allocator that does not resolve, so a caller that records the
// allocation as an external effect checks it first and leaves a missing
// allocator as a plain, retryable refusal.
func (allocatedSessionProvider) RequireAllocator(l sessionLaunch) error {
	if resolveAgent(l.Definition.Allocate[0]) == nil {
		return launchError("launch.agent_is_not_in_path", l.Definition.Allocate[0])
	}
	return nil
}
