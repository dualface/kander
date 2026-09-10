package board

// ValidateTodoContract runs the read-only checks of a backlog -> todo move over
// one card body and size. It changes nothing: a caller may show the verdict
// while the creator confirms a contract, and the move repeats every check under
// the board lock before it happens.
func ValidateTodoContract(size, text string) error {
	return validateTarget(Entry{TaskID: "todo-contract", Kind: size}, "todo", text)
}

// RequiresCardReview reports whether the todo gate of this size and body waits
// for an independent CARD_REVIEW record. The tool only checks that the record
// line exists; the independence of the reviewer stays a human duty.
func RequiresCardReview(size, text string) bool {
	return size == "large" || taskGroupFrom(text) != ""
}
