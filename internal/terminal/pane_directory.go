package terminal

import "context"

// PaneDirectoryReader is an optional Backend extension: the working directory
// of the pane's foreground process. A launch uses it to prove that the pane
// entered the startup directory. Definitions without pane_directory stay valid
// and answer ErrUnsupported.
type PaneDirectoryReader interface {
	// PaneDirectory returns an empty directory while the pane has no process
	// to report yet.
	PaneDirectory(ctx context.Context, conn Conn, pane string) (string, error)
}

func (b *DeclarativeBackend) PaneDirectory(ctx context.Context, conn Conn, pane string) (string, error) {
	if _, ok := b.op(OpPaneDirectory); !ok {
		return "", ErrUnsupported
	}
	result, err := b.probeOp(ctx, OpPaneDirectory, conn, map[string]string{"pane": pane}, false)
	if err != nil {
		return "", err
	}
	return result["directory"], nil
}
