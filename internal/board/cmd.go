package board

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	return 1
}

func usage(w io.Writer, cmd string) {
	messages := map[string]string{
		"init":        "board.messages.init",
		"list":        "board.messages.list",
		"show":        "board.messages.show",
		"new":         "board.messages.new",
		"move":        "board.messages.move",
		"pick":        "board.messages.pick",
		"check":       "board.messages.check",
		"guard-write": "board.messages.guard-write",
	}
	pair := messages[cmd]
	fmt.Fprintln(w, t(pair))
}

func usageFail(cmd, id string, args ...any) int {
	usage(os.Stderr, cmd)
	if id != "" {
		fmt.Fprintln(os.Stderr, t(id, args...))
	}
	return 2
}

func takeFlag(args []string, name string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == name {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return out, found
}

func requireRoot() (string, error) {
	return BoardRoot()
}

// RunInit 实现 kander init.
func RunInit(args []string) int {
	if len(args) > 1 {
		return usageFail("init", "board.too_many_arguments")
	}
	project := ""
	if len(args) == 1 {
		if strings.HasPrefix(args[0], "-") {
			return usageFail("init", "board.unknown_option", args[0])
		}
		project = args[0]
	}
	root, exclude, rules, err := InitBoard(project)
	if err != nil {
		return fail(err)
	}
	fmt.Println(t("board.initialized", root))
	if exclude != "" {
		fmt.Println(t("board.git_exclude", exclude))
	}
	fmt.Println(t("board.rules", rules))
	return 0
}

// RunList 实现 kander list / ls.
func RunList(args []string) int {
	args, mobile := takeFlag(args, "--mobile")
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usageFail("list", "board.unknown_option", arg)
		}
	}
	state := ""
	if len(args) > 1 {
		return usageFail("list", "board.too_many_arguments")
	}
	if len(args) == 1 {
		state = args[0]
		if _, ok := stateSet[state]; !ok {
			return usageFail("list", "board.unknown_state", state)
		}
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	board, err := LoadBoard(root)
	if err != nil {
		return fail(err)
	}
	out, err := FormatList(board, state, mobile)
	if err != nil {
		return fail(err)
	}
	os.Stdout.WriteString(out)
	return 0
}

// RunShow 实现 kander show.
func RunShow(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usageFail("show", "board.task_id_required")
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	board, err := LoadBoard(root)
	if err != nil {
		return fail(err)
	}
	entry, err := Locate(board, args[0])
	if err != nil {
		return fail(err)
	}
	text, err := ReadDocument(entry)
	if err != nil {
		return fail(err)
	}
	// 定位头供 Agent 写卡前重新定位; 卡片可能已被迁移, 不得复用旧路径.
	fmt.Println(t("board.show_location", entry.State, entry.Path))
	fmt.Println()
	os.Stdout.WriteString(text)
	return 0
}

// RunNew 实现 kander new.
func RunNew(args []string) int {
	args, large := takeFlag(args, "--large")
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usageFail("new", "board.unknown_option", arg)
		}
	}
	if len(args) < 3 {
		return usageFail("new", "board.type_slug_and_title_are_required")
	}
	kind, slug := args[0], args[1]
	title := strings.Join(args[2:], " ")
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	target, err := NewTask(root, kind, slug, title, large)
	if err != nil {
		return fail(err)
	}
	fmt.Println(target)
	return 0
}

// RunMove 实现 kander move.
func RunMove(args []string) int {
	if len(args) != 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return usageFail("move", "board.task_id_and_state_are_required")
	}
	if _, ok := stateSet[args[1]]; !ok {
		return usageFail("move", "board.unknown_state", args[1])
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	board, err := LoadBoard(root)
	if err != nil {
		return fail(err)
	}
	entry, err := Locate(board, args[0])
	if err != nil {
		return fail(err)
	}
	moved, err := MoveEntry(entry, root, args[1])
	if err != nil {
		return fail(err)
	}
	fmt.Println(moved.Path)
	return 0
}

// RunPick 实现 kander pick.
func RunPick(args []string) int {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usageFail("pick", "board.unknown_option", arg)
		}
	}
	if len(args) > 1 {
		return usageFail("pick", "board.too_many_arguments")
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	board, err := LoadBoard(root)
	if err != nil {
		return fail(err)
	}
	var entry Entry
	if len(args) == 1 {
		entry, err = Locate(board, args[0])
	} else {
		entry, err = selectFromState(board, "backlog", os.Stdin, os.Stdout)
	}
	if err != nil {
		return fail(err)
	}
	moved, err := MoveEntry(entry, root, "todo")
	if err != nil {
		return fail(err)
	}
	fmt.Println(moved.Path)
	return 0
}

// RunCheck 实现 kander check, 不含存活探测.
func RunCheck(args []string) int {
	args, all := takeFlag(args, "--all")
	var tasks []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usageFail("check", "board.unknown_option", arg)
		}
		tasks = append(tasks, arg)
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	code, stdout, stderr, err := CheckBoard(root, tasks, all)
	if err != nil {
		return fail(err)
	}
	for _, line := range stderr {
		fmt.Fprintln(os.Stderr, line)
	}
	os.Stdout.WriteString(stdout)
	return code
}
