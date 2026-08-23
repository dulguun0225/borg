// The database tests of this package are in policy_test rather than in policy,
// because they open the pool through package postgres, which imports this one to
// apply its DDL. deps.txt records the edge as "test policy -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package policy_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

var (
	owner              = record.Actor{Kind: record.KindHuman, Name: "owner"}
	decompositionActor = record.Actor{Kind: record.KindComponent, Name: "decomposition"}
)

var credential = secretref.MustNew("deploy.local")

// installed is a factory an owner could author on: the two records Install
// creates, plus a service and an area for the parameters that are fields of
// those. The service is written by decomposition, which is its other writer, and the
// area by an owner.
type installed struct {
	pool     *pgxpool.Pool
	factory  *policy.Factory
	reader   *policy.Reader
	settings factorysettings.Settings
	prod     environment.Environment
	service  service.Service
	area     area.Area
}

// subjects is what a gate firing on this service in this area reads against.
func (i installed) subjects(row string) policy.Subjects {
	return policy.Subjects{
		GateRow:       row,
		EnvironmentID: i.prod.ID,
		ServiceID:     i.service.ID,
		AreaID:        i.area.ID,
		Stage:         item.StageImplementation,
	}
}

func newFactory(t *testing.T) (context.Context, installed) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_policy_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	factory := policy.NewFactory(pool)
	install, err := factory.Install(ctx, owner, []string{"/srv/targets"}, credential)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	svc, err := service.NewWriter(pool).Create(ctx, decompositionActor, "checkout", "/repos/checkout")
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	ar, err := area.NewWriter(pool).Declare(ctx, owner, "payments", "")
	if err != nil {
		t.Fatalf("declaring the area: %v", err)
	}
	return ctx, installed{
		// The reader is composed with the version in force, which is what a run
		// does: the supplied half of every value is a field of that version.
		pool: pool, factory: factory, reader: policy.NewReader(pool, scoreVersion(t, ctx, pool)),
		settings: install.Settings, prod: install.Production, service: svc, area: ar,
	}
}

func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func effectiveOf(t *testing.T, all []policy.Effective, parameter gatepolicy.Parameter) policy.Effective {
	t.Helper()
	for _, e := range all {
		if e.Parameter == parameter {
			return e
		}
	}
	t.Fatalf("nothing resolved %s", parameter)
	return policy.Effective{}
}

// TestInstallIsTheTwoRecordsAnOwnerAuthorsOnAndIsIdempotent: the factory-wide
// settings record exists before any project does and production's environment is one an
// owner does not choose, so both are created here — and creating them is an
// authoring write, so the factory has a policy version in force with nothing
// authored.
func TestInstallIsTheTwoRecordsAnOwnerAuthorsOnAndIsIdempotent(t *testing.T) {
	ctx, in := newFactory(t)

	version, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if version.Action != policy.ActionCreated {
		t.Errorf("the version in force is %q, want a creation", version.Action)
	}
	if version.Parameter != "" {
		t.Errorf("a creation names parameter %q, and it authors none", version.Parameter)
	}

	all, err := policy.All(ctx, in.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("the install left %d versions, want one per record created", len(all))
	}
	if all[0].Supersedes != "" || all[1].Supersedes != all[0].ID {
		t.Errorf("the versions do not chain: %q then %q superseding %q",
			all[0].Supersedes, all[1].ID, all[1].Supersedes)
	}

	// Running it again writes nothing: the crude interface calls it at every
	// start, and a version per start would be a sequence that says nothing.
	again, err := in.factory.Install(ctx, owner, []string{"/srv/targets"}, credential)
	if err != nil {
		t.Fatalf("Install again: %v", err)
	}
	if again.Settings.ID != in.settings.ID || again.Production.ID != in.prod.ID {
		t.Errorf("a second install created new records: %+v", again)
	}
	if again.Version.ID != version.ID {
		t.Errorf("a second install moved the version to %s", again.Version.ID)
	}
}

