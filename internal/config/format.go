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

func FormatKanbanModelSummary(agent string, entry map[string]string) string {
	cliDefault := Text("config.cli_default")
	large := KanbanModelFor(entry, "large")
	if large == "" {
		large = cliDefault
	}
	small := KanbanModelFor(entry, "small")
	if small == "" {
		small = cliDefault
	}
	if agent == "cursor" {
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

func FormatReviewStagesSummary(stages map[string]string) string {
	parts := make([]string, 0, len(ReviewRoles))
	for _, role := range ReviewRoles {
		parts = append(parts, role+"="+stages[role])
	}
	return strings.Join(parts, " ")
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
	lines = append(lines, Text("config.welcome")+": "+status)
	lines = append(lines, Text("config.kanban_agent")+": "+FormatKanbanAgentsSummary(effective))
	lines = append(lines, Text("config.launcher")+": "+effective.Launcher)
	inUse := ExecutionAgentsInUse(effective)
	for _, agent := range inUse {
		entry := effective.Models.Kanban[agent]
		label := ""
		if len(inUse) > 1 {
			label = " " + agent
		}
		lines = append(lines, Text("config.kanban_model")+label+": "+FormatKanbanModelSummary(agent, entry))
	}
	for _, role := range ReviewRoles {
		reviewer := effective.Reviewers[role]
		model, effort := ReviewModelFor(effective, reviewer, role)
		lines = append(lines, role+": "+reviewer+" "+formatModelEffort(model, effort))
	}
	lines = append(lines, Text("config.review_stages")+": "+FormatReviewStagesSummary(effective.ReviewStages))
	lines = append(lines, Text("rules.modules")+": "+FormatRulesSummary(effective.Rules))
	stored := ConfiguredLanguage()
	language := stored
	if language == "" {
		language = ResolveLanguage()
	}
	lines = append(lines, Text("config.language")+": "+FormatLanguageSummary(language))
	return lines, nil
}

// ReviewModelLines returns two lines, model and effort, for the review entry point to read.
func ReviewModelLines(cfg *Config, agent string) ([]string, error) {
	effective, err := Effective(cfg)
	if err != nil {
		return nil, err
	}
	if !contains(ReviewAgents, agent) {
		return nil, choiceError("agent", strings.Join(ReviewAgents, ", "))
	}
	entry := effective.Models.Review[agent]
	return []string{entry["model"], entry["effort"]}, nil
}

// ReviewStageLines prints auto|skip|required in PM/CSA/Hacker/QA order.
func ReviewStageLines(cfg *Config) ([]string, error) {
	effective, err := Effective(cfg)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(ReviewRoles))
	for _, role := range ReviewRoles {
		lines = append(lines, effective.ReviewStages[role])
	}
	return lines, nil
}
