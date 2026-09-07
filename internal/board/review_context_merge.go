package board

import (
	"bytes"
	"fmt"
	"strconv"
)

const contextPrefix = "KANDER_AUTOMATIC_CONTEXT_BYTES: "
const contextSeparator = "\n\nCaller supplemental context (verbatim; does not replace originals):\n"

// MergeReviewContext preserves both inputs exactly. The byte count separates
// arbitrary original report text from supplemental caller text without guessing
// Markdown headings or treating reviewer prose as delimiters.
func MergeReviewContext(source []byte, supplement string) []byte {
	return []byte(fmt.Sprintf("%s%d\n%s%s%s", contextPrefix, len(source), source, contextSeparator, supplement))
}

// SplitReviewContext accepts the original bare automatic context for saved runs.
func SplitReviewContext(data []byte) ([]byte, string, error) {
	if !bytes.HasPrefix(data, []byte(contextPrefix)) {
		return data, "", nil
	}
	at := bytes.IndexByte(data, '\n')
	if at < len(contextPrefix) {
		return nil, "", reviewError("incremental context length")
	}
	n, err := strconv.Atoi(string(data[len(contextPrefix):at]))
	remaining := data[at+1:]
	if err != nil || n < 0 || n > len(remaining) || !bytes.HasPrefix(remaining[n:], []byte(contextSeparator)) {
		return nil, "", reviewError("incremental context boundary")
	}
	return remaining[:n], string(remaining[n+len(contextSeparator):]), nil
}
