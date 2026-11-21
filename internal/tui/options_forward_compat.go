package tui

import (
	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/config"
)

// noteUnknownKanbanAgent shows the legacy kanban_agent when it names an agent this
// version does not know. The key has no control of its own, so without the note the
// execution page would hide a value that a save keeps.
func (p *optionsPanel) noteUnknownKanbanAgent(bind *formBinding) {
	cfg := p.session.Config
	if name := cfg.KanbanAgent; name != "" && !config.HasAgent(cfg, name) {
		bind.formFields = append(bind.formFields, huh.NewNote().Title(t("tui.kanban_agent_unknown_note", name)))
	}
}
