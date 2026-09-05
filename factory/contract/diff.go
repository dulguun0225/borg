package contract

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Change is what one form does to the one before it, as lists of element names.
// Every list is sorted, so two runs over one pair of forms produce one answer.
//
// Breaking is not a list of its own kind: it is the elements whose change the
// contract's promise does not allow, computed from the position and the kind at
// the same time as the rest, because which changes break is a fact about those
// two and a caller recomputing it would be a second place the rule lives.
type Change struct {
	Added    []string
	Removed  []string
	Retyped  []string
	Weakened []string
	// Required is an element added as required, or an optional one that became
	// required. In input position it is breaking; nowhere else does it mean
	// anything.
	Required []string
	// Narrowed is an accepted value withdrawn from a domain or a range made
	// tighter, and Widened is the other direction, which breaks nobody.
	Narrowed []string
	Widened  []string
	// Constrained is a store element that gained a not-null constraint, a
	// uniqueness rule, or a domain check. The store rule reads it against every
	// declaration in force: a write declared inside the old range that the new
	// constraint would reject is what makes it breaking.
	Constrained []string
	Marked      []string
	Unmarked    []string
	Breaking    []string
}

// Moved reports whether the form moved at all, which is what says a release
// publishes a version. An element added, removed, or redeclared moves it, and so
// does a mark placed or taken off: the version tracks the form, so a mark mints
// one — a consumer seeing a bump for an annotation nothing breaks on, which is
// the cost of that choice and not an oversight.
func (c Change) Moved() bool {
	return len(c.Added)+len(c.Removed)+len(c.Retyped)+len(c.Weakened)+len(c.Required)+
		len(c.Narrowed)+len(c.Widened)+len(c.Constrained)+len(c.Marked)+len(c.Unmarked) > 0
}

// Describe is the change in the words a rejection row and an owner read. It names
// what changed per list and says nothing about whether it breaks, which the caller
// reads off Breaking.
func (c Change) Describe() string {
	var parts []string
	for _, of := range []struct {
		what  string
		names []string
	}{
		{"added", c.Added}, {"removed", c.Removed}, {"retyped", c.Retyped},
		{"no longer always populated", c.Weakened}, {"now required", c.Required},
		{"narrowed", c.Narrowed}, {"widened", c.Widened},
		{"newly constrained", c.Constrained},
		{"marked deprecated", c.Marked}, {"no longer marked", c.Unmarked},
	} {
		if len(of.names) > 0 {
			parts = append(parts, of.what+" "+strings.Join(of.names, ", "))
		}
	}
	if len(parts) == 0 {
		return "the form is unchanged"
	}
	return strings.Join(parts, "; ")
}

// Diff is what after does to before, over one contract of one kind.
//
// Which a change is depends on the position of the element it touches, because
// compatibility runs one way for what an interface returns and the other for what
// it accepts.
//
// In output position an element added is compatible, and one removed or
// redeclared — its type changed, or an element that was always populated
// becoming optional — is breaking. Each of the second sort breaks a consumer that
// reads the element, and nothing here asks whether one does: who is affected is a
// separate query over the consumer contracts, and this is the diff.
//
// In input position an element added as required, an accepted range narrowed, an
// accepted value withdrawn, and an element the caller supplies going away — an
// argument dropped — are breaking, where an element added as optional and a range
// widened are not. A retyped input element is breaking for the same reason a
// withdrawn value is: what the caller sends is no longer what the interface
// accepts.
//
// A store is read and written by the same service, so its promise runs forward as
// well and its elements break on the union of the two: an element added and
// always populated (the build being restored does not write it), an element
// removed, retyped or weakened, and a constraint added — a not-null constraint, a
// uniqueness rule, or a domain check narrowed — which a write a declaration in
// force still makes can violate. The store rule's own answer to a constraint is
// the backfill's completion and the declarations in force, which is enforcement's
// question and not the diff's.
//
// before is the zero [Form] where the contract is published for the first time,
// and everything is then an addition — which breaks nothing in any position: there
// is no consumer of a form nothing has published and no earlier build to restore.
func Diff(before, after Form) Change {
	before, after = before.Sorted(), after.Sorted()
	var c Change
	first := len(before.Elements) == 0 && before.Name == ""
	for _, e := range after.Elements {
		was, found := before.Element(e.Name)
		if !found {
			c.Added = append(c.Added, e.Name)
			if first {
				continue
			}
			if e.Required && e.Position == PositionInput {
				c.Required = append(c.Required, e.Name)
				c.breaks(e.Name)
			}
			if e.Position == PositionStore && e.Populated {
				c.breaks(e.Name)
			}
			continue
		}
		c.compare(was, e)
	}
	for _, e := range before.Elements {
		if _, found := after.Element(e.Name); !found {
			c.Removed = append(c.Removed, e.Name)
			// Removed breaks in every position: a consumer that reads it, a
			// caller that supplies it, and a restored build that reads what the
			// store no longer holds.
			c.breaks(e.Name)
		}
	}
	for _, list := range []*[]string{
		&c.Added, &c.Removed, &c.Retyped, &c.Weakened, &c.Required, &c.Narrowed,
		&c.Widened, &c.Constrained, &c.Marked, &c.Unmarked, &c.Breaking,
	} {
		slices.Sort(*list)
	}
	return c
}

