//go:build realmodel

// The real-model test: roadmap M1's demonstration driven against the model API
// instead of a fake, which is the only thing that shows the path works. What a
// fake model cannot fail on is everything between the request and the reply —
// the credential's header, the shape of a real answer, a reply the protocol
// refuses, an encoding named the way a model chooses to name it. Every one of
// those has been broken here at least once while the fake-model suite stayed
// green.
//
// It is behind the realmodel build tag because it spends a credential no clone
// has and quota that is somebody's, so it must not run in `go test ./...` or in
// CI. What the tag costs: this file is not compiled in the default build, so a
// change that breaks it is found only by someone running it. Run it whenever
// something on the path between a role and the provider changes, and before
// calling a milestone done:
//
//	FACTORY_MODEL=deepseek/deepseek-v4-flash go test -tags realmodel -count=1 -v -run RealModel ./cmd/factory/
//
// FACTORY_PROVIDER names which provider answers, openrouter by default, and
// FACTORY_MODEL is that provider's model id — OpenRouter's ids are namespaced
// and Anthropic's are not, so the two are set together or neither is.
//
// The credential comes from factory/secrets.local, which .gitignore refuses to
// track, under the name the named provider resolves. When it holds no key this
// test fails rather than skipping, for the reason the database tests do not skip
// either: a silent skip is how a green run comes to mean nothing.
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/window"
)

// realModelSecrets is the gitignored credential file, relative to this package's
// directory, which is where `go test` runs.
const realModelSecrets = "../../secrets.local"

// realModelPlaceholder is what the file ships holding. A run against it would
// send the placeholder as a credential and read a 401 back, so it is named as
// what it is instead. One string for either provider's line, because what the
// test can say about it is the same either way.
const realModelPlaceholder = "PASTE_THE_KEY_HERE"

// realModelPace is the least time between two model calls, the same default the
// run subcommand's -pace flag carries. A take makes a handful of calls, so this
// adds a few seconds to the test and keeps it sending requests at the rate a
// real run sends them.
const realModelPace = 2 * time.Second

// realModelStatement is the intent a demo would type, from DEMO.md's
// Statements that work: one behaviour a sentence can state and a test can
// decide, the module and the standard library named so the build has what it
// needs, and a port high enough not to meet anything else on the machine.
//
// It says nothing about the quantity the factory watches by, and nothing about a
// contract. Both are the implementer's standing instructions and neither is part of
// any request, so a statement that asked for either would be testing whether a model
// can follow a statement rather than whether it follows the prompt — which is the
// thing here that only a real call answers.
//
// The Go version is named rather than asked for. It said "a Go version" until
// 2026-08-20, and deepseek/deepseek-v4-flash wrote `go 1.x` into go.mod — a
// placeholder go refuses to parse, from a statement that left the choice open. An
// underdetermined request is what the interview exists for and the model did not ask,
// so what is fixed here is the request: an owner supplying a constraint (2) names the
// value.
const realModelStatement = "A Go HTTP service, module borg.demo/realmodel, package main in main.go at the repository root, " +
	"standard library only. The change must include a go.mod file declaring that module and go 1.24. " +
	"It answers GET /health with status 200 and the body ok, on port 8199. " +
	"Test the handler through net/http/httptest rather than by binding the port."

