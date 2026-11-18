package board

import "os"

// Remark reports a note of a board operation that is not an error: text is ready to print, and
// code names the kind for a caller that wants it structured. The default writes text to
// stderr; the command layer replaces it so that --json carries the note as a notice.
var Remark = func(_, text string) { _, _ = os.Stderr.WriteString(text) }
