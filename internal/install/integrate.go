package install

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

var negationRE = regexp.MustCompile(`(?i)(禁用|废弃|停用|不要遵守|勿遵守|不遵守|不要使用|不使用|请勿使用|未导入|尚未导入|不再生效|已失效|请忽略|do not follow|do not use|disabled|deprecated|\bignore\b)`)

// ProjectEntrySpelling is how a project reference names the global rules entry. It is the
// same text on every machine, so the reference can be committed to the repository.
const ProjectEntrySpelling = "~/.agents/kander/KANDER-AGENTS.md"

// ProjectRules is one checkout's project rules files and the global rules entry they
// reference. Kander never writes an agent's global rules file: kander init writes a project
// reference into the project rules files, and the Kander rules apply in the projects whose
// committed rules files carry it.
type ProjectRules struct {
	// Root is the Git main worktree; every project rules target is relative to it.
	Root string
	// Paths is the global install whose rules entry the references name.
	Paths config.InstallPaths
}

// NewProjectRules returns the project rules of the main worktree root, with the root's
// symlinks resolved so every write below it is checked component by component.
func NewProjectRules(root string, paths config.InstallPaths) (ProjectRules, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ProjectRules{}, err
	}
	return ProjectRules{Root: resolved, Paths: paths}, nil
}

// IntegrationStatus reports what EnsureProjectReference did to one project rules file.
type IntegrationStatus int

const (
	// IntegrationPresent means the file already references the Kander entry; nothing was written.
	IntegrationPresent IntegrationStatus = iota
	// IntegrationCreated means the file did not exist and was created with the reference.
	IntegrationCreated
	// IntegrationUpdated means the reference was appended to an existing file; a stale or
	// duplicate load command, such as one naming the retired .kander/rules/ entry, was dropped
	// at the same time when Removed is above zero. A .mdc rule without frontmatter also gained
	// the alwaysApply frontmatter.
	IntegrationUpdated
	// IntegrationCleaned means invalid or duplicate load commands were removed and no new
	// reference had to be appended.
	IntegrationCleaned
	// IntegrationFrontmatterAdded means a .mdc rule that already referenced the entry gained
	// the alwaysApply frontmatter and no reference had to be appended.
	IntegrationFrontmatterAdded
	// IntegrationDisabled means an agent that reads the file has install_rules off, so
	// nothing was read or written; DisabledBy names those agents.
	IntegrationDisabled
)

// IntegrationOutcome is the result of ensuring one project rules file references the Kander entry.
type IntegrationOutcome struct {
	Agent  string
	Target string
	Status IntegrationStatus
	// Removed counts the invalid or duplicate Kander rules load commands the cleanup dropped.
	Removed int
	// Override reports the AGENTS.override.md sibling the same call processed when that file
	// already exists next to an AGENTS.md target; nil otherwise. It never carries its own
	// Override.
	Override *IntegrationOutcome
	// Inactive marks a file that still mentions the entry after the call, but only where the
	// agent does not load it (an HTML comment, a code fence, or a negated sentence): the
	// mention blocks another append, so the file stays without an active reference.
	Inactive bool
	// DisabledBy lists the agents whose install_rules switch is off and that read the file.
	DisabledBy []string
}

// AgentIntegration records ensuring one project rules file references the Kander entry.
type AgentIntegration struct {
	IntegrationOutcome
	Err error
}

