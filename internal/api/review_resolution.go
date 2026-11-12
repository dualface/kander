package api

import "github.com/dualface/kander/internal/board"

// ReviewResolution binds client Git verification to the exact historical
// plan, batch and report hashes returned by the read-only facts producer.
type ReviewResolution struct {
	Extension board.ReviewPlanExtension   `json:"extension"`
	Git       board.ReviewResolutionGit   `json:"git"`
	Facts     board.ReviewResolutionFacts `json:"facts"`
}
