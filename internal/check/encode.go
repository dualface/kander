package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

func encodeJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJSON(value any) error {
	data, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func writeJSONOrFallback(value any, mode string) int {
	if err := writeJSON(value); err == nil {
		return exitExec
	}
	fallback := map[string]any{
		"schema_version": schemaVersion,
		"check":          mode,
		"status":         statusError,
		"error":          CheckError{Code: errCodeInternal, Message: t("check.internal")},
	}
	if err := writeJSON(fallback); err != nil {
		fmt.Fprintln(os.Stderr, "kander: "+t("check.internal"))
		return exitExec
	}
	return exitExec
}