// EnsureProjectReferences writes the project reference into the project rules file of every
// covered agent, once per file. The switches decide the files, as DisabledReaders describes.
func EnsureProjectReferences(project ProjectRules, switches config.InstallRules) []AgentIntegration {
	var out []AgentIntegration
	seen := map[string]struct{}{}
	for _, agent := range ProjectReferenceAgents() {
		target := ProjectRulesTarget(agent, project.Root)
		key := rulesFileKey(target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		outcome, err := EnsureProjectReference(agent, project, switches)
		out = append(out, AgentIntegration{IntegrationOutcome: outcome, Err: err})
	}
	return out
}

// ProjectReferenceAgents lists the embedded agents that declare a project rules file. The
// agents whose style imports another file come first, so a file such as CLAUDE.md that may
// import AGENTS.md is settled before the files it imports.
func ProjectReferenceAgents() []string {
	var importFirst, rest []string
	for _, agent := range config.ExecutionAgents {
		spec, ok := config.RulesSpec(agent)
		if !ok || strings.TrimSpace(spec.Project) == "" {
			continue
		}
		if referenceStyleFor(agent).ProjectWriteFirst() {
			importFirst = append(importFirst, agent)
		} else {
			rest = append(rest, agent)
		}
	}
	return append(importFirst, rest...)
}

// RulesEntry returns the Kander rules entry file of the install.
func RulesEntry(paths config.InstallPaths) string {
	return filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
}

// ProjectRulesTarget returns the project rules file one agent reads below the main worktree
// root, or "" when the agent declares none.
func ProjectRulesTarget(agent, root string) string {
	spec, ok := config.RulesSpec(agent)
	if !ok || strings.TrimSpace(spec.Project) == "" || root == "" {
		return ""
	}
	return filepath.Join(root, filepath.FromSlash(spec.Project))
}

// ProjectRulesRelative returns the project rules file of one agent relative to the main
// worktree, with forward slashes, or "" when the agent declares none.
func ProjectRulesRelative(agent string) string {
	spec, ok := config.RulesSpec(agent)
	if !ok {
		return ""
	}
	return strings.TrimSpace(spec.Project)
}

// GlobalRulesTarget returns the global rules file of one agent, below its resolved
// configuration directory (see agentConfigRoot). Kander only reads it, to remove what earlier
// releases wrote there.
func GlobalRulesTarget(agent string, paths config.InstallPaths) (string, error) {
	spec, ok := config.RulesSpec(agent)
	if !ok || strings.TrimSpace(spec.Global) == "" {
		return "", nil
	}
	return globalAgentTarget(agent, spec.Global, paths)
}

// ProjectReferenceStatus reports whether the agent's project rules file references the Kander
// entry where the agent loads it. When an AGENTS.override.md sibling exists next to an
// AGENTS.md target, both files must reference the entry: the agent reads the override
// instead of the main file. The second value is the file, or a localized explanation.
func ProjectReferenceStatus(agent string, project ProjectRules) (bool, string) {
	target := ProjectRulesTarget(agent, project.Root)
	if target == "" {
		return false, config.Text("menu.not_found", agent)
	}
	integrated, detail := rulesFileIntegration(agent, target, project)
	if !integrated {
		return false, detail
	}
	if override := rulesOverrideTarget(target); override != "" {
		return rulesFileIntegration(agent, override, project)
	}
	return true, target
}

// rulesFileIntegration reports whether one project rules file references the Kander entry.
func rulesFileIntegration(agent, target string, project ProjectRules) (bool, string) {
	entry := RulesEntry(project.Paths)
	info, err := os.Lstat(target)
	if err != nil {
		return false, config.Text("menu.not_found", target)
	}
	format := fileFormatFor(target)
	if key := format.SymlinkRefusal(); key != "" && info.Mode()&os.ModeSymlink != 0 {
		return false, config.Text(key, target)
	}
	style := referenceStyleFor(agent)
	resolved := target
	if info.Mode()&os.ModeSymlink != 0 {
		linked, err := filepath.EvalSymlinks(target)
		if err != nil {
			return false, config.Text("menu.cannot_read", target)
		}
		if !withinDirectory(project.Root, linked) {
			return false, config.Text("install.rules_target_symlink_escapes_project", target)
		}
		if !style.ProjectSymlinkMayDiffer() {
			return false, config.Text("menu.does_not_point_to_the_project_rules_entry", target, entry)
		}
		resolved = linked
	}
	text, err := os.ReadFile(resolved)
	if err != nil {
		return false, config.Text("menu.cannot_read", target)
	}
	if state, problem := format.Inspect(string(text)); state != formatReady {
		return false, config.Text(problem, target)
	}
	if style.Present(string(text), entry, filepath.Dir(target)) {
		return true, target
	}
	return false, config.Text("menu.does_not_import_or_include_kander_agents_md", target)
}

// EnsureProjectReference makes the agent's project rules file reference the Kander entry: it
// creates the file when missing, appends the reference when absent, and removes invalid or
// duplicate load commands, such as one naming the retired .kander/rules/KANDER-AGENTS.md of a
// project install; a file whose references are already clean is left untouched. A file that
// an agent with its switch off reads is neither read nor written.
// When an AGENTS.override.md sibling already exists next to an AGENTS.md target, the same
// pipeline runs on it too and its result is reported through the outcome's Override field; the
// override expresses the user's choice to shadow the main file, so it is only ever edited,
// never created.
// The write gate is strictReferencePresent, not ProjectReferenceStatus: appending must never
// repeat, so any literal mention of the entry blocks it, even an inactive one.
func EnsureProjectReference(agent string, project ProjectRules, switches config.InstallRules) (IntegrationOutcome, error) {
	outcome := IntegrationOutcome{Agent: agent}
	target := ProjectRulesTarget(agent, project.Root)
	if target == "" {
		return outcome, fmt.Errorf("%s", config.Text("menu.not_found", agent))
	}
	outcome.Target = target
	if disabled := DisabledReaders(target, project.Root, switches); len(disabled) > 0 {
		outcome.Status = IntegrationDisabled
		outcome.DisabledBy = disabled
		return outcome, nil
	}
	main, err := ensureTargetIntegration(agent, target, true, project)
	outcome.Status = main.Status
	outcome.Removed = main.Removed
	outcome.Inactive = main.Inactive
	if err != nil {
		return outcome, err
	}
	if override := rulesOverrideTarget(target); override != "" {
		secondary, err := ensureTargetIntegration(agent, override, false, project)
		outcome.Override = &secondary
		if err != nil {
			return outcome, err
		}
	}
	return outcome, nil
}

// rulesOverrideTarget returns the AGENTS.override.md sibling of an AGENTS.md rules target when
// that file already exists; it is never created by Kander, so a missing file yields "".
func rulesOverrideTarget(target string) string {
	if filepath.Base(target) != "AGENTS.md" {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(target), "AGENTS.override.md")
	if _, err := os.Lstat(candidate); err != nil {
		return ""
	}
	return candidate
}

// ensureTargetIntegration runs the reference pipeline on one rules file. The configured target
// may be created when missing (create=true); an override sibling (create=false) is only edited.
// The write is atomic and anchored at the main worktree, so no component below it is followed
// through a symlink.
func ensureTargetIntegration(agent, target string, create bool, project ProjectRules) (IntegrationOutcome, error) {
	edit, err := planTargetIntegration(agent, target, create, project)
	if err != nil || edit.path == "" {
		return edit.outcome, err
	}
	if err := fs.WriteBytesAtomicInherited(project.Root, edit.path, edit.content, !edit.created); err != nil {
		return edit.outcome, err
	}
	if !edit.appended && edit.outcome.Status != IntegrationCreated {
		edit.outcome.Inactive = !referenceActive(agent, target, project)
	}
	return edit.outcome, nil
}

// targetEdit is the write the reference pipeline plans for one rules file; path is empty when
// the file stays as it is.
type targetEdit struct {
	outcome  IntegrationOutcome
	path     string
	content  []byte
	created  bool
	appended bool
}

// planTargetIntegration computes the reference pipeline's result for one rules file without
// writing. A rules file that is a symlink is edited at its target, which must stay inside the
// main worktree: a cloned repository controls its own CLAUDE.md and AGENTS.md and must never
// aim the write at a file outside the project, such as the user's dotfiles.
func planTargetIntegration(agent, target string, create bool, project ProjectRules) (targetEdit, error) {
	edit := targetEdit{outcome: IntegrationOutcome{Agent: agent, Target: target}}
	block := referenceBlock(agent)
	format := fileFormatFor(target)
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		if !create {
			return edit, err
		}
		edit.path = target
		edit.content = []byte(format.Prefix() + block)
		edit.created = true
		edit.appended = true
		edit.outcome.Status = IntegrationCreated
		return edit, nil
	}
	if err != nil {
		return edit, err
	}
	if key := format.SymlinkRefusal(); key != "" && info.Mode()&os.ModeSymlink != 0 {
		return edit, fmt.Errorf("%s", config.Text(key, target))
	}
	resolved := target
	if info.Mode()&os.ModeSymlink != 0 {
		if resolved, err = filepath.EvalSymlinks(target); err != nil {
			return edit, err
		}
	}
	if !withinDirectory(project.Root, resolved) {
		return edit, fmt.Errorf("%s", config.Text("install.rules_target_symlink_escapes_project", target))
	}
	if info, err := os.Stat(resolved); err != nil {
		return edit, err
	} else if !info.Mode().IsRegular() {
		return edit, fmt.Errorf("%s", config.Text("menu.cannot_read", target))
	}
	existing, err := os.ReadFile(resolved)
	if err != nil {
		return edit, err
	}
	text := string(existing)
	needFrontmatter := false
	switch state, problem := format.Inspect(text); state {
	case formatForeign:
		return edit, fmt.Errorf("%s", config.Text(problem, target))
	case formatMissingPrefix:
		needFrontmatter = true
	}
	entry := RulesEntry(project.Paths)
	// Dead and duplicate load commands are dropped first, so a reference to the retired
	// project install entry or a repeated import can neither satisfy the strict gate nor
	// survive the write.
	cleaned, removed := CleanRulesReferences(text, entry, filepath.Dir(target))
	if !needFrontmatter && removed == 0 && strictReferencePresent(text, entry, filepath.Dir(target)) {
		edit.outcome.Status = IntegrationPresent
		edit.outcome.Inactive = !referenceActive(agent, target, project)
		return edit, nil
	}
	final := cleaned
	if !strictReferencePresent(cleaned, entry, filepath.Dir(target)) {
		// One blank line separates the block from the content above, however many the
		// removed load commands left behind.
		final = block
		if base := strings.TrimRight(cleaned, "\n"); base != "" {
			final = base + "\n\n" + block
		}
		edit.appended = true
	}
	if needFrontmatter {
		final = format.Prefix() + final
	}
	edit.path = resolved
	edit.content = []byte(final)
	edit.outcome.Removed = removed
	switch {
	case edit.appended:
		edit.outcome.Status = IntegrationUpdated
	case needFrontmatter:
		edit.outcome.Status = IntegrationFrontmatterAdded
	default:
		edit.outcome.Status = IntegrationCleaned
	}
	return edit, nil
}

