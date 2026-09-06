package gate

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/score"
)

// Kind is which gate row a firing is at. Thirteen of the fourteen are fixed
// rows; [KindDeployToEnvironment] is the one a customer's own environment
// parameterises, a [Row] naming the environment beside it.
type Kind string

const (
	// KindDecomposition is the stage's own gate: the one row where approving
	// admits several timelines at once. It fires where decomposition yielded
	// more than one item and nowhere else, and what is approved is the set.
	KindDecomposition Kind = "decomposition"
	// KindSpec is where the acceptance criteria of one item are confirmed.
	KindSpec Kind = "spec"
	// KindImplementationPlan is where how the item will be built is decided.
	KindImplementationPlan Kind = "implementation_plan"
	// KindTasks is where the approved plan divided into work is decided.
	KindTasks Kind = "tasks"
	// KindImplementation is where the build's diff against master is decided.
	KindImplementation Kind = "implementation"
	// KindDeployToCandidateEnvironment is where the candidate's own environment
	// is created and the candidate's build is put on it. Nothing else attaches
	// to this row: no strategy, no rollout, no analysis window.
	KindDeployToCandidateEnvironment Kind = "deploy_to_candidate_environment"
	// KindMergeToMaster is the release event, and the latest row anything may
	// be sent back from.
	KindMergeToMaster Kind = "merge_to_master"
	// KindDeployToProduction is the last row before a release takes traffic,
	// and the one that picks the rollout strategy.
	KindDeployToProduction Kind = "deploy_to_production"
	// KindDeployToEnvironment is the row a customer that defines a further
	// environment gets, one per environment. It is fed from master, so it takes
	// deploy to production's actions and neither a strategy nor a window.
	KindDeployToEnvironment Kind = "deploy_to_environment"
	// KindRolePromptOrSkill is the first row outside every item: a version of
	// what an agent is told.
	KindRolePromptOrSkill Kind = "a_role_prompt_or_a_skill"
	// KindSafeguardWithdrawal is the second: the record that removes a human
	// from a gate. It reads no factor set and no threshold.
	KindSafeguardWithdrawal Kind = "a_safeguards_withdrawal"
	// KindHaltWithdrawal is the third: the record that ends a halt. It reads no
	// factor set and no threshold either, and routes to the owner.
	KindHaltWithdrawal Kind = "a_halts_withdrawal"
	// KindLegalHoldWithdrawal is the record that ends a legal hold. It reads no
	// factor set and no threshold either, and routes away from the human who
	// wrote the withdrawal.
	KindLegalHoldWithdrawal Kind = "a_legal_holds_withdrawal"
	// KindDecisionLogRetentionShortening is the row that decides a shortening of
	// decision-log retention, which takes the safeguard withdrawal's shape.
	KindDecisionLogRetentionShortening Kind = "decision_log_retention_shortening"
)

// Kinds is every kind a row may have, in the order the design names them: the
// eight of the default path, the further deploy row, and the five that belong to
// no item.
var Kinds = []Kind{
	KindDecomposition, KindSpec, KindImplementationPlan, KindTasks, KindImplementation,
	KindDeployToCandidateEnvironment, KindMergeToMaster, KindDeployToProduction,
	KindDeployToEnvironment,
	KindRolePromptOrSkill, KindSafeguardWithdrawal, KindHaltWithdrawal,
	KindLegalHoldWithdrawal, KindDecisionLogRetentionShortening,
}

// Row is one gate row: its kind and, on [KindDeployToEnvironment] alone, the
// environment that row deploys into. It is a value rather than a string so that
// a customer defining a further environment gets a row of its own without a
// second vocabulary, and so that no caller composes the environment into a name
// of its own.
type Row struct {
	Kind Kind
	// EnvironmentID is the environment a further deploy row deploys into. It is
	// required on [KindDeployToEnvironment] and refused on every other kind:
	// production's own row is [KindDeployToProduction], and a candidate
	// environment is composed at the row rather than named on it.
	EnvironmentID string
}

// Of is the row of one kind, for every kind but [KindDeployToEnvironment].
func Of(kind Kind) Row { return Row{Kind: kind} }

// DeployTo is the further deploy row for one customer-defined environment.
func DeployTo(environmentID string) Row {
	return Row{Kind: KindDeployToEnvironment, EnvironmentID: environmentID}
}

