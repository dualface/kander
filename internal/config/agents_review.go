package config

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/process"
)

const (
	ReviewCWDRoot          = "root"
	ReviewCWDRuntime       = "runtime"
	ReviewHomeRequired     = "required"
	ReviewHomeOptional     = "optional"
	ReviewStdinInstruction = "instruction"
	ReviewStdinNone        = "none"
)

// AgentReview is the reviewer invocation declared next to args.review.
type AgentReview struct {
	Env           map[string]string   `json:"env,omitempty"`
	CWD           string              `json:"cwd,omitempty"`
	HomeEnv       string              `json:"home_env,omitempty"`
	HomePolicy    string              `json:"home_policy,omitempty"`
	OutputName    string              `json:"output_name,omitempty"`
	Inspection    string              `json:"inspection,omitempty"`
	SpawnsHelpers bool                `json:"spawns_helpers,omitempty"`
	SnapshotSpec  bool                `json:"snapshot_spec,omitempty"`
	Path          string              `json:"path,omitempty"`
	Stdin         string              `json:"stdin,omitempty"`
	PromptFiles   []ReviewPromptFile  `json:"prompt_files,omitempty"`
	Output        *process.OutputSpec `json:"output,omitempty"`
}

// ReviewPromptFile is one extra prompt rendered into the review runtime.
type ReviewPromptFile struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Template string `json:"template"`
}

func ReviewArgvPlaceholders(files []ReviewPromptFile) []string {
	names := []string{"model", "effort", "root", "runtime", "home", "output", "prompt_file", "instruction"}
	for _, file := range files {
		names = append(names, "prompt_file:"+file.Name)
	}
	return names
}

func ReviewEnvPlaceholders() []string {
	return []string{"model", "effort", "root", "runtime", "home", "output"}
}

func ReviewPromptFilePlaceholders() []string {
	return []string{"inspection", "prompt", "role", "report_language", "root", "runtime", "output"}
}

func validateReviewerChoice(value any, cfg *Config, field string, reviewNames []string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", choiceError(field, strings.Join(reviewNames, ", "))
	}
	if HasAgent(cfg, text) && !HasReviewTemplate(cfg, text) {
		return "", configErrorf("config.reviewer_missing_template", field, text)
	}
	if !contains(reviewNames, text) {
		return "", choiceError(field, strings.Join(reviewNames, ", "))
	}
	return text, nil
}

func userDeclaredReviewTemplate(d AgentDefinition) bool {
	return d.Review != nil || (d.Args != nil && d.Args.Review != nil)
}

// HasReviewTemplate reports whether the agent may be selected as a reviewer.
// Built-in names keep the embedded args.review. A custom name must declare
// args.review or review itself; dialect backfill is not a declaration.
func HasReviewTemplate(cfg *Config, name string) bool {
	if contains(ExecutionAgents, name) {
		emb, ok := embeddedByName(name)
		return ok && emb.Args.Review != nil
	}
	if cfg == nil {
		return false
	}
	return userDeclaredReviewTemplate(cfg.Agents[name])
}

// ReviewAgentNames lists agents that declare a review argv template.
func ReviewAgentNames(cfg *Config) []string {
	var out []string
	for _, name := range AgentNames(cfg) {
		if HasReviewTemplate(cfg, name) {
			out = append(out, name)
		}
	}
	return out
}

// ReviewExecutable is the program used for review, not task execution.
// Built-in names keep *_REVIEW_BIN then the embedded path. Custom names use
// review.path and fall back to path.
func ReviewExecutable(cfg *Config, name string) string {
	if contains(ExecutionAgents, name) {
		if override := getenvReviewBin(name); override != "" {
			return override
		}
		return AgentExecutableName(name)
	}
	d := AgentFor(cfg, name)
	if d.Review != nil && d.Review.Path != "" {
		return d.Review.Path
	}
	return d.Path
}

func getenvReviewBin(name string) string {
	return strings.TrimSpace(os.Getenv(strings.ToUpper(name) + "_REVIEW_BIN"))
}

func cloneReview(src *AgentReview) *AgentReview {
	if src == nil {
		return nil
	}
	out := *src
	if src.Env != nil {
		out.Env = cloneStringMap(src.Env)
	}
	if src.PromptFiles != nil {
		out.PromptFiles = append([]ReviewPromptFile{}, src.PromptFiles...)
	}
	if src.Output != nil {
		cp := *src.Output
		if src.Output.Select != nil {
			cp.Select = append([]process.LineCondition{}, src.Output.Select...)
		}
		if src.Output.Success != nil {
			cp.Success = append([]process.LineCondition{}, src.Output.Success...)
		}
		if src.Output.Join != nil {
			join := *src.Output.Join
			cp.Join = &join
		}
		out.Output = &cp
	}
	return &out
}

