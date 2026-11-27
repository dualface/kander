package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// An absent embedded field is empty. A present field must be a JSON string,
// including when the definition explicitly disables rename.
type embeddedRenameCommand string

func (c *embeddedRenameCommand) UnmarshalJSON(data []byte) error {
	var value *string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("rename_command: %w", err)
	}
	if value == nil {
		return errors.New("rename_command must be a string")
	}
	*c = embeddedRenameCommand(*value)
	return nil
}

// RenameDelivery describes when an interactive client can accept its rename
// command. It is separate from baseline prompt delivery so that disabling
// rename restores the original launch policy without changing any arguments.
type RenameDelivery struct {
	Ready     *PromptReady    `json:"ready,omitempty"`
	Blocked   []PromptBlocked `json:"blocked,omitempty"`
	Complete  string          `json:"complete,omitempty"`
	AfterArgv bool            `json:"after_argv,omitempty"`
}

func validRenameCommand(value string) bool {
	return value == "" || (strings.Contains(value, "{name}") &&
		!strings.ContainsFunc(value, func(r rune) bool {
			return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
				unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
		}))
}

func validateRenameDelivery(name string, delivery *RenameDelivery) error {
	if delivery == nil {
		return nil
	}
	field := invalidDeliveryReadiness(delivery.Ready, delivery.Blocked)
	if field == "" && delivery.Complete != "" &&
		(!validAgentText(delivery.Complete) || compileDeliveryPattern(strings.ReplaceAll(delivery.Complete, "{name}", "example")) != nil) {
		field = "complete"
	}
	if field != "" {
		return agentDefinitionError(name, Text("config.agent_rename_delivery", field))
	}
	return nil
}

func cloneRenameDelivery(src *RenameDelivery) *RenameDelivery {
	if src == nil {
		return nil
	}
	out := *src
	if src.Ready != nil {
		ready := *src.Ready
		out.Ready = &ready
	}
	out.Blocked = slices.Clone(src.Blocked)
	return &out
}
