// The tests of this package are in dispatch_test, opening the pool through
// package postgres: one dispatch writes an item's stage, an input manifest, an
// agent run record and a hold row of the log, so the whole schema is applied
// rather than one package's.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package dispatch_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

const (
	oneProject = "pr_00000000000000000000000000000000"
	oneService = "svc_0000000000000000000000000000000"
	oneArea    = "ar_00000000000000000000000000000000"
	modelName  = "vendor/test-model"
)

var decompositionActor = record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed}

// fakeModel answers a canned reply per call, in order, and records the
// principal and the prompt it was called with.
type fakeModel struct {
	replies []agent.Reply
	errs    []error
	calls   int
	prompt  string
	as      principal.Principal
}

func (f *fakeModel) Complete(_ context.Context, p principal.Principal, system, _ string) (agent.Reply, error) {
	f.prompt, f.as = system, p
	n := f.calls
	f.calls++
	if n >= len(f.replies) {
		n = len(f.replies) - 1
	}
	var err error
	if n < len(f.errs) {
		err = f.errs[n]
	}
	return f.replies[n], err
}

// oneEntry is the fleet an owner composed for these tests: one entry per role
// on the whole factory, all on one model.
type oneEntry struct {
	model *fakeModel
	// covers is false where no entry covers the role, which is the first
	// condition that stops a dispatch.
	covers bool
	scope  dispatch.Scope
}

func (o *oneEntry) EntryFor(_ context.Context, role dispatch.Role, on dispatch.On) (dispatch.Entry, bool, error) {
	if !o.covers || !o.scope.Covers(on) {
		return dispatch.Entry{}, false, nil
	}
	return dispatch.Entry{
		Role: role, Scope: o.scope, Model: o.model,
		ModelVersion: modelName, CredentialName: "model.test",
	}, true, nil
}

// shippedPrompts is the role prompt version in force per role, which the
// composition reads off the artifact store. inForce is false where a role has
// none, which is the second condition that stops a dispatch.
type shippedPrompts struct {
	inForce bool
	version artifact.Artifact
}

func (s *shippedPrompts) InForce(context.Context, dispatch.Role) (artifact.Artifact, bool, error) {
	return s.version, s.inForce, nil
}

// fixedLimit is the attempt limit in force, which a run reads through package
// policy and a test states.
type fixedLimit struct{ limit float64 }

func (f fixedLimit) AttemptLimit(context.Context, policy.Subjects) (policy.Effective, error) {
	return policy.Effective{Number: f.limit}, nil
}

// countingEscalation records what was escalated, standing in for the gate
// component's own enforcement, which writes the escalated value, abandons the
// pending rows and pages.
type countingEscalation struct {
	items  []string
	stages []item.Stage
}

func (c *countingEscalation) Escalate(_ context.Context, _ record.Actor, itemID string, stage item.Stage) error {
	c.items = append(c.items, itemID)
	c.stages = append(c.stages, stage)
	return nil
}

// composed is everything one test drives.
type composed struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	dispatch   *dispatch.Dispatch
	items      *item.Dispatch
	fleet      *oneEntry
	prompts    *shippedPrompts
	escalation *countingEscalation
	model      *fakeModel
	intake     *intent.Intake

	decomposition *item.Decomposition
}