// The rows of the default path and the four outside it, as values, so that a
// caller names a row rather than composing one.
var (
	Decomposition                  = Of(KindDecomposition)
	Spec                           = Of(KindSpec)
	ImplementationPlan             = Of(KindImplementationPlan)
	Tasks                          = Of(KindTasks)
	Implementation                 = Of(KindImplementation)
	DeployToCandidateEnvironment   = Of(KindDeployToCandidateEnvironment)
	MergeToMaster                  = Of(KindMergeToMaster)
	DeployToProduction             = Of(KindDeployToProduction)
	RolePromptOrSkill              = Of(KindRolePromptOrSkill)
	SafeguardWithdrawal            = Of(KindSafeguardWithdrawal)
	HaltWithdrawal                 = Of(KindHaltWithdrawal)
	LegalHoldWithdrawal            = Of(KindLegalHoldWithdrawal)
	DecisionLogRetentionShortening = Of(KindDecisionLogRetentionShortening)
)

// Rows is every row of the default path, in the order the path reaches them. A
// further deploy row is not here: there is one per environment a customer
// defined, so the set is the environment records and not a list.
var Rows = []Row{
	Decomposition, Spec, ImplementationPlan, Tasks, Implementation,
	DeployToCandidateEnvironment, MergeToMaster, DeployToProduction,
}

// String is how a row is written onto an event and read back: the kind, and the
// environment after it on a further deploy row.
func (r Row) String() string {
	if r.Kind == KindDeployToEnvironment {
		return string(r.Kind) + ":" + r.EnvironmentID
	}
	return string(r.Kind)
}

var (
	// ErrRowUnknown is returned for a kind outside [Kinds].
	ErrRowUnknown = errors.New("gate: not a gate row")
	// ErrRowEnvironment is returned for a further deploy row naming no
	// environment, and for any other row naming one.
	ErrRowEnvironment = errors.New("gate: a further deploy row names the environment it deploys into, and no other row names one")
)

// Validate refuses a row whose kind is unknown and one that names an environment
// where its kind takes none.
func (r Row) Validate() error {
	if !slices.Contains(Kinds, r.Kind) {
		return fmt.Errorf("%w: %q", ErrRowUnknown, r.Kind)
	}
	if r.Kind == KindDeployToEnvironment && r.EnvironmentID == "" {
		return fmt.Errorf("%w: %s names none", ErrRowEnvironment, r.Kind)
	}
	if r.Kind != KindDeployToEnvironment && r.EnvironmentID != "" {
		return fmt.Errorf("%w: %s names %q", ErrRowEnvironment, r.Kind, r.EnvironmentID)
	}
	return nil
}

// RowFrom is the row an open event's stored name reads back as, which is what a
// reader of a pending row and of a decision already closed composes a [Row]
// from.
func RowFrom(name string) (Row, error) {
	kind, environmentID, parameterised := cut(name)
	row := Row{Kind: Kind(kind)}
	if parameterised {
		row.EnvironmentID = environmentID
	}
	if err := row.Validate(); err != nil {
		return Row{}, err
	}
	return row, nil
}

// cut splits a stored row name at the one colon a further deploy row carries.
func cut(name string) (kind, environmentID string, parameterised bool) {
	for i := range len(name) {
		if name[i] == ':' {
			return name[:i], name[i+1:], true
		}
	}
	return name, "", false
}

// ArtifactGate reports whether the row decides over a document a stage has
// written, which is the kind of gate that names an artifact version on its open
// event. The others are event gates: they decide whether a merge or a deploy
// happens at all, and Decomposition, which decides a set, and the four rows
// that decide a record rather than a version.
func (r Row) ArtifactGate() bool {
	switch r.Kind {
	case KindSpec, KindImplementationPlan, KindTasks, KindImplementation, KindRolePromptOrSkill:
		return true
	default:
		return false
	}
}

// DecidesAnItem reports whether the row is on an item's path. The five rows that
// are not — a role prompt or a skill, the three withdrawals, and the shortening
// of decision-log retention — have no stage to be at, no build to point at, and
// no release to reach, so nothing reads an intent's state for them, no attempt is
// counted at them, and a reject at one sends nothing back.
func (r Row) DecidesAnItem() bool {
	switch r.Kind {
	case KindRolePromptOrSkill, KindSafeguardWithdrawal, KindHaltWithdrawal,
		KindLegalHoldWithdrawal, KindDecisionLogRetentionShortening:
		return false
	default:
		return true
	}
}