// referenceActive reports whether the rules file references the entry where the agent loads
// it, applying the comment-, fence- and negation-aware check ProjectReferenceStatus uses.
func referenceActive(agent, target string, project ProjectRules) bool {
	active, _ := rulesFileIntegration(agent, target, project)
	return active
}

func referenceBlock(agent string) string {
	return referenceStyleFor(agent).Block(ProjectEntrySpelling)
}

func stripCommentsAndFences(text string) string {
	withoutComments := regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(text, "")
	withoutComments = regexp.MustCompile(`(?s)<!--.*\z`).ReplaceAllString(withoutComments, "")
	var lines []string
	var fence *byte
	fenceLen := 0
	for _, raw := range strings.Split(withoutComments, "\n") {
		stripped := strings.TrimLeft(raw, " \t")
		if fence == nil {
			matched := false
			for _, marker := range []string{"```", "~~~"} {
				if strings.HasPrefix(stripped, marker) {
					ch := marker[0]
					fence = &ch
					fenceLen = len(stripped) - len(strings.TrimLeft(stripped, string(marker[:1])))
					matched = true
					break
				}
			}
			if !matched {
				lines = append(lines, raw)
			}
			continue
		}
		if strings.HasPrefix(stripped, strings.Repeat(string([]byte{*fence}), 3)) {
			closing := len(stripped) - len(strings.TrimLeft(stripped, string([]byte{*fence})))
			if closing >= fenceLen {
				fence = nil
				fenceLen = 0
			}
		}
	}
	return strings.Join(lines, "\n")
}

