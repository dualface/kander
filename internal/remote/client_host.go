package remote

import "strings"

// clientIDRandomLength is the hex length of the random part of a client ID.
const clientIDRandomLength = 12

// ClientHost returns the suffix after "c-<12 lowercase hex>-" in a client
// ID, or "" when the ID has another shape or no suffix. ClientID writes that
// shape once per client home with the sanitized host name of the first run
// as the suffix. The match is structural only: it is a display hint, not a
// proof that ClientID made the ID.
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
