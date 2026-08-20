package contract

import (
	"errors"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned where the named contract or version does not
	// exist.
	ErrNotFound = errors.New("contract: no contract has that id")
	// ErrVersionNotFound is returned where the named version does not exist.
	ErrVersionNotFound = errors.New("contract: no version has that id")
	// ErrPublishIncomplete is returned by [Publish] for a publication missing
	// something every one names: the service, the release and its number, the
	// item, or a form that does not validate.
	ErrPublishIncomplete = errors.New("contract: the publication is missing something every one names")
	// ErrKindChanged is returned by [Publish] where the form's kind is not the
	// kind the contract already has. The kind has to be single-valued across
	// versions: two versions disagreeing about whether the thing is a store would
	// enforce two promises on one interface, which is the whole reason a contract
	// is a record and not a name for a service plus an interface name.
	ErrKindChanged = errors.New("contract: the kind of a contract does not change between versions")
)

// Contract is one contract as it is stored: one published interface or store of
// one service, identified by that service and the interface's own name in its
// build, with the kind everything else follows from.
type Contract struct {
	ID        string
	Actor     record.Actor
	At        string
	ServiceID string
	Name      string
	Kind      Kind
}

// Version is one contract version as it is stored: the semver, the release that
// published it and that release's own number, and the item that release was cut
// from.
//
// The release's number is here and the release record is not read to get it,
// which schema.go says the reason and the cost of. The item is here for the same
// kind of reason one level along: the declarations in force over a range of
// releases are the ones derived by those releases' items, and a reader walking
// from a version to what it was authored from would otherwise read a release
// record to find out.
type Version struct {
	ID            string
	Actor         record.Actor
	At            string
	ContractID    string
	ServiceID     string
	ReleaseID     string
	ReleaseNumber int64
	ItemID        string
	Semver        Semver
}

// ElementSubject is how a pin names one element of one contract: the contract's
// id and the element's name. It is not the element row's id — that changes at
// every version, and a pin has to outlive one — and it is not the element's name
// alone, which two contracts can share.
func ElementSubject(contractID, element string) string { return contractID + "." + element }