// TestTheDemonstrationAgainstARealModel is M1's demonstration end to end, under
// M2's score and along M3's path: an intent taken in, an item decomposed, a spec and an
// implementation authored by a real model, a candidate environment of the item's
// own with the build running on it and the criteria decided there, all three gate
// rows approved by the human the score puts there, release 1 minted by the merge
// queue, a deploy without a control running on the target, and the walk back to
// the intent over a clean chain.
//
// It asserts that a human decided rather than what the number was. A real
// model's diff is whatever it wrote, so the number moves run to run — but every
// factor of a service's first release reads at the risky end whatever the diff
// is, so a human at every row is the one thing this take can promise. What the
// number came out as is in the run's logged output.
func TestTheDemonstrationAgainstARealModel(t *testing.T) {
	name := os.Getenv("FACTORY_MODEL")
	if name == "" {
		t.Fatal("FACTORY_MODEL names the provider's model id and has no default, roadmap M1 requiring the model named in configuration")
	}
	// The same default the run subcommand's -provider flag carries, so what
	// this test drives is what a take drives.
	provider := os.Getenv("FACTORY_PROVIDER")
	if provider == "" {
		provider = "openrouter"
	}

	// The credential name this provider's calls resolve, switched on here
	// rather than reached through newModel, because the value is read before
	// anything is spent: a file still holding the placeholder fails the test
	// instead of being sent as a key. It is the same two constants newModel
	// uses, so there is no second list of names to keep in step, and what it
	// answers is which name and not which implementation.
	var credentialName, mints string
	switch provider {
	case "openrouter":
		credentialName, mints = openRouterCredentialName, "an OpenRouter API key"
	case "anthropic":
		credentialName, mints = anthropicCredentialName, "the token `claude setup-token` mints"
	default:
		t.Fatalf("FACTORY_PROVIDER=%q is not one of %s", provider, providers)
	}

	resolver, err := secretref.Load(realModelSecrets)
	if err != nil {
		t.Fatalf("loading %s: %v", realModelSecrets, err)
	}
	value, err := resolver.Resolve(secretref.MustNew(credentialName))
	if err != nil {
		t.Fatalf("resolving %s from %s: %v", credentialName, realModelSecrets, err)
	}
	if value == "" || value == realModelPlaceholder {
		t.Fatalf("%s still holds the placeholder for %s; put %s in it", realModelSecrets, credentialName, mints)
	}
	provided, err := newModel(provider, name, resolver)
	if err != nil {
		t.Fatal(err)
	}

	// The interview asks at most one question and may ask none, so the first
	// scripted line has to be a valid verdict as well as an answer. What that
	// costs is the answer's quality where a question does come: the spec author
	// is answered with the word approve and authors on it. Three lines, because a
	// first release puts a human at both gate rows and the answer may take the
	// first of them.
	//
	// Four lines, because a first release puts a human at all three rows and the
	// answer may take the first of them.
	//
	// Paced as the run subcommand paces it, so what this test drives is what a
	// take drives — a run that sends requests back to back is one of the things
	// only a real provider can object to.
	// The repository outlives a failing run, which the temp directory newPath
	// hands out does not: what a real model wrote is the evidence a failure is
	// diagnosed from, and a test that deletes it leaves a reader with the error
	// text alone. A passing run has nothing to look at, so that one is removed.
	//
	// It is named before the install rather than swapped in afterwards, because the
	// repository a run works in is the service record's own field and decomposition writes
	// that record — so a directory chosen after the record exists reaches nothing.
	repo, err := os.MkdirTemp("", "factory-realmodel-")
	if err != nil {
		t.Fatalf("making the repository directory: %v", err)
	}
	ctx, d, out := newPathIn(t, "approve\napprove\napprove\napprove\n",
		[]serviceRepo{{name: theService, repo: repo}})
	d.model = agent.NewPaced(provided, realModelPace)
	// The author every version this take writes names is the model that wrote it,
	// which is what a per-author prior is kept on.
	d.modelName = name

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the repository is kept for reading: %s", repo)
			return
		}
		if err := os.RemoveAll(repo); err != nil {
			t.Errorf("removing %s: %v", repo, err)
		}
	})

	res, err := run(ctx, d, of(realModelStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\n\nthe run's output:\n%s", err, out)
	}
	t.Logf("the run's output:\n%s", out)
	c := only(t, res)

	if c.rejected {
		t.Fatal("the run reports rejected, and the scripted verdict was approve")
	}
	if c.releaseID == "" || c.deployID == "" {
		t.Fatalf("the run names release %q and deploy %q, an approved take ships both", c.releaseID, c.deployID)
	}
	if c.environmentID == "" || !c.tornDown {
		t.Errorf("the candidate ran on environment %q and torn down = %v, want an environment of its own torn down at the merge",
			c.environmentID, c.tornDown)
	}

	// The release is the service's first.
	rel, err := release.Get(ctx, d.pool, c.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("release number = %d, a service's first release is 1", rel.Number)
	}

	// The deploy completed, and the target is running what it put there.
	current, found, err := deploy.Current(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil {
		t.Fatalf("reading the current deploy: %v", err)
	}
	if !found || current.ID != c.deployID || current.Status != deploy.StatusComplete {
		t.Errorf("the current deploy is %q found=%t status=%s, the path deployed %s and completes it",
			current.ID, found, current.Status, c.deployID)
	}
	running, err := d.targets.at(d.dir).ReadRunning(ctx, theService, d.credential)
	if err != nil {
		t.Fatalf("reading what the target runs: %v", err)
	}
	if running.Build != rel.BuildID {
		t.Errorf("the target runs %q, the deploy put build %s there", running.Build, rel.BuildID)
	}

	// Every attempt the two authoring stages made is on the item, refused ones
	// included — which is the number a real model moves and a fake one does not.
	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		t.Logf("stage %s: %d attempt(s), %d tokens", st.Stage, st.Attempts, st.SpendTokens)
		if st.Attempts < 1 || st.SpendTokens <= 0 {
			t.Errorf("stage %s reports %d attempts and %d tokens, a real call spends both",
				st.Stage, st.Attempts, st.SpendTokens)
		}
	}

	// Every row put a human there and every decision names a score version and a
	// policy version that are records rather than names.
	for _, fired := range []struct {
		row string
		got fired
	}{
		{"deploy to candidate environment", c.candidateGate},
		{"merge to master", c.mergeGate},
		{"deploy to production", c.deployGate},
	} {
		t.Logf("%s: number %.3f against threshold %.3f (%s), human %v (%s)",
			fired.row, fired.got.number, fired.got.threshold, fired.got.thresholdFrom,
			fired.got.humanDecided, fired.got.whyHuman)
		if !fired.got.humanDecided {
			t.Errorf("%s auto-passed a service's first release at %.3f against %.3f",
				fired.row, fired.got.number, fired.got.threshold)
		}
		if fired.got.scoreVersion == "" || fired.got.policyVersion == "" {
			t.Errorf("%s names score version %q and policy version %q",
				fired.row, fired.got.scoreVersion, fired.got.policyVersion)
		}
	}

	// The watch window, which is the one thing on this stretch a fake model cannot
	// fail: the implementer is told to emit the quantity the factory watches by, and
	// whether a real model does what an instruction asks is exactly what only a real
	// call answers. A build that emitted nothing leaves a window that can end at its
	// cap and nowhere else, however healthy the service is.
	if c.windowID == "" {
		t.Fatalf("no watch window opened over the production deploy:\n%s", out)
	}
	w, err := window.Get(ctx, d.pool, c.windowID)
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	t.Logf("window %s: size %v, confidence %v, cap %vs, cleared available %v, exit %q",
		w.ID, w.Size, w.Confidence, w.CapSeconds, w.ClearedAvailable, w.Exit)
	if w.ClearedAvailable {
		t.Error("the window says clean was available to a service's first release, and there is nothing below it to compare against")
	}
	if w.Exit != window.ExitTimedOut {
		t.Errorf("the window closed %q, and a first release can end at the cap and nowhere else", w.Exit)
	}

	units, failures, err := countSignal(localtarget.SignalFile(d.dir, rel.BuildID))
	if err != nil {
		t.Fatalf("reading the quantity build %s emitted: %v", rel.BuildID, err)
	}
	t.Logf("build %s emitted %d unit(s), %d failed", rel.BuildID, units, failures)
	if units == 0 {
		t.Errorf("build %s emitted nothing into %s, so this release cannot be watched at all — the implementer is told to append one line per unit of work it does, and what a real model did with that instruction is what this test is for",
			rel.BuildID, localtarget.SignalFile(d.dir, rel.BuildID))
	}

	// The walk reaches the statement from the deploy, over a clean chain.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, c.deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), realModelStatement) {
		t.Errorf("the walk from %s does not reach the statement:\n%s", c.deployID, walked.String())
	}
	if !strings.Contains(walked.String(), "the chain is clean") {
		t.Errorf("the walk does not report the chain clean:\n%s", walked.String())
	}
}
