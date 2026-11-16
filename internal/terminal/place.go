package terminal

import "context"

// ContainerPlacer is an optional Backend extension. It places a container
// that CreateContainer just made in the caller's layout and names it, and it
// reports a problem as a warning because placement never fails a launch. A
// backend whose definition declares no place_container hook returns an empty
// warning without doing anything.
type ContainerPlacer interface {
	PlaceContainer(conn Conn, address Address, target Target, label string) (warning string)
}

// PlaceContainer calls the place_container hook once. A degraded result is the
// warning; a failed result cannot stop the launch, so its error is the warning
// too. An empty target title falls back to the container label.
func (b *DeclarativeBackend) PlaceContainer(conn Conn, address Address, target Target, label string) string {
	hook, ok := b.hook(HookPointPlaceContainer)
	if !ok {
		return ""
	}
	result := runHook(context.Background(), hook, HookCall{
		Launcher: b.launcher, Point: HookPointPlaceContainer, Conn: conn, Getenv: b.getenv,
		Values: map[string]string{
			"container": address.Container, "pane": address.Pane, "title": containerTitle(target, label), "label": label,
		},
	})
	switch result.Status {
	case HookDegraded:
		return result.Note
	case HookFailed:
		return result.Err.Error()
	}
	return ""
}

// containerTitle is the display title of a new container.
func containerTitle(target Target, label string) string {
	if target.Title != "" {
		return target.Title
	}
	return label
}
