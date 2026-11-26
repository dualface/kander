package install

import (
	"path/filepath"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
)

// Rules integration has two independent axes, each behind its own provider kind. A
// referenceStyle decides how a rules file points at the Kander entry; a fileFormat decides what
// the target file itself must look like. Callers never branch on a style name or a file
// extension: they ask the provider. Neither kind decides whether or where a file may be
// written; the install_rules gate, the shared-file check, the symlink and project escape checks
// and the atomic write stay with the callers.
//
// To add a reference style, implement referenceStyle in its own file, register it in
// referenceStyles, and add its name to config.RulesIntegrations (the consistency test keeps the
// two lists equal). To add a file format, implement fileFormat and register its extension in
// fileFormatsByExtension.

// referenceStyle is one way of referencing the Kander rules entry from an agent rules file,
// selected by the rules_integration value of the agent's embedded definition.
type referenceStyle interface {
	// Name is the rules_integration value that selects this style.
	Name() string
	// Block is the text appended to a rules file; spelling is the portable entry path.
	Block(spelling string) string
	// Present reports whether the text references the entry where the agent loads it. It is the
	// comment-, fence- and negation-aware check of the reporting side, not the append gate.
	Present(text, entry, baseDir string) bool
	// LoadCommand returns the entry path of a whole-line load command in this style, given the
	// trimmed line. The cleaner asks every style for every file.
	LoadCommand(trimmed string) (token string, ok bool)
	// ProjectWriteFirst reports whether a project install writes this style's files before the
	// others, so the files that import another one are settled first.
	ProjectWriteFirst() bool
	// ProjectSymlinkMayDiffer reports whether, in project scope, the rules file may be a
	// symlink that does not point at the rules entry; its content is then read like any file.
	ProjectSymlinkMayDiffer() bool
}

// fileFormat is the kind of target file a reference is written into, selected by the target
// path.
type fileFormat interface {
	// Name identifies the format.
	Name() string
	// SymlinkRefusal is the message key explaining why the format never reads or writes through
	// a symlink, or "" when symlinks are allowed.
	SymlinkRefusal() string
	// Inspect classifies the existing text. For a state other than formatReady, problem is the
	// message key that describes it.
	Inspect(text string) (state formatState, problem string)
	// Prefix is the text a new file starts with, and what a file in formatMissingPrefix gains.
	Prefix() string
	// OnlyKander reports whether rest, the trimmed content left after the Kander load commands
	// were removed, is only what Kander itself wrote, so uninstall deletes the file.
	OnlyKander(rest string) bool
}

// formatState is what a fileFormat finds in the existing text of a target file.
type formatState int

const (
	// formatReady means the text needs no change for the reference to take effect.
	formatReady formatState = iota
	// formatMissingPrefix means the text lacks the prefix Kander adds.
	formatMissingPrefix
	// formatForeign means the text carries a user-owned prefix that is never rewritten.
	formatForeign
)

// referenceStyles is the registry of reference styles, in the order the cleaner asks them.
var referenceStyles = []referenceStyle{markdownReference{}, claudeImport{}}

// fileFormatsByExtension is the one place that selects a file format by the lower-case
// extension of the target path; every other path uses the plain markdown format.
var fileFormatsByExtension = map[string]fileFormat{".mdc": mdcRule{}}

// referenceStyleFor returns the style of an agent's embedded definition. An agent without a
// definition, or with a name this build does not register, uses the markdown reference.
func referenceStyleFor(agent string) referenceStyle {
	spec, _ := config.RulesSpec(agent)
	for _, style := range referenceStyles {
		if style.Name() == spec.Integration {
			return style
		}
	}
	return markdownReference{}
}

// fileFormatFor returns the format of a rules target file.
func fileFormatFor(target string) fileFormat {
	if format, ok := fileFormatsByExtension[strings.ToLower(filepath.Ext(target))]; ok {
		return format
	}
	return markdownFile{}
}

// loadCommandToken returns the Kander rules entry path of a whole-line load command written in
// any registered style.
func loadCommandToken(trimmed string) (string, bool) {
	for _, style := range referenceStyles {
		if token, ok := style.LoadCommand(trimmed); ok {
			return token, true
		}
	}
	return "", false
}
