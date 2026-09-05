package menu

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/config"
)

var negationRE = regexp.MustCompile(`(?i)(禁用|废弃|停用|不要遵守|勿遵守|不遵守|不要使用|不使用|请勿使用|未导入|尚未导入|不再生效|已失效|请忽略|do not follow|do not use|disabled|deprecated|\bignore\b)`)

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

func agentRulesTarget(agent string, paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject {
		if agent == "claude" {
			return filepath.Join(paths.ProjectRoot, "CLAUDE.md")
		}
		return filepath.Join(paths.ProjectRoot, "AGENTS.md")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch agent {
	case "codex":
		return filepath.Join(home, ".codex", "AGENTS.md")
	case "claude":
		return filepath.Join(home, ".claude", "CLAUDE.md")
	case "cursor":
		return filepath.Join(home, ".cursor", "AGENTS.md")
	default:
		return filepath.Join(home, ".grok", "AGENTS.md")
	}
}

func rulesIntegration(agent string, paths config.InstallPaths) (bool, string) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return false, config.Text("config.project_install_paths_are_missing_the_main_worktree")
	}
	entry := rulesEntry(paths)
	target := agentRulesTarget(agent, paths)
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
			if paths.Mode == config.ModeProject && agent != "claude" {
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
	if agent == "claude" {
		integrated = claudeRulesImportPresent(string(text), entry, filepath.Dir(target))
	} else {
		integrated = mergedRulesPresent(string(text), entry)
	}
	if integrated {
		return true, target
	}
	return false, config.Text(
		"menu.does_not_import_or_include_kander_agents_md", target,
	)
}
