// What holds over all five episodes, over HTTP against the real composition:
// the refusal of a call whose factory version is not the store's, the chain
// clean over a log holding decisions closed from a screen, the link walk from
// the last deploy back to its intent, and Factory's own numbers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestAVersionThatIsNotTheStoresIsARequiredReload is the last of the five more
// things, against the real composition: a call whose factory version is not
// this binary's is refused with a required reload rather than a failed action,
// on every call and not on the load alone — so a screen open across an upgrade
// is stopped by it.
func TestAVersionThatIsNotTheStoresIsARequiredReload(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	for _, address := range []string{"/api/home", "/api/work", "/api/factory", "/api/people"} {
		answer := s.at(t, "GET", address, nil, "a version this binary never shipped")
		if answer.status != http.StatusConflict {
			t.Errorf("GET %s under another version answered %d, want 409", address, answer.status)
		}
		if !answer.reloadRequired() {
			t.Errorf("GET %s answered %s, want a required reload", address, answer.body)
		}
	}

	// A write is refused the same way, which is what keeps a human mid-edit
	// from writing under a version the store no longer holds.
	answer := s.at(t, "POST", "/api/call/supplyIntent", screens.SupplyIntentArgs{
		Statement: theStatement, Services: []string{theService},
	}, "a version this binary never shipped")
	if answer.status != http.StatusConflict || !answer.reloadRequired() {
		t.Errorf("a call under another version answered %d: %s", answer.status, answer.body)
	}
	if answer.expected != factoryVersion {
		t.Errorf("the refusal names %q as the version to reload to, want %q", answer.expected, factoryVersion)
	}

	// Under this binary's own version the same call is answered, so what
	// refused it was the version and nothing else.
	if id := s.mustCall(t, "supplyIntent", screens.SupplyIntentArgs{
		Statement: theStatement, Services: []string{theService},
	}); id == "" {
		t.Error("supplyIntent under the right version answered with no id")
	}
}

// TestOverAllOfItTheChainVerifiesAndFactoryReportsItsNumbers is what the
// demonstration asks of the whole of it: one change carried to production with
// every verdict typed at a screen, the chain clean over a log holding those
// decisions with the time the row was opened in Work on each, the link walk
// from the last deploy back to its intent, and Factory's numbers over the one
// factory-owned span — the auto-approval rate against how often it was undone,
// the human's load split, and the page channel's own numbers beside them.
func TestOverAllOfItTheChainVerifiesAndFactoryReportsItsNumbers(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	s := newScreens(t, ctx, d, out)

	// One item taken in at Work and carried as far as the records take it, with
	// every row a human decides closed through the call a screen makes and
	// each close carrying when the row was opened there.
	var taken shipped
	if err := s.p.takeIn(ctx, &taken, of(theStatement)); err != nil {
		t.Fatalf("taking the intent in: %v\n%s", err, out)
	}
	deployID := ""
	for pass := 0; pass < 12; pass++ {
		advanced, err := s.p.advance(ctx)
		if err != nil {
			t.Fatalf("pass %d: %v\n%s", pass, err, out)
		}
		if advanced.deployed != "" {
			deployID = advanced.deployed
		}
		decided, err := decideEveryPendingRow(t, ctx, s)
		if err != nil {
			t.Fatalf("deciding what pass %d left pending: %v\n%s", pass, err, out)
		}
		if !advanced.moved && decided == 0 {
			break
		}
	}
	if deployID == "" {
		t.Fatalf("nothing reached production through the screens alone:\n%s", out)
	}

	// A page a human fired, so the page channel has something to report.
	svc := theServiceRecord(t, ctx, s.p)
	s.mustCall(t, "firePage", screens.FirePageArgs{
		ServiceID: svc.ID, Reason: "the release is live and the incident is not what the window measured",
	})

	// Verify over a log holding decisions closed from a screen.
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}

	// The link walk from the last deploy back to its intent, which is the walk
	// subcommand's own function.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, d.token,
		asPrincipal(owner(t, ctx, d.pool, d.token, d.human)), deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", deployID, theStatement, walked.String())
	}
	if !strings.Contains(walked.String(), "the chain is clean") {
		t.Errorf("the walk does not report the chain clean:\n%s", walked.String())
	}

	// Factory's own numbers.
	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	if len(factory.ApproveUndone) == 0 {
		t.Error("Factory reports no approve-and-undone pair, and a human approved rows on this span")
	}
	approvedByAnybody := int64(0)
	for _, pair := range factory.ApproveUndone {
		approvedByAnybody += pair.Approved
	}
	if approvedByAnybody == 0 {
		t.Errorf("Factory reports nothing approved: %+v", factory.ApproveUndone)
	}
	if len(factory.LoadSplits) == 0 {
		t.Error("Factory reports no load split, and rows waited on a human and were decided")
	}
	if len(factory.HumanLoad) == 0 {
		t.Error("Factory reports no human load, and rows waited on a human")
	}
	if factory.PageChannel.HumanFiredPages != 1 {
		t.Errorf("Factory reports %d page(s) fired on a human's own judgment, want the one",
			factory.PageChannel.HumanFiredPages)
	}
	if factory.PageChannel.PagesPerService[svc.ID] == 0 {
		t.Errorf("Factory counts no page against service %s, and the page fired above named it: %+v",
			svc.ID, factory.PageChannel)
	}
	if len(factory.Numbers.ThroughputPerService) == 0 {
		t.Errorf("Factory reports no throughput per service after a release shipped: %+v", factory.Numbers)
	}
}

