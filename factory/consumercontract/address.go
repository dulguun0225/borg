package consumercontract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Which producer a consumer reaches. A call site does not say: the address it
// calls arrives through the service's configuration file, a base URL, a topic, or
// a discovery name, and the value comes from the file at deploy. So each such
// address is an entry of that file naming the service and the interface it
// reaches, authored beside the code that reads it and diffed like the rest of the
// file, and the extractor pairs each call site with the entry it reads its address
// from.
//
// The file is one line per entry:
//
//	<address> <producer service> <interface>
//	<address> <producer service> <interface> store
//	<address> outside
//
// A fourth word of `store` is an address that reaches a store contract rather than
// a published interface, which is how a service declares against its own store —
// its consumer being its own past. `outside` is an address outside the factory,
// and a call through it is covered by nothing. A line beginning with # is a
// comment.
//
// An address a mirror names and this file does not hold is could not derive, and
// so is a line that names no service for an address inside the factory. That is
// what tells an edge the analysis did not find from an edge that does not exist.

// ConfigurationFile is the name of the file at the root of the consumer's
// repository that names, per address, which producer it reaches.
const ConfigurationFile = "consumes.txt"

// outsideTheFactory is the word an entry uses for an address outside the factory.
const outsideTheFactory = "outside"

// storeEntry is the fourth word of an entry that reaches a store contract.
const storeEntry = "store"

// Entry is one entry of the configuration file: one address, and what it reaches.
type Entry struct {
	Address string
	// ProducerService and Interface are what the address reaches, and are empty
	// on an address outside the factory.
	ProducerService string
	Interface       string
	// Store is whether what it reaches is a store contract rather than a
	// published interface.
	Store bool
	// Outside is whether the entry declares its address outside the factory.
	Outside bool
}

// Entries is the configuration file's entries by address, and false where the
// checkout holds no such file. A malformed line is an error: an entry naming no
// service for an address inside the factory is what the caller turns into a
// could-not-derive record, and reading it as absent would make it an edge that
// does not exist.
func Entries(root string) (map[string]Entry, bool, error) {
	text, err := os.ReadFile(filepath.Join(root, ConfigurationFile))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("consumercontract: reading %s: %w", ConfigurationFile, err)
	}
	entries := map[string]Entry{}
	for number, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words := strings.Fields(line)
		entry := Entry{Address: words[0]}
		switch {
		case len(words) == 2 && words[1] == outsideTheFactory:
			entry.Outside = true
		case len(words) == 3, len(words) == 4 && words[3] == storeEntry:
			entry.ProducerService, entry.Interface = words[1], words[2]
			entry.Store = len(words) == 4
		default:
			return nil, false, fmt.Errorf("consumercontract: %s line %d names no producer for an address inside the factory: %q",
				ConfigurationFile, number+1, line)
		}
		entries[entry.Address] = entry
	}
	return entries, true, nil
}
