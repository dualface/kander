package api

import "encoding/json"

// IssueResultQuery addresses the imported anchor by canonical source identity.
// It carries no provider credentials or client filesystem location.
type IssueResultQuery struct {
	SourceKey string `json:"source_key"`
	TaskID    string `json:"task_id"`
}
type IssueResultState struct {
	Revision uint64          `json:"revision"`
	Work     json.RawMessage `json:"work"`
	Store    json.RawMessage `json:"store"`
}
type IssueResultWrite struct {
	Query            IssueResultQuery `json:"query"`
	ExpectedRevision uint64           `json:"expected_revision"`
	WorkDigest       string           `json:"work_digest"`
	Store            json.RawMessage  `json:"store"`
}
