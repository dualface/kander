package server

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/dualface/kander/v2/api"
)

// receiptScope is what an idempotency receipt is kept under besides the
// operation: the Idempotency-Key of the request, a one-way digest of the claim
// credentials it presents, and the caller the embedding host attributes it to.
// A write by one scope never answers, nor refuses, the same key in another, so
// a caller without the holder's credentials cannot take the holder's key in
// advance. Only scopeOf builds a value: the fields are not exported and every
// route derives the scope from the request the same way.
type receiptScope struct {
	key string
	// holder is the digest of the claim token and the multi-task claim proofs,
	// empty when the request presents neither.
	holder string
	// caller is the value of Hooks.Attribution when it reported a caller.
	caller string
}

// scopeOf is the receipt scope of one request. The claim token and the
// multi-task claim proofs are derived by the server from its secret and cannot
// be forged; the attribution comes from the embedding host. Self-reported
// headers such as the client identity never take part. Every client path that
// resends a key, from a journal or a transport retry, resends the claim
// credentials of its first attempt, so the holder digest of a retry matches.
func scopeOf(r *http.Request) receiptScope {
	scope := receiptScope{key: r.Header.Get(api.HeaderIdempotencyKey), holder: holderDigest(holderOf(r))}
	if caller, ok := r.Context().Value(attributionKey{}).(callerAttribution); ok && caller.found {
		scope.caller = caller.value
	}
	return scope
}

// holderDigest is the one-way digest of the claim credentials a request
// presents, empty when it presents none. The single claim token comes first
// and the multi-task proofs, ordered by task ID, second; every part is length
// prefixed so no two credential sets encode alike.
func holderDigest(h holder) string {
	if h.token == "" && len(h.members) == 0 {
		return ""
	}
	var single, members string
	if h.token != "" {
		single = tokenHash(h.token)
	}
	if len(h.members) > 0 {
		sum := sha256.New()
		for _, id := range slices.Sorted(maps.Keys(h.members)) {
			writeLengthPrefixed(sum, id)
			writeLengthPrefixed(sum, tokenHash(h.members[id].Token))
		}
		members = hex.EncodeToString(sum.Sum(nil))
	}
	sum := sha256.New()
	writeLengthPrefixed(sum, single)
	writeLengthPrefixed(sum, members)
	return hex.EncodeToString(sum.Sum(nil))
}

// receiptScopeDomain separates the names of scoped receipts from the legacy
// names, which hash the key alone. It ends in a NUL byte, which no key can
// hold (api.ValidIdentity), so no legacy name hashes the same bytes even when
// a key spells out the printable rest of a scoped encoding.
const receiptScopeDomain = "receipt-scope-v1\x00"

// receipt is where the receipt of operation in this scope is kept: one file
// below the receipts directory whose name hashes the operation, the scope, and
// the key, each length prefixed because an attribution may hold any byte.
func (s receiptScope) receipt(operation string) receiptAt {
	sum := sha256.New()
	sum.Write([]byte(receiptScopeDomain))
	for _, part := range []string{operation, s.holder, s.caller, s.key} {
		writeLengthPrefixed(sum, part)
	}
	return receiptAt{name: filepath.Join("receipts", hex.EncodeToString(sum.Sum(nil))+".json")}
}

// legacyReceipt is where a server before the receipt scopes kept the receipt
// of key. It is only read, by idempotentAt.
func legacyReceipt(key string) receiptAt {
	sum := sha256.Sum256([]byte(key))
	return receiptAt{name: filepath.Join("receipts", hex.EncodeToString(sum[:])+".json")}
}

func writeLengthPrefixed(h hash.Hash, part string) {
	h.Write([]byte(strconv.Itoa(len(part)) + ":" + part))
}