// newDispatch gives a test a schema of its own, the whole schema applied in
// it, and the component over it.
func newDispatch(t *testing.T, replies []agent.Reply, errs []error, limit float64) composed {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1ds_" + hex.EncodeToString(suffix[:])

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
		t.Fatalf("Apply: %v", err)
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	model := &fakeModel{replies: replies, errs: errs}
	c := composed{
		ctx:        ctx,
		pool:       pool,
		items:      item.NewDispatch(pool, token),
		fleet:      &oneEntry{model: model, covers: true},
		prompts:    &shippedPrompts{inForce: true, version: artifact.Artifact{ID: "art_role_prompt", Content: "the role prompt in force"}},
		escalation: &countingEscalation{},
		model:      model,
		intake:     intent.NewIntake(pool, token),
	}
	c.dispatch, err = dispatch.New(dispatch.Composition{
		Pool: pool, Token: token,
		Fleet: c.fleet, Prompts: c.prompts, Items: c.items,
		Policy:     fixedLimit{limit: limit},
		Log:        decisionlog.NewWriter(pool, token),
		Reader:     decisionlog.NewReader(pool, token),
		Manifests:  inputmanifest.NewWriter(pool, token),
		Runs:       agentrun.NewWriter(pool, token),
		Escalation: c.escalation,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.decomposition = item.NewDecomposition(pool, token)
	return c
}

// inSchema points a connection URL at one schema and nothing else.
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

// oneItem is an item at the spec stage, decomposed from an intent in the state
// the test names.
func (c composed) oneItem(t *testing.T, state intent.State) item.Item {
	t.Helper()
	in, err := c.intake.TakeIn(c.ctx, record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed},
		intent.Arrival{Source: intent.SourceOwner, Statement: "a health endpoint", ProjectID: oneProject})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	if state != intent.StateUnrefined {
		if _, err := c.pool.Exec(c.ctx, `update `+intent.Table+` set state = $1 where id = $2`, string(state), in.ID); err != nil {
			t.Fatalf("putting the intent in %s: %v", state, err)
		}
	}
	it, err := c.decomposition.Create(c.ctx, decompositionActor, item.New{
		IntentID: in.ID, ServiceID: oneService, AreaID: oneArea, Branch: "item/health",
	}, oneProject, oneProject, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return it
}

// aSpec is a reply the spec author's protocol accepts.
const aSpec = "SPEC:\nThe service exposes /healthz.\nCRITERION rq_a: The system shall answer."

// on is the dispatch one item's spec stage is for.
func on(it item.Item) dispatch.On {
	return dispatch.On{
		ItemID: it.ID, Stage: item.StageSpec, IntentID: it.IntentID,
		ProjectID: oneProject, ServiceID: oneService, AreaID: oneArea,
	}
}

// TestOneDispatchWritesTheManifestTheRunAndTheTransition is the whole sequence
// on a stage that authors first time: the manifest before the run, the agent
// run record after it naming the role prompt version in force and the units
// per kind, and the item's own count for the stage standing at the one entry
// decomposition wrote.
func TestOneDispatchWritesTheManifestTheRunAndTheTransition(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsInput: 20, agent.UnitsOutput: 5}}}, nil, 3)
	it := c.oneItem(t, intent.StateRefined)

	refined, run, err := c.dispatch.SpecAuthor(c.ctx, on(it),
		[]inputmanifest.Material{{Class: "intent", Reference: it.IntentID, Bytes: 17}}, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("SpecAuthor: %v", err)
	}
	if len(refined.Criteria) != 1 {
		t.Fatalf("the spec author authored %d criteria, want the one the reply named", len(refined.Criteria))
	}
	if c.model.prompt != "the role prompt in force" {
		t.Errorf("the role was told %q, want the version in force", c.model.prompt)
	}
	if c.model.as.Actor.Key != modelName || c.model.as.DispatchID != run.ID {
		t.Errorf("the call was made as %+v, want the model version and this dispatch", c.model.as)
	}

	manifest, err := inputmanifest.Get(c.ctx, c.pool, run.InputManifestID)
	if err != nil {
		t.Fatalf("Get the manifest: %v", err)
	}
	if manifest.ItemID != it.ID || manifest.Stage != string(item.StageSpec) || len(manifest.Materials) != 1 {
		t.Errorf("the manifest is %+v, want this item's stage and the one material handed over", manifest)
	}

	runs, err := agentrun.ForItem(c.ctx, c.pool, it.ID)
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("%d agent run records, want one per call", len(runs))
	}
	recorded := runs[0]
	if recorded.Role != string(dispatch.RoleSpecAuthor) || recorded.RolePromptVersionID != "art_role_prompt" {
		t.Errorf("the run names role %q and prompt %q, want the role and the version in force",
			recorded.Role, recorded.RolePromptVersionID)
	}
	if recorded.UnitsByKind[agent.UnitsInput] != 20 || recorded.UnitsByKind[agent.UnitsOutput] != 5 {
		t.Errorf("the run spent %v, want the units the provider returned per kind", recorded.UnitsByKind)
	}
	if recorded.InputManifestID != run.InputManifestID {
		t.Errorf("the run names manifest %q, want the one written before it", recorded.InputManifestID)
	}

	stages, err := item.Stages(c.ctx, c.pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 1 || stages[0].Attempts != 1 {
		t.Errorf("the item stands at %+v, want the one entry that put it at spec", stages)
	}
}

// TestARefusedReplyIsEnteredAgainAndCountedOnTheItem: a second attempt at one
// stage is the item entering it again, so the count rises on the record rather
// than in the process — which is what makes a second run of the factory carry
// on from what the first spent.
func TestARefusedReplyIsEnteredAgainAndCountedOnTheItem(t *testing.T) {
	c := newDispatch(t, []agent.Reply{
		{Text: "not the protocol", Units: map[string]int64{agent.UnitsOutput: 1}},
		{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 2}},
	}, nil, 3)
	it := c.oneItem(t, intent.StateRefined)

	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("SpecAuthor: %v", err)
	}
	if len(run.AgentRunIDs) != 2 {
		t.Errorf("%d run records, want one per call including the refused one", len(run.AgentRunIDs))
	}
	stages, err := item.Stages(c.ctx, c.pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 1 || stages[0].Attempts != 2 {
		t.Fatalf("the item stands at %+v, want two attempts at spec", stages)
	}
}

// TestTheStoredCountIsWhatTheLimitIsComparedAgainst: a dispatch onto an item
// that has already spent its allowance escalates on its first refused reply,
// because the count it reads is the item's own and not this call's.
func TestTheStoredCountIsWhatTheLimitIsComparedAgainst(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: "not the protocol"}}, nil, 2)
	it := c.oneItem(t, intent.StateRefined)
	// The item has been entered once by decomposition; two more entries put its
	// count above the limit, which is what exceeding one is.
	for range 2 {
		if _, err := c.items.Enter(c.ctx, dispatch.Actor, it.ID, item.StageSpec); err != nil {
			t.Fatalf("Enter: %v", err)
		}
	}

	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Fatalf("SpecAuthor = %v, want ErrOutOfAttempts", err)
	}
	if !run.Escalated {
		t.Error("the run does not say the item escalated")
	}
	if len(c.escalation.items) != 1 || c.escalation.items[0] != it.ID {
		t.Errorf("escalated %v, want this item", c.escalation.items)
	}
	if c.model.calls != 0 {
		t.Errorf("%d calls, want no agent put on a stage whose count already exceeds the limit", c.model.calls)
	}
}
