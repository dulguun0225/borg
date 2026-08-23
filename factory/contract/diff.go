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
// Breaking is not a sixth list of its own kind: it is the elements whose change
// the contract's promise does not allow, computed from the kind at the same time
// as the rest, because which changes break is a fact about the kind and a caller
// recomputing it would be a second place the rule lives.
type Change struct {
	Added    []string
	Removed  []string
	Retyped  []string
	Weakened []string
	Marked   []string
	Unmarked []string
	Breaking []string
}

// Moved reports whether the form moved at all, which is what says a release
// publishes a version. An element added, removed, or redeclared moves it, and so
// does a mark placed or taken off: the version tracks the form, so a mark mints
// one — a consumer seeing a bump for an annotation nothing breaks on, which is
// the cost of that choice and not an oversight.
func (c Change) Moved() bool {
	return len(c.Added)+len(c.Removed)+len(c.Retyped)+len(c.Weakened)+len(c.Marked)+len(c.Unmarked) > 0
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
		{"no longer always populated", c.Weakened}, {"marked deprecated", c.Marked},
		{"no longer marked", c.Unmarked},
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
// Three changes break a published interface's backward promise: an element
// removed, an element whose type changed, and an element that was always
// populated becoming optional. Each breaks a consumer that reads the element, and
// nothing here asks whether one does — who is affected is a separate query over
// the consumer contracts, and this is the diff.
//
// A store breaks on a fourth, because its promise runs forward as well: an
// element added and always populated. The build being restored does not write it,
// so the newer build's own reads of it find nothing — which is the direction
// backward alone does not cover and the one a rollback reads in.
//
// An element added and a mark placed or taken off break nothing. before is the
// zero [Form] where the contract is published for the first time, and everything
// is then an addition — which breaks nothing on an interface and, on a store,
// breaks nothing either: there is no earlier build to restore.
func Diff(before, after Form) Change {
	before, after = before.Sorted(), after.Sorted()
	var c Change
	first := len(before.Elements) == 0 && before.Name == ""
	for _, e := range after.Elements {
		was, found := before.Element(e.Name)
		if !found {
			c.Added = append(c.Added, e.Name)
			if !first && after.Kind.Forward() && e.Populated {
				c.Breaking = append(c.Breaking, e.Name)
			}
			continue
		}
		if was.Type != e.Type {
			c.Retyped = append(c.Retyped, e.Name)
			c.Breaking = append(c.Breaking, e.Name)
		}
		if was.Populated && !e.Populated {
			c.Weakened = append(c.Weakened, e.Name)
			if !slices.Contains(c.Breaking, e.Name) {
				c.Breaking = append(c.Breaking, e.Name)
			}
		}
		switch {
		case !was.Deprecated && e.Deprecated:
			c.Marked = append(c.Marked, e.Name)
		case was.Deprecated && !e.Deprecated:
			c.Unmarked = append(c.Unmarked, e.Name)
		}
	}
	for _, e := range before.Elements {
		if _, found := after.Element(e.Name); !found {
			c.Removed = append(c.Removed, e.Name)
			c.Breaking = append(c.Breaking, e.Name)
		}
	}
	for _, list := range []*[]string{&c.Added, &c.Removed, &c.Retyped, &c.Weakened, &c.Marked, &c.Unmarked, &c.Breaking} {
		slices.Sort(*list)
	}
	return c
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
