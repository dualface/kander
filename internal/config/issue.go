package config

// Issue holds the GitHub issue workflow preferences.
type Issue struct {
	// ResultOnDone selects the completion handoff once every card of an issue's
	// work set is complete: publish the result comment without asking (auto),
	// show the draft and wait for consent (ask), or only report readiness (off).
	// Closing the issue always needs separate explicit consent.
	ResultOnDone string `json:"result_on_done"`
}

const (
	IssueResultAuto = "auto"
	IssueResultAsk  = "ask"
	IssueResultOff  = "off"
)

// IssueResultModes lists the accepted values of issue.result_on_done.
var IssueResultModes = []string{IssueResultAuto, IssueResultAsk, IssueResultOff}

func DefaultIssue() Issue {
	return Issue{ResultOnDone: IssueResultAuto}
}

// validateIssue accepts a missing key as the default; a config written before
// the section existed therefore keeps the default handoff.
func validateIssue(raw any) (Issue, error) {
	obj, ok := asObject(raw)
	if !ok {
		return Issue{}, configErrorf("config.issue_must_be_a_json_object")
	}
	issue := DefaultIssue()
	if value, exists := obj["result_on_done"]; exists {
		mode, err := validateChoice(value, IssueResultModes, "issue.result_on_done")
		if err != nil {
			return Issue{}, err
		}
		issue.ResultOnDone = mode
	}
	return issue, nil
}

// IssueResultLabel is the localized name of one result_on_done value.
func IssueResultLabel(mode string) string {
	switch mode {
	case IssueResultAuto, IssueResultAsk, IssueResultOff:
		return Text("config.issue_result_" + mode)
	}
	return mode
}
