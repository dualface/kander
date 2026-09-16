package check

import (
	"strings"
)

type options struct {
	json   bool
	help   bool
	base   string
	commit string
	source string
	head   string
}

func parseOptions(mode string, args []string) (options, *CheckError) {
	opt := options{commit: "HEAD", head: "HEAD", json: jsonRequested(args)}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return opt, usageErr(t("check.unexpected_argument", args[i+1]))
			}
			break
		}
		if arg == "-h" || arg == "--help" {
			opt.help = true
			continue
		}
		name, value, ok, err := nextFlag(args, &i)
		if err != nil {
			return opt, err
		}
		if !ok {
			if strings.HasPrefix(arg, "-") {
				return opt, usageErr(t("check.unknown_option", arg))
			}
			return opt, usageErr(t("check.unexpected_argument", arg))
		}
		if !flagAllowed(mode, name) {
			return opt, usageErr(t("check.unknown_option", name))
		}
		if seen[name] {
			return opt, usageErr(t("check.duplicate_flag", name))
		}
		seen[name] = true
		switch name {
		case "--json":
			opt.json = true
		case "--base":
			opt.base = value
		case "--commit":
			opt.commit = value
		case "--source":
			opt.source = value
		case "--head":
			opt.head = value
		}
	}
	if opt.help {
		return opt, nil
	}
	if mode == checkDelivery && strings.TrimSpace(opt.base) == "" {
		return opt, usageErr(t("check.missing_base"))
	}
	if mode == checkOverlap && strings.TrimSpace(opt.source) == "" {
		return opt, usageErr(t("check.missing_source"))
	}
	if mode == checkDelivery && strings.TrimSpace(opt.commit) == "" {
		return opt, usageErr(t("check.missing_flag_value", "--commit"))
	}
	if mode == checkOverlap && strings.TrimSpace(opt.head) == "" {
		return opt, usageErr(t("check.missing_flag_value", "--head"))
	}
	return opt, nil
}

func flagAllowed(mode, name string) bool {
	switch mode {
	case checkDelivery:
		return name == "--json" || name == "--base" || name == "--commit"
	case checkOverlap:
		return name == "--json" || name == "--source" || name == "--head"
	default:
		return false
	}
}

func nextFlag(args []string, i *int) (name, value string, ok bool, err *CheckError) {
	arg := args[*i]
	if arg == "--json" {
		return "--json", "", true, nil
	}
	if name, value, found := strings.Cut(arg, "="); found && strings.HasPrefix(name, "--") {
		if name == "--json" {
			return "", "", false, usageErr(t("check.flag_does_not_take_value", name))
		}
		if value == "" {
			return "", "", false, usageErr(t("check.missing_flag_value", name))
		}
		return name, value, true, nil
	}
	switch arg {
	case "--base", "--commit", "--source", "--head":
		if *i+1 >= len(args) || reservedFlag(args[*i+1]) || args[*i+1] == "" {
			return "", "", false, usageErr(t("check.missing_flag_value", arg))
		}
		*i++
		return arg, args[*i], true, nil
	default:
		return "", "", false, nil
	}
}

func jsonRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			return true
		}
	}
	return false
}

func reservedFlag(arg string) bool {
	name, _, _ := strings.Cut(arg, "=")
	switch name {
	case "--json", "--base", "--commit", "--source", "--head", "-h", "--help":
		return true
	default:
		return false
	}
}

func usageErr(message string) *CheckError {
	return &CheckError{Code: errCodeUsage, Message: message}
}

func usageFor(mode string) string {
	switch mode {
	case checkDelivery:
		return t("check.usage_delivery")
	case checkOverlap:
		return t("check.usage_overlap")
	default:
		return t("check.usage")
	}
}
