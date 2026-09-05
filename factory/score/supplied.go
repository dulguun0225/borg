package score

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// Supplied is one value the score supplies where an owner authored nothing: the
// parameter, the subject the value stands for, the number, and why that number.
// The reason is published with the number, because a default nobody chose is
// still a decision and it can stay invisible until it takes effect.
//
// The subject is the record an authored value would be a field of — a service, an
// area, a stage, a gate row — and is empty on a starting value, which stands for
// every subject that has no row of its own. That is what makes the value in force
// answerable per subject: an owner authors the window limit on one service record,
// so the score supplies it for one service, and the two are the same key.
//
// The threshold's subject is the gate row and not the environment its authored
// value is a field of. What an outcome teaches is about the row — a change
// auto-passed at the merge row and rolled back says nothing about the row that
// deployed it to a candidate environment — and every row of the default path
// reads production's environment record anyway, so there is one environment and
// eight rows to tell apart.
type Supplied struct {
	Parameter gatepolicy.Parameter `json:"parameter"`
	Subject   string               `json:"subject,omitempty"`
	Value     float64              `json:"value"`
	Why       string               `json:"why"`
}

// Moved reports whether this value is one an outcome moved rather than the
// starting value. A moved value names the subject it moved for; a starting value
// names none.
func (s Supplied) Moved() bool { return s.Subject != "" }

// SuppliedValues is every value the score supplies: the starting value for each
// parameter, and a row per subject whose value an outcome has moved. It is what a
// [Version] stores and what package policy reads the value in force out of.
type SuppliedValues []Supplied

// Value is what the score supplies for one parameter on one subject: the row
// for that subject where an outcome has moved it, the starting value otherwise,
// and false for a parameter the score supplies nothing for. The list of allowed
// predicate kinds is the only one of those, and a caller reading false there
// has an empty list rather than a missing value.
func (v SuppliedValues) Value(p gatepolicy.Parameter, subject string) (Supplied, bool) {
	if subject != "" {
		for _, s := range v {
			if s.Parameter == p && s.Subject == subject {
				return s, true
			}
		}
	}
	for _, s := range v {
		if s.Parameter == p && s.Subject == "" {
			return s, true
		}
	}
	// A version appended before this parameter had a starting value holds no row
	// for it, so the source's own starting value answers. That is also the answer
	// for a factory that has appended no version at all.
	return Starting(p)
}

// Text renders the table for a reader: the starting value of each parameter in
// the order [starting] lists them, and under each the subjects an outcome has
// moved it for. It is what a printer shows and what the command-line interface
// reads aloud; what a version stores is the structure, because package policy
// reads a number out of it and no reader of prose can.
func (v SuppliedValues) Text() string {
	var b strings.Builder
	for _, s := range starting {
		row, _ := v.Value(s.Parameter, "")
		fmt.Fprintf(&b, "%s = %v: %s\n", row.Parameter, row.Value, row.Why)
		moved := v.movedFor(s.Parameter)
		for _, m := range moved {
			fmt.Fprintf(&b, "    %s = %v: %s\n", m.Subject, m.Value, m.Why)
		}
	}
	return b.String()
}

// movedFor is every moved row of one parameter, ordered by subject so that two
// tables holding the same values read the same.
func (v SuppliedValues) movedFor(p gatepolicy.Parameter) []Supplied {
	var moved []Supplied
	for _, s := range v {
		if s.Parameter == p && s.Moved() {
			moved = append(moved, s)
		}
	}
	sort.Slice(moved, func(i, j int) bool { return moved[i].Subject < moved[j].Subject })
	return moved
}

// StartingHeldOutSampleRate is where the held-out sample rate starts. One in
// ten: lower and the unbiased evidence the threshold's rise depends on arrives
// too slowly to move anything on an install shipping a few items a day; higher
// and the factory is auto-passing changes it wanted gated often enough that an
// owner would notice it as the score having changed its mind.
const StartingHeldOutSampleRate = 0.10

