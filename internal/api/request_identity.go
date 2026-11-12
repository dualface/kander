package api

import (
	"crypto/sha256"
	"encoding/hex"
)

// CollaborationRequestID lets a disconnected sender inspect the exact request
// derived from its saved operation key without guessing from message text.
func CollaborationRequestID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "req-" + hex.EncodeToString(sum[:8])
}