// TestTheValueInForceIsAReadOfThreeThings: what an owner authored, what the score
// supplies where they authored nothing, and the clamp a safeguard applies.
func TestTheValueInForceIsAReadOfThreeThings(t *testing.T) {
	ctx, in := newFactory(t)

	// Nothing authored: the score supplies, and a factory with nothing authored
	// in it runs.
	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	limit := effectiveOf(t, all, gatepolicy.WindowLimit)
	supplied := startingValue(t, gatepolicy.WindowLimit)
	if limit.Source != policy.FromSupplied || limit.Number != supplied {
		t.Errorf("the window limit with nothing authored reads %v from %s, want the supplied %v", limit.Number, limit.Source, supplied)
	}

	// Authored: the owner's value stands, and the score's does not.
	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 4); err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if limit = effectiveOf(t, all, gatepolicy.WindowLimit); limit.Source != policy.FromAuthored || limit.Number != 4 {
		t.Errorf("the window limit reads %v from %s, want the authored 4", limit.Number, limit.Source)
	}

	// A safeguard: a ceiling over the window limit caps the authored value, and the safeguard
	// that did it is named.
	placed, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 2})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	limit = effectiveOf(t, all, gatepolicy.WindowLimit)
	if limit.Number != 2 || !limit.Clamped {
		t.Errorf("the window limit reads %v clamped %v, want the safeguard's ceiling of 2", limit.Number, limit.Clamped)
	}
	if !slices.Contains(limit.Safeguards, placed.ID) {
		t.Errorf("the window limit names safeguards %v, want the one placed", limit.Safeguards)
	}
	if limit.Source != policy.FromAuthored {
		t.Errorf("the window limit says its value came from %s, and a safeguard is a bound rather than a source", limit.Source)
	}
}

// TestASafeguardNeverWidens: a safeguard is a bound and not a precedence, so a
// ceiling of five over an authored two leaves the two — read as a precedence it
// would raise the number, which is a safeguard adding throughput and removing
// safety.
func TestASafeguardNeverWidens(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 2); err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 5}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	limit := effectiveOf(t, all, gatepolicy.WindowLimit)
	if limit.Number != 2 {
		t.Errorf("a safeguard's ceiling of 5 over an authored 2 reads %v, want 2", limit.Number)
	}
	if limit.Clamped {
		t.Error("the safeguard is recorded as having clamped a value already narrower than itself")
	}
	if len(limit.Safeguards) != 1 {
		t.Errorf("the safeguard that clamped nothing is not named: %v", limit.Safeguards)
	}

	// A floor is the same rule the other way: a safeguard puts a floor under the
	// window's confidence, and one under the authored value leaves it.
	if _, err := in.factory.AuthorWindowConfidence(ctx, owner, in.service.ID, 0.99); err != nil {
		t.Fatalf("AuthorWindowConfidence: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowConfidence,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 0.9}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if confidence := effectiveOf(t, all, gatepolicy.WindowConfidence); confidence.Number != 0.99 {
		t.Errorf("a safeguard's floor of 0.9 under an authored 0.99 reads %v, want 0.99", confidence.Number)
	}
}

