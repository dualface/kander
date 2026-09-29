package herdr

import (
	"context"
	"net"
)

// dialChannel opens the herdr control channel named by HERDR_SOCKET_PATH. POSIX herdr listens on a
// unix socket at that path; Windows herdr listens on the named pipe `\\.\pipe\` plus that path. The
// context bounds the dial, and a failure returns the original error without any fallback.
func dialChannel(ctx context.Context, socket string) (net.Conn, error) {
	return dialPlatform(ctx, socket)
}
