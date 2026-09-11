package criterion

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
)

// Reliability is a criterion's outcome history read against a bound: how many
// builds it was decided over, how many of them disagreed with what it usually
// answers, that share, and whether the share is above the bound.
//
// It is derived and never authored. Each run's results already attach to the
// build and carry the composition with them, so nothing is written to produce
// this.
type Reliability struct {
	Builds        int
	Disagreements int
	Rate          float64
	// Unreliable is whether the rate is above the bound — what test practice
	// calls flaky. While it holds, the criterion's failure rejects nothing,
	// counts no attempt, and moves no prior, and Merge to master reads it as
	// absent; the result is still recorded. [Outcome.Blocks] is where that
	// exception is applied.
	Unreliable bool
	// EnteredAt is the build, among the ones read, at which the disagreement
	// rate first rose above the bound, in the order the caller gave them —
	// derived the same way the rate is, and empty where it never has. Leaving
	// unreliable needs it: the rate falling back under the bound is not
	// enough on its own, because a rate that recovers before the encoding is
	// re-authored is luck and not a fix.
	EnteredAt string
}

// Unreliable reads the criterion's outcome history over the builds given and
// says whether it is above the bound. The disagreement is a build whose
// outcome differs from the outcome that criterion returned on most of the
// builds composed from the same seed version as it; the rate is that count
// over the builds read, and fewer than two builds read is a rate of zero,
// there being nothing for an outcome to disagree with.
//
// buildIDs is the caller's own history, oldest first: which builds to read at
// all is the caller's, this package holding no fact about which builds belong
// to one item or one candidate's run order. Two narrowings happen inside:
// reaching is every build among them whose diff reaches the requirement the
// criterion names, supplied by the caller as a plain input since the diff is
// the build's and not this package's, and excluded before anything else is
// read; and the builds that remain are grouped by the seed version their
// composition carries, so two builds composed from different seeds are two
// answers to two questions and never a disagreement with each other. A
// composition this cannot read the seed version out of groups by its whole
// text unchanged, which keeps every build of it in a group by itself rather
// than merging builds this cannot tell apart — the safe direction of the two.
//
// The bound is a field of the service record: bound is what the caller read
// off it, authored or not, and this reads it back through
// [service.UnreliableBoundInForce] rather than trusting a raw number — an
// unauthored field read as its zero value would mark a criterion unreliable
// at its first disagreement, which is not what "nothing authored" means for
// this field.
//
// reauthoredAt is the build, among buildIDs, that the caller derived as
// carrying the earliest version of the criterion's encoding the recovery
// intent introduced, or empty where none has shipped yet. Once the rate has
// crossed the bound, at [Reliability.EnteredAt], it leaves unreliable only
// where reauthoredAt names a build at or after that one: the encoding a
// version fixed is in force, and the rate is under the bound. Raising the
// intent that fix comes from is the caller's, which doc.go names and the
// command-line interface now does.
func Unreliable(ctx context.Context, pool *pgxpool.Pool, criterionID string, buildIDs []string,
	bound gatepolicy.Authored, reaching []string, reauthoredAt string,
) (Reliability, error) {
	if criterionID == "" || len(buildIDs) == 0 {
		return Reliability{}, nil
	}
	inForce := service.UnreliableBoundInForce(bound)

	read := make([]string, 0, len(buildIDs))
	for _, id := range buildIDs {
		if !slices.Contains(reaching, id) {
			read = append(read, id)
		}
	}
	if len(read) == 0 {
		return Reliability{}, nil
	}

	rows, err := pool.Query(ctx, `select distinct on (build_id) build_id, outcome, composition
		from `+ResultTable+` where criterion_id = $1 and build_id = any($2)
		order by build_id, run desc`, criterionID, read)
	if err != nil {
		return Reliability{}, fmt.Errorf("criterion: reading the outcome history of %s: %w", criterionID, err)
	}
	defer rows.Close()

	outcomes := make(map[string]Outcome, len(read))
	compositions := make(map[string]string, len(read))
	for rows.Next() {
		var buildID, outcome, composition string
		if err := rows.Scan(&buildID, &outcome, &composition); err != nil {
			return Reliability{}, fmt.Errorf("criterion: reading an outcome of %s: %w", criterionID, err)
		}
		outcomes[buildID] = Outcome(outcome)
		compositions[buildID] = composition
	}
	if err := rows.Err(); err != nil {
		return Reliability{}, fmt.Errorf("criterion: reading the outcome history of %s: %w", criterionID, err)
	}

	// ordered is read narrowed to the builds this criterion was actually
	// decided against, in the order the caller gave them — what lets
	// EnteredAt be read as a position in it.
	ordered := make([]string, 0, len(read))
	for _, id := range read {
		if _, ok := outcomes[id]; ok {
			ordered = append(ordered, id)
		}
	}

	r := reliabilityOver(ordered, outcomes, compositions, inForce)
	r.EnteredAt = enteredAt(ordered, outcomes, compositions, inForce)

	switch {
	case r.EnteredAt == "":
		// The rate never crossed the bound over this history: not unreliable,
		// and nothing to have left.
		r.Unreliable = false
	case !r.Unreliable && reauthoredAt != "" && atOrAfter(ordered, reauthoredAt, r.EnteredAt):
		// The rate is under the bound, and a version of the encoding
		// introduced at or after the entry is in force: it leaves.
	default:
		r.Unreliable = true
	}
	return r, nil
}

