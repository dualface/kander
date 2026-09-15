package launch

import (
	"errors"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

// ChatPreview contains the execution agent and launcher a chat session would
// use. The TUI resolves it when the chat box opens, so an unusable launcher is
// reported before the user submits anything.
type ChatPreview struct {
	Agent    string
	Launcher string
}

// ChatRequest is one message the user typed into the chat box.
type ChatRequest struct {
	Root    string
	Message string
}

// ChatResult describes one started chat session. Address is the complete
// terminal address (launcher prefix included) the focus path accepts.
type ChatResult struct {
	Agent    string
	Launcher string
	Address  string
	Warnings []string
}

// PreviewChat resolves the Chat Agent and the configured launcher without
// allocating a session or creating a container.
func PreviewChat() (ChatPreview, error) {
	cfg, err := loadEffective()
	if err != nil {
		return ChatPreview{}, err
	}
	settings, launcher, err := chatDefaults(cfg)
	if err != nil {
		return ChatPreview{}, err
	}
	return ChatPreview{Agent: settings.Agent, Launcher: launcher}, nil
}

// StartChat starts one session that owns no card and hands it the message. The
// message is written verbatim to a temporary task file and the agent receives
// only the one-line instruction to read it. The board is never read or
// written. A failed start closes the container it created and removes the
// task file.
func StartChat(request ChatRequest) (result ChatResult, err error) {
	if strings.TrimSpace(request.Message) == "" {
		return result, launchError("launch.message_must_not_be_empty", "chat")
	}
	if strings.TrimSpace(request.Root) == "" {
		return result, launchError("launch.board_root_is_required")
	}
	cfg, err := loadEffective()
	if err != nil {
		return result, err
	}
	settings, launcher, err := chatDefaults(cfg)
	if err != nil {
		return result, err
	}
	agent := settings.Agent
	plan, err := prepareLaunch(launcher, parentDir(request.Root), "chat")
	if err != nil {
		return result, err
	}
	if err := applyAgentDelivery(&plan, cfg, agent); err != nil {
		return result, err
	}
	plan.warning = func(message string) { result.Warnings = append(result.Warnings, message) }
	program, err := requireAgentProgram(agent, cfg)
	if err != nil {
		return result, err
	}
	session, err := newAgentSession(agent, program, cfg)
	if err != nil {
		return result, err
	}
	taskFile, err := createTaskFile(request.Message, "kander-chat-")
	if err != nil {
		return result, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt := taskInstruction(t("launch.prompt.chat_head"), taskFile)
	args, err := chatAgentArguments(agent, settings.Model, settings.Effort, session, cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	// A chat session owns no card, so it records no pane session marker and
	// reports no Herdr agent session; those identities belong to card launches.
	outcome, err := launchAgent(plan, request.Root, chatWindowName(), inv, nil, nil, nil)
	if err != nil {
		// A container that could not be closed is left behind, so its close
		// error is reported next to the start error.
		var failure *LaunchFailure
		if errors.As(err, &failure) && failure.CloseError != "" {
			return result, launchError("launch.value", failure.Err.Error(), failure.CloseError)
		}
		return result, err
	}
	handedOff = true
	result.Agent, result.Launcher = agent, plan.Launcher
	result.Address = terminal.FormatAddress(plan.backend(), plan.address(outcome))
	return result, nil
}

// chatDefaults resolves the Chat Agent/model/effort and the configured launcher.
// The session has no card, so it uses ChatSettingsFor, and it must start in a
// terminal container: a launcher that occupies the caller's terminal would take
// the board's terminal away and leave nothing to focus.
func chatDefaults(cfg *config.Config) (config.ChatSettings, string, error) {
	_, launcher, err := sessionDefaults(cfg, "large", "", "")
	if err != nil {
		return config.ChatSettings{}, "", err
	}
	settings := config.ChatSettingsFor(cfg)
	if !terminal.HasCapability(launcher, func(c terminal.Capabilities) bool { return c.Container }) {
		return config.ChatSettings{}, "", launchError("launch.chat_requires_container_launcher", launcher)
	}
	return settings, launcher, nil
}

func chatAgentArguments(agent, model, effort string, session AgentSession, cfg *config.Config) ([]string, error) {
	return expandAgentInvocation(agent, model, effort, session, false, cfg)
}

// chatWindowName names the container after the local start time, so repeated
// chats stay distinguishable: chat-YYYYMMDD-HHMMSS.
func chatWindowName() string {
	return "chat-" + nowFn().Format("20060102-150405")
}
