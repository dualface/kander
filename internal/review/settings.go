package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

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
	homePolicy            string
	outputName            string
	inspectionRules       string
	spawnsHelperProcesses bool
	snapshotSpec          bool
	stdin                 string
	cwd                   string
	env                   map[string]string
	reviewArgs            []string
	output                process.OutputSpec
	promptFiles           []config.ReviewPromptFile
}

type reviewContext struct {
	archive       *archiveExecution
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
	program         process.AgentProgram
	tempRoot        string
	instruction     string
	promptFilePaths map[string]string
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
	cfg, err := config.Load(false)
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
	cfg, err := config.Effective(nil)
	if err != nil {
		cfg = nil
	}
	if !config.HasReviewTemplate(cfg, agent) {
		return agentSettings{}, newGate(2, "review.unsupported_reviewer_agent", agent)
	}
	def := config.AgentFor(cfg, agent)
	if def.Review == nil || def.Args == nil || def.Args.Review == nil {
		return agentSettings{}, newGate(2, "review.unsupported_reviewer_agent", agent)
	}
	review := def.Review
	prefix := strings.ToUpper(agent)
	checkName := prefix + "_REVIEW_CHECK_INTERVAL_SECONDS"
	runtimeName := prefix + "_REVIEW_MAX_RUNTIME_SECONDS"
	checkInterval, err := positiveInteger(getenvDefault(checkName, "600"), checkName)
	if err != nil {
		return agentSettings{}, err
	}
	maxRuntime, err := positiveInteger(getenvDefault(runtimeName, "1800"), runtimeName)
	if err != nil {
		return agentSettings{}, err
	}
	modelOverride := os.Getenv(prefix + "_REVIEW_MODEL")
	effortOverride := os.Getenv(prefix + "_REVIEW_REASONING_EFFORT")
	cfgModel, cfgEffort, cfgOK := configuredModel(agent, role)
	model := ""
	if defaults := config.DefaultModels().Review[agent]; defaults != nil {
		model = defaults["model"]
	}
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
	homeDefault := filepath.Join(userHome(), "."+agent)
	reviewHome := homeDefault
	homeError := "HOME"
	if review.HomeEnv != "" {
		reviewHome = getenvDefault(review.HomeEnv, homeDefault)
		homeError = review.HomeEnv
	}
	if !filepath.IsAbs(reviewHome) {
		return agentSettings{}, newGate(2,
			"review.must_be_an_absolute_path", homeError, reviewHome,
		)
	}
	homePolicy := review.HomePolicy
	if homePolicy == "" {
		homePolicy = config.ReviewHomeRequired
	}
	stdin := review.Stdin
	if stdin == "" {
		stdin = config.ReviewStdinInstruction
	}
	output := process.OutputSpec{}
	if review.Output != nil {
		output = *review.Output
	}
	env := map[string]string{}
	for key, value := range review.Env {
		env[key] = value
	}
	// ReviewExecutable keeps built-in review off the execution path overlay.
	return agentSettings{
		name:                  config.AgentDisplayName(agent),
		checkInterval:         checkInterval,
		maxRuntime:            maxRuntime,
		executable:            config.ReviewExecutable(cfg, agent),
		model:                 model,
		effort:                effort,
		reviewHome:            reviewHome,
		homePolicy:            homePolicy,
		outputName:            review.OutputName,
		inspectionRules:       review.Inspection,
		spawnsHelperProcesses: review.SpawnsHelpers,
		snapshotSpec:          review.SnapshotSpec,
		stdin:                 stdin,
		cwd:                   review.CWD,
		env:                   env,
		reviewArgs:            append([]string{}, def.Args.Review...),
		output:                output,
		promptFiles:           append([]config.ReviewPromptFile{}, review.PromptFiles...),
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
	cfg, err := config.Effective(nil)
	if err != nil {
		cfg = nil
	}
	return config.HasReviewTemplate(cfg, name)
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
// A missing or invalid config is an error rather than an empty language. A config that has not
// finished initialization still counts, because its explicit agent_language is the user's choice.
func reportLanguageFromConfig() (string, error) {
	cfg, err := config.Load(false)
	if err != nil {
		return "", err
	}
	return cfg.AgentLanguage, nil
}

func reviewerFromConfig(role string) (string, error) {
	cfg, err := config.Load(false)
	if err != nil {
		return "", err
	}
	agent := cfg.Reviewers[role]
	if !containsAgent(agent) {
		return "", newGate(1, "review.unsupported_reviewer_agent", agent)
	}
	return agent, nil
}