func candidateContext(text string, start, end int, includeMatch bool) string {
	before := strings.LastIndex(text[:start], "\n\n")
	after := strings.Index(text[end:], "\n\n")
	left := 0
	if before >= 0 {
		left = before + 2
	}
	right := len(text)
	if after >= 0 {
		right = end + after
	}
	headingBefore := strings.LastIndex(text[left:start], "\n#")
	if headingBefore >= 0 {
		left = left + headingBefore + 1
	}
	headingAfter := strings.Index(text[end:right], "\n#")
	if headingAfter >= 0 {
		right = end + headingAfter
	}
	var section string
	if includeMatch {
		section = text[left:right]
	} else {
		section = text[left:start] + text[end:right]
	}
	prior := strings.Split(text[:start], "\n")
	if len(prior) > 40 {
		prior = prior[len(prior)-40:]
	}
	following := strings.Split(text[end:], "\n")
	if len(following) > 40 {
		following = following[:40]
	}
	parts := append([]string{section}, prior...)
	parts = append(parts, following...)
	return strings.Join(parts, "\n")
}

func ruleEntryCandidates(entry string) map[string]struct{} {
	candidates := map[string]struct{}{entry: {}}
	expanded := entry
	if strings.HasPrefix(entry, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = home + entry[1:]
		}
	}
	resolved, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		resolved = expanded
	}
	candidates[expanded] = struct{}{}
	candidates[resolved] = struct{}{}
	homes := map[string]struct{}{}
	if home, err := os.UserHomeDir(); err == nil {
		homes[home] = struct{}{}
		if rh, err := filepath.EvalSymlinks(home); err == nil {
			homes[rh] = struct{}{}
		}
	}
	for path := range map[string]struct{}{expanded: {}, resolved: {}} {
		for home := range homes {
			if path == home || strings.HasPrefix(path, home+string(os.PathSeparator)) {
				candidates["~"+path[len(home):]] = struct{}{}
			}
		}
	}
	// Written references always use forward slashes, so on Windows every candidate must
	// also match in that spelling without relying on the entry file existing on disk.
	native := make([]string, 0, len(candidates))
	for candidate := range candidates {
		native = append(native, candidate)
	}
	for _, candidate := range native {
		candidates[filepath.ToSlash(candidate)] = struct{}{}
	}
	return candidates
}

