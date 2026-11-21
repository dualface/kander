package install

// markdownFile is a plain Markdown rules file: no prefix, symlinks allowed, and a file with
// nothing left after the load commands is blank.
type markdownFile struct{}

func (markdownFile) Name() string { return "markdown" }

func (markdownFile) SymlinkRefusal() string { return "" }

func (markdownFile) Inspect(string) (formatState, string) { return formatReady, "" }

func (markdownFile) Prefix() string { return "" }

func (markdownFile) OnlyKander(rest string) bool { return false }
