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
