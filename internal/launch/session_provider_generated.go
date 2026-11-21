package launch

// generatedSessionProvider serves the modes whose id Kander makes up itself:
// "generated" hands the id to the agent, and "none" generates one too but the
// agent keeps no session, so it is never passed on and never resumed.
type generatedSessionProvider struct {
	mode      string
	resumable bool
}

func init() {
	registerSessionProvider(generatedSessionProvider{mode: "generated", resumable: true})
	registerSessionProvider(generatedSessionProvider{mode: "none", resumable: false})
}

func (p generatedSessionProvider) Mode() string    { return p.mode }
func (p generatedSessionProvider) Resumable() bool { return p.resumable }

func (generatedSessionProvider) Allocate(sessionLaunch) (string, error) { return newUUID(), nil }
