package launch

// ResumeLaunch is the launcher plus the process/terminal address after the notify recovery channel succeeds.
type ResumeLaunch struct {
	Plan    LaunchPlan
	Outcome LaunchOutcome
}

// ValidateTimeout requires timeout to be a finite number of seconds greater than 60.
func ValidateTimeout(timeout float64, command string) error {
	return validateLivenessTimeout(timeout, command)
}

// ReadMessage reads --message or --message-file; exactly one of them must be given.
func ReadMessage(message string, messageSet bool, messageFile, command string) (string, error) {
	return readTaskMessage(message, messageSet, messageFile, command)
}
