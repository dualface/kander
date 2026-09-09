package config

import (
	"fmt"
	"strings"
)

func FormatLanguageSummary(language string) string {
	labels, ok := languageLabels[language]
	if !ok {
		return language
	}
	return Text(labels)
}

func FormatKanbanAgentsSummary(cfg *Config) string {
	agents := cfg.KanbanAgents
	unique := map[string]struct{}{}
	for _, agent := range agents {
		unique[agent] = struct{}{}
	}
	if len(unique) == 1 {
		return agents["large"]
	}
	return fmt.Sprintf("%s %s / %s %s",
		Text("config.large"), agents["large"],
		Text("config.small"), agents["small"],
	)
}

// KanbanModelFor returns the model id in force for one task scale.
// An empty scale model falls back to the shared "model" key of legacy configs.
func KanbanModelFor(entry map[string]string, scale string) string {
	if model := entry[scale+"_model"]; model != "" {
		return model
	}
	return entry["model"]
}

func FormatKanbanModelSummary(cfg *Config, agent string, entry map[string]string) string {
	cliDefault := Text("config.cli_default")
	large := KanbanModelFor(entry, "large")
	if large == "" {
		large = cliDefault
	}
	small := KanbanModelFor(entry, "small")
	if small == "" {
		small = cliDefault
	}
	if !AgentSupportsEffort(cfg, agent) {
		return fmt.Sprintf("%s %s / %s %s",
			Text("config.large"), large,
			Text("config.small"), small,
		)
	}
	return fmt.Sprintf("%s %s (%s) / %s %s (%s)",
		Text("config.large"), large, entry["large_effort"],
		Text("config.small"), small, entry["small_effort"],
	)
}

func FormatReviewModelSummary(entry map[string]string) string {
	if _, ok := entry["effort"]; !ok {
		return formatModelEffort(entry["model"], "")
	}
	return formatModelEffort(entry["model"], entry["effort"])
}

func formatModelEffort(model, effort string) string {
	if model == "" {
		model = Text("config.cli_default")
	}
	if effort == "" {
		return model
	}
	return fmt.Sprintf("%s (%s)", model, effort)
}

func formatReviewRoleModes(stages map[string]string) string {
	parts := make([]string, 0, len(ReviewRoles))
	for _, role := range ReviewRoles {
		parts = append(parts, role+"="+stages[role])
	}
	return strings.Join(parts, " ")
}

// FormatReviewStagesSummary returns one line when both scales match, or one line
// per scale (large then small) when they differ.
func FormatReviewStagesSummary(stages map[string]map[string]string) []string {
	large := formatReviewRoleModes(stages["large"])
	small := formatReviewRoleModes(stages["small"])
	if large == small {
		return []string{large}
	}
	return []string{
		Text("config.large") + ": " + large,
		Text("config.small") + ": " + small,
	}
}

// FormatConfigLines produces the read-only human-readable output of kander config.
func FormatConfigLines(cfg *Config) ([]string, error) {
	effective, err := Effective(cfg)
	if err != nil {
		return nil, err
	}
	status := Text("config.complete")
	if !cfg.WelcomeComplete {
		status = Text("config.incomplete")
	}
	var lines []string
	overlayPath, overlayErr := OverlayPath("")
	if overlayErr != nil {
		return nil, overlayErr
	}
	if overlayPath != "" {
		lines = append(lines, Text("config.overlay_file")+": "+overlayPath)
	}
	lines = append(lines, Text("config.welcome")+": "+status)
	lines = append(lines, AgentWarnings(effective)...)
	lines = append(lines, Text("config.kanban_agent")+": "+FormatKanbanAgentsSummary(effective))
	lines = append(lines, Text("config.launcher")+": "+effective.Launcher)
	inUse := ExecutionAgentsInUse(effective)
	for _, agent := range inUse {
		entry := effective.Models.Kanban[agent]
		label := ""
		if len(inUse) > 1 {
			label = " " + agent
		}
		lines = append(lines, Text("config.kanban_model")+label+": "+FormatKanbanModelSummary(effective, agent, entry))
	}
	for _, role := range ReviewRoles {
		reviewer := effective.Reviewers[role]
		model, effort := ReviewModelFor(effective, reviewer, role)
		lines = append(lines, role+": "+reviewer+" "+formatModelEffort(model, effort))
	}
	stageSummaries := FormatReviewStagesSummary(effective.ReviewStages)
	if len(stageSummaries) == 1 {
		lines = append(lines, Text("config.review_stages")+": "+stageSummaries[0])
	} else {
		for _, summary := range stageSummaries {
			lines = append(lines, Text("config.review_stages")+" "+summary)
		}
	}
	lines = append(lines, Text("rules.modules")+": "+FormatRulesSummary(effective.Rules))
	stored := ConfiguredLanguage()
	language := stored
	if language == "" {
		language = ResolveLanguage()
	}
	lines = append(lines, Text("config.language")+": "+FormatLanguageSummary(language))
	lines = append(lines, Text("config.agent_language")+": "+effective.AgentLanguage)
	return lines, nil
}

// ReviewModelLines returns two lines, model and effort, for the review entry point to read.
func ReviewModelLines(cfg *Config, agent string) ([]string, error) {
	effective, err := Effective(cfg)
	if err != nil {
		return nil, err
	}
	if !HasReviewTemplate(effective, agent) {
		return nil, choiceError("agent", strings.Join(ReviewAgentNames(effective), ", "))
	}
	entry := effective.Models.Review[agent]
	return []string{entry["model"], entry["effort"]}, nil
}

// ReviewStageLines returns auto|skip|required for large then small, each in PM/CSA/Hacker/QA order.
func ReviewStageLines(cfg *Config) ([]string, error) {
	effective, err := Effective(cfg)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(TaskScales)*len(ReviewRoles))
	for _, scale := range TaskScales {
		for _, role := range ReviewRoles {
			lines = append(lines, effective.ReviewStages[scale][role])
		}
	}
	return lines, nil
}