// TestASafeguardOnTheThresholdAddsAHumanRatherThanMovingTheNumber: the risk
// threshold's safeguard is the one that is not arithmetic, and what it does is
// the whole of what a gate reads from it.
func TestASafeguardOnTheThresholdAddsAHumanRatherThanMovingTheNumber(t *testing.T) {
	ctx, in := newFactory(t)

	before, err := in.reader.AtGate(ctx, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	supplied := startingValue(t, gatepolicy.RiskThreshold)
	if before.HumanBySafeguard || before.Threshold != supplied || before.ThresholdFrom != policy.FromSupplied {
		t.Errorf("with nothing authored the gate reads %+v, want the supplied threshold and no safeguard", before)
	}

	placed, version, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.RiskThreshold,
		safeguard.Subject{Kind: safeguard.SubjectGateRow, ID: "deploy_to_production"}, safeguard.Bound{Number: 0})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	after, err := in.reader.AtGate(ctx, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if !after.HumanBySafeguard {
		t.Error("the safeguard adds no human at the row")
	}
	if after.Threshold != before.Threshold {
		t.Errorf("the safeguard moved the threshold to %v from %v", after.Threshold, before.Threshold)
	}
	if !slices.Contains(after.Safeguards, placed.ID) {
		t.Errorf("the firing names safeguards %v, want the one placed", after.Safeguards)
	}
	if after.PolicyVersion != version.ID {
		t.Errorf("the firing names policy version %q, want the one the safeguard appended %q", after.PolicyVersion, version.ID)
	}

	// The other row has no safeguard: a safeguard on a gate row reaches that row
	// and no other.
	elsewhere, err := in.reader.AtGate(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if elsewhere.HumanBySafeguard {
		t.Error("a safeguard on the deploy row reached the merge row")
	}

	// Withdrawing it stops it applying, and the firing that follows names no
	// safeguard.
	if _, err := in.factory.WithdrawSafeguard(ctx, owner, placed.ID); err != nil {
		t.Fatalf("WithdrawSafeguard: %v", err)
	}
	withdrawn, err := in.reader.AtGate(ctx, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if withdrawn.HumanBySafeguard || len(withdrawn.Safeguards) != 0 {
		t.Errorf("a withdrawn safeguard still applies: %+v", withdrawn)
	}
}

// TestASafeguardOnAnAreaReachesAnItemInTheChain: a safeguard drawn on any area
// in the chain reaches an item in the narrowest, which is why the walk exists —
// without it an owner who declared a narrower area inside one with a safeguard
// on it would lose it.
func TestASafeguardOnAnAreaReachesAnItemInTheChain(t *testing.T) {
	ctx, in := newFactory(t)

	inner, err := area.NewWriter(in.pool).Declare(ctx, owner, "payments/refunds", in.area.ID)
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.RiskThreshold,
		safeguard.Subject{Kind: safeguard.SubjectArea, ID: in.area.ID}, safeguard.Bound{Number: 0}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	subjects := in.subjects("merge_to_master")
	subjects.AreaID = inner.ID
	applied, err := in.reader.AtGate(ctx, subjects)
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if !applied.HumanBySafeguard {
		t.Error("a safeguard on the outer area does not reach an item in the inner one")
	}
}

// TestTheAllowedKindsAreTheOneListAndASafeguardMayOnlyExtendIt: the score
// supplies no list, so an unauthored one is the kinds the factory itself can
// decide rather than empty — gate policy has an owner extend the list, which
// presupposes something to extend — and both an authored value and a safeguard
// are a union over it.
func TestTheAllowedKindsAreTheOneListAndASafeguardMayOnlyExtendIt(t *testing.T) {
	ctx, in := newFactory(t)

	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	allowed := effectiveOf(t, all, gatepolicy.AllowedPredicateKinds)
	own := gatepolicy.AllowedPredicateKindNames()
	slices.Sort(own)
	if allowed.Source != policy.FromFactory || !slices.Equal(allowed.List, own) {
		t.Errorf("an unauthored allowed reads %v from %s, want the factory's own %v",
			allowed.List, allowed.Source, own)
	}

	if _, err := in.factory.AuthorAllowedPredicateKinds(ctx, owner, []string{"status", "field-present"}); err != nil {
		t.Fatalf("AuthorAllowedPredicateKinds: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.AllowedPredicateKinds,
		safeguard.Subject{Kind: safeguard.SubjectFactorySettings, ID: in.settings.ID}, safeguard.Bound{List: []string{"schema", "status"}}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	allowed = effectiveOf(t, all, gatepolicy.AllowedPredicateKinds)
	want := append([]string{"field-present", "schema", "status"}, own...)
	slices.Sort(want)
	if !slices.Equal(allowed.List, want) {
		t.Errorf("the allowed reads %v, want the union %v", allowed.List, want)
	}
	if !allowed.Clamped || allowed.Source != policy.FromAuthored {
		t.Errorf("the allowed reads clamped %v from %s", allowed.Clamped, allowed.Source)
	}
}

// TestEverySevenRowsResolveAndOneIsReadByNothing: an owner can author all of
// them, and the read says which of them changes anything at this milestone rather
// than leaving an owner to discover it.
func TestEverySevenRowsResolveAndOneIsReadByNothing(t *testing.T) {
	ctx, in := newFactory(t)

	authorings := []struct {
		parameter gatepolicy.Parameter
		author    func() (policy.Version, error)
		want      float64
	}{
		{gatepolicy.RiskThreshold, func() (policy.Version, error) {
			return in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.5)
		}, 0.5},
		{gatepolicy.AttemptLimit, func() (policy.Version, error) {
			return in.factory.AuthorAttemptLimit(ctx, owner, item.StageImplementation, 5)
		}, 5},
		{gatepolicy.ItemSizeTarget, func() (policy.Version, error) {
			return in.factory.AuthorItemSizeTarget(ctx, owner, in.area.ID, 400)
		}, 400},
		{gatepolicy.WindowSize, func() (policy.Version, error) {
			return in.factory.AuthorWindowSize(ctx, owner, in.service.ID, 0.01)
		}, 0.01},
		{gatepolicy.WindowConfidence, func() (policy.Version, error) {
			return in.factory.AuthorWindowConfidence(ctx, owner, in.service.ID, 0.98)
		}, 0.98},
		{gatepolicy.WindowCap, func() (policy.Version, error) {
			return in.factory.AuthorWindowCap(ctx, owner, in.service.ID, 3600)
		}, 3600},
		{gatepolicy.WindowLimit, func() (policy.Version, error) {
			return in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
		}, 3},
	}
	for _, a := range authorings {
		version, err := a.author()
		if err != nil {
			t.Fatalf("authoring %s: %v", a.parameter, err)
		}
		if version.Parameter != a.parameter {
			t.Errorf("authoring %s appended a version naming %s", a.parameter, version.Parameter)
		}
	}
	if _, err := in.factory.AuthorAllowedPredicateKinds(ctx, owner, []string{"status"}); err != nil {
		t.Fatalf("AuthorAllowedPredicateKinds: %v", err)
	}

	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != len(gatepolicy.Definitions) {
		t.Fatalf("%d parameters resolved, want %d", len(all), len(gatepolicy.Definitions))
	}
	for _, a := range authorings {
		e := effectiveOf(t, all, a.parameter)
		if e.Source != policy.FromAuthored || e.Number != a.want {
			t.Errorf("%s reads %v from %s, want the authored %v", a.parameter, e.Number, e.Source, a.want)
		}
	}

	read := 0
	for _, e := range all {
		if e.ReadBy != "" {
			read++
		}
	}
	// Seven of the eight are read by something now that contracts are built: the
	// threshold, the limit, the window's four, and the list of allowed predicate
	// kinds, whose reader is the derivation of a consumer contract. The
	// one left is the item-size target, which nothing sizes an item against yet.
	if read != 7 {
		t.Errorf("%d parameters are read by something at this milestone, want all but the item-size target", read)
	}
	for _, unread := range []gatepolicy.Parameter{gatepolicy.ItemSizeTarget} {
		if e := effectiveOf(t, all, unread); e.ReadBy != "" {
			t.Errorf("%s says it is read by %q, and nothing reads it yet", unread, e.ReadBy)
		}
	}

	// The role-prompt-or-skill threshold is the same parameter on the factory-wide
	// settings record, which is where the row that decides what an agent is told reads it.
	if _, err := in.factory.AuthorRolePromptOrSkillThreshold(ctx, owner, 0.15); err != nil {
		t.Fatalf("AuthorRolePromptOrSkillThreshold: %v", err)
	}
	stored, err := factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !stored.RolePromptOrSkillThreshold.Present || stored.RolePromptOrSkillThreshold.Number != 0.15 {
		t.Errorf("the role-prompt-or-skill threshold reads back as %+v", stored.RolePromptOrSkillThreshold)
	}
}

// TestTheAttemptLimitIsReadThroughTheSameThreeReads: the one parameter besides
// the threshold that a mechanism reads at this milestone.
func TestTheAttemptLimitIsReadThroughTheSameThreeReads(t *testing.T) {
	ctx, in := newFactory(t)

	limit, err := in.reader.AttemptLimit(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	supplied := startingValue(t, gatepolicy.AttemptLimit)
	if limit.Source != policy.FromSupplied || limit.Number != supplied {
		t.Errorf("the limit reads %v from %s, want the supplied %v", limit.Number, limit.Source, supplied)
	}

	if _, err := in.factory.AuthorAttemptLimit(ctx, owner, item.StageImplementation, 6); err != nil {
		t.Fatalf("AuthorAttemptLimit: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.AttemptLimit,
		safeguard.Subject{Kind: safeguard.SubjectFactorySettings, ID: in.settings.ID}, safeguard.Bound{Number: 4}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	limit, err = in.reader.AttemptLimit(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	if limit.Number != 4 || !limit.Clamped {
		t.Errorf("the limit reads %v clamped %v, want the safeguard's ceiling of 4", limit.Number, limit.Clamped)
	}

	// A limit authored on another stage is that stage's and not this one's.
	other := in.subjects("merge_to_master")
	other.Stage = item.StageSpec
	spec, err := in.reader.AttemptLimit(ctx, other)
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	if spec.Source != policy.FromSupplied {
		t.Errorf("the spec stage's limit reads from %s, want the supplied value", spec.Source)
	}
	// The safeguard over the factory-wide settings record reaches this stage too, and
	// clamps nothing: the supplied value is already under its ceiling, which is a
	// safeguard being a bound rather than a precedence on a stage nobody authored.
	if spec.Number != supplied || spec.Clamped {
		t.Errorf("the spec stage's limit reads %v clamped %v, want the supplied %v untouched",
			spec.Number, spec.Clamped, supplied)
	}
	if len(spec.Safeguards) != 1 {
		t.Errorf("the safeguard over the record does not reach the spec stage: %v", spec.Safeguards)
	}
}

// TestGatePolicyIsAuthoredByAHuman: duty 8 is an owner's, so a component
// authoring a parameter would be the factory setting its own bounds — which is
// the one thing kept apart from what the score supplies.
func TestGatePolicyIsAuthoredByAHuman(t *testing.T) {
	ctx, in := newFactory(t)

	component := record.Actor{Kind: record.KindComponent, Name: "score"}
	if _, err := in.factory.AuthorWindowLimit(ctx, component, in.service.ID, 2); !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component authoring the window limit = %v, want ErrNotAnOwner", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, component, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 2}); !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component placing a safeguard = %v, want ErrNotAnOwner", err)
	}
	if _, err := in.factory.Install(ctx, component, []string{"/srv"}, credential); !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component installing = %v, want ErrNotAnOwner", err)
	}
	if _, err := in.factory.AuthorWindowLimit(ctx, record.Actor{}, in.service.ID, 2); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("authoring with no actor = %v, want ErrKindUnknown", err)
	}
}

// TestAFailedWriteAppendsNoVersion: the write and the version are one
// transaction, so a value that moved without the version moving is not a state
// the store can be left in — and neither is a version naming a write that did not
// happen.
func TestAFailedWriteAppendsNoVersion(t *testing.T) {
	ctx, in := newFactory(t)

	before, err := policy.All(ctx, in.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if _, err := in.factory.AuthorItemSizeTarget(ctx, owner, "ar_nothing", 400); !errors.Is(err, area.ErrNotFound) {
		t.Fatalf("authoring on an area that does not exist = %v, want ErrNotFound", err)
	}
	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Fatalf("authoring a window limit of zero = %v, want ErrNotPositive", err)
	}

	after, err := policy.All(ctx, in.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("a refused write left %d versions, want the %d that were there", len(after), len(before))
	}
}

// TestEveryAuthoringWriteMovesTheVersion: an owner re-authors gate policy, and a
// decision read back against today's values is not the decision that was made —
// so what changes the policy changes the version, and the version names the write.
func TestEveryAuthoringWriteMovesTheVersion(t *testing.T) {
	ctx, in := newFactory(t)

	installVersion, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}

	authored, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.5)
	if err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	if authored.Supersedes != installVersion.ID {
		t.Errorf("the authoring version supersedes %q, want the install's %q", authored.Supersedes, installVersion.ID)
	}
	if authored.Action != policy.ActionAuthored || authored.Parameter != gatepolicy.RiskThreshold {
		t.Errorf("the version says %q %q", authored.Action, authored.Parameter)
	}
	if authored.Subject.Kind != "environment" || authored.Subject.ID != in.prod.ID ||
		authored.Subject.Qualifier != "merge_to_master" {
		t.Errorf("the version names subject %s, want production's record and the row", authored.Subject)
	}
	if authored.Actor != owner {
		t.Errorf("the version's actor is %+v, want the owner", authored.Actor)
	}

	placed, added, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 2})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	if added.Action != policy.ActionSafeguardAdded || added.SafeguardID != placed.ID {
		t.Errorf("the safeguard's version says %q of safeguard %q", added.Action, added.SafeguardID)
	}

	withdrawn, err := in.factory.WithdrawSafeguard(ctx, owner, placed.ID)
	if err != nil {
		t.Fatalf("WithdrawSafeguard: %v", err)
	}
	if withdrawn.Action != policy.ActionWithdrawn || withdrawn.SafeguardID != placed.ID {
		t.Errorf("the withdrawal's version says %q of safeguard %q", withdrawn.Action, withdrawn.SafeguardID)
	}

	inForce, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if inForce.ID != withdrawn.ID {
		t.Errorf("the version in force is %s, want the newest write %s", inForce.ID, withdrawn.ID)
	}

	read, err := policy.Get(ctx, in.pool, authored.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != authored {
		t.Errorf("the version reads back as %+v", read)
	}
	if _, err := in.factory.WithdrawSafeguard(ctx, owner, "sfg_nothing"); !errors.Is(err, safeguard.ErrNotFound) {
		t.Errorf("withdrawing a safeguard that does not exist = %v, want ErrNotFound", err)
	}
}

// TestAGateWithNoRecordsToReadFallsBackToWhatTheScoreSupplies: a firing whose
// subjects name no environment reads the supplied threshold rather than failing,
// which is what keeps a factory with nothing authored running.
func TestAGateWithNoRecordsToReadFallsBackToWhatTheScoreSupplies(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.5); err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	applied, err := in.reader.AtGate(ctx, policy.Subjects{GateRow: "merge_to_master"})
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	supplied := startingValue(t, gatepolicy.RiskThreshold)
	if applied.ThresholdFrom != policy.FromSupplied || applied.Threshold != supplied {
		t.Errorf("a firing naming no environment reads %v from %s, want the supplied %v",
			applied.Threshold, applied.ThresholdFrom, supplied)
	}
}

