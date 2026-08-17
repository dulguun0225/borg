package record

import (
	"crypto/rand"
	"encoding/hex"
)

// idBytes is how many random bytes an identifier carries. Sixteen is 128 bits,
// so two identifiers colliding is not a case any caller has to handle.
const idBytes = 16

// NewID returns an identifier for a new record: the prefix, an underscore, and
// 32 hexadecimal characters read from crypto/rand. The prefix names the record
// kind — "dl" for a decision log row — and is the only part a reader may
// interpret. The rest is opaque: it encodes no time, no sequence, and no
// counter, so nothing about the record can be recovered from it and nothing
// may sort by it.
//
// A record's row order is a column the table declares, never its identifier.
func NewID(prefix string) string {
	var b [idBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read does not return an error as of Go 1.24; it panics
		// if the operating system's source fails. The branch is here so that a
		// reader does not have to know that.
		panic("record: crypto/rand failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
