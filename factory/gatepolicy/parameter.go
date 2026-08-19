package gatepolicy

import (
	"errors"
	"fmt"
)

// Parameter is one value an owner may author. There are eight and gate policy
// is seven rows; doc.go says which row carries two.
type Parameter string

const (
	// RiskThreshold is where the score stops auto-passing and puts a human at
	// the gate. It is a field of an environment record per gate row, and of the
	// factory policy record for the row that decides what an agent is told.
	RiskThreshold Parameter = "risk_threshold"
	// AttemptBound is how many times a stage is retried before the item
	// escalates. It is a field of the factory policy record, per stage.
	AttemptBound Parameter = "attempt_bound"
	// ItemSizeTarget is how large an item is meant to be, above the minimum
	// that it ships by itself. It is a field of the area record.
	ItemSizeTarget Parameter = "item_size_target"
	// PredicateCatalog is what kinds of assertion a consumer's declaration may
	// draw from. It is a field of the factory policy record.
	PredicateCatalog Parameter = "predicate_catalog"
	// WindowSize is the smallest regression the comparison must rule out to
	// close a watch window clean. It is a field of the service record.
	WindowSize Parameter = "window_size"
	// WindowConfidence is how sure the comparison must be. It is a field of the
	// service record, and shares a row with WindowSize.
	WindowConfidence Parameter = "window_confidence"
	// WindowCap is the elapsed time that ends a window which will never reach
	// its volume. It is a field of the service record.
	WindowCap Parameter = "window_cap"
	// K is how many watch windows one service may hold open at once. It is a
	// field of the service record.
	K Parameter = "k"
)

// Kind is what a parameter's value is, which decides how a pin clamps it and
// how a value is written and printed.
type Kind string

const (
	// KindFraction is a value between nothing and one.
	KindFraction Kind = "fraction"
	// KindCount is a whole number above zero.
	KindCount Kind = "count"
	// KindSeconds is an elapsed time in seconds.
	KindSeconds Kind = "seconds"
	// KindList is a list of names, clamped by union.
	KindList Kind = "list"
)

// Direction is which way a pin on a parameter may move the value in force. All
// three point toward more protection; doc.go says why that is the whole rule.
type Direction string

const (
	// DirectionCeiling caps the value in force.
	DirectionCeiling Direction = "ceiling"
	// DirectionFloor raises the value in force, and for a list is the union of
	// the pinned names and the value in force.
	DirectionFloor Direction = "floor"
	// DirectionAddsAHuman adds a human at the gate and carries no bound. It is
	// the risk threshold's direction and no other's.
	DirectionAddsAHuman Direction = "adds_a_human"
)

// Scope is the record a parameter is authored on.
type Scope string

const (
	// ScopeEnvironment is a field of an environment record, per gate row.
	ScopeEnvironment Scope = "environment"
	// ScopeService is a field of the service record.
	ScopeService Scope = "service"
	// ScopeArea is a field of the area record.
	ScopeArea Scope = "area"
	// ScopeFactoryPolicy is a field of the factory policy record.
	ScopeFactoryPolicy Scope = "factory_policy"
)

// Definition is everything this package knows about one parameter.
type Definition struct {
	Parameter Parameter
	// Row is the gate-policy row the parameter belongs to. Two parameters
	// share one row; every other row has one parameter.
	Row       string
	Kind      Kind
	Direction Direction
	Scope     Scope
	// Unit is what the number means, for a printer and for an owner typing
	// one. A parameter of KindList has none.
	Unit string
	// ReaderAtThisMilestone says which mechanism reads the value in force, and
	// is empty for a parameter nothing reads yet — which is four of the eight.
	// It is here so that a printer can say so rather than leaving an owner to
	// discover that what they authored changed nothing.
	ReaderAtThisMilestone string
}

// Definitions is every parameter, in the order gate policy's own table lists
// the rows.
var Definitions = []Definition{
	{
		Parameter: RiskThreshold, Row: "risk threshold",
		Kind: KindFraction, Direction: DirectionAddsAHuman, Scope: ScopeEnvironment,
		Unit:                  "the number a gate compares, between 0 and 1",
		ReaderAtThisMilestone: "both gate rows",
	},
	{
		Parameter: AttemptBound, Row: "attempt bound",
		Kind: KindCount, Direction: DirectionCeiling, Scope: ScopeFactoryPolicy,
		Unit:                  "attempts at one stage",
		ReaderAtThisMilestone: "the stages that retry",
	},
	{
		Parameter: ItemSizeTarget, Row: "item-size target",
		Kind: KindCount, Direction: DirectionCeiling, Scope: ScopeArea,
		Unit: "lines an item changes",
	},
	{
		Parameter: PredicateCatalog, Row: "the predicate catalog",
		Kind: KindList, Direction: DirectionFloor, Scope: ScopeFactoryPolicy,
	},
	{
		Parameter: WindowSize, Row: "the watch window's size and confidence",
		Kind: KindFraction, Direction: DirectionCeiling, Scope: ScopeService,
		Unit: "the smallest regression ruled out, as a share",
	},
	{
		Parameter: WindowConfidence, Row: "the watch window's size and confidence",
		Kind: KindFraction, Direction: DirectionFloor, Scope: ScopeService,
		Unit: "the confidence required, as a share",
	},
	{
		Parameter: WindowCap, Row: "the watch window's cap",
		Kind: KindSeconds, Direction: DirectionFloor, Scope: ScopeService,
		Unit: "seconds",
	},
	{
		Parameter: K, Row: "K",
		Kind: KindCount, Direction: DirectionCeiling, Scope: ScopeService,
		Unit: "windows open at once, per service",
	},
}

// ErrUnknown is returned by [Define] for a name that is not one of the eight.
var ErrUnknown = errors.New("gatepolicy: not one of gate policy's parameters")

// Define is one parameter's definition. A name outside the eight is
// [ErrUnknown] rather than a zero definition, so a caller that took a
// parameter from an owner's input cannot resolve one that does not exist.
func Define(p Parameter) (Definition, error) {
	for _, d := range Definitions {
		if d.Parameter == p {
			return d, nil
		}
	}
	return Definition{}, fmt.Errorf("%w: %q", ErrUnknown, p)
}

// Rows is every gate-policy row, in the order [Definitions] lists them, each
// named once however many parameters it carries.
func Rows() []string {
	var rows []string
	for _, d := range Definitions {
		if len(rows) == 0 || rows[len(rows)-1] != d.Row {
			rows = append(rows, d.Row)
		}
	}
	return rows
}
