package install

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/config"
)

var negationRE = regexp.MustCompile(`(?i)(禁用|废弃|停用|不要遵守|勿遵守|不遵守|不要使用|不使用|请勿使用|未导入|尚未导入|不再生效|已失效|请忽略|do not follow|do not use|disabled|deprecated|\bignore\b)`)

// IntegrationStatus reports what EnsureRulesIntegration did to one agent rules file.
type IntegrationStatus int

const (
	// IntegrationPresent means the file already references the Kander entry; nothing was written.
	IntegrationPresent IntegrationStatus = iota
	// IntegrationCreated means the file did not exist and was created with the reference.
	IntegrationCreated
	// IntegrationUpdated means the reference was appended to an existing file.
	IntegrationUpdated
)

// IntegrationOutcome is the result of ensuring one agent rules file references the Kander entry.
type IntegrationOutcome struct {
	Agent  string
	Target string
	Status IntegrationStatus
}

// integrateAgentRules ensures agent rules files reference the Kander entry after an install.
// A project install always covers CLAUDE.md and AGENTS.md at the project root; a global install
// only touches agents whose configuration directory already exists, so absent tools gain no files.
func integrateAgentRules(paths config.InstallPaths) []AgentIntegration {
	var out []AgentIntegration
	seen := map[string]struct{}{}
	for _, agent := range integrationAgents(paths) {
		target := AgentRulesTarget(agent, paths)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		outcome, err := EnsureRulesIntegration(agent, paths)
		out = append(out, AgentIntegration{IntegrationOutcome: outcome, Err: err})
	}
	return out
}

func integrationAgents(paths config.InstallPaths) []string {
	var importFirst, rest []string
	for _, agent := range config.ExecutionAgents {
		target := AgentRulesTarget(agent, paths)
		if target == "" {
			continue
		}
		if paths.Mode == config.ModeProject {
			spec, _ := config.RulesSpec(agent)
			if spec.Integration == "claude-import" {
				importFirst = append(importFirst, agent)
			} else {
				rest = append(rest, agent)
			}
			continue
		}
		if info, err := os.Stat(filepath.Dir(target)); err == nil && info.IsDir() {
			rest = append(rest, agent)
		}
	}
	if paths.Mode == config.ModeProject {
		return append(importFirst, rest...)
	}
	return rest
}

// RulesEntry returns the Kander rules entry file of the given scope.
func RulesEntry(paths config.InstallPaths) string {
	return filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
}

// AgentRulesTarget returns the rules file one agent reads in the given scope.
func AgentRulesTarget(agent string, paths config.InstallPaths) string {
	spec, ok := config.RulesSpec(agent)
	if !ok {
		return ""
	}
	rel := spec.Global
	if paths.Mode == config.ModeProject {
		rel = spec.Project
	}
	if strings.TrimSpace(rel) == "" {
		return ""
	}
	if paths.Mode == config.ModeProject {
		if paths.ProjectRoot == "" {
			return ""
		}
		return filepath.Join(paths.ProjectRoot, filepath.FromSlash(rel))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(rel))
}

// RulesIntegration reports whether the agent rules file of the given scope references the Kander entry.
// The second value is the target path when integrated, or a localized explanation when not.
func RulesIntegration(agent string, paths config.InstallPaths) (bool, string) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return false, config.Text("config.project_install_paths_are_missing_the_main_worktree")
	}
	entry := RulesEntry(paths)
	target := AgentRulesTarget(agent, paths)
	info, err := os.Lstat(target)
	if err != nil {
		return false, config.Text("menu.not_found", target)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linked, err := filepath.EvalSymlinks(target)
		if err == nil {
			expected, expErr := filepath.EvalSymlinks(entry)
			if expErr != nil {
				expected = entry
			}
			if linked == expected {
				return true, target
			}
			spec, _ := config.RulesSpec(agent)
			if paths.Mode == config.ModeProject && spec.Integration != "claude-import" {
				return false, config.Text(
					"menu.does_not_point_to_the_project_rules_entry", target, entry,
				)
			}
		}
	}
	text, err := os.ReadFile(target)
	if err != nil {
		return false, config.Text("menu.cannot_read", target)
	}
	var integrated bool
	spec, _ := config.RulesSpec(agent)
	if spec.Integration == "claude-import" {
		integrated = claudeRulesImportPresent(string(text), entry, filepath.Dir(target))
	} else {
		integrated = mergedRulesPresent(string(text), entry) ||
			entryReferencePresent(string(text), entry, filepath.Dir(target))
	}
	if integrated {
		return true, target
	}
	return false, config.Text(
		"menu.does_not_import_or_include_kander_agents_md", target,
	)
}