// reliabilityOver is the disagreement rate over ordered, grouping by the seed
// version [seedOf] reads off each build's composition and comparing outcomes
// only within one group. A group with nothing to disagree with contributes no
// disagreement, and fewer than two builds read at all is a rate of zero.
func reliabilityOver(ordered []string, outcomes map[string]Outcome, compositions map[string]string, bound float64) Reliability {
	total := len(ordered)
	if total < 2 {
		return Reliability{Builds: total}
	}
	groups := map[string]map[Outcome]int{}
	for _, id := range ordered {
		seed := seedOf(compositions[id])
		if groups[seed] == nil {
			groups[seed] = map[Outcome]int{}
		}
		groups[seed][outcomes[id]]++
	}
	disagreements := 0
	for _, counts := range groups {
		most := 0
		for _, n := range counts {
			if n > most {
				most = n
			}
		}
		groupTotal := 0
		for _, n := range counts {
			groupTotal += n
		}
		disagreements += groupTotal - most
	}
	r := Reliability{Builds: total, Disagreements: disagreements}
	r.Rate = float64(disagreements) / float64(total)
	r.Unreliable = r.Rate > bound
	return r
}

// enteredAt is the build, in ordered, at which the rate — computed over every
// build read up to and including it — first rises above bound, and empty
// where it never does over the whole of ordered.
func enteredAt(ordered []string, outcomes map[string]Outcome, compositions map[string]string, bound float64) string {
	for i := 1; i < len(ordered); i++ {
		if r := reliabilityOver(ordered[:i+1], outcomes, compositions, bound); r.Unreliable {
			return ordered[i]
		}
	}
	return ""
}

// atOrAfter is whether target sits at or after anchor in ordered, both being
// build ids the caller named. A target ordered does not hold is never at or
// after anything.
func atOrAfter(ordered []string, target, anchor string) bool {
	t, a := slices.Index(ordered, target), slices.Index(ordered, anchor)
	return t >= 0 && t >= a
}

// composedSeed is the one field this package reads out of a run's
// composition: package environment marshals [environment.Composition] onto
// that column, and criterion may not import it, so this reads the field it
// needs by name rather than the type.
type composedSeed struct {
	SeedVersion string
}

// seedOf is the seed version a run's composition carries, so that the outcome
// history groups by it rather than by the whole composition, which also names
// the dependency releases and the value-set version — two builds started from
// different seeds are two answers to two questions, but two builds differing
// only in a dependency release are the same question the design still reads as
// a disagreement. A composition this cannot parse, or that names no seed
// version, groups by the whole string unchanged.
func seedOf(composition string) string {
	var c composedSeed
	if err := json.Unmarshal([]byte(composition), &c); err == nil && c.SeedVersion != "" {
		return c.SeedVersion
	}
	return composition
}
