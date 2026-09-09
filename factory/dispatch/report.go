// report.go is one report an agent makes: the units a call returned with the
// time the provider returned them, and the spend ceiling compared at each of
// them. It is split from run.go by subject at the 500-line bound.
package dispatch

import (
	"context"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/principal"
)

// reporting is the entry's client with the last report kept. A role method
// hands back the units it was answered with and never the reply, so the time
// the provider returned them is kept here, where the call is made.
//
// A call is the finest report the provider gives: [agent.Model] answers one
// completion at a time and streams nothing, so a report is a call and the
// ceiling compared at each report is compared after each call. That is the
// bound the design accepts — overshoot of one report's worth of units.
type reporting struct {
	inner agent.Model
	// at is when the provider returned the units of the last call, and empty
	// where the client said nothing about it.
	at string
}

// Complete makes the call and keeps what this reply said about when it was
// answered, the empty string included: each run record carries its own call's
// time, so a call that said nothing falls back rather than repeating the last
// one's. A reply that carries units with an error keeps its time too — a
// refused reply cost units and enters the same sum.
func (r *reporting) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	reply, err := r.inner.Complete(ctx, as, call)
	r.at = reply.ReturnedAt
	return reply, err
}

// returnedAt is what the run record carries as the time the provider returned
// the units. A client that said nothing about it is recorded at the call's own
// end, which is the closest time this component holds and is later than the
// answer by whatever the parse cost.
func (r *reporting) returnedAt(finishedAt string) string {
	if r.at == "" {
		return finishedAt
	}
	return r.at
}

// atEachReport is the spend ceiling compared at one report the agent made,
// which is the comparison the design puts at each report rather than at a
// stage's end alone: a run whose last call put the sum past the ceiling is
// found here, one report's worth of units over, instead of at whatever the
// next dispatch onto that credential is.
//
// It opens the credential's own ceiling row where the sum has reached it,
// through the same read every other comparison makes, and returns the cause
// the caller writes its own row from — a caller that could not proceed. A
// caller whose call succeeded proceeded: the row about the credential stands
// and the stage that just finished is not held for it.
func (d *Dispatch) atEachReport(ctx context.Context, on On, entry Entry) (Hold, error) {
	read, err := d.credentialWaits(ctx)
	if err != nil {
		return Hold{}, err
	}
	return d.credentialStops(ctx, read, on, entry)
}