func sameResolved(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return ra == rb
}

func splitKeepEnds(text string) []string {
	if text == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

func mergedRulesPresent(text, entry string) bool {
	expectedBytes, err := os.ReadFile(entry)
	if err != nil {
		return false
	}
	expected := strings.TrimSpace(string(expectedBytes))
	if expected == "" {
		return false
	}
	body := stripCommentsAndFences(text)
	index := strings.Index(body, expected)
	if index < 0 {
		return false
	}
	context := candidateContext(body, index, index+len(expected), false)
	return !negationRE.MatchString(context)
}

// entrySpellings is every accepted spelling of the entry path: absolute, ~/-relative,
// base-directory-relative, with either slash style.
func entrySpellings(entry, baseDir string) map[string]struct{} {
	spellings := map[string]struct{}{}
	for candidate := range ruleEntryCandidates(entry) {
		spellings[candidate] = struct{}{}
		spellings[filepath.ToSlash(candidate)] = struct{}{}
	}
	if rel, err := filepath.Rel(baseDir, entry); err == nil && !strings.HasPrefix(rel, "..") {
		spellings[rel] = struct{}{}
		spellings[filepath.ToSlash(rel)] = struct{}{}
	}
	return spellings
}

// strictReferencePresent is the append gate: any literal mention of the entry path, or an embedded
// copy of the entry content, counts as present. Unlike the reporting-side checks it deliberately
// skips comment/fence stripping and the negation heuristic — words like "deprecated" or "ignore"
// near the reference, or a commented-out reference, must block another append, never trigger one.
func strictReferencePresent(text, entry, baseDir string) bool {
	for spelling := range entrySpellings(entry, baseDir) {
		if spelling != "" && strings.Contains(text, spelling) {
			return true
		}
	}
	if expected, err := os.ReadFile(entry); err == nil {
		trimmed := strings.TrimSpace(string(expected))
		if trimmed != "" && strings.Contains(text, trimmed) {
			return true
		}
	}
	return false
}

// withinDirectory reports whether path is root or lexically inside root, after resolving root.
func withinDirectory(root, path string) bool {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// entryReferencePresent reports whether the body mentions the entry path in a non-negated context,
// in any accepted spelling: absolute, ~/-relative, base-directory-relative, with either slash style.
func entryReferencePresent(text, entry, baseDir string) bool {
	spellings := entrySpellings(entry, baseDir)
	body := stripCommentsAndFences(text)
	for spelling := range spellings {
		if spelling == "" {
			continue
		}
		offset := 0
		for {
			index := strings.Index(body[offset:], spelling)
			if index < 0 {
				break
			}
			start := offset + index
			end := start + len(spelling)
			context := candidateContext(body, start, end, true)
			if !negationRE.MatchString(context) {
				return true
			}
			offset = end
		}
	}
	return false
}
