package launch

import (
	"strings"
	"time"
)

// Most agent CLIs take the opening prompt as a positional argument. kimi-code does not: a bare
// argument is parsed as a subcommand ("unknown command"), and its --prompt flag is one-shot
// print mode that cannot be combined with the --auto permission mode an execution agent needs.
// For such dialects the agent is started with no prompt and the instruction is typed into the
// pane once its interface is up, which is exactly how a person would drive it.
func dialectAcceptsPromptArgument(dialect string) bool { return dialect != "kimi" }

// paneReadyMarker is text a dialect only shows once it can accept typed input. Typing before it
// appears would be swallowed by the still-booting interface.
//
// kimi-code is matched on its status bar rather than its start-up banner: the banner is
// decorative and has already been reworded between releases, while the status bar is only drawn
// once the input box is live. It is also not drawn while a first-run question is on screen —
// kimi-code asks whether to trust a folder the first time it opens one — so a pane waiting on
// that answer correctly reads as not ready.
func paneReadyMarker(dialect string) string {
	if dialect == "kimi" {
		return "context:"
	}
	return ""
}

const (
	panePromptWaitMS    = 60000
	panePromptPollDelay = 500 * time.Millisecond
)

// typePanePrompt waits for the agent interface to come up in the pane and then types the
// instruction into it, followed by Enter.
func typePanePrompt(plan LaunchPlan, pane, dialect, instruction string) error {
	if err := rejectPaneControlChars(instruction); err != nil {
		return err
	}
	marker := paneReadyMarker(dialect)
	if plan.Launcher == "herdr" {
		if err := waitHerdrPaneMarker(plan.HerdrBin, pane, marker); err != nil {
			return err
		}
		res, err := herdrCapture(plan.HerdrBin, []string{"agent", "prompt", pane, instruction}, 0)
		if err != nil {
			return err
		}
		if res.Code != 0 {
			return launchError("launch.agent_prompt_could_not_be_typed_into_the_pane", herdrFailureDetail(res))
		}
		return nil
	}
	if err := waitTmuxPaneMarker(plan.Tmux, pane, marker); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"send-keys", "-t", pane, "-l", instruction},
		{"send-keys", "-t", pane, "Enter"},
	} {
		if res := tmuxCapture(plan.Tmux, args...); res.Code != 0 {
			return launchError(
				"launch.agent_prompt_could_not_be_typed_into_the_pane",
				orExit(trimNL(res.Stderr), res.Code),
			)
		}
	}
	return nil
}

func waitHerdrPaneMarker(herdr, pane, marker string) error {
	if marker == "" {
		return nil
	}
	res, err := herdrCapture(herdr, []string{
		"pane", "wait-output", pane, "--match", marker, "--source", "recent",
		"--timeout", itoa(panePromptWaitMS),
	}, 0)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return launchError("launch.agent_interface_did_not_come_up_in_the_pane", marker)
	}
	return nil
}

// tmux has no wait-for-output primitive, so the pane is polled until the marker shows up.
func waitTmuxPaneMarker(tmux, pane, marker string) error {
	if marker == "" {
		return nil
	}
	deadline := nowFn().Add(panePromptWaitMS * time.Millisecond)
	for {
		res := tmuxCapture(tmux, "capture-pane", "-p", "-t", pane)
		if res.Code == 0 && strings.Contains(res.Stdout, marker) {
			return nil
		}
		if !nowFn().Before(deadline) {
			return launchError("launch.agent_interface_did_not_come_up_in_the_pane", marker)
		}
		sleepFn(panePromptPollDelay)
	}
}

// startArguments splits the opening instruction between argv and the pane: dialects that accept
// a positional prompt carry it in argv, the rest have it typed in after launch. A dialect that
// has to be typed into needs a pane, so the launchers that spawn a bare process are refused
// with a diagnostic instead of silently starting an agent that was never told what to do.
func startArguments(plan LaunchPlan, dialect string, args []string, prompt string) (argv []string, typed string, err error) {
	if dialectAcceptsPromptArgument(dialect) {
		return append(args, prompt), "", nil
	}
	if !paneLauncher(plan.Launcher) {
		return nil, "", launchError(
			"launch.agent_requires_a_pane_launcher_for_its_prompt", dialect, plan.Launcher,
		)
	}
	return args, prompt, nil
}

// promptTyper returns the launchAgent callback for an instruction that has to be typed, or nil
// when the prompt already travels in argv.
func promptTyper(plan LaunchPlan, dialect, typed string) func(pane string) error {
	if typed == "" {
		return nil
	}
	return func(pane string) error { return typePanePrompt(plan, pane, dialect, typed) }
}
