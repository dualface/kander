package board

// FinalizeUploadedReview freezes verified runtime bytes and formal originals
// together. The caller validates upload digests and execution authority; this
// producer limits the writable paths and shares its transaction with finalization.
func FinalizeUploadedReview(root string, run ReviewRun, report []byte, files map[string][]byte, extensions ...TransactionExtension) (ReviewRun, error) {
	upload := TransactionExtension{Before: func(tx *Transaction) error {
		for name, data := range files {
			if name != "output.raw" && name != "stdout.log" && name != "error.log" && name != ReviewProcessEvidence {
				return reviewError("runtime upload name")
			}
			if err := tx.putGroupBytes(reviewControlGroup, "runs/"+run.RunID+"/staging/"+name, data); err != nil {
				return err
			}
		}
		return nil
	}}
	// The authority extension runs first. Originals then read the staged bytes.
	return FinalizeReviewRunWithTransaction(root, run, report, append(extensions, upload)...)
}
