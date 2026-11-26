package board

import (
	"fmt"
	"os"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
)

// The board tests drive the card library through these helpers. Each one makes
// the library call a fixture needs against the board of the current test and
// reports a failure on stderr the way a command would, so a test keeps asserting
// the library's behavior and messages without a command front end.

func reportCard(err error) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
	return 0
}

// newCard creates a backlog card in the language the test configuration names.
func newCard(kind, slug, title string, large bool) int {
	return newCardIn(kind, slug, title, "", large)
}

// newCardIn is newCard with an explicit card language; empty takes the configured one.
func newCardIn(kind, slug, title, language string, large bool) int {
	root, err := BoardRoot()
	if err != nil {
		return reportCard(err)
	}
	if language == "" {
		cfg, err := config.Load(false)
		if err != nil {
			return reportCard(err)
		}
		language = cfg.AgentLanguage
	}
	language, err = config.ValidateAgentLanguage(language)
	if err != nil {
		return reportCard(err)
	}
	target, err := NewTask(root, kind, slug, title, language, large)
	if err == nil {
		fmt.Println(target)
	}
	return reportCard(err)
}

func moveCard(id, state string) int { return moveCardWith(id, state, MoveOptions{}) }

func moveCardWith(id, state string, options MoveOptions) int {
	root, err := BoardRoot()
	if err != nil {
		return reportCard(err)
	}
	loaded, err := LoadBoard(root)
	if err != nil {
		return reportCard(err)
	}
	entry, err := Locate(loaded, id)
	if err != nil {
		return reportCard(err)
	}
	moved, err := MoveWithOptions(entry, root, state, options)
	if err == nil {
		fmt.Println(moved.Path)
	}
	return reportCard(err)
}

func pickCard(id string) int { return moveCard(id, "todo") }

// checkBoardCmd runs the structural check of the current test board and prints
// its report the way a command would.
func checkBoardCmd(all bool, tasks ...string) int {
	root, err := BoardRoot()
	if err != nil {
		return reportCard(err)
	}
	code, stdout, stderr, err := CheckBoard(root, tasks, all)
	if err != nil {
		return reportCard(err)
	}
	for _, line := range stderr {
		fmt.Fprintln(os.Stderr, line)
	}
	os.Stdout.WriteString(stdout)
	return code
}

// loadBoardCmd loads the current test board, which repairs a missing review
// directory and rejects an unusable state directory.
func loadBoardCmd() int {
	root, err := BoardRoot()
	if err != nil {
		return reportCard(err)
	}
	_, err = LoadBoard(root)
	return reportCard(err)
}

// initBoardCmd initializes the board of project and prints where it landed.
func initBoardCmd(project string) int {
	root, _, rules, _, err := InitBoardWithOptions(project, InitOptions{})
	if err == nil {
		fmt.Println(t("board.initialized", root))
		fmt.Println(t("board.rules", rules))
	}
	return reportCard(err)
}

// endDispatchCmd ends a dispatch as failed or cancelled by verb. A decision
// that is present but blank is refused before any write.
func endDispatchCmd(verb, task, id string, revision uint64, reason, decision string) int {
	root, err := BoardRoot()
	if err != nil {
		return reportCard(err)
	}
	state := DispatchFailed
	if verb == "cancel" {
		state = DispatchCancelled
	}
	var decisions []string
	if decision != "" {
		if strings.TrimSpace(decision) == "" {
			return reportCard(fmt.Errorf("blank decision"))
		}
		decisions = append(decisions, decision)
	}
	return reportCard(EndDispatch(root, task, id, revision, state, reason, decisions...))
}
