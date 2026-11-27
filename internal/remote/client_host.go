package remote

import "strings"

// clientIDRandomLength is the hex length of the random part of a client ID.
const clientIDRandomLength = 12

// ClientHost returns the host name a client ID carries, or "" when the ID
// has none or was not made by ClientID. ClientID writes
// "c-<12 lowercase hex>[-<host>]" once per client home, so the name is the
// sanitized host name of that first run.
func ClientHost(id string) string {
	rest, ok := strings.CutPrefix(id, "c-")
	if !ok || len(rest) < clientIDRandomLength+2 || rest[clientIDRandomLength] != '-' {
		return ""
	}
	for _, r := range rest[:clientIDRandomLength] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return rest[clientIDRandomLength+1:]
}
