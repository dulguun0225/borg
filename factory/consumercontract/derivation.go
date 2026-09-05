package consumercontract

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/record"
)

// Cause is why a derivation could not run at all. The two call for different
// responses — the first is lifted by the extractor shipping, the second by an item
// on the consumer or on the extractor — and a record that cannot tell them apart
// leaves the reader at the gate to guess which, on a consumer the record already
// says nobody could read.
type Cause string

const (
	// CauseNoExtractor is that no extractor covers the consumer's toolchain.
	CauseNoExtractor Cause = "no_extractor"
	// CauseExtractionFailed is that an extractor ran and failed, and the record
	// carries what it reported.
	CauseExtractionFailed Cause = "extraction_failed"
)

// Causes is every cause a could-not-derive record may name. The CHECK in [DDL]
// lists the same two.
var Causes = []Cause{CauseNoExtractor, CauseExtractionFailed}

var (
	// ErrDerivationIncomplete is returned for a derivation missing something
	// every one names, and for one whose facts contradict each other: a
	// could-not-derive record carrying predicates, an extraction that failed
	// reporting nothing, or a cause outside [Causes].
	ErrDerivationIncomplete = errors.New("consumercontract: the derivation is missing something every one names")
	// ErrExtractorUnchanged is returned by [DeriveAgain] where the newest
	// derivation of the item already names this extractor at this version. An
	// upgrade derives again where the shipped extractor for a toolchain changed
	// or was added, and deriving again with the same extractor would write a
	// second record saying what the first says.
	ErrExtractorUnchanged = errors.New("consumercontract: that item's newest derivation already names this extractor")
	// ErrDerivationNotFound is returned where the named derivation does not
	// exist.
	ErrDerivationNotFound = errors.New("consumercontract: no derivation has that id")
)

// Extractor is which extractor derived a consumer contract: its name and version,
// the toolchain it covers, and the factory version that shipped it. A derivation
// is a function of the code and of the factory version, so a record naming only
// the code would be right about the code and silent about the extractor.
type Extractor struct {
	Name           string
	Version        string
	Toolchain      string
	FactoryVersion string
}

// Derived is what one run of an extractor produced, before it is a record: the
// extractor, the constructs it met and could not follow, why it could not run at
// all, and the predicates it found.
type Derived struct {
	Extractor Extractor
	// Unfollowed is the constructs the extractor met and could not follow — a
	// read through reflection, a string-keyed access, a generated accessor, a
	// mapping read from configuration. A run whose list is empty is complete and
	// one whose list is not is partial.
	Unfollowed []string
	// Cause is empty on a run that derived, and names why on one that could not.
	Cause Cause
	// Reported is what the extractor reported, on [CauseExtractionFailed] and
	// nowhere else.
	Reported string
	Drafts   []Draft
}

// Partial reports whether the extractor met something it could not follow. See
// [Derivation.Partial]: the two carry the same fact before and after storage.
func (d Derived) Partial() bool { return len(d.Unfollowed) > 0 }

// CouldNotDerive reports whether the run could not derive at all.
func (d Derived) CouldNotDerive() bool { return d.Cause != "" }

// Describe is the run in the words a reader at a gate sees. See
// [Derivation.Describe].
func (d Derived) Describe() string {
	switch d.Cause {
	case CauseNoExtractor:
		return "could not derive: no extractor covers " + d.Extractor.Toolchain
	case CauseExtractionFailed:
		return "could not derive: the extractor " + d.Extractor.Name + " failed: " + d.Reported
	}
	if d.Partial() {
		return fmt.Sprintf("partial: %s could not follow %d construct(s)", d.Extractor.Name, len(d.Unfollowed))
	}
	return "complete: derived by " + d.Extractor.Name + " " + d.Extractor.Version
}

// Derivation is one consumer contract's derivation as it is stored: which
// extractor produced it, what that extractor could not follow, and why one could
// not run at all. It is one row per consumer contract version, beside the
// predicate rows that version introduced.
//
// A record whose Unfollowed list is empty is complete, and one whose list is not
// is partial. A record with a cause is could not derive — a record and not an
// empty list, because "no consumer reads this" and "no consumer's read was
// visible" call for opposite responses and a record that cannot tell them apart
// licenses the wrong one silently.
type Derivation struct {
	ID    string
	Actor record.Actor
	At    string
	// ItemID and ServiceID are the consumer's, and ArtifactID is the consumer
	// contract version this derivation is of.
	ItemID     string
	ServiceID  string
	ArtifactID string
	Extractor  Extractor
	Unfollowed []string
	Cause      Cause
	Reported   string
}

// Partial reports whether the extractor met something it could not follow. The
// deprecation list reads a partial record the way it reads a could-not-derive
// one: the consumer stays on the list of every marked element of every producer
// contract the record names. Nothing else reads a partial record differently from
// a complete one.
func (d Derivation) Partial() bool { return len(d.Unfollowed) > 0 }

// CouldNotDerive reports whether the derivation could not run at all.
func (d Derivation) CouldNotDerive() bool { return d.Cause != "" }

// Describe is the derivation in the words a reader at a gate sees.
func (d Derivation) Describe() string {
	switch d.Cause {
	case CauseNoExtractor:
		return "could not derive: no extractor covers " + d.Extractor.Toolchain
	case CauseExtractionFailed:
		return "could not derive: the extractor " + d.Extractor.Name + " failed: " + d.Reported
	}
	if d.Partial() {
		return fmt.Sprintf("partial: %s could not follow %d construct(s)", d.Extractor.Name, len(d.Unfollowed))
	}
	return "complete: derived by " + d.Extractor.Name + " " + d.Extractor.Version
}

// validate refuses a derivation nothing can read: one missing a link every
// derivation has, one naming neither an extractor nor a cause, one whose cause is
// outside [Causes], an extraction that failed reporting nothing, and a report on
// anything but a failure.
func (d Derived) validate(of Of) error {
	for _, required := range []struct{ what, value string }{
		{"consumer's item", of.ItemID}, {"consumer's service", of.ServiceID},
		{"consumer contract version", of.ArtifactID}, {"toolchain", d.Extractor.Toolchain},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: it names no %s", ErrDerivationIncomplete, required.what)
		}
	}
	if d.Cause != "" && !slices.Contains(Causes, d.Cause) {
		return fmt.Errorf("%w: %q is neither of the two causes", ErrDerivationIncomplete, d.Cause)
	}
	if d.Cause == "" && d.Extractor.Name == "" {
		return fmt.Errorf("%w: it names neither an extractor nor a cause", ErrDerivationIncomplete)
	}
	if d.Cause != "" && len(d.Drafts) > 0 {
		return fmt.Errorf("%w: it could not derive and carries %d predicate(s)",
			ErrDerivationIncomplete, len(d.Drafts))
	}
	if d.Cause == CauseExtractionFailed && d.Reported == "" {
		return fmt.Errorf("%w: an extraction that failed reports what the extractor said",
			ErrDerivationIncomplete)
	}
	if d.Cause != CauseExtractionFailed && d.Reported != "" {
		return fmt.Errorf("%w: only an extraction that failed reports anything", ErrDerivationIncomplete)
	}
	return nil
}
