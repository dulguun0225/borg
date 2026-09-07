// Episode two of the demonstration, over HTTP: one item from an intent
// supplied at Work through every gate to production, with every act a human
// makes given as the call a screen makes and every reading taken from the view
// that human would be looking at.
package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/screens"
)

// theEditedPlan is the implementation plan a human authors at the gate through
// Edit in place, which is duty 11: the plan is a document, so a human who wants
// a different approach writes one into it rather than rejecting the item to get
// one.
const theEditedPlan = "Answer the health check from the handler the service already registers, " +
	"and add no dependency for it."

// TestEpisodeTwoIsOneItemThroughTheScreens is the second episode: the
// interview's question answered at Work (3), the confirming round beside it,
// the Spec row's criteria confirmed (6), an implementation plan written
// together with the factory through Edit in place (11), an item the factory
// gave up on taken over at its escalation (12), UAT performed at the Merge to
// master row (7), and the deploy to production, after which the home view is
// the digest and the badge is at zero.
//
// Every verdict is written against the open event the row was rendered from
// and every close event names when the row was opened in Work, which is the
// one field no terminal could fill.
//
// Two of the things DEMO.md does here this test does not drive. One row opened
// in two browsers and decided in the first, which the second is told of by its
// own subscription: a second client is a second process, and what that step
// shows — the stream carrying a change before its human acts on it — is what
// [TestAVerdictAtAScreenCarriesWhenTheRowWasOpened] holds over one row. And
// what prints on serve's terminal between the verdicts, which is the run
// subcommand's own output and no screen's reading.
func TestEpisodeTwoIsOneItemThroughTheScreens(t *testing.T) {
	// The composition answers no round of the interview and closes no pending
	// row: both are the human's in this episode, and both are made through
	// package screens' own calls.
	ctx, d, out := newPath(t, "\n")
	d.decide = nil
	// The factory gives up on the item once, at implementation. The
	// implementer's first attemptLimit replies are prose outside the block
	// protocol, which is what spends the limit and escalates; every reply after
	// them is the fake model's own, so the stage the take-over returns the item
	// to can finish.
	d.model = &refusingModel{inner: &fakeModel{}, refusals: attemptLimit}
	s := newScreens(t, ctx, d, out)

	// Duty 1: the intent supplied at Work.
	intentID := s.mustCall(t, "supplyIntent", screens.SupplyIntentArgs{
		Statement: theStatement, Services: []string{theService},
	})
	if intentID == "" {
		t.Fatal("supplyIntent answered with no id")
	}

	// Duty 3: the interview's one question, answered at Work on the intent.
	// The pass asks it and leaves it waiting; the badge counts it and the
	// digest is not shown while it does.
	s.pass(t, ctx, out)
	var home screens.Home
	s.get(t, "/api/home", &home)
	if home.Badge.InterviewQuestions != 1 {
		t.Fatalf("the badge counts %d interview question(s) with one round waiting: %+v",
			home.Badge.InterviewQuestions, home.Badge)
	}
	if home.Digest != nil {
		t.Error("the digest is shown with a round waiting, and it is the part that appears only at zero")
	}
	asked := roundWaitingOn(t, ctx, s.p, intentID)
	if asked.Question != theQuestion {
		t.Errorf("the round waiting asks %q, want the interview's own question", asked.Question)
	}
	s.mustCall(t, "answerQuestion", screens.AnswerQuestionArgs{QuestionID: asked.ID, Answer: theAnswer})

	// The confirming round, on the same intent: whether what the factory
	// understood is what was wanted.
	s.pass(t, ctx, out)
	confirming := roundWaitingOn(t, ctx, s.p, intentID)
	if readingIn(confirming.Question) == nil {
		t.Fatalf("the round waiting after the answer asks %q, want the confirming round", confirming.Question)
	}
	s.mustCall(t, "confirmReading", screens.ConfirmReadingArgs{IntentID: intentID, Confirmed: true})

	// The pass decomposes the refined intent into its one item and carries it
	// to the first row a human decides, and the item is on the Work board from
	// here until nothing waits on it.
	s.pass(t, ctx, out)
	itemID := theOneItemWaiting(t, s)

	decided := decideEveryRowAtWork(t, ctx, s, itemID, out)
	for _, kind := range []gate.Kind{
		gate.KindSpec, gate.KindImplementationPlan, gate.KindTasks, gate.KindImplementation,
		gate.KindDeployToCandidateEnvironment, gate.KindMergeToMaster, gate.KindDeployToProduction,
	} {
		if decided[kind] == 0 {
			t.Errorf("no verdict was given at %s, and a service's first release puts a human at every row", kind)
		}
	}
	// The plan row twice: once on the version the factory wrote, which the edit
	// superseded, and once on the version the human typed.
	if decided[gate.KindImplementationPlan] < 2 {
		t.Errorf("the implementation plan row was decided %d time(s), want the edit in place and the verdict on what it authored",
			decided[gate.KindImplementationPlan])
	}

	// The item is live: the release minted, the deploy on production's own
	// target, and the analysis window opened over it.
	var view screens.Item
	s.get(t, "/api/item/"+itemID, &view)
	if view.Release == nil || view.Release.Number != 1 {
		t.Fatalf("the item reads release %+v, want the service's first", view.Release)
	}
	onProduction := false
	for _, one := range view.Deploys {
		if one.EnvironmentID == s.p.production.ID && one.CompletedAt != "" {
			onProduction = true
		}
	}
	if !onProduction {
		t.Errorf("the item shows no completed deploy on production: %+v", view.Deploys)
	}
	if len(view.Windows) == 0 {
		t.Errorf("the item shows no analysis window, and one is opened over every production deploy")
	}
	svc := theServiceRecord(t, ctx, s.p)
	var running screens.Service
	s.get(t, "/api/service/"+svc.ID+"/on/"+s.p.production.ID, &running)
	if release := runningOnTheTarget(t, running); release.ReleaseNumber != 1 {
		t.Errorf("production runs release %d, want the one this episode shipped", release.ReleaseNumber)
	}

	// The acceptance round: the one round that follows production, asked of the
	// requester once every item of the intent is live. Intake asks it on
	// serve's own pass rather than on the path's, so the test calls it where
	// serve would and answers it at Work.
	set := s.p.sets[intentID]
	if set == nil {
		t.Fatalf("the pass holds no decomposition of intent %s, and the acceptance round is asked per set", intentID)
	}
	if _, err := s.p.acceptanceRounds(ctx, []*decompositionSet{set}); err != nil {
		t.Fatalf("asking the acceptance round: %v\noutput so far:\n%s", err, out)
	}
	s.get(t, "/api/home", &home)
	if home.Badge.InterviewQuestions != 1 {
		t.Fatalf("the badge counts %d round(s) with the acceptance round waiting: %+v",
			home.Badge.InterviewQuestions, home.Badge)
	}
	s.mustCall(t, "confirmReading", screens.ConfirmReadingArgs{IntentID: intentID, Confirmed: true})
	if in, err := intent.Get(ctx, d.pool, intentID); err != nil || in.State != intent.StateDelivered {
		t.Errorf("the intent reads %q after the acceptance round, want delivered: %v", in.State, err)
	}

	// Nothing waits on a human, so the board is empty and the home view is the
	// digest. The last check rows beside it are episode three's subject and are
	// outside the badge, so nothing here reads them.
	var board screens.Work
	s.get(t, "/api/work?waiting_on_a_human=true", &board)
	if len(board.Rows) != 0 {
		t.Errorf("the board holds %d row(s) waiting on a human with the item live: %+v", len(board.Rows), board.Rows)
	}
	s.get(t, "/api/home", &home)
	if home.Badge.Total != 0 {
		t.Fatalf("the badge totals %d with the item live and every round answered: %+v", home.Badge.Total, home.Badge)
	}
	if home.Digest == nil {
		t.Fatal("the digest is not shown at zero, and it is the part that appears only at zero")
	}
	if home.Digest.Releases != 1 || home.Digest.Decisions == 0 {
		t.Errorf("the digest reads %+v, want the one release this episode shipped and the decisions it took", home.Digest)
	}

	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after the episode: %v", err)
	}
}