// ReadsAThreshold reports whether the row is decided against the risk threshold
// at all. Four rows are not: a safeguard's withdrawal, a halt's withdrawal, a
// legal hold's withdrawal, and the shortening of decision-log retention each
// read no factor set and no threshold, a human being at them always — a row the
// score could auto-pass would be the score deciding to remove a human from a
// gate, to stop being stopped, to lift a hold counsel put on, or to destroy the
// evidence it learns from.
func (r Row) ReadsAThreshold() bool {
	switch r.Kind {
	case KindSafeguardWithdrawal, KindHaltWithdrawal, KindLegalHoldWithdrawal,
		KindDecisionLogRetentionShortening:
		return false
	default:
		return true
	}
}

// Deploys reports whether the row decides a deploy, which is the set of rows a
// hold is available at.
func (r Row) Deploys() bool {
	switch r.Kind {
	case KindDeployToCandidateEnvironment, KindDeployToProduction, KindDeployToEnvironment:
		return true
	default:
		return false
	}
}

// Verdict is what a decision closes with. There are four, and the log's writer
// accepts the same four.
type Verdict string

const (
	// VerdictApprove admits the event or the document.
	VerdictApprove Verdict = "approve"
	// VerdictReject sends the item back to the stage the verdict names and
	// requires a reason. It is available up to the merge to master and nowhere
	// after it.
	VerdictReject Verdict = "reject"
	// VerdictHold leaves the event queued with the change still good. It counts
	// no attempt and teaches the score nothing, and only a deploy row offers it.
	// This is the hold a human sets, which is the one of the design's three
	// written as a decision; the factory's own are the conditions hold.go names,
	// and none of them is a verdict.
	VerdictHold Verdict = "hold"
	// VerdictRefer is a human saying they cannot judge what they were shown:
	// not a fault found, which is a reject, and not a stop on the event, which
	// is a hold. It is on every row because it is about the human and not the
	// event. [Gate.Refer] is what gives it, because a refer re-fires the row to
	// a holder who has not referred it and is refused where none is left.
	VerdictRefer Verdict = "refer"
)

// Verdicts is every verdict, in the order the design names them.
var Verdicts = []Verdict{VerdictApprove, VerdictReject, VerdictHold, VerdictRefer}

// ErrVerdictUnknown is returned for a verdict the row does not offer.
var ErrVerdictUnknown = errors.New("gate: the row does not offer that verdict")

// Actions is what may be done at one row. Refer is on all of them. Reject is
// available up to the merge to master and nowhere after it, so the production
// deploy row and every further deploy row offer approve, hold and refer alone.
// Hold is a deploy row's: it stops an event that would otherwise happen, and at
// the five rows outside an item a hold would name a state the row is already in
// — a version or a withdrawal nobody approved simply is not in force.
func Actions(row Row) ([]Verdict, error) {
	if err := row.Validate(); err != nil {
		return nil, err
	}
	switch row.Kind {
	case KindDecomposition, KindSpec, KindImplementationPlan, KindTasks, KindImplementation,
		KindMergeToMaster, KindRolePromptOrSkill, KindSafeguardWithdrawal, KindHaltWithdrawal,
		KindLegalHoldWithdrawal, KindDecisionLogRetentionShortening:
		return []Verdict{VerdictApprove, VerdictReject, VerdictRefer}, nil
	case KindDeployToCandidateEnvironment:
		return []Verdict{VerdictApprove, VerdictReject, VerdictHold, VerdictRefer}, nil
	case KindDeployToProduction, KindDeployToEnvironment:
		return []Verdict{VerdictApprove, VerdictHold, VerdictRefer}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrRowUnknown, row.Kind)
	}
}

// permits reports whether the row offers the verdict.
func permits(row Row, verdict Verdict) error {
	actions, err := Actions(row)
	if err != nil {
		return err
	}
	if !slices.Contains(actions, verdict) {
		return fmt.Errorf("%w: %s offers %v", ErrVerdictUnknown, row, actions)
	}
	return nil
}

