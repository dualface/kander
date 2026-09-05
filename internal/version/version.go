// Package version 暴露构建时注入的 Kander 版本标识.
package version

import "strings"

var (
	// BuildTimestamp 由构建入口以 UTC YYYYMMDDTHHMMSSZ 格式注入.
	BuildTimestamp = "dev"
	// GitHash 由构建入口注入当前提交的短 hash.
	GitHash = "unknown"
)

func component(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return strings.Join(strings.Fields(value), "_")
}

// String 返回稳定的 <build-timestamp>-<git-hash> 版本号.
func String() string {
	return component(BuildTimestamp, "dev") + "-" + component(GitHash, "unknown")
}
