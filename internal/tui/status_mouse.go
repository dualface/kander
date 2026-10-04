package tui

// statusAction names the board action a status bar entry opens.
type statusAction int

const (
	statusActionChat statusAction = iota
	statusActionOptions
	statusActionHelp
)

type statusActionHit struct {
	x, width int
	action   statusAction
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
	if y != height-1 || a.transientNotice() != "" || mouseWheelDelta(bstate) != 0 {
		return false
	}
	if a.popupClick(x, y, bstate) {
		for _, hit := range a.statusHits {
			if x >= hit.x && x < hit.x+hit.width {
				switch hit.action {
				case statusActionChat:
					a.openChat()
				case statusActionOptions:
					a.openOptions()
				case statusActionHelp:
					a.openHelp()
				}
				break
			}
		}
	}
	return true
}
