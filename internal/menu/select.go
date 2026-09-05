package menu

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/dualface/kander/internal/config"
)

var (
	errMenuCancelled = errors.New("menu cancelled")
	errMenuEnded     = errors.New("menu ended")
	errInputEnded    = errors.New("input ended")
)

// choice 是 Choice 的包内别名, 保留行式菜单里的旧写法.
type choice = Choice

var pendingMu sync.Mutex
var pendingBytes []byte

func cursesMenuAvailable() bool {
	if isWindowsOS() {
		return false
	}
	return stdinStderrTTY()
}

func digitTarget(buffer string, count int) int {
	if buffer == "" {
		return -1
	}
	number, err := strconv.Atoi(buffer)
	if err != nil {
		return -1
	}
	if number >= 1 && number <= count {
		return number - 1
	}
	return -1
}

func digitPrefixPossible(buffer string, count int) bool {
	for index := 1; index <= count; index++ {
		if strings.HasPrefix(strconv.Itoa(index), buffer) {
			return true
		}
	}
	return false
}

func choiceLabels(choices []choice, defaultValue string) []string {
	labels := make([]string, 0, len(choices))
	for _, item := range choices {
		suffix := ""
		if item.Value == defaultValue {
			suffix = config.Text("menu.current")
		}
		labels = append(labels, item.Label+suffix)
	}
	return labels
}

func askChoice(prompt string, choices []choice, defaultValue string) (string, error) {
	if len(choices) == 0 {
		return "", errors.New(config.Text("menu.no_choices_available", prompt))
	}
	found := false
	for _, item := range choices {
		if item.Value == defaultValue {
			found = true
			break
		}
	}
	if !found {
		defaultValue = choices[0].Value
	}
	defaultIndex := 0
	for i, item := range choices {
		if item.Value == defaultValue {
			defaultIndex = i
			break
		}
	}
	labels := choiceLabels(choices, defaultValue)
	if cursesMenuAvailable() {
		footer := config.Text(
			"menu.jk_move_enter_confirm_digits_jump",
		)
		index, err := selectIndex(prompt, labels, defaultIndex, footer, false)
		if err == nil {
			announceChoice(prompt, labels[index])
			return choices[index].Value, nil
		}
		if errors.Is(err, errMenuEnded) {
			return "", errInputEnded
		}
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		os.Stderr.WriteString("\n" + color("1;36", prompt) + "\n")
		for i, label := range labels {
			os.Stderr.WriteString("  " + strconv.Itoa(i+1) + ". " + label + "\n")
		}
		os.Stderr.WriteString(config.Text(
			"menu.choose_1_or_press_enter_to_keep_the_current", strconv.Itoa(len(choices)),
		))
		answer, err := reader.ReadString('\n')
		if err != nil && answer == "" {
			return "", errInputEnded
		}
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return defaultValue, nil
		}
		if n, convErr := strconv.Atoi(answer); convErr == nil && n >= 1 && n <= len(choices) {
			return choices[n-1].Value, nil
		}
		hint(config.Text("menu.invalid_input_choose_again"))
	}
}

func askText(prompt, defaultValue string) (string, error) {
	shown := defaultValue
	if shown == "" {
		shown = config.Text("menu.empty_cli_default")
	}
	os.Stderr.WriteString("\n" + color("1;36", prompt) + " [" + shown + "]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return "", errInputEnded
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return defaultValue, nil
	}
	return answer, nil
}
