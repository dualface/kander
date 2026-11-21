package config

import "log/slog"

// LogIgnored writes one warning for each item cfg ignores, for a long-running process (the
// server, the worker) that would otherwise drop the settings of a newer version without a
// trace. Only the JSON path and the kind are logged: the value may be a setting this version
// does not understand and that is sensitive.
func LogIgnored(logger *slog.Logger, cfg *Config) {
	if logger == nil || cfg == nil {
		return
	}
	for _, item := range cfg.Ignored {
		logger.Warn("the configuration holds a setting this version ignores", "path", item.PathString(), "kind", item.Kind)
	}
}