// decideEveryRowAtWork closes every row the passes fire on one item, each
// through the call Work makes for it, and answers with how many verdicts each
// row took. Approve is what DEMO.md gives every row of this episode; the two
// actions beside it are taken where they arise — Edit in place at the
// implementation plan, and the take-over of the item the factory gives up on.
func decideEveryRowAtWork(t *testing.T, ctx context.Context, s *screenServer,
	itemID string, out *bytes.Buffer) map[gate.Kind]int {
	t.Helper()
	decided := map[gate.Kind]int{}
	edited, acknowledged, tookOver := false, false, false
	// The bound is what says the loop ended because the item was done and not
	// because a row kept re-firing: this episode's item takes fewer than twenty
	// passes and a run that takes more has stopped making progress.
	for pass := 0; pass < 20; pass++ {
		moved, escalated := s.passAllowingEscalation(t, ctx, out)
		if escalated {
			takeOverAtWork(t, s, itemID)
			tookOver = true
			continue
		}
		// The rows are read from the gate component and not from the item's
		// timeline: the timeline carries no verdict on a row an edit in place
		// abandoned and none on one still pending, so which of the two a row is
		// cannot be told there.
		pending, err := s.p.gate.Pending(ctx)
		if err != nil {
			t.Fatalf("reading the pending rows: %v", err)
		}
		acted := false
		for _, opened := range pending {
			if !opened.HumanDecides || opened.Subject.ItemID != itemID {
				continue
			}
			acted = true
			decided[opened.Gate.Kind]++
			if opened.Gate.Kind == gate.KindImplementationPlan && !edited {
				editThePlanAtWork(t, s, opened, itemID)
				edited = true
				continue
			}
			if opened.Gate.Kind == gate.KindTasks && !acknowledged {
				acknowledgeAtWork(t, s, opened, itemID)
				acknowledged = true
			}
			approveAtWork(t, s, opened, itemID)
		}
		if !acted && !moved {
			break
		}
	}
	if !edited {
		t.Error("no implementation plan was written together with the factory, and duty 11 is what that row is here for")
	}
	if !acknowledged {
		t.Error("no row was acknowledged, and what acknowledging decides is what this episode shows")
	}
	if !tookOver {
		t.Errorf("the factory never gave up on the item, so duty 12 was not performed\noutput so far:\n%s", out)
	}
	return decided
}
