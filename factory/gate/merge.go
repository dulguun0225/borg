package gate

import (
	"errors"
	"fmt"
	"slices"
)

// The Merge to master row's own vocabulary: the checks that reject on their own
// terms before anyone gives a verdict, and the derivations that could not derive
// and so put a human there instead.

// The mechanical checks that reject at the merge row, in the words a close event
// names one by. They are constants here so that a caller cannot report a
// rejection under a name of its own, which is the arrangement the holds already
// have; what computes each of them reads the criteria results, the contracts,
// the consumer contracts, the mutation reading and the security predicates, and
// this package imports none of those stores.
const (
	// AutoRejectedByCriterion is an acceptance criterion in force the
	// candidate's run did not pass. An undecided criterion is read the way a
	// failure is, and one the service's own unreliable bound marks unreliable
	// reads as absent.
	AutoRejectedByCriterion = "an acceptance criterion in force that the candidate's run did not pass"
	// AutoRejectedByEncoding is an encoding check the candidate's run did not
	// pass: the checks over the encoding itself rather than over the behaviour
	// it encodes.
	AutoRejectedByEncoding = "an encoding check the candidate's run did not pass"
	// AutoRejectedByContractDiff is the producer's own diff: the form the
	// candidate publishes against the version its service's current release
	// publishes, breaking, with the migration not shipped ahead of it.
	AutoRejectedByContractDiff = "the producer's own contract diff"
	// AutoRejectedByConsumerContract is a consumer contract in force that the
	// candidate does not satisfy, decided against the candidate's own run.
	AutoRejectedByConsumerContract = "a consumer contract"
	// AutoRejectedBySafeguardPredicate is a safeguard's predicate naming an
	// element the candidate removes. It is told apart from a consumer contract
	// because an owner placed it and a derivation did not, and what a reader of
	// the rejection needs is the safeguard and its author.
	AutoRejectedBySafeguardPredicate = "a safeguard's predicate"
	// AutoRejectedByMutationFloor is a mutation score — the share of seeded
	// defects the encodings caught — below the mutation floor. The score
	// supplies the floor where none is authored, and a safeguard may raise it
	// and never lower it.
	AutoRejectedByMutationFloor = "the mutation score is below the mutation floor"
	// AutoRejectedBySecurityPredicate is a security predicate the candidate's
	// run decided against it and did not satisfy.
	AutoRejectedBySecurityPredicate = "a security predicate"
)

// MechanicalChecks is every check that rejects on its own terms at the merge
// row, in the order the design names them.
var MechanicalChecks = []string{
	AutoRejectedByCriterion, AutoRejectedByEncoding, AutoRejectedByContractDiff,
	AutoRejectedByConsumerContract, AutoRejectedBySafeguardPredicate,
	AutoRejectedByMutationFloor, AutoRejectedBySecurityPredicate,
}

// The derivations that put a human at the merge row where they could not derive
// a result, in the words the open event stores. A derivation that could not
// derive is not a rejection: what it says is that nothing decided, which is what
// an undecided criterion already says.
const (
	// CouldNotDeriveEncoding is an encoding whose derivation produced no result
	// over the candidate's run.
	CouldNotDeriveEncoding = "an encoding's derivation could not derive a result"
	// CouldNotDeriveSecurityPredicate is a security predicate whose derivation
	// produced no result, which puts a human here the way it already does at a
	// contract-touching gate.
	CouldNotDeriveSecurityPredicate = "a security predicate's derivation could not derive a result"
	// CouldNotDeriveNoticeFile is a release whose notice file reads could not
	// derive because its build's resolved set does.
	CouldNotDeriveNoticeFile = "the release's notice file could not be derived from the build's resolved set"
)

// Derivations is every derivation that puts a human at the merge row by failing
// to derive.
var Derivations = []string{
	CouldNotDeriveEncoding, CouldNotDeriveSecurityPredicate, CouldNotDeriveNoticeFile,
}

var (
	// ErrCheckUnknown is returned by [Gate.AutoReject] for a check outside
	// [MechanicalChecks].
	ErrCheckUnknown = errors.New("gate: that is not a check this row rejects on")
	// ErrDerivationUnknown is returned by [Gate.Fire] for a could-not-derive
	// outside [Derivations].
	ErrDerivationUnknown = errors.New("gate: that is not a derivation that puts a human at this row")
)

// checkDerivations refuses a could-not-derive this package does not own, and one
// reported at a row that has no derivation to fail: the three are the merge
// row's.
func checkDerivations(row Row, couldNotDerive []string) error {
	if len(couldNotDerive) == 0 {
		return nil
	}
	if row.Kind != KindMergeToMaster {
		return fmt.Errorf("%w: %v at %s", ErrDerivationUnknown, couldNotDerive, row)
	}
	for _, d := range couldNotDerive {
		if !slices.Contains(Derivations, d) {
			return fmt.Errorf("%w: %q", ErrDerivationUnknown, d)
		}
	}
	return nil
}
