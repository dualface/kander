package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dualface/kander/internal/process"
)

// json_regex limits a field regex to the JSON value at one exact path.
// Missing or null values are represented by null; invalid parents fail.
func jsonRegexParts(parse string) (path, pattern string, scoped bool) {
	body, scoped := strings.CutPrefix(parse, "json_regex:")
	if scoped {
		path, pattern, _ = strings.Cut(body, ":")
	}
	return path, pattern, scoped
}

func validateFieldParser(parse string) error {
	path, pattern, scoped := jsonRegexParts(parse)
	if scoped {
		if err := process.ValidateTerminalOutput(process.OutputSpec{Source: process.SourceStdout, Parse: "json_field:" + path}); err != nil {
			return err
		}
		parse = "regex:" + pattern
	}
	return process.ValidateTerminalOutput(process.OutputSpec{Source: process.SourceStdout, Parse: parse})
}

func parseJSONRegex(path, pattern, text string) (string, error) {
	value := json.RawMessage(text)
	for part := range strings.SplitSeq(path, ".") {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return "", err
		}
		value = object[part]
		if len(value) == 0 {
			value = json.RawMessage("null")
			break
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, value); err != nil {
		return "", fmt.Errorf("JSON field %s: %w", path, err)
	}
	return process.ParseTerminalOutput(process.OutputSpec{Source: process.SourceStdout, Parse: "regex:" + pattern}, compact.String())
}
