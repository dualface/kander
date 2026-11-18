package launch

func repairError(id string, args ...any) error {
	return launchError(id, args...)
}
