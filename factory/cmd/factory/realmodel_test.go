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
//	FACTORY_MODEL=claude-opus-5 go test -tags realmodel -count=1 -v -run RealModel ./cmd/factory/
//
// The credential comes from factory/secrets.local, which .gitignore refuses to
// track. When it holds no token this test fails rather than skipping, for the
// reason the database tests do not skip either: a silent skip is how a green run
// comes to mean nothing.
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
)

// realModelSecrets is the gitignored credential file, relative to this package's
// directory, which is where `go test` runs.
const realModelSecrets = "../../secrets.local"

// realModelPlaceholder is what the file ships holding. A run against it would
// send the placeholder as a credential and read a 401 back, so it is named as
// what it is instead.
const realModelPlaceholder = "PASTE_THE_TOKEN_HERE"

// realModelStatement is the intent a demo would type, from DEMO.md's
// Statements that work: one behaviour a sentence can state and a test can
// decide, the module and the standard library named so the build has what it
// needs, and a port high enough not to meet anything else on the machine.
const realModelStatement = "A Go HTTP service, module borg.demo/realmodel, package main in main.go at the repository root, " +
	"standard library only. The change must include a go.mod file declaring the module and a Go version. " +
	"It answers GET /health with status 200 and the body ok, on port 8199. " +
	"Test the handler through net/http/httptest rather than by binding the port."

// TestTheDemonstrationAgainstARealModel is M1's demonstration end to end: an
// intent taken in, an item cut, a spec and an implementation authored by a real
// model, the gate approved, release 1 minted, a straight deploy running on the
// target, and the walk back to the intent over a clean chain.
func TestTheDemonstrationAgainstARealModel(t *testing.T) {
	name := os.Getenv("FACTORY_MODEL")
	if name == "" {
		t.Fatal("FACTORY_MODEL names the provider's model id and has no default, roadmap M1 requiring the model named in configuration")
	}

	resolver, err := secretref.Load(realModelSecrets)
	if err != nil {
		t.Fatalf("loading %s: %v", realModelSecrets, err)
	}
	credential := secretref.MustNew("model.anthropic")
	value, err := resolver.Resolve(credential)
	if err != nil {
		t.Fatalf("resolving the model credential from %s: %v", realModelSecrets, err)
	}
	if value == "" || value == realModelPlaceholder {
		t.Fatalf("%s still holds the placeholder; put a token in it — `claude setup-token` mints one", realModelSecrets)
	}

	// The interview asks at most one question and may ask none, so the first
	// scripted line has to be a valid verdict as well as an answer. What that
	// costs is the answer's quality where a question does come: the spec author
	// is answered with the word approve and authors on it.
	ctx, d, out := newPath(t, "approve\napprove\n")
	d.model = agent.Anthropic{ModelName: name, Credential: credential, Resolver: resolver}

	// The repository outlives a failing run, which the temp directory newPath
	// hands out does not: what a real model wrote is the evidence a failure is
	// diagnosed from, and a test that deletes it leaves a reader with the error
	// text alone. A passing run has nothing to look at, so that one is removed.
	repo, err := os.MkdirTemp("", "factory-realmodel-")
	if err != nil {
		t.Fatalf("making the repository directory: %v", err)
	}
	d.repo = repo
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the repository is kept for reading: %s", repo)
			return
		}
		if err := os.RemoveAll(repo); err != nil {
			t.Errorf("removing %s: %v", repo, err)
		}
	})

	res, err := run(ctx, d, realModelStatement)
	if err != nil {
		t.Fatalf("the path stopped: %v\n\nthe run's output:\n%s", err, out)
	}
	t.Logf("the run's output:\n%s", out)

	if res.rejected {
		t.Fatal("the run reports rejected, and the scripted verdict was approve")
	}
	if res.releaseID == "" || res.deployID == "" {
		t.Fatalf("the run names release %q and deploy %q, an approved take ships both", res.releaseID, res.deployID)
	}

	// The release is the service's first.
	rel, err := release.Get(ctx, d.pool, res.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("release number = %d, a service's first release is 1", rel.Number)
	}

	// The deploy completed, and the target is running what it put there.
	current, found, err := deploy.Current(ctx, d.pool, res.serviceID, "production")
	if err != nil {
		t.Fatalf("reading the current deploy: %v", err)
	}
	if !found || current.ID != res.deployID || current.Status != deploy.StatusComplete {
		t.Errorf("the current deploy is %q found=%t status=%s, the path deployed %s and completes it",
			current.ID, found, current.Status, res.deployID)
	}
	running, err := d.target.ReadRunning(ctx, d.service, d.credential)
	if err != nil {
		t.Fatalf("reading what the target runs: %v", err)
	}
	if running.Release != res.releaseID {
		t.Errorf("the target runs %q, the deploy put %s there", running.Release, res.releaseID)
	}

	// Every attempt the two authoring stages made is on the item, refused ones
	// included — which is the number a real model moves and a fake one does not.
	stages, err := item.Stages(ctx, d.pool, res.itemID)
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

	// The walk reaches the statement from the deploy, over a clean chain.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, res.deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), realModelStatement) {
		t.Errorf("the walk from %s does not reach the statement:\n%s", res.deployID, walked.String())
	}
	if !strings.Contains(walked.String(), "the chain is clean") {
		t.Errorf("the walk does not report the chain clean:\n%s", walked.String())
	}
}