// ReturnsTo is a stage a reject may send an item back to: the four an item
// authors at, and the two things below Spec that are not among its stages,
// because a defect above the item is not repairable inside it.
type ReturnsTo string

const (
	// ReturnsToSpec is the earliest of an item's own stages.
	ReturnsToSpec = ReturnsTo(item.StageSpec)
	// ReturnsToImplementationPlan is the plan stage.
	ReturnsToImplementationPlan = ReturnsTo(item.StageImplementationPlan)
	// ReturnsToTasks is the tasks stage.
	ReturnsToTasks = ReturnsTo(item.StageTasks)
	// ReturnsToImplementation is the implementation stage, and the default at
	// the two event gates that reject, there being no stage of their own and
	// none between.
	ReturnsToImplementation = ReturnsTo(item.StageImplementation)
	// ReturnsToDecomposition is decomposition running again over the intent: an
	// item that should have been three names it, and the set that comes out is
	// decided at Decomposition like any other.
	ReturnsToDecomposition ReturnsTo = "decomposition"
	// ReturnsToTheInterview is the earliest thing reachable: the intent returns
	// to the unrefined state it starts in and raises a question for whoever
	// holds duty 3.
	ReturnsToTheInterview ReturnsTo = "the interview"
)

// ReturnsToTargets is every stage a reject may name, in the order a reject
// reaches back through them.
var ReturnsToTargets = []ReturnsTo{
	ReturnsToTheInterview, ReturnsToDecomposition,
	ReturnsToSpec, ReturnsToImplementationPlan, ReturnsToTasks, ReturnsToImplementation,
}

// ErrReturnsToUnknown is returned for a reject naming a target outside
// [ReturnsToTargets], and for one at a row whose reject names nothing.
var ErrReturnsToUnknown = errors.New("gate: the reject names no stage the item may return to")

// DefaultReturnsTo is the stage a reject sends the item to where the verdict
// names none: the row's own stage at an artifact gate, and Implementation at the
// two event gates that reject, there being no stage of their own and none
// between. Decomposition names nothing at all — its reject re-decomposes the set
// rather than sending an item anywhere — and neither does a row outside every
// item, there being no stage above it to send anything to.
func DefaultReturnsTo(row Row) (ReturnsTo, bool) {
	switch row.Kind {
	case KindSpec:
		return ReturnsToSpec, true
	case KindImplementationPlan:
		return ReturnsToImplementationPlan, true
	case KindTasks:
		return ReturnsToTasks, true
	case KindImplementation, KindDeployToCandidateEnvironment, KindMergeToMaster:
		return ReturnsToImplementation, true
	default:
		return "", false
	}
}

// ErrEditInPlaceRefused is returned for an Edit in place at a row that decides
// neither a document nor a set: an event gate has no version under decision, and
// the rows outside every item that decide a record have nothing in the record to
// edit. Decomposition takes [Gate.EditSetInPlace], which supersedes the set
// rather than a version.
var ErrEditInPlaceRefused = errors.New("gate: this row decides no document, so there is nothing to edit in place")

// ErrNoFactorSet is returned for a row that reads no factor set at all, which is
// the four rows [Row.ReadsAThreshold] names: a human is at each of them always,
// so there is nothing for a set of factors to decide.
var ErrNoFactorSet = errors.New("gate: this row reads no factor set and no threshold")

// FactorSetAt is which of the score's three sets a firing at this row is scored
// on. Which set a firing is on is this package's to say, because a gate row is
// this package's vocabulary: the four rows below a build weigh four groups, the
// four above weigh three, exposure being inapplicable there rather than
// unavailable, and the row that decides a version of what an agent is told
// weighs a set of its own.
func FactorSetAt(row Row) (score.FactorSet, error) {
	switch row.Kind {
	case KindDecomposition, KindSpec, KindImplementationPlan, KindTasks:
		return score.SetAboveABuild, nil
	case KindImplementation, KindDeployToCandidateEnvironment, KindMergeToMaster,
		KindDeployToProduction, KindDeployToEnvironment:
		return score.SetWithABuild, nil
	case KindRolePromptOrSkill:
		return score.SetRolePromptOrSkill, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrNoFactorSet, row)
	}
}