// decideEveryPendingRow types an approve at Work against every row a pass left
// waiting on a human, each carrying when the actor opened it, and answers with
// how many it closed.
func decideEveryPendingRow(t *testing.T, ctx context.Context, s *screenServer) (int, error) {
	t.Helper()
	pending, err := s.p.gate.Pending(ctx)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, opened := range pending {
		if !opened.HumanDecides {
			// A row open only because a hold stands is the gate's own to
			// re-evaluate, and closing it would decide the event the hold
			// exists to stop.
			continue
		}
		status, body := s.call(t, "decide", screens.DecideArgs{
			OpenEventID: opened.Row.ID, Verdict: string(gate.VerdictApprove),
			OpenedInWorkAt: theOpenedInWorkAt,
		})
		if status != http.StatusNoContent && status != http.StatusOK {
			t.Fatalf("deciding %s answered %d: %s", opened.Gate, status, body)
		}
		closed++
	}
	return closed, nil
}

// answered is one request's own answer: its status, its body, and the two
// fields the version refusal carries.
type answered struct {
	status   int
	body     string
	expected string
	reload   bool
}

func (a answered) reloadRequired() bool { return a.reload }

// at makes one request under a factory version the caller names, which is how a
// test drives the refusal a client renders as a required reload.
func (s *screenServer) at(t *testing.T, method, address string, args any, version string) answered {
	t.Helper()
	var body *bytes.Reader
	if args != nil {
		encoded, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("marshalling the arguments of %s: %v", address, err)
		}
		body = bytes.NewReader(encoded)
	} else {
		body = bytes.NewReader(nil)
	}
	request, err := http.NewRequest(method, s.url+address, body)
	if err != nil {
		t.Fatalf("building the request for %s: %v", address, err)
	}
	request.Header.Set("X-Factory-Version", version)
	request.Header.Set("X-Factory-Principal", s.principal)
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, address, err)
	}
	defer answer.Body.Close()
	read := &bytes.Buffer{}
	_, _ = read.ReadFrom(answer.Body)
	given := answered{status: answer.StatusCode, body: read.String()}
	var refusal struct {
		ReloadRequired bool   `json:"reload_required"`
		Expected       string `json:"expected"`
	}
	if json.Unmarshal(read.Bytes(), &refusal) == nil {
		given.reload, given.expected = refusal.ReloadRequired, refusal.Expected
	}
	return given
}
