package menu

import "runtime"

var windowsOS = runtime.GOOS == "windows"

func isWindowsOS() bool {
	return windowsOS
}
