package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// models is [dispatch.Models]: the client one fleet entry's agent calls, which
// is the one thing the fleet entry record cannot hold. A run builds it per
// entry, from the entry's own model version and the provider its credential
// name resolves to; a composition given a client outright answers every entry
// with that client, which is what a test composes and what [deps.model] is.
type models struct{ d deps }

// For is the client for this entry.
func (m models) For(_ context.Context, entry fleetentry.Entry) (agent.Model, error) {
	if m.d.modelFor == nil {
		return m.d.model, nil
	}
	return m.d.modelFor(entry.ModelVersion, entry.CredentialName)
}

// The two fields of a composed entry the command line is not told. How much the
// model reads at once is a fact at the provider, which the owner writes beside
// the credential; how many dispatches pass between evaluation-set runs is read
// by nothing at this milestone, the evaluation set being content the product
// does not ship. Each is written because the record refuses a value that is not
// positive, and neither bounds anything here: the read-at-once bound is
// recorded on every manifest and nothing truncates a read against it.
const (
	composedReadsAtOnce                     = 200_000
	composedDispatchesBetweenEvaluationRuns = 50
)

// ensureFleetEntries is this terminal's stand-in for the owner's first act at
// Factory: where the install holds no entry in force for a role, one is written
// from -model, -provider, -effort and the credential that provider reads, as
// the human -human names, scoped to the whole factory and naming every class of
// material. So a fresh install dispatches, and an owner who wrote entries of
// their own keeps them — a role already covered is left alone, whatever the
// flags say.
//
// It returns the roles it wrote an entry for. doc.go says why `serve` will not
// do this.
func ensureFleetEntries(ctx context.Context, d deps, owner record.Actor) ([]string, error) {
	if err := ensureModelCredentialLent(ctx, d, owner); err != nil {
		return nil, err
	}
	covered, err := fleetentry.CoveredRoles(ctx, d.pool)
	if err != nil {
		return nil, err
	}
	writer := fleetentry.NewWriter(d.pool, d.token)
	var written []string
	for _, role := range dispatch.Roles {
		if covered[string(role)] {
			continue
		}
		if _, err := writer.Write(ctx, owner, fleetentry.New{
			ModelVersion:                    d.modelName,
			Effort:                          d.effort,
			Role:                            string(role),
			CredentialName:                  d.modelCredentialName,
			ProcessingLocation:              processingLocationOf(d.modelCredentialName),
			MaterialClasses:                 fleetentry.MaterialClasses,
			ReadsAtOnce:                     composedReadsAtOnce,
			DispatchesBetweenEvaluationRuns: composedDispatchesBetweenEvaluationRuns,
		}); err != nil {
			return nil, err
		}
		written = append(written, string(role))
	}
	return written, nil
}

// ensureModelCredentialLent declares the credential those entries run on as the
// owner's own account, where the People declaration does not already hold it.
// The design has a fleet entry name a credential the declaration records as
// lent — every agent run record names whose account it spent and under a
// ceiling that name is what the spend is summed under — and this terminal has
// no People screen at the install, so it declares what -human already stands
// for.
//
// A name the declaration holds is left alone, taken back included: re-lending
// one an owner took back would undo the taking back, and a second lending would
// append a declaration version at every start.
func ensureModelCredentialLent(ctx context.Context, d deps, owner record.Actor) error {
	_, found, err := people.CredentialNamed(ctx, d.pool, d.modelCredentialName)
	if err != nil || found {
		return err
	}
	if _, err := people.NewWriter(d.pool, d.token, newFactory(d.pool, d.token)).
		Lend(ctx, owner, owner.Key, d.modelCredentialName, people.AccountPerson); err != nil {
		return fmt.Errorf("declaring that %s lent %s: %w", owner.Key, d.modelCredentialName, err)
	}
	return nil
}

// processingLocationOf is the processing location a composed entry names: the
// provider the credential name resolves to, and no region. The design has the
// owner write the provider and the region beside the credential, and this
// terminal is told neither — so the entry names the provider it does know, and
// what it costs is that a reading of where an item was processed is as coarse
// as the provider on every entry this interface wrote.
func processingLocationOf(credentialName string) string {
	return strings.TrimPrefix(credentialName, "model.")
}

// roleReadiness is the readiness reading for one role: whether an entry in
// force covers it, whether a role prompt version is in force, and how long the
// oldest item dispatch holds unmatched has been held. A first install shows a
// row for every role before anything is wrong, which is what the reading is
// for.
type roleReadiness struct {
	role       dispatch.Role
	entry      bool
	rolePrompt bool
	// oldestUnmatched is the age of the oldest open "no fleet entry covers this
	// stage" row naming the role, and zero where none stands.
	oldestUnmatched time.Duration
}

