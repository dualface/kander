//go:build unix

package menu

import (
	"os"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	rawMu     sync.Mutex
	rawCooked *unix.Termios
	rawFd     int
	rawOn     bool
)

func markRawTTY(fd int, cooked *unix.Termios) {
	rawMu.Lock()
	defer rawMu.Unlock()
	copy := *cooked
	rawCooked = &copy
	rawFd = fd
	rawOn = true
}

func restoreRawTTY() {
	rawMu.Lock()
	defer rawMu.Unlock()
	if !rawOn || rawCooked == nil {
		return
	}
	showCursor()
	_ = unix.IoctlSetTermios(rawFd, ioctlWriteTermiosDrain, rawCooked)
	rawOn = false
	rawCooked = nil
}

func writeMenu(text string) {
	_, _ = os.Stderr.WriteString(text)
}

func hideCursor() { writeMenu("\033[?25l") }
func showCursor() { writeMenu("\033[?25h") }

func moveUp(lines int) {
	if lines > 0 {
		writeMenu("\033[" + itoa(lines) + "A")
	}
}

func clearLine() { writeMenu("\033[2K\r") }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func takePending() []byte {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	out := pendingBytes
	pendingBytes = nil
	return out
}

func storePending(queue []byte) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	pendingBytes = append([]byte(nil), queue...)
}

func readByteFrom(fd int, queue *[]byte) (byte, error) {
	if len(*queue) > 0 {
		ch := (*queue)[0]
		*queue = (*queue)[1:]
		return ch, nil
	}
	var buf [1]byte
	n, err := unix.Read(fd, buf[:])
	if n == 1 {
		return buf[0], nil
	}
	if err != nil {
		return 0, err
	}
	return 0, errMenuEnded
}

func deferByte(queue *[]byte, data byte) {
	*queue = append(*queue, data)
}

func readKey(fd int, queue *[]byte) (string, error) {
	ch, err := readByteFrom(fd, queue)
	if err != nil {
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return "", errMenuEnded
		}
		return "", err
	}
	if ch == 0 {
		return "", errMenuEnded
	}
	if ch == 0x1b {
		rest := []byte{}
		_ = unix.SetNonblock(fd, true)
		defer func() { _ = unix.SetNonblock(fd, false) }()
		for len(rest) < 2 {
			next, err := readByteFrom(fd, queue)
			if err != nil {
				break
			}
			rest = append(rest, next)
		}
		joined := string(rest)
		if strings.HasPrefix(joined, "[A") || joined == "OA" {
			return "up", nil
		}
		if strings.HasPrefix(joined, "[B") || joined == "OB" {
			return "down", nil
		}
		for i := len(rest) - 1; i >= 0; i-- {
			*queue = append([]byte{rest[i]}, *queue...)
		}
		return "esc", nil
	}
	if ch == '\r' {
		_ = unix.SetNonblock(fd, true)
		next, err := readByteFrom(fd, queue)
		_ = unix.SetNonblock(fd, false)
		if err == nil && next != '\n' {
			deferByte(queue, next)
		}
		return "enter", nil
	}
	if ch == '\n' {
		return "enter", nil
	}
	return string([]byte{ch}), nil
}

func renderMenu(prompt string, labels []string, index int, footer string, painted int) int {
	lines := strings.Split(prompt, "\n")
	if len(lines) == 0 {
		lines = []string{prompt}
	}
	out := append([]string{}, lines...)
	out = append(out, "")
	for i, label := range labels {
		marker := " "
		if i == index {
			marker = ">"
		}
		prefix := " " + marker + " "
		if i == index {
			out = append(out, "\033[7m"+prefix+label+"\033[0m")
		} else {
			out = append(out, prefix+label)
		}
	}
	out = append(out, "", "\033[2m"+footer+"\033[0m")
	if painted > 0 {
		moveUp(painted)
	}
	for _, line := range out {
		clearLine()
		writeMenu(line + "\n")
	}
	return len(out)
}

func selectIndex(prompt string, labels []string, defaultIndex int, footer string, allowCancel bool) (int, error) {
	if len(labels) == 0 {
		return 0, errMenuEnded
	}
	if !cursesMenuAvailable() {
		return 0, errMenuEnded
	}
	queue := takePending()
	index := defaultIndex
	if index < 0 {
		index = 0
	}
	if index >= len(labels) {
		index = len(labels) - 1
	}
	digitBuffer := ""
	fd := int(os.Stdin.Fd())
	old, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return 0, err
	}
	raw := *old
	raw.Lflag &^= unix.ICANON | unix.ECHO
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return 0, err
	}
	markRawTTY(fd, old)
	painted := 0
	defer func() {
		restoreRawTTY()
		storePending(nil)
	}()
	hideCursor()
	writeMenu("\n")
	for {
		painted = renderMenu(prompt, labels, index, footer, painted)
		key, err := readKey(fd, &queue)
		if err != nil {
			if errorsIsEOF(err) {
				return 0, errMenuEnded
			}
			return 0, err
		}
		if key == "enter" {
			return index, nil
		}
		if allowCancel && (key == "q" || key == "Q" || key == "esc") {
			return 0, errMenuCancelled
		}
		if key == "up" || key == "k" || key == "K" {
			index = (index - 1 + len(labels)) % len(labels)
			digitBuffer = ""
			continue
		}
		if key == "down" || key == "j" || key == "J" {
			index = (index + 1) % len(labels)
			digitBuffer = ""
			continue
		}
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			candidate := digitBuffer + key
			if target := digitTarget(candidate, len(labels)); target >= 0 {
				digitBuffer = candidate
				index = target
				continue
			}
			if digitPrefixPossible(candidate, len(labels)) {
				digitBuffer = candidate
				continue
			}
			single := digitTarget(key, len(labels))
			if single >= 0 {
				digitBuffer = key
				index = single
			} else {
				digitBuffer = ""
			}
			continue
		}
	}
}

func errorsIsEOF(err error) bool {
	return err == errMenuEnded || err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EIO
}
