package server

// scopedReceiptPath is the receipt file, below the project directory, of
// operation for a request that sends key with the claim token token (empty for
// none) and no caller attribution.
func scopedReceiptPath(operation, key, token string) string {
	return receiptScope{key: key, holder: holderDigest(holder{token: token})}.receipt(operation).rel()
}

// legacyReceiptPath is where a server before the receipt scopes kept the
// receipt of key, below the project directory.
func legacyReceiptPath(key string) string { return legacyReceipt(key).rel() }