// TestAGateBeforeTheFactoryIsInstalledHasNoVersionToName: an opening row requires
// a policy version, so a factory nobody installed cannot fire a gate — which is
// better than a firing naming an empty version.
func TestAGateBeforeTheFactoryIsInstalledHasNoVersionToName(t *testing.T) {
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_policy_bare_" + hex.EncodeToString(suffix[:])
	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	if _, err := policy.NewReader(pool, score.Version{}).AtGate(ctx, policy.Subjects{GateRow: "merge_to_master"}); !errors.Is(err, policy.ErrNoVersion) {
		t.Errorf("AtGate on a factory nobody installed = %v, want ErrNoVersion", err)
	}
}

// startingValue is what the score supplies for a parameter before any outcome has
// moved it, which is what a factory with no outcomes in it reads.
func startingValue(t *testing.T, parameter gatepolicy.Parameter) float64 {
	t.Helper()
	supplied, found := score.Starting(parameter)
	if !found {
		t.Fatalf("the score supplies no %s", parameter)
	}
	return supplied.Value
}

// scoreVersion is the version in force, appended if there is none. The reader is
// composed with it rather than reading the newest at each answer: a supplied value
// moves as outcomes arrive, and a reader that re-read it could give one gate
// firing a threshold from a version its own decision row does not name.
func scoreVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool) score.Version {
	t.Helper()
	version, err := score.NewWriter(pool).Ensure(ctx, record.Actor{Kind: record.KindComponent, Name: "score"})
	if err != nil {
		t.Fatalf("ensuring the score version: %v", err)
	}
	return version
}

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes the key from the name, so a
// change to either is caught here rather than by two processes that stopped
// serialising against each other.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	sum := sha256.Sum256([]byte("borg/factory/policy"))
	want := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if got := policy.AdvisoryLockKey(); got != want {
		t.Errorf("AdvisoryLockKey = %d, want %d", got, want)
	}
	if policy.AdvisoryLockKey() < 0 {
		t.Error("the key is negative, and the top bit is meant to be cleared")
	}
}

