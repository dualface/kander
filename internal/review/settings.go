package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

const (
	treeObserveInterval = 0.2
	windowsJobBootstrap = "--kander-windows-job-bootstrap"
	emptyReviewContext  = "None provided."
)

type gateError struct {
	message string
	code    int
}

func (e *gateError) Error() string { return e.message }

func newGate(code int, id string, args ...any) *gateError {
	return &gateError{message: config.Text(id, args...), code: code}
}

func newGateMsg(code int, message string) *gateError {
	return &gateError{message: message, code: code}
}

type agentSettings struct {
	name                  string
	checkInterval         int
	maxRuntime            int
	executable            string
	model                 string
	effort                string
	reviewHome            string
	outputName            string
	inspectionRules       string
	spawnsHelperProcesses bool
}

type reviewContext struct {
	agent         string
	settings      agentSettings
	root          string
	base          string
	commit        string
	role          string
	taskContext   string
	taskSpec      string
	reviewContext string
	reviewed      string
	program       process.AgentProgram
	tempRoot      string
	// reportLanguage is the config agent_language; the reviewer writes its report in it. Empty leaves the language unspecified.
	reportLanguage string
}

func userError(message string) {
	fmt.Fprintln(os.Stderr, config.Text("review.error")+": "+message)
}

func usage() {
	fmt.Fprintln(os.Stderr, config.Text("review.usage"))
}

func positiveInteger(value, variable string) (int, error) {
	if matched, _ := regexp.MatchString(`^[1-9][0-9]*$`, value); !matched {
		return 0, newGate(2, "review.must_be_a_positive_integer", variable)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, newGate(2, "review.must_be_a_positive_integer", variable)
	}
	return n, nil
}

// configuredModel returns the model and reasoning effort in force for a review role:
// the role's own override first, falling back to the value of its selected reviewer when empty.
func configuredModel(agent, role string) (string, string, bool) {
	cfg, err := config.Effective(nil)
	if err != nil || cfg == nil {
		return "", "", false
	}
	entry, ok := cfg.Models.Review[agent]
	if !ok {
		return "", "", false
	}
	model, effort := config.ReviewModelFor(cfg, agent, role)
	if model == "" {
		if _, has := entry["model"]; !has {
			return "", "", false
		}
	}
	if _, hasEffort := entry["effort"]; !hasEffort {
		return model, "", true
	}
	if effort == "" {
		return "", "", false
	}
	return model, effort, true
}

func userHome() string {
	if runtime.GOOS == "windows" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
	}
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return home
}

