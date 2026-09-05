//go:build windows

package menu

func selectIndex(prompt string, labels []string, defaultIndex int, footer string, allowCancel bool) (int, error) {
	_ = prompt
	_ = labels
	_ = defaultIndex
	_ = footer
	_ = allowCancel
	return 0, errMenuEnded
}
