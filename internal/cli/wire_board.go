package cli

// The client never binds the storage package's command runners. The server,
// explicit migration and storage tests own those helpers.
func init() {
	for name, runner := range RemoteCommands {
		Commands[name] = runner
	}
}
