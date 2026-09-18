package install

// previousExtensionHashes lists SHA-256 digests of the pi rules-extension
// payloads shipped by earlier Kander releases, in the order they shipped.
// Doctor and the installer distinguish an outdated official copy (a digest in
// this list, rewritten by repair) from a local edit (a digest in no list, never
// overwritten). Append the previous payload's digest whenever the embedded
// extension file changes; never rewrite an existing entry.
var previousExtensionHashes = []string{}

func isPreviousExtensionVersion(digest string) bool {
	for _, known := range previousExtensionHashes {
		if known == digest {
			return true
		}
	}
	return false
}
