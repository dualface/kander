package config

// ProgramStatus is an agent's OSC 7501 (Program Status Protocol) declaration:
// whether the agent reports its status to the terminal, and the status.app
// value it reports. A terminal that identifies agents by these reports (the
// herdr-core launcher) runs only agents that declare Reports.
type ProgramStatus struct {
	Reports bool   `json:"reports"`
	App     string `json:"app,omitempty"`
}

func cloneProgramStatus(src *ProgramStatus) *ProgramStatus {
	if src == nil {
		return nil
	}
	out := *src
	return &out
}

// validProgramStatus requires an app exactly when the agent reports.
func validProgramStatus(status *ProgramStatus) bool {
	if status == nil {
		return true
	}
	if status.Reports {
		return validAgentText(status.App)
	}
	return status.App == ""
}

// ProgramStatusApp returns the status.app an agent declares it reports, or
// false when the agent has no declaration or declares that it does not report.
func ProgramStatusApp(definition AgentDefinition) (string, bool) {
	if definition.ProgramStatus == nil || !definition.ProgramStatus.Reports {
		return "", false
	}
	return definition.ProgramStatus.App, true
}
