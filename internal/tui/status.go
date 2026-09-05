package tui

func (a *App) statusError() string {
	parts := []string{}
	if err := a.Model.Error(); err != "" {
		parts = append(parts, err)
	}
	if a.PrefsError != "" {
		parts = append(parts, a.PrefsError)
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += " | " + parts[i]
	}
	return out
}

func (a *App) footerMessage(detail bool) string {
	if a.Now().Before(a.CopyNoticeUntil) && a.CopyNotice != "" {
		return a.CopyNotice
	}
	if status := a.statusError(); status != "" {
		return a.Context.Error + ": " + status
	}
	if detail && a.DetailSearching {
		return a.Context.SearchHelp
	}
	if !detail && a.Searching {
		return a.Context.SearchHelp
	}
	if detail {
		return a.Context.DetailHelp
	}
	return a.Context.Help
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

func orUnassigned(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
