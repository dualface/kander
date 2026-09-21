//go:build windows

package tui

import "time"

// waitReadableWorks is false on Windows: console input arrives as event
// records, not as an ANSI byte stream, so an OSC 11 reply cannot be
// intercepted the same way. The runtime background probe stays disabled and
// the "auto" theme keeps the startup fallback there.
const waitReadableWorks = false

func waitReadable(uintptr, time.Duration) bool { return false }