// readiness is the reading per role, in the order the path reaches the roles.
// It is composed here rather than in either package because it reads two
// records and the log: the entries in force are package fleetentry's, the
// version in force is the artifact store's read this interface holds the
// approved ids for, and the unmatched items are dispatch's own holds.
//
// The home view is what shows it, and the badge counts the roles it reads as
// uncovered: a role with no matching entry is a wait on a human and not a pass
// that merely ran late.
func (p *path) readiness(ctx context.Context) ([]roleReadiness, error) {
	covered, err := fleetentry.CoveredRoles(ctx, p.d.pool)
	if err != nil {
		return nil, err
	}
	held, rows, err := p.dispatch.Open(ctx)
	if err != nil {
		return nil, err
	}
	oldest := map[dispatch.Role]time.Time{}
	for n, one := range held {
		if one.Condition != dispatch.HoldNoEntryCoversTheStage {
			continue
		}
		at, err := record.ParseTime(rows[n].At)
		if err != nil {
			return nil, err
		}
		role := dispatch.Role(one.Role)
		if was, seen := oldest[role]; !seen || at.Before(was) {
			oldest[role] = at
		}
	}

	now := time.Now()
	reading := make([]roleReadiness, 0, len(dispatch.Roles))
	for _, role := range dispatch.Roles {
		_, inForce, err := p.prompts.InForce(ctx, role)
		if err != nil {
			return nil, err
		}
		one := roleReadiness{role: role, entry: covered[string(role)], rolePrompt: inForce}
		if at, seen := oldest[role]; seen {
			one.oldestUnmatched = now.Sub(at)
		}
		reading = append(reading, one)
	}
	return reading, nil
}

// rolePrompts is [dispatch.Prompts]: the role prompt version in force per
// role. An install's entry is in force ungated and the store's own in-force
// read finds it by the event that entered it. Every other version is in force
// only once the gate every version fires has approved it, so what this names
// to the store's read is every version a close event at that row approved.
//
// A chain whose head an upgrade entered therefore reads as the version below it
// in force until that row is decided at Factory, and the version in force moves
// when it is.
//
// The approved ids are read once and kept for the life of the value, because
// every read of the log appends a read event and the readiness reading asks
// this of every role. [rolePrompts.forget] is what a decision at that row
// calls, so an approval is in force at the next read rather than at the next
// start of the process.
type rolePrompts struct {
	pool  *pgxpool.Pool
	token lease.Token
	mu    sync.Mutex
	// approved is the versions a close event at the role-prompt row approved,
	// and read whether that list has been taken yet — an install with none
	// approved is an empty list and not an unread one.
	approved []string
	read     bool
}

// InForce is the version in force for the role, read through the store's own
// in-force query with the approved versions this composition has read.
func (r *rolePrompts) InForce(ctx context.Context, role dispatch.Role) (artifact.Artifact, bool, error) {
	approved, err := r.approvedVersions(ctx)
	if err != nil {
		return artifact.Artifact{}, false, err
	}
	return artifact.InForce(ctx, r.pool, artifact.KindRolePrompt, string(role), "", approved)
}

// forget drops the approved versions this value read, so the next read takes
// them again. A decision at the role-prompt row calls it.
func (r *rolePrompts) forget() {
	r.mu.Lock()
	r.approved, r.read = nil, false
	r.mu.Unlock()
}

// approvedVersions is every version a close event at the role-prompt row
// approved, read off the log once.
func (r *rolePrompts) approvedVersions(ctx context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.read {
		return r.approved, nil
	}
	closed, err := decisionlog.NewReader(r.pool, r.token).ClosedDecisions(ctx, rolePromptReader)
	if err != nil {
		return nil, err
	}
	for _, one := range closed {
		if one.CloseEvent.Verdict != string(gate.VerdictApprove) {
			continue
		}
		opened, is := openingOn(one.OpenEvent, func(o gate.Opened) bool {
			return o.Gate.Kind == gate.KindRolePromptOrSkill
		})
		if !is || opened.ArtifactID == "" {
			continue
		}
		r.approved = append(r.approved, opened.ArtifactID)
	}
	r.read = true
	return r.approved, nil
}

// rolePromptReader is who this read of the log is made as. Which version a
// role runs on is the component that hands one to a role asking, so the read
// event names dispatch and not the owner: no human asked.
var rolePromptReader = principal.OfComponent("dispatch")

