package board

// ReviewRun separates execution facts from semantic interpretation. An ok run
// never asserts PASS. Publication is complete only after all receipts verify.
type ReviewRun struct {
	Schema int `json:"schema"`
	ReviewInput
	KanderVersion   string            `json:"kander_version"`
	Phase           string            `json:"phase"`
	LaunchStatus    string            `json:"launch_status"`
	ExecutionStatus string            `json:"execution_status"`
	SemanticStatus  string            `json:"semantic_status"`
	FailureReason   string            `json:"failure_reason,omitempty"`
	ExitCode        int               `json:"exit_code"`
	CreatedAt       string            `json:"created_at"`
	FinishedAt      string            `json:"finished_at,omitempty"`
	DurationMS      int64             `json:"duration_ms"`
	Hashes          map[string]string `json:"hashes"`
	Published       map[string]bool   `json:"published"`
	// SkippedMembers took no copy: they left the batch before publication.
	SkippedMembers []string `json:"skipped_members,omitempty"`
	// MemberCycles binds the run to the execution cycle of each member when it was
	// created. omitempty keeps runs written before the field byte-identical.
	MemberCycles map[string]string `json:"member_cycles,omitempty"`
}
