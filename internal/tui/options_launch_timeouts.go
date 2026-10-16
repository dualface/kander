package tui

func launchTimeoutsFocusKey() string { return "extended_launch_timeouts" }

// addLaunchTimeoutsField appends the extended launch timeouts switch below the
// launcher it qualifies, separated from it by a blank line like other top-level items.
// The blank line below comes from the page restore control or the page end.
func (b *formBinding) addLaunchTimeoutsField(p *optionsPanel) {
	b.addSpacer()
	b.launchTimeouts = p.session.Config.ExtendedLaunchTimeouts
	b.fieldIndex[launchTimeoutsFocusKey()] = b.focusable
	b.addField(inlineConfirm().
		Title(p.inheritTitle(t("tui.extended_launch_timeouts"), formatBool(b.launchTimeouts), "extended_launch_timeouts")).
		Description(tip(t("tui.extended_launch_timeouts_tip"))).
		Value(&b.launchTimeouts))
}

func (b *formBinding) applyLaunchTimeouts(p *optionsPanel) {
	session := p.session
	if session.Config.ExtendedLaunchTimeouts == b.launchTimeouts {
		return
	}
	before := p.overridePresence("extended_launch_timeouts")
	session.SetExtendedLaunchTimeouts(b.launchTimeouts)
	p.markDirty()
	p.rebuildIfOverrideChanged(before, launchTimeoutsFocusKey(), "extended_launch_timeouts")
}
