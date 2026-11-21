package launch

// markerRepairable reports from static facts alone whether adoption can write
// the session marker a launch left out: the card names the session, or the
// agent's sessions can be found again. It runs no discovery and writes nothing,
// so recovery hints may call it. A configuration that cannot be read keeps the
// refusal, and so does an agent the configuration marks unusable.
func markerRepairable(session string) bool {
	parsed := parseSessionValue(session)
	if parsed == nil {
		return false
	}
	if parsed.Reference != "" {
		return true
	}
	cfg, err := loadEffective()
	if err != nil {
		return false
	}
	provider, err := sessionProviderOf(cfg, parsed.Agent)
	return err == nil && sessionMarkerRepairable(provider)
}