// compare is one element against the one of the same name in the form below it.
func (c *Change) compare(was, e Element) {
	if was.Type != e.Type {
		c.Retyped = append(c.Retyped, e.Name)
		c.breaks(e.Name)
	}
	if was.Populated && !e.Populated {
		c.Weakened = append(c.Weakened, e.Name)
		if e.Position != PositionInput {
			c.breaks(e.Name)
		}
	}
	if !was.Required && e.Required {
		c.Required = append(c.Required, e.Name)
		if e.Position == PositionInput {
			c.breaks(e.Name)
		}
	}
	narrowed, widened := accepted(was, e)
	if narrowed {
		c.Narrowed = append(c.Narrowed, e.Name)
		if e.Position != PositionOutput {
			c.breaks(e.Name)
		}
	}
	if widened {
		c.Widened = append(c.Widened, e.Name)
	}
	if constrained(was, e) {
		c.Constrained = append(c.Constrained, e.Name)
		if e.Position == PositionStore {
			c.breaks(e.Name)
		}
	}
	switch {
	case !was.Deprecated && e.Deprecated:
		c.Marked = append(c.Marked, e.Name)
	case was.Deprecated && !e.Deprecated:
		c.Unmarked = append(c.Unmarked, e.Name)
	}
}

// breaks records one element as breaking, once however many of its facts changed.
func (c *Change) breaks(name string) {
	if !slices.Contains(c.Breaking, name) {
		c.Breaking = append(c.Breaking, name)
	}
}

// accepted is what happened to the domain and the range an element accepts: a
// value withdrawn or an end brought in is a narrowing, a value admitted or an end
// let out is a widening, and an element can do both at once.
func accepted(was, e Element) (narrowed, widened bool) {
	// A nil domain accepts any name, the way a nil range accepts any number, so
	// one appearing where there was none is a narrowing and one going away is a
	// widening — checked before the name-by-name comparison, which only applies
	// where both sides already name a domain.
	switch {
	case was.Domain == nil && e.Domain != nil:
		narrowed = true
	case was.Domain != nil && e.Domain == nil:
		widened = true
	default:
		for _, name := range was.Domain {
			if !slices.Contains(e.Domain, name) {
				narrowed = true
			}
		}
		for _, name := range e.Domain {
			if !slices.Contains(was.Domain, name) {
				widened = true
			}
		}
	}
	// A nil range accepts any number, so one appearing where there was none is a
	// narrowing and one going away is a widening.
	switch {
	case was.Range == nil && e.Range != nil:
		narrowed = true
	case was.Range != nil && e.Range == nil:
		widened = true
	case was.Range != nil && e.Range != nil:
		if e.Range.Low > was.Range.Low || e.Range.High < was.Range.High {
			narrowed = true
		}
		if e.Range.Low < was.Range.Low || e.Range.High > was.Range.High {
			widened = true
		}
	}
	return narrowed, widened
}

// constrained is whether the element gained one of the two constraints that are
// not a domain or a range: a not-null constraint or a uniqueness rule. A domain
// check narrowed is already a narrowing, and the store rule reads the two the
// same way.
func constrained(was, e Element) bool {
	return (!was.NotNull && e.NotNull) || (!was.Unique && e.Unique)
}

// Semver is a contract version: the compatibility promise to whoever calls it.
// Major means a consumer breaks, which is what semver is for and the whole reason
// the contract version is a separate axis from the release number.
type Semver struct {
	Major int
	Minor int
	Patch int
}

// FirstVersion is what a contract's first version is: one-nothing-nothing. The
// first version of a form breaks nobody, there being no earlier form, so it is a
// major of one rather than of nothing.
var FirstVersion = Semver{Major: 1}

func (v Semver) String() string {
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}

// Next is the version a moved form mints from this one: a major where the diff
// breaks and a minor where it does not.
//
// The patch never moves, here or anywhere. Nothing in a form is a patch-level
// change — every change to it is either something a consumer can break on or
// something it cannot — and the third number exists because semver has three. A
// reader who sees a patch above nothing is reading a version this factory did not
// mint.
func (v Semver) Next(breaking bool) Semver {
	if breaking {
		return Semver{Major: v.Major + 1}
	}
	return Semver{Major: v.Major, Minor: v.Minor + 1}
}

// ParseSemver is the version that string names, for a caller reading one back out
// of text rather than out of the three columns.
func ParseSemver(s string) (Semver, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("contract: %q is not a version of three numbers", s)
	}
	var v Semver
	for i, into := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return Semver{}, fmt.Errorf("contract: %q is not a version of three numbers", s)
		}
		*into = n
	}
	return v, nil
}
