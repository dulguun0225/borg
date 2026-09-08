package wayin

import "strings"

// keyLength is how long an opaque source key is: the hexadecimal of a
// SHA-256 digest, which is what the shipped way in derives and sends.
const keyLength = 64

// opaqueKey is the source key as it may be stored, and empty for anything
// else. The key is derived in the deployed software and this entrance
// forwards it rather than deriving anything, so what arrives under that name
// is whatever reached this address — and the one thing a report may never
// carry is a field a person can be read out of. Holding the key to the one
// shape a derived key has is what keeps a name, an address or a handle from
// being written onto a report by calling it a key.
//
// It decides nothing else: a key of the right shape is forwarded as
// received, whether or not any way in derived it, because the store rates a
// source by it and rating is all it is for.
func opaqueKey(key string) string {
	if len(key) != keyLength {
		return ""
	}
	if strings.Trim(key, "0123456789abcdef") != "" {
		return ""
	}
	return key
}