// shippedPromptFor is the words the product ships for one role. It is the one
// place package agent's seven constants are read: what a run reads is the
// version in force, and these are only what the first start enters. There is
// one per role, the decomposer's and the grouper's included, whose words no
// run of this interface reads — package agent's doc.go says why.
func shippedPromptFor(role dispatch.Role) (string, error) {
	switch role {
	case dispatch.RoleInterviewer:
		return agent.ShippedInterviewerPrompt, nil
	case dispatch.RoleDecomposer:
		return agent.ShippedDecomposerPrompt, nil
	case dispatch.RoleGrouper:
		return agent.ShippedGrouperPrompt, nil
	case dispatch.RoleSpecAuthor:
		return agent.ShippedSpecAuthorPrompt, nil
	case dispatch.RoleImplementationPlanner:
		return agent.ShippedPlannerPrompt, nil
	case dispatch.RoleTaskAuthor:
		return agent.ShippedTaskAuthorPrompt, nil
	case dispatch.RoleImplementer:
		return agent.ShippedImplementerPrompt, nil
	default:
		return "", fmt.Errorf("%w: %q", dispatch.ErrRoleUnknown, role)
	}
}

// enterShippedPrompts is the install's first-start step for what an agent is
// told: at install, and at the factory's first start on a new version of the
// product, the factory itself calls the artifact store to enter what shipped,
// with the factory's own start as the actor and the author pair empty.
//
// What says whether this is a first start is the version's identity and not the
// words: the newest entry a start wrote carries the shipped-bundle identity it
// entered under, so a start under that same identity enters nothing, however
// many versions an agent has authored over it since. Keyed on the words instead,
// every start after the first authored version would re-enter what shipped.
//
// A version whose shipped words are the ones the last entry carried enters
// nothing either: an upgrade that changed nothing moves nothing. A version whose
// words differ gets an entry awaiting the gate every version fires, and nothing
// here puts it in force — the words the install ran on stand until that row is
// decided, and this interface fires none.
func enterShippedPrompts(ctx context.Context, store *artifact.Store, pool *pgxpool.Pool,
	token lease.Token, actor record.Actor, bundle string) (*rolePrompts, []string, error) {
	prompts := &rolePrompts{pool: pool, token: token}
	var entered []string
	for _, role := range dispatch.Roles {
		shipped, err := shippedPromptFor(role)
		if err != nil {
			return nil, nil, err
		}
		last, found, err := artifact.NewestShipped(ctx, pool, artifact.KindRolePrompt, string(role), "")
		if err != nil {
			return nil, nil, err
		}
		if found && (last.ShippedBundleIdentity == bundle || last.Content == shipped) {
			continue
		}
		// No entry of a start at all is an install, and its entry is in force
		// ungated; an entry under another identity carrying other words is an
		// upgrade that changed them, and its entry awaits the gate every
		// version fires.
		enteredBy := artifact.EnteredByInstall
		if found {
			enteredBy = artifact.EnteredByUpgradeFirstStart
		}
		if _, err := store.EnterShipped(ctx, actor, artifact.KindRolePrompt, string(role), "",
			shipped, enteredBy, bundle); err != nil {
			return nil, nil, err
		}
		entered = append(entered, string(role))
	}
	return prompts, entered, nil
}

// intentLimits is [dispatch.Limits]: the attempt limit in force, read per
// subject. A stage's is package policy's read, where a safeguard on the area
// may clamp it; the rounds of an intent name no stage — a role put on an intent
// runs before there is an item — so that one is [intentAttemptLimit], the read
// decomposition's own count is already compared against.
type intentLimits struct {
	reader *policy.Reader
	pool   *pgxpool.Pool
}

// AttemptLimit is the limit in force at a stage.
func (l intentLimits) AttemptLimit(ctx context.Context, s policy.Subjects) (policy.Effective, error) {
	return l.reader.AttemptLimit(ctx, s)
}

// RoundsOnAnIntent is the limit the interview's rounds are counted against.
func (l intentLimits) RoundsOnAnIntent(ctx context.Context) (policy.Effective, error) {
	limit, err := intentAttemptLimit(ctx, l.pool, factorysettings.SubjectInterview)
	if err != nil {
		return policy.Effective{}, err
	}
	return policy.Effective{Parameter: gatepolicy.AttemptLimit, Number: float64(limit)}, nil
}

// gateEscalation is [dispatch.Escalation]: dispatch decides that the stage has
// spent its limit and this performs it, which is the gate component's
// enforcement — the escalated value onto the item and every pending row of the
// item abandoned naming the limit. Telling the notifier is dispatch's own call
// and not part of this one.
//
// It is composed here because ../../../end-goal/components.md's row for
// dispatch names no gate: the two meet in the composition and not in either
// package.
type gateEscalation struct{ gate *gate.Gate }

// Escalate performs the escalation.
func (g gateEscalation) Escalate(ctx context.Context, actor record.Actor, itemID string, stage item.Stage) error {
	_, err := g.gate.EnforceAttemptLimit(ctx, actor, itemID, stage)
	return err
}
