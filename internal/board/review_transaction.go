package board

// ExtendReviewPlan uses the standalone board transaction.
func ExtendReviewPlan(root string, x ReviewPlanExtension) error {
	return ExtendReviewPlanWithTransaction(root, x)
}

// AdvanceReviewBatch uses the standalone board transaction.
func AdvanceReviewBatch(root string, x ReviewBatchAdvance) error {
	return AdvanceReviewBatchWithTransaction(root, x)
}

// AssignReviewFindings uses the standalone board transaction.
func AssignReviewFindings(root string, a ReviewAssignment) error {
	return AssignReviewFindingsWithTransaction(root, a)
}

// SubmitReviewDisposition uses the standalone board transaction.
func SubmitReviewDisposition(root string, d ReviewDisposition, expectedRevision uint64) error {
	return SubmitReviewDispositionWithTransaction(root, d, expectedRevision)
}

// MapLegacyReview uses the standalone board transaction.
func MapLegacyReview(root string, m LegacyFindingMap) error {
	return MapLegacyReviewWithTransaction(root, m)
}

// InterpretReview uses the standalone board transaction.
func InterpretReview(root string, m ReviewInterpretation) error {
	return InterpretReviewWithTransaction(root, m)
}

// PublishReviewDisposition uses the standalone board transaction.
func PublishReviewDisposition(root, id string) (ReviewBatchView, error) {
	return PublishReviewDispositionWithTransaction(root, id)
}

// CloseReviewBatch uses the standalone board transaction.
func CloseReviewBatch(root string, r ReviewCloseRequest, evidence ReviewGitEvidence) (ReviewClosure, error) {
	return CloseReviewBatchWithTransaction(root, r, evidence)
}

// ReviewPreparation is the durable outcome of an invocation preparation.
type ReviewPreparation struct {
	Run   ReviewRun `json:"run"`
	Fresh bool      `json:"fresh"`
}

func PrepareReviewRun(root string, input ReviewInput, requirements map[string]string, advance *ReviewAdvance, originals map[string][]byte, version string) (ReviewRun, bool, error) {
	return PrepareReviewRunWithTransaction(root, input, requirements, advance, originals, version)
}
func UpdateReviewRun(root string, run ReviewRun) error {
	return UpdateReviewRunWithTransaction(root, run)
}
func StoreReviewArtifact(root, runID, name string, data []byte) error {
	return StoreReviewArtifactWithTransaction(root, runID, name, data)
}
func FinalizeReviewRun(root string, run ReviewRun, report []byte) (ReviewRun, error) {
	return FinalizeReviewRunWithTransaction(root, run, report)
}
