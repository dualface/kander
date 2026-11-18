package check

import "strings"

type options struct {
	json   bool
	base   string
	commit string
	source string
	head   string
}

// validate applies what the flags alone cannot: a delivery needs a base, an overlap a source,
// and neither accepts a blank ref.
func (opt options) validate(mode string) *CheckError {
	switch {
	case mode == checkDelivery && strings.TrimSpace(opt.base) == "":
		return usageErr(t("check.missing_base"))
	case mode == checkOverlap && strings.TrimSpace(opt.source) == "":
		return usageErr(t("check.missing_source"))
	case mode == checkDelivery && strings.TrimSpace(opt.commit) == "":
		return usageErr(t("check.missing_flag_value", "--commit"))
	case mode == checkOverlap && strings.TrimSpace(opt.head) == "":
		return usageErr(t("check.missing_flag_value", "--head"))
	}
	return nil
}

func usageErr(message string) *CheckError {
	return &CheckError{Code: errCodeUsage, Message: message}
}
