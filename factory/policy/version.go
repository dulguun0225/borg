package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// ErrNoVersion is returned where no policy version has been appended yet — a
// factory that has not been installed has none, and a gate cannot fire without
// one — and by [Reader.Version] where no version has the id asked for.
var ErrNoVersion = errors.New("policy: no policy version")

// FormatVersion is what every policy version row of the decision log declares
// itself as, and what [decisionlog.Formats] maps to
// [decisionlog.ShapePolicyVersion].
const FormatVersion = "policy_version/1"

// Caller is the component that called for the append. The log is the writer of
// every policy version; who called is a field of the row.
type Caller string

const (
	// CallerFactory is an owner's write at Factory.
	CallerFactory Caller = "Factory"
	// CallerPeople is a write to the People declaration other than the
	// key-to-name mapping, which stays outside the chain.
	CallerPeople Caller = "People"
	// CallerInstall is the install's first-start step, which appends for the
	// shipped list of allowed predicate kinds at install and at an upgrade's
	// first start that changed it. Nothing calls as this yet: the install step
	// is not built.
	CallerInstall Caller = "the install's first-start step"
)

// Action is what the write was.
type Action string

const (
	// ActionCreated is a record Factory created: the factory-wide settings
	// record, a project with production's environment, a customer's
	// environment, or an area.
	ActionCreated Action = "created"
	// ActionAuthored is an owner authoring one value on one record.
	ActionAuthored Action = "authored"
	// ActionSafeguardAdded is an owner placing a safeguard.
	ActionSafeguardAdded Action = "safeguard_added"
	// ActionHaltSet is an owner setting the halt whose subject is the factory.
	ActionHaltSet Action = "halt_set"
	// ActionLegalHoldSet is an owner setting a legal hold.
	ActionLegalHoldSet Action = "legal_hold_set"
	// ActionWithdrawalWritten is a withdrawal of a safeguard, a halt or a legal
	// hold written pending. What it withdraws stands until the gate row that
	// decides it approves it.
	ActionWithdrawalWritten Action = "withdrawal_written"
	// ActionWithdrawalApproved is that gate row's approval, which is where the
	// withdrawal comes into force.
	ActionWithdrawalApproved Action = "withdrawal_approved"
	// ActionDeclarationWritten is a write to the People declaration, with
	// People as the caller.
	ActionDeclarationWritten Action = "declaration_written"
	// ActionWithdrawn is a record an owner withdrew that no gate row decides:
	// an environment a customer defined.
	ActionWithdrawn Action = "withdrawn"
)

// Scope is the record an authored value is a field of, and the value of the
// parameter's own key where the parameter has one: the gate row for a
// threshold, the stage for an attempt limit, the quantity for the window's size
// and power, the duty for the review sample rate.
type Scope struct {
	Kind string
	ID   string
	Key  string
}

// The record kinds a scope names.
const (
	ScopeEnvironment     = "environment"
	ScopeService         = "service"
	ScopeArea            = "area"
	ScopeFactorySettings = "factory_settings"
	ScopeProject         = "project"
)

func (s Scope) String() string {
	if s.Key == "" {
		return s.Kind + ":" + s.ID
	}
	return s.Kind + ":" + s.ID + ":" + s.Key
}

// AuthoredValue is one authored parameter as a version names it: the parameter,
// the scope it was authored on, and the value.
type AuthoredValue struct {
	Parameter gatepolicy.Parameter `json:"parameter"`
	Scope     Scope                `json:"scope"`
	Number    float64              `json:"number,omitempty"`
	List      []string             `json:"list,omitempty"`
}

// AutoPassRate is the realized auto-pass rate at a threshold for one factor
// set, as it stood when the write happened.
type AutoPassRate struct {
	FactorSet string  `json:"factor_set"`
	Rate      float64 `json:"rate"`
}

// PersonDeclaration is one row of the People declaration as a version names it:
// by per-person key and never by name.
type PersonDeclaration struct {
	Key            string     `json:"key"`
	Duties         []int      `json:"duties,omitempty"`
	CredentialName string     `json:"credential_name,omitempty"`
	SpendCeiling   float64    `json:"spend_ceiling,omitempty"`
	Rates          []UnitRate `json:"rates,omitempty"`
}

// UnitRate is one rate an owner authored per kind of unit a provider returns,
// per model version and effort.
type UnitRate struct {
	Unit         string  `json:"unit"`
	ModelVersion string  `json:"model_version,omitempty"`
	Effort       string  `json:"effort,omitempty"`
	Rate         float64 `json:"rate"`
}

// DeclarationSnapshot is the People declaration in force, by key. Package
// policy cannot read it — the direction between the two packages is People to
// here — so it is supplied to [Factory] by the composition and copied onto each
// version.
type DeclarationSnapshot struct {
	People []PersonDeclaration `json:"people,omitempty"`
}

