//go:build !windows

package herdr

import (
	"context"
	"net"
)

func dialPlatform(ctx context.Context, socket string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socket)
}