// TestTheSequenceCannotFork: supersedes is what makes the sequence readable
// without a column that orders it, and two versions naming one predecessor would
// make it branch. The store refuses that, and the lock is what means a second
// writer waits rather than meeting the refusal.
func TestTheSequenceCannotFork(t *testing.T) {
	ctx, in := newFactory(t)

	inForce, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}

	// A second version naming the same predecessor is refused by the store.
	_, err = in.pool.Exec(ctx, `insert into `+policy.Table+`
		(id, actor_kind, actor_name, at, action, parameter, subject_kind, subject_id, qualifier, safeguard_id, supersedes)
		values ($1, 'human', 'owner', $2, 'created', '', 'factory_settings', 'fs_x', '', '', $3)`,
		record.NewID(policy.IDPrefix), record.Now(), inForce.Supersedes)
	if err == nil {
		t.Error("the store accepted two versions naming one predecessor, and the sequence would fork")
	}

	// And a second version naming none is refused for the same reason: a sequence
	// has one beginning.
	_, err = in.pool.Exec(ctx, `insert into `+policy.Table+`
		(id, actor_kind, actor_name, at, action, parameter, subject_kind, subject_id, qualifier, safeguard_id, supersedes)
		values ($1, 'human', 'owner', $2, 'created', '', 'factory_settings', 'fs_y', '', '', '')`,
		record.NewID(policy.IDPrefix), record.Now())
	if err == nil {
		t.Error("the store accepted a second version superseding nothing, and the sequence would have two beginnings")
	}

	// Concurrent authoring serialises on the lock, so both writes land and the
	// chain each of them appended is still a chain.
	const writers = 4
	done := make(chan error, writers)
	for i := range writers {
		go func(n int) {
			_, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, float64(n+1))
			done <- err
		}(i)
	}
	for range writers {
		if err := <-done; err != nil {
			t.Errorf("a concurrent authoring failed rather than waiting: %v", err)
		}
	}

	var rows, roots, distinct int
	if err := in.pool.QueryRow(ctx, `select count(*), count(*) filter (where supersedes = ''),
		count(distinct supersedes) from `+policy.Table).Scan(&rows, &roots, &distinct); err != nil {
		t.Fatalf("counting the versions: %v", err)
	}
	if roots != 1 {
		t.Errorf("%d versions supersede nothing, want the one beginning", roots)
	}
	if distinct != rows {
		t.Errorf("%d versions name %d distinct predecessors, and a chain names one each", rows, distinct)
	}
}
