package tui

type statusActionHit struct {
	x, width int
	options  bool
}

// popupClick activates once on release, at the position where the press began.
func (a *App) popupClick(x, y, bstate int) bool {
	if mouseLeftClicked(bstate) {
		return true
	}
	return mouseButton1Released(bstate) && x == a.mouse.downX && y == a.mouse.downY
}

func (a *App) handleStatusMouse(x, y, bstate int) bool {
	height, _ := a.size()
	if y != height-1 || a.transientNotice() != "" {
		return false
	}
	if a.popupClick(x, y, bstate) {
		for _, hit := range a.statusHits {
			if x >= hit.x && x < hit.x+hit.width {
				if hit.options {
					a.openOptions()
				} else {
					a.openHelp()
				}
				break
			}
		}
	}
	return true
}