func validateReviewDefinition(name string, d AgentDefinition) error {
	hasArgs := d.Args != nil && d.Args.Review != nil
	hasReview := d.Review != nil
	if !hasArgs && !hasReview {
		return nil
	}
	if hasArgs != hasReview {
		return agentDefinitionError(name, Text("config.agent_review_pair"))
	}
	review := d.Review
	if err := process.ValidateArgv(d.Args.Review, ReviewArgvPlaceholders(review.PromptFiles), process.TemplateArgv); err != nil {
		return agentDefinitionError(name, Text("config.agent_review_field", "args.review", err.Error()))
	}
	switch review.CWD {
	case ReviewCWDRoot, ReviewCWDRuntime:
	default:
		return agentDefinitionError(name, Text("config.agent_review_field", "review.cwd", review.CWD))
	}
	switch review.HomePolicy {
	case "", ReviewHomeRequired, ReviewHomeOptional:
	default:
		return agentDefinitionError(name, Text("config.agent_review_field", "review.home_policy", review.HomePolicy))
	}
	stdin := review.Stdin
	switch stdin {
	case "", ReviewStdinInstruction, ReviewStdinNone:
	default:
		return agentDefinitionError(name, Text("config.agent_review_field", "review.stdin", review.Stdin))
	}
	if stdin == "" {
		stdin = ReviewStdinInstruction
	}
	hasInstruction, err := argvHasPlaceholder(d.Args.Review, "instruction")
	if err != nil {
		return agentDefinitionError(name, Text("config.agent_review_field", "args.review", err.Error()))
	}
	if stdin == ReviewStdinNone && !hasInstruction {
		return agentDefinitionError(name, Text("config.agent_review_stdin_none"))
	}
	if stdin == ReviewStdinInstruction && hasInstruction {
		return agentDefinitionError(name, Text("config.agent_review_stdin_dup"))
	}
	if strings.TrimSpace(review.OutputName) == "" || strings.IndexFunc(review.OutputName, unicode.IsControl) >= 0 || strings.ContainsAny(review.OutputName, `/\`) {
		return agentDefinitionError(name, Text("config.agent_review_field", "review.output_name", review.OutputName))
	}
	if review.HomeEnv != "" && !validAgentText(review.HomeEnv) {
		return agentDefinitionError(name, Text("config.agent_review_field", "review.home_env", review.HomeEnv))
	}
	if strings.IndexFunc(review.Inspection, func(r rune) bool {
		return unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r'
	}) >= 0 {
		return agentDefinitionError(name, Text("config.agent_review_field", "review.inspection", "control"))
	}
	if review.Path != "" && !validateAgentProgram(review.Path) {
		return agentDefinitionError(name, Text("config.agent_path", review.Path))
	}
	if review.Output == nil {
		return agentDefinitionError(name, Text("config.agent_review_field", "review.output", "missing"))
	}
	if err := process.ValidateReviewOutput(*review.Output); err != nil {
		return agentDefinitionError(name, Text("config.agent_review_field", "review.output", err.Error()))
	}
	for key, value := range review.Env {
		if !validAgentText(key) {
			return agentDefinitionError(name, Text("config.agent_review_field", "review.env", key))
		}
		if err := process.ValidateTemplate(value, ReviewEnvPlaceholders(), process.TemplateArgv); err != nil {
			return agentDefinitionError(name, Text("config.agent_review_field", "review.env."+key, err.Error()))
		}
	}
	seen := map[string]struct{}{}
	for i, file := range review.PromptFiles {
		label := "review.prompt_files[" + strconv.Itoa(i) + "]"
		if !validPromptFileName(file.Name) {
			return agentDefinitionError(name, Text("config.agent_review_field", label, file.Name))
		}
		if _, ok := seen[file.Name]; ok {
			return agentDefinitionError(name, Text("config.agent_review_field", label, "duplicate "+file.Name))
		}
		seen[file.Name] = struct{}{}
		if err := validReviewPromptPath(file.Path); err != nil {
			return agentDefinitionError(name, Text("config.agent_review_field", label+".path", err.Error()))
		}
		if err := process.ValidateTemplate(file.Template, ReviewPromptFilePlaceholders(), process.TemplateText); err != nil {
			return agentDefinitionError(name, Text("config.agent_review_field", label+".template", err.Error()))
		}
	}
	if err := argvPromptFileRefsDeclared(d.Args.Review, seen); err != nil {
		return agentDefinitionError(name, Text("config.agent_review_field", "args.review", err.Error()))
	}
	return nil
}

func validPromptFileName(name string) bool {
	if !validAgentText(name) || strings.ContainsAny(name, `/:\\{}`) {
		return false
	}
	return true
}

func validReviewPromptPath(p string) error {
	if strings.TrimSpace(p) == "" || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return processSpec("path is empty or has control characters")
	}
	if path.IsAbs(p) || filepath.IsAbs(p) || strings.Contains(p, `\`) {
		return processSpec("path must be a portable relative path")
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return processSpec("path escapes the runtime")
	}
	if clean != p {
		return processSpec("path is not canonical")
	}
	return nil
}

func processSpec(msg string) error {
	return &process.SpecError{Field: "path", Msg: msg}
}

func argvHasPlaceholder(args []string, name string) (bool, error) {
	for _, arg := range args {
		found, err := templateHasName(arg, name)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func argvPromptFileRefsDeclared(args []string, declared map[string]struct{}) error {
	for _, arg := range args {
		if err := walkTemplateNames(arg, func(name string) error {
			if !strings.HasPrefix(name, "prompt_file:") {
				return nil
			}
			file := strings.TrimPrefix(name, "prompt_file:")
			if _, ok := declared[file]; !ok {
				return processSpec("unknown prompt file " + file)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func templateHasName(arg, want string) (bool, error) {
	found := false
	err := walkTemplateNames(arg, func(name string) error {
		if name == want {
			found = true
		}
		return nil
	})
	return found, err
}

func walkTemplateNames(arg string, fn func(string) error) error {
	for i := 0; i < len(arg); {
		switch arg[i] {
		case '{':
			if i+1 < len(arg) && arg[i+1] == '{' {
				i += 2
				continue
			}
			end := strings.IndexByte(arg[i+1:], '}')
			if end < 0 {
				return processSpec("unclosed {")
			}
			end += i + 1
			if err := fn(arg[i+1 : end]); err != nil {
				return err
			}
			i = end + 1
		case '}':
			if i+1 < len(arg) && arg[i+1] == '}' {
				i += 2
				continue
			}
			return processSpec("unescaped }")
		default:
			i++
		}
	}
	return nil
}
