package api

// EvidenceCleanup names the Git evidence a finished card or task group no
// longer needs. Ready is false while a reader may still need the evidence:
// the card is not done, or a group member is not finished, or a completed
// wrap-up of the group has no wrap-up Git evidence record yet. A client reads
// the refs from it and deletes only those it also lists on the common remote.
type EvidenceCleanup struct {
	Ready bool `json:"ready"`
	// Tasks are the done cards whose task evidence refs (DeliveryRef, every
	// generation) go.
	Tasks []string `json:"tasks,omitempty"`
	// Sources are the integration executions whose IntegrationSourceRef goes,
	// including retired and failed attempts.
	Sources []EvidenceSource `json:"sources,omitempty"`
}

// EvidenceSource is one integration execution and the commit its source ref
// names.
type EvidenceSource struct {
	ExecutionID string `json:"execution_id"`
	Commit      string `json:"commit"`
}
