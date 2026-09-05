// The four seams enforcement is composed with, as fakes: what a candidate's
// build publishes and declares, what its run wrote, what its environment's own
// store holds, and which backfills a deploy record marks complete. Each stands
// where the deployer would — every one of them reaches a repository, a process
// or a store, and a package test has none of the three.
package contractcheck_test

import (
	"context"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
)

// fakeCheckout is what a candidate's build publishes and declares, by item. It
// stands where the deployer would: the derivation is one toolchain's and what
// enforcement needs is the answer, so a test hands it one rather than writing Go
// source to a directory.
type fakeCheckout struct {
	publishes map[string][]contract.Form
	declares  map[string][]consumercontract.Draft
	// noSchemaChange is the items whose build declares no schema change. The
	// unremarkable answer is that a build whose store form moved ships the change
	// that moves it, so a test that does not care about the reading configures
	// nothing.
	noSchemaChange map[string]bool
}

func (f *fakeCheckout) Publishes(_ context.Context, c contractcheck.Candidate) ([]contract.Form, error) {
	return f.publishes[c.ItemID], nil
}

func (f *fakeCheckout) Declares(_ context.Context, c contractcheck.Candidate, _ []string) (consumercontract.Derived, error) {
	return consumercontract.Derived{Extractor: consumercontract.GoExtractor("test"), Drafts: f.declares[c.ItemID]}, nil
}

func (f *fakeCheckout) DeclaresSchemaChange(_ context.Context, c contractcheck.Candidate) (bool, error) {
	return !f.noSchemaChange[c.ItemID], nil
}

// fakeExchanges is the documents one build wrote, by build. No entry is no document
// observed, which enforcement treats as a failure wherever there is a consumer
// contract to decide.
type fakeExchanges struct {
	observed map[string][]consumercontract.Document
}

func (f *fakeExchanges) Observed(_ context.Context, c contractcheck.Candidate) ([]consumercontract.Document, error) {
	return f.observed[c.BuildID], nil
}

// fakeStoreState is the candidate environment's own store, keyed by item: what
// its run left behind for one store contract, and what the two exercises the
// store rule asks of the environment found. A candidate with no entry gets the
// unremarkable answer — a second application that changed nothing, a snapshot
// taken and verified — so a test that does not care about the store rule does
// not have to configure it.
type fakeStoreState struct {
	rows         map[string][]consumercontract.Document
	appliedTwice map[string]contractcheck.SecondApplication
	snapshot     map[string]contractcheck.Snapshot
}

func newFakeStoreState() *fakeStoreState {
	return &fakeStoreState{
		rows:         map[string][]consumercontract.Document{},
		appliedTwice: map[string]contractcheck.SecondApplication{},
		snapshot:     map[string]contractcheck.Snapshot{},
	}
}

func (f *fakeStoreState) Rows(_ context.Context, c contractcheck.Candidate, storeName string) ([]consumercontract.Document, error) {
	return f.rows[c.ItemID+"/"+storeName], nil
}

func (f *fakeStoreState) AppliedTwice(_ context.Context, c contractcheck.Candidate) (contractcheck.SecondApplication, error) {
	if sa, found := f.appliedTwice[c.ItemID]; found {
		return sa, nil
	}
	return contractcheck.SecondApplication{Ran: true}, nil
}

func (f *fakeStoreState) Snapshot(_ context.Context, c contractcheck.Candidate) (contractcheck.Snapshot, error) {
	if s, found := f.snapshot[c.ItemID]; found {
		return s, nil
	}
	return contractcheck.Snapshot{Taken: true, Verified: true}, nil
}

// fakeBackfills is which backfills a deploy record marks complete, keyed by
// service, contract and element. An element with no entry is not complete: the
// unremarkable answer is that no migration is in progress, which is what every
// element this fake is never asked about is.
type fakeBackfills struct {
	complete map[string]string
}

func newFakeBackfills() *fakeBackfills {
	return &fakeBackfills{complete: map[string]string{}}
}

func (f *fakeBackfills) mark(serviceID, contractName, element, deployID string) {
	f.complete[serviceID+"/"+contractName+"/"+element] = deployID
}

func (f *fakeBackfills) Complete(_ context.Context, serviceID, contractName, element string) (string, bool, error) {
	deployID, found := f.complete[serviceID+"/"+contractName+"/"+element]
	return deployID, found, nil
}