// EnsureRulesIntegration makes the agent rules file reference the Kander entry: it creates the file
// when missing, appends the reference when absent, and leaves every already-integrated file untouched.
// The write gate is strictReferencePresent, not RulesIntegration: appending must never repeat, so any
// literal mention of the entry blocks it, even one RulesIntegration would report as inactive.
func EnsureRulesIntegration(agent string, paths config.InstallPaths) (IntegrationOutcome, error) {
	outcome := IntegrationOutcome{Agent: agent}
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return outcome, fmt.Errorf("%s", config.Text("config.project_install_paths_are_missing_the_main_worktree"))
	}
	target := AgentRulesTarget(agent, paths)
	if target == "" {
		return outcome, fmt.Errorf("%s", config.Text("config.cannot_resolve_home_directory"))
	}
	outcome.Target = target
	block := referenceBlock(agent, paths)
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return outcome, err
		}
		if err := os.WriteFile(target, []byte(block), 0o644); err != nil {
			return outcome, err
		}
		outcome.Status = IntegrationCreated
		return outcome, nil
	}
	if err != nil {
		return outcome, err
	}
	resolved := target
	if info.Mode()&os.ModeSymlink != 0 {
		if resolved, err = filepath.EvalSymlinks(target); err != nil {
			return outcome, err
		}
		// A cloned repository controls its own CLAUDE.md/AGENTS.md; never let one aim a
		// project install's append at a file outside the project (e.g. the user's dotfiles).
		if paths.Mode == config.ModeProject && !withinDirectory(paths.ProjectRoot, resolved) {
			return outcome, fmt.Errorf("%s", config.Text("install.rules_target_symlink_escapes_project", target))
		}
	}
	if info, err := os.Stat(resolved); err != nil {
		return outcome, err
	} else if info.IsDir() {
		return outcome, fmt.Errorf("%s", config.Text("menu.cannot_read", target))
	}
	existing, err := os.ReadFile(resolved)
	if err != nil {
		return outcome, err
	}
	if strictReferencePresent(string(existing), RulesEntry(paths), filepath.Dir(target)) {
		outcome.Status = IntegrationPresent
		return outcome, nil
	}
	separator := "\n"
	if len(existing) == 0 || strings.HasSuffix(string(existing), "\n\n") {
		separator = ""
	} else if strings.HasSuffix(string(existing), "\n") {
		separator = "\n"
	} else {
		separator = "\n\n"
	}
	file, err := os.OpenFile(resolved, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return outcome, err
	}
	_, writeErr := file.WriteString(separator + block)
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return outcome, writeErr
	}
	outcome.Status = IntegrationUpdated
	return outcome, nil
}

// entrySpelling is the portable spelling of the rules entry written into agent rules files:
// project scope uses a path relative to the project root, global scope prefers a ~/ path,
// and both use forward slashes on every platform.
func entrySpelling(paths config.InstallPaths) string {
	entry := RulesEntry(paths)
	if paths.Mode == config.ModeProject && paths.ProjectRoot != "" {
		if rel, err := filepath.Rel(paths.ProjectRoot, entry); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, entry); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(entry)
}

func referenceBlock(agent string, paths config.InstallPaths) string {
	spelling := entrySpelling(paths)
	spec, _ := config.RulesSpec(agent)
	if spec.Integration == "claude-import" {
		return "@" + spelling + "\n"
	}
	return "## Kander Rules Entry\n\n" +
		"At the start of every session, read `" + spelling +
		"` and follow it as the Kander workflow rules entry.\n"
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

func claudeRulesImportPresent(text, entry, baseDir string) bool {
	candidates := ruleEntryCandidates(entry)
	body := stripCommentsAndFences(text)
	offset := 0
	atLine := regexp.MustCompile(`^@([^\s]+)$`)
	for _, raw := range splitKeepEnds(body) {
		line := strings.TrimSpace(raw)
		lineStart := offset
		offset += len(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := atLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		ref := strings.TrimSpace(match[1])
		accepted := false
		if _, ok := candidates[ref]; ok {
			accepted = true
		}
		if !accepted {
			expanded := ref
			if strings.HasPrefix(ref, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					expanded = home + ref[1:]
				}
			}
			if !filepath.IsAbs(expanded) {
				expanded = filepath.Join(baseDir, expanded)
			}
			accepted = sameResolved(expanded, entry)
		}
		if !accepted {
			continue
		}
		context := candidateContext(body, lineStart, offset, true)
		if negationRE.MatchString(context) {
			continue
		}
		return true
	}
	return false
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
