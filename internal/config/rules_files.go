package config

import (
	"fmt"
	"io"
	"sync"

	"github.com/dualface/kander/rules"
)

// ReadRulesFile returns the installed copy of the rules-root file name (a slash-separated
// path such as "snippets/README.md"). When the installed copy is missing, the embedded copy
// is used and one localized line goes to warn, so the user learns to run `kander doctor`.
func ReadRulesFile(paths InstallPaths, name string, warn io.Writer) ([]byte, error) {
	if paths.RulesDir == "" {
		warn = nil // no installation to compare against
	}
	data, source, err := rules.Installed(paths.RulesDir, name)
	if err != nil {
		return nil, err
	}
	warnEmbeddedFallback(warn, name, source)
	return data, nil
}

// RenderRulesTemplate renders the template at name (below templates/) with data, reading the
// installed copy like ReadRulesFile. A missing data key or a template error fails the whole
// render.
func RenderRulesTemplate(paths InstallPaths, name string, data any, warn io.Writer) (string, error) {
	if paths.RulesDir == "" {
		warn = nil
	}
	text, source, err := rules.Render(paths.RulesDir, name, data)
	if err != nil {
		return "", err
	}
	warnEmbeddedFallback(warn, name, source)
	return text, nil
}

// RenderRulesTemplateSet renders the template named name from the set of every file below dir
// (for example "templates/review"), so templates in that directory may include each other.
// Installed copies win over embedded ones like RenderRulesTemplate.
func RenderRulesTemplateSet(paths InstallPaths, dir, name string, data any, warn io.Writer) (string, error) {
	if paths.RulesDir == "" {
		warn = nil
	}
	text, source, err := rules.RenderSet(paths.RulesDir, dir, name, data)
	if err != nil {
		return "", err
	}
	warnEmbeddedFallback(warn, dir, source)
	return text, nil
}

// warnedFallbacks remembers the names already reported, so one command run prints the
// embedded-fallback hint once per file instead of once per render.
var warnedFallbacks sync.Map

func warnEmbeddedFallback(warn io.Writer, name string, source rules.Source) {
	if warn == nil || source != rules.SourceEmbedded {
		return
	}
	if _, seen := warnedFallbacks.LoadOrStore(name, true); seen {
		return
	}
	fmt.Fprintln(warn, Text("install.rules_embedded_fallback", name))
}