func agentSettingsFor(agent, role string) (agentSettings, error) {
	home := userHome()
	type def struct {
		name, prefix, executable, defaultModel, reviewHome, homeError, output, inspection string
		helpers                                                                           bool
	}
	definitions := map[string]def{
		"codex": {
			name: "Codex", prefix: "CODEX", executable: "codex", defaultModel: "gpt-5.6-sol",
			reviewHome: getenvDefault("CODEX_HOME", filepath.Join(home, ".codex")),
			homeError:  "CODEX_REVIEW_HOME", output: "output.txt",
			inspection: "Use only read-only filesystem and shell operations needed to inspect code.",
		},
		"claude": {
			name: "Claude", prefix: "CLAUDE", executable: "claude", defaultModel: "opus",
			reviewHome: getenvDefault("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")),
			homeError:  "CLAUDE_CONFIG_DIR", output: "output.json",
			inspection: "Use only the Read, Grep, and Glob tools to inspect code.",
		},
		"grok": {
			name: "Grok", prefix: "GROK", executable: "grok", defaultModel: "",
			reviewHome: getenvDefault("GROK_HOME", filepath.Join(home, ".grok")),
			homeError:  "GROK_REVIEW_HOME", output: "output.json",
			inspection: "Use only read_file, grep, and list_dir to inspect code.",
		},
		"cursor": {
			name: "Cursor", prefix: "CURSOR", executable: "cursor-agent", defaultModel: "cursor-grok-4.6-xhigh",
			reviewHome: getenvDefault("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor")),
			homeError:  "CURSOR_CONFIG_DIR", output: "output.json",
			inspection: "Prefer read-only inspection. Do not modify the target worktree; the review gate fails if HEAD moves or the worktree is dirty.",
			helpers:    true,
		},
	}
	definition, ok := definitions[agent]
	if !ok {
		return agentSettings{}, newGate(2, "review.unsupported_reviewer_agent", agent)
	}
	checkName := definition.prefix + "_REVIEW_CHECK_INTERVAL_SECONDS"
	runtimeName := definition.prefix + "_REVIEW_MAX_RUNTIME_SECONDS"
	checkInterval, err := positiveInteger(getenvDefault(checkName, "600"), checkName)
	if err != nil {
		return agentSettings{}, err
	}
	maxRuntime, err := positiveInteger(getenvDefault(runtimeName, "1800"), runtimeName)
	if err != nil {
		return agentSettings{}, err
	}
	modelOverride := os.Getenv(definition.prefix + "_REVIEW_MODEL")
	effortOverride := os.Getenv(definition.prefix + "_REVIEW_REASONING_EFFORT")
	cfgModel, cfgEffort, cfgOK := configuredModel(agent, role)
	model := definition.defaultModel
	if modelOverride != "" {
		model = modelOverride
	} else if cfgOK {
		model = cfgModel
	}
	effort := "high"
	if effortOverride != "" {
		effort = effortOverride
	} else if cfgOK {
		effort = cfgEffort
		if effort == "" {
			effort = "high"
		}
	}
	reviewHome := definition.reviewHome
	if !filepath.IsAbs(reviewHome) {
		return agentSettings{}, newGate(2,
			"review.must_be_an_absolute_path", definition.homeError, reviewHome,
		)
	}
	return agentSettings{
		name:                  definition.name,
		checkInterval:         checkInterval,
		maxRuntime:            maxRuntime,
		executable:            getenvDefault(definition.prefix+"_REVIEW_BIN", definition.executable),
		model:                 model,
		effort:                effort,
		reviewHome:            reviewHome,
		outputName:            definition.output,
		inspectionRules:       definition.inspection,
		spawnsHelperProcesses: definition.helpers,
	}, nil
}

func getenvDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func looksLikeAbsolutePath(value string) bool {
	if filepath.IsAbs(value) {
		return true
	}
	if runtime.GOOS == "windows" {
		return len(value) >= 3 && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
	}
	return false
}

func containsAgent(name string) bool {
	for _, agent := range config.ReviewAgents {
		if agent == name {
			return true
		}
	}
	return false
}

func splitAgentArgs(args []string) (agent string, rest []string, err error) {
	if len(args) == 0 {
		return "", nil, errUsage
	}
	if containsAgent(args[0]) {
		return args[0], args[1:], nil
	}
	if looksLikeAbsolutePath(args[0]) {
		return "", args, nil
	}
	if len(args) >= 2 && looksLikeAbsolutePath(args[1]) {
		return "", nil, newGate(2, "review.unsupported_reviewer_agent", args[0])
	}
	return "", args, nil
}

var errUsage = errors.New("usage")

// reportLanguageFromConfig returns the agent_language the review report should be written in.
// It is empty when no config.json exists or the file does not validate; a config that has not
// finished initialization still counts, because its explicit agent_language is the user's choice.
func reportLanguageFromConfig() string {
	cfg, err := config.Load(false)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.AgentLanguage
}

func reviewerFromConfig(role string) string {
	cfg, err := config.Effective(nil)
	if err != nil || cfg == nil {
		return "codex"
	}
	if agent := cfg.Reviewers[role]; containsAgent(agent) {
		return agent
	}
	return "codex"
}