// Version is one policy version: a row of the decision log, naming the write
// and the whole authored state as it stood after it.
type Version struct {
	// ID is the log row's id, which is what a decision names.
	ID    string
	Actor record.Actor
	At    string

	Caller    Caller
	Action    Action
	Parameter gatepolicy.Parameter
	Scope     Scope
	Number    float64
	List      []string

	SafeguardID  string
	HaltID       string
	LegalHoldID  string
	WithdrawalID string

	// Key is the deterministic key of the write: a step taken again carries the
	// same key as the version in force and appends nothing.
	Key string

	// Authored is every authored parameter and the scope it was authored on.
	Authored []AuthoredValue
	// Safeguards, Halts and LegalHolds are the ids of each in force.
	Safeguards []string
	Halts      []string
	LegalHolds []string
	// Declaration is the People declaration in force, by per-person key.
	Declaration DeclarationSnapshot
	// AutoPassRates is the realized auto-pass rate at the threshold this write
	// set, one per factor set, and is empty on every version that set none. It
	// is the one field a later version does not restate.
	AutoPassRates []AutoPassRate
}

// payload is the version as it is serialised into the log row. The actor, the
// time and the id are the row's own columns and are not repeated here.
type payload struct {
	Caller        Caller               `json:"caller"`
	Action        Action               `json:"action"`
	Parameter     gatepolicy.Parameter `json:"parameter,omitempty"`
	Scope         Scope                `json:"scope"`
	Number        float64              `json:"number,omitempty"`
	List          []string             `json:"list,omitempty"`
	SafeguardID   string               `json:"safeguard_id,omitempty"`
	HaltID        string               `json:"halt_id,omitempty"`
	LegalHoldID   string               `json:"legal_hold_id,omitempty"`
	WithdrawalID  string               `json:"withdrawal_id,omitempty"`
	Key           string               `json:"key"`
	Authored      []AuthoredValue      `json:"authored,omitempty"`
	Safeguards    []string             `json:"safeguards,omitempty"`
	Halts         []string             `json:"halts,omitempty"`
	LegalHolds    []string             `json:"legal_holds,omitempty"`
	Declaration   DeclarationSnapshot  `json:"declaration"`
	AutoPassRates []AutoPassRate       `json:"auto_pass_rates,omitempty"`
}

func (v Version) marshal() (string, error) {
	body, err := json.Marshal(payload{
		Caller: v.Caller, Action: v.Action, Parameter: v.Parameter, Scope: v.Scope,
		Number: v.Number, List: v.List, SafeguardID: v.SafeguardID, HaltID: v.HaltID,
		LegalHoldID: v.LegalHoldID, WithdrawalID: v.WithdrawalID, Key: v.Key,
		Authored: v.Authored, Safeguards: v.Safeguards, Halts: v.Halts, LegalHolds: v.LegalHolds,
		Declaration: v.Declaration, AutoPassRates: v.AutoPassRates,
	})
	if err != nil {
		return "", fmt.Errorf("policy: serialising the version of %s: %w", v.Scope, err)
	}
	return string(body), nil
}

// versionOf reads one log row back as a version. A row of another shape is
// refused rather than half-read.
func versionOf(row decisionlog.Row) (Version, error) {
	if row.Shape != decisionlog.ShapePolicyVersion {
		return Version{}, fmt.Errorf("policy: row %s is a %s and not a policy version", row.ID, row.Shape)
	}
	var p payload
	if err := json.Unmarshal([]byte(row.Payload), &p); err != nil {
		return Version{}, fmt.Errorf("policy: reading the version %s: %w", row.ID, err)
	}
	return Version{
		ID: row.ID, Actor: row.Actor, At: row.At,
		Caller: p.Caller, Action: p.Action, Parameter: p.Parameter, Scope: p.Scope,
		Number: p.Number, List: p.List, SafeguardID: p.SafeguardID, HaltID: p.HaltID,
		LegalHoldID: p.LegalHoldID, WithdrawalID: p.WithdrawalID, Key: p.Key,
		Authored: p.Authored, Safeguards: p.Safeguards, Halts: p.Halts, LegalHolds: p.LegalHolds,
		Declaration: p.Declaration, AutoPassRates: p.AutoPassRates,
	}, nil
}

// writeKey is the deterministic key of one write: the caller, the actor, what
// was done, the parameter, the scope, and the value. A step taken again derives
// the same key, and [Factory] appends nothing where the version in force
// already carries it, so a repeated step writes nothing.
//
// The time of the call is not in it, because a key that carried the time would
// differ at every repeat and there would be no repeated step to recognise. The
// comparison is against the version in force and not against every version ever
// appended, so an owner who sets a value, sets another, and sets the first again
// writes all three.
func writeKey(caller Caller, actor record.Actor, action Action, parameter gatepolicy.Parameter,
	scope Scope, number float64, list []string, named string) string {
	h := sha256.New()
	for _, field := range []string{
		string(caller), string(actor.Kind), actor.Key, string(actor.Basis), string(action),
		string(parameter), scope.Kind, scope.ID, scope.Key,
		strconv.FormatFloat(number, 'g', -1, 64), strings.Join(list, "\n"), named,
	} {
		h.Write([]byte(strconv.Itoa(len(field))))
		h.Write([]byte(":"))
		h.Write([]byte(field))
	}
	return hex.EncodeToString(h.Sum(nil))
}