// starting is the value the score supplies for ten of gate policy's eleven rows
// before any outcome has moved it. There is none for the list of allowed
// predicate kinds, which no outcome teaches, so a factory with nothing authored
// has an empty list and not a supplied one.
//
// These are the numbers the formula was calibrated at against a factory that has
// just been installed, and every one of them is where the movement starts rather
// than where it stays. [Rules] is what moves each.
var starting = []Supplied{
	{
		Parameter: gatepolicy.RiskThreshold, Value: 0.30,
		Why: "calibrated so that a service's first release — no earlier release to return to, an author nobody has approved, an area with no history — is decided by a human, and the item after it is not; measured on the fake-model run in cmd/factory's tests at 0.34 for a first release and 0.14 for the item after it, once the exposure factor is added to impact rather than folded into its mean",
	},
	{
		Parameter: gatepolicy.ExposureBound, Value: 0.70,
		Why: "the exposure factor's value above which it stops being weighed and a human decides at Implementation instead, as a share of the factor's own scale; exposure only ever raises the number, so a low bound would put a human at nearly every diff that touches an outbound call or a credential",
	},
	{
		Parameter: gatepolicy.AdvisorySeverity, Value: 7.0,
		Why: "the bound at or above which a matching advisory rejects at Implementation and holds at Deploy to production, at the conventional boundary between a high and a medium severity on the advisory feed's own scale",
	},
	{
		Parameter: gatepolicy.AttemptLimit, Value: 3,
		Why: "a stage that fails once has usually had a reply the protocol refused rather than work the factory cannot do, and a limit this low turns solvable work into human work no more than a few tokens later",
	},
	{
		Parameter: gatepolicy.ItemSizeTarget, Value: 5,
		Why: "the count of the intent's requirements an item answers, above the minimum that it ships by itself, which is the unit decomposition sets",
	},
	{
		Parameter: gatepolicy.WindowSize, Value: 0.02,
		Why: "the smallest regression a comparison must rule out, as a share, one value per quantity on a subject [QuantitySubject] keys; the traffic a comparison needs scales as the inverse square of this, so it is the coarse end of what is worth catching",
	},
	{
		Parameter: gatepolicy.WindowConfidence, Value: 0.95,
		Why: "the confidence required of that comparison, at the convention a reader of a sequential test expects; no outcome moves it, because nothing in the record says a confidence was too high",
	},
	{
		Parameter: gatepolicy.WindowPower, Value: 0.80,
		Why: "how reliably a regression of the size in force is caught rather than reaching passed, at the convention a reader of a sequential test expects, one value per quantity on a subject [QuantitySubject] keys",
	},
	{
		Parameter: gatepolicy.WindowCap, Value: 86400,
		Why: "seconds — a day, after which a window that will never reach its volume ends unresolved rather than holding the next deploy indefinitely",
	},
	{
		Parameter: gatepolicy.WindowLimit, Value: 1,
		Why: "the serial factory: one window open per service, so a rollback undoes one release, which is the safe end of a parameter whose cost appears only at the first rollback",
	},
	{
		Parameter: gatepolicy.HeldOutSampleRate, Value: StartingHeldOutSampleRate,
		Why: "how often the score auto-passes a change it would have gated, to keep unbiased signal on the authors and areas it has stopped trusting; [Score.HoldOut] draws against the value in force, which is what an owner authored where they authored one, this where they did not, and a safeguard's ceiling over either",
	},
	{
		Parameter: gatepolicy.ReviewSampleRate, Value: 0.05,
		Why: "how often a change the score would have auto-passed is put in front of a duty's human anyway, one value for every duty until an outcome moves one apart from the rest; low enough that it samples rather than replaces the gate it stands beside",
	},
}

// QuantitySubject is the subject a value authored per service and per quantity
// is supplied for: the analysis window's size and its power, which are one value
// per quantity because a detectable change in an error rate and one in a latency
// quantile are not one number. It is the key both this package and package
// policy read the row by, declared once here so the two cannot spell it apart.
func QuantitySubject(serviceID string, quantity gatepolicy.Quantity) string {
	return serviceID + "/" + string(quantity)
}

// Starting is the value the score supplies for one parameter before any outcome
// has moved it, and false for one it supplies none for.
func Starting(p gatepolicy.Parameter) (Supplied, bool) {
	for _, s := range starting {
		if s.Parameter == p {
			return s, true
		}
	}
	return Supplied{}, false
}

// StartingValues is the table a factory with no outcomes in it supplies: the
// starting value of each parameter and nothing moved. It is what [Learn] begins
// from and what a reader compares a moved table against.
func StartingValues() SuppliedValues {
	return append(SuppliedValues{}, starting...)
}
