package score

// LearningVersion names the published rules by which a supplied value moves. It
// moves when a rule changes, the way [FormulaVersion] moves when the formula
// does, and a version naming it is readable against the rules that were in force.
const LearningVersion = "outcomes-2"

// Rules is the published rules, in the words the score version stores and an
// owner disagreeing with a moved value reads. A learned value nobody can argue
// with is a value nobody will trust, which is the score's own rule about its
// number applied to the values it supplies.
//
// Every rule is a function of the whole graph and never a step from the value in
// force, so two passes over one store produce one table and a pass that runs twice
// moves nothing twice. TestLearningIsIdempotent is what holds that true.
const Rules = `Each value the score supplies starts where the formula was calibrated and moves as outcomes arrive.
An outcome is read off records that already exist: a human's verdict on a decision and how a rejection
resolved, an analysis window's exit and the finest size it reports the traffic reached, a rollback and
the releases it undid, an incident against a release, and the attempts a stage took. A rollback a human
marked as not caused by the release is excluded from every rule here: it measured something other than
the release, which is evidence about nothing.

Which window exits are outcomes is stated exit by exit and never as closing without failing. passed
ruled a regression out and failed ruled one in; timed out and skipped ruled nothing out and move no
value in either direction. Counting silence as success is what a held-out sample cannot correct.

Every value moves by harm on one side and by its own stated cost on the other. Gate policy's own table
says what goes wrong at each end of each of them, and both ends are the evidence: one end is something
going wrong, the other is the parameter costing more than it returns. Where a value moves one way only,
the reason is that its loose end is not observable here, and that reason is stated with it.

The risk threshold is the one value whose loose end is observable and is deliberately not read. How
often a row puts a human at it is a count, and it is what that row's loose end costs — but it is not
evidence about safety. A window too fine to resolve rules nothing out, so loosening it gives up
nothing; a human at a gate reads the change and stops some of them, so loosening the threshold on its
cost alone would be the score deciding that human review is not worth its price. That is an owner's
judgment, made by authoring the value. What raises a threshold here is the held-out sample and nothing
else, and a factory whose sample never selects has a threshold that can fall and cannot rise.

  risk threshold      per gate row. It falls to one band (0.05) below the lowest number the score
                      auto-passed on the number at that row and whose item turned out badly, floored
                      at 0.05 — so the next change scoring what that one scored is decided by a human.
                      It rises one band per 3 held-out firings at that row whose releases reached a
                      window that closed passed, ceilinged at 0.90, and nothing else raises it. A
                      gated change a human approved is not evidence the gate was unnecessary: it is
                      consistent with the change never having been risky, with the human having caught
                      what would have made it risky, and with its author having been more careful for
                      knowing a human would read it, and no record tells those apart.
                      A human's rejection is read only once it has resolved, one of four ways: the
                      re-authored version approved and differing by content digest in what the
                      rejection named, approval without differing there, a second rejection, or the
                      item reaching the attempt limit. The first, third and fourth are read as a gate
                      the factory needed and move the threshold down; the second is a false alarm,
                      moves nothing, and is published per human.

  attempt limit       per stage, both ways, once 3 items have reported at that stage. One above the
                      highest attempt at which the stage produced work that got past it, floored at 2
                      and ceilinged at 6 — so a stage that has needed a third attempt gets a fourth,
                      and a stage nothing has ever needed more than one attempt at keeps one retry and
                      not two. The floor is the design's own reasoning about a reply the protocol
                      refused, which one retry is what covers.

  item-size target    per area, in the count of the intent's requirements an item answers, which is the
                      unit decomposition sets. Halved per stall in that area — an item whose attempts at
                      a stage reached the limit above and which has no release, which is work spent and
                      thrown away — floored at 1 requirement. One way only: the other end of a bad size
                      is cost per feature and rework rate, and cost per feature needs features counted,
                      which nothing here does.

  analysis window size   per service and per quantity, and the coarser of two numbers. What the evidence
                      asks for is the starting size halved per miss on a window that timed out — an
                      incident raised against a release whose window ruled nothing out — floored at
                      0.002. What the traffic reaches is the finest size the window itself computes and
                      reports on its record, which is the arithmetic that also decides the exits. The
                      size in force is the coarser of the two, because a size finer than the traffic can
                      resolve rules nothing out: the window ends at the cap every time, protects nothing,
                      and holds the next deploy for the whole cap. Where an incident names the quantity
                      it crossed on, the miss moves that quantity; where it names none, it moves every
                      quantity, which is the reading a human's undo already gets.

  analysis window power  per service and per quantity. Each false pass — a window that closed passed over
                      a release an incident was later raised against — halves the distance from the power
                      in force to one, ceilinged at 0.99. It falls one step of 0.05, floored at 0.50,
                      where 3 windows of that service in a row timed out on traffic that reached the size
                      in force: that is volume a lower power would have closed passed within, which is the
                      power's own observable and not the size's. The two are read apart because they are
                      read off the same windows, and reading one event as both ends at once would move two
                      values against a single piece of evidence.

  window confidence   nothing. No outcome says the confidence a comparison must reach was too high. What
                      would show it is a rollback that turned out unnecessary, and the one thing that
                      establishes a failed release was fine is a human marking the rollback as not caused
                      by the release — which says the comparison was confounded, nothing about how
                      confident it should have been.

  analysis window cap    per service, both ways. Twice the longest a window of that service took to close
                      on evidence — passed or failed, the two exits that read the quantity rather than the
                      clock — floored at 60 and ceilinged at a week. A cap under what a window actually
                      needed closes unresolved one that would have resolved; a cap far above it holds
                      the next deploy for nothing.

  window limit        per service. Folded over that service's own history in order: from 1, every 3
                      windows closing passed raise it by one, ceilinged at 5, and a rollback that undid
                      more than its target lowers it by one and resets the count, floored at 1. timed out
                      and skipped raise nothing: a release nothing ruled anything out on is not a release
                      that behaved, and counting one would let a service whose instrumentation stopped
                      ratchet its own limit upward while measuring nothing at all.

  held-out sample     per factory. It does not move: how often the score auto-passes a change it would
  rate                have gated is bounded by an owner and not by the mechanism doing it, so what the
                      score supplies is the starting value and a safeguard's ceiling is what narrows it.

  review sample rate  per duty. It does not move here either, for the same reason: it is how much of what
                      the factory may do unattended an owner has a human read anyway.

  the exposure bound  per service. It does not move: the exposure factor learns from no outcome, so
                      nothing in the store speaks to where it should stop being weighed.

  the advisory        per factory. It does not move: no outcome teaches at what severity a matching
  severity            advisory should reject.

  allowed predicate   nothing. No outcome teaches which kinds of assertion a consumer contract may draw
  kinds               from, so the score supplies none and the value in force is the kinds this
                      factory can decide, what an owner authored, and what a safeguard adds.

Calibration reads two more things and publishes both on the version. Each factor's distribution over the
decisions that named it is read against its own history, the older half against the newer, and a factor
whose mean level has moved by more than 0.25 over at least 8 decisions is found drifted. The per-author
prior is exempt from that reading, its distribution moving being what it working looks like, and gets one
of its own: the prior each held-out decision was taken on, against what that release's window then closed.
A prior that no longer separates the held-out releases whose windows failed from the ones whose windows
passed, over at least 3 of each, is drifted whatever its distribution did. A drifted factor and a drifted
prior take the treatment an unavailable factor takes — resolved and never valued, a human deciding
whatever the formula returns — until a recalibration is in force at that gate. Where a truncation of the
log has removed every held-out decision on a drifted author, this reading finds nothing and the prior
restarts as an unseen author's, at the width a count of no closes supports.

A recalibration refits each factor set's weights on the held-out decisions taken on that set alone, once
10 of them have reached a window that closed on evidence. Within each term of the formula a factor's
weight is its own separation — how far its mean level on held-out releases whose windows failed sits from
its mean level on ones whose windows passed — as a share of that term's separations. A set with too few,
or a term whose factors separate nothing, keeps the weights the product shipped for it, and the counts on
its bands say so. A recalibration writes a version differing in the weights and in nothing else, and it
takes the branch a formula change takes: under new weights the same change gets a different number.

The held-out sample is how the threshold gets evidence its own decisions did not select. A firing the
score would have gated is held out at the rate in force: the item auto-passes every gate the score would
have gated from that firing onward, its close event says the sample and not the threshold passed it, the
decision carries the rate it was selected at, and its release is watched to the cap rather than stopping
where the boundary would allow. It reaches no row a safeguard reached and no vector that resolved
anything — the score is in no position to auto-pass a gate its own number never decided.

Beside the bands of the number the version publishes the share of held-out releases whose windows failed
within each band, per factor set and within each set per service and factory-wide, each with the count of
resolved held-out windows behind it. That is the one reading over the number rather than over a factor,
and what it answers is whether the number ranks anything at all.
`

// The published rules' own numbers, named where they are applied. Every one of
// them is in [Rules] and TestRulesStateEveryBound holds the two together.
const (
	// thresholdBand is how far the threshold moves in one step, and how far below
	// a number it goes to gate what that number did not gate.
	thresholdBand = 0.05
	// thresholdFloor is as low as the threshold goes. A threshold below this puts
	// a human at nearly every gate, which is the human work the factory exists to
	// remove.
	thresholdFloor = 0.05
	// thresholdCeiling is as high as it goes. Above this the score is auto-passing
	// changes it reads as nearly the riskiest there are.
	thresholdCeiling = 0.90
	// heldOutPerBand is how many held-out firings that turned out well it takes to
	// raise the threshold one band.
	heldOutPerBand = 3
	// attemptLimitCeiling is as high as the limit goes, whatever the evidence: a
	// stage retried more than this has stopped being a stage that retries.
	attemptLimitCeiling = 6
	// attemptLimitFloor is as low as it goes. One retry is the one the design's own
	// reasoning is about — a stage that failed once has usually had a reply the
	// protocol refused — so a limit of two survives whatever the evidence says.
	attemptLimitFloor = 2
	// attemptLimitEvidence is how many items must have reported at a stage before
	// the limit moves off its starting value at all.
	attemptLimitEvidence = 3
	// itemSizeFloor is as small as the target goes, in requirements. An item that
	// answers one requirement is the smallest thing that ships by itself.
	itemSizeFloor = 1
	// windowSizeFloor is as fine as the size goes. The traffic a comparison needs
	// at this size is more than any install the design describes receives.
	windowSizeFloor = 0.002
	// windowPowerCeiling is as reliably as a regression of the size in force is
	// asked to be caught.
	windowPowerCeiling = 0.99
	// windowPowerFloor is as low as the power goes. Below it passed would be an
	// exit reached as often by a regression as by its absence.
	windowPowerFloor = 0.50
	// windowPowerStep is how far the power falls in one step.
	windowPowerStep = 0.05
	// windowsPerPowerFall is how many windows in a row must time out on traffic
	// that reached the size in force before the power falls a step.
	windowsPerPowerFall = 3
	// windowCapCeiling is as long as a window is held open, in seconds: a week,
	// after which the next deploy has waited longer than any release should.
	windowCapCeiling = 7 * 24 * 60 * 60
	// windowCapFloor is as short as it goes. A cap under a minute would end a
	// window before traffic had arrived to read, which is not a cap but a refusal
	// to watch.
	windowCapFloor = 60
	// windowLimitCeiling is as many windows as one service holds open, whatever the
	// evidence. Every increment is one more release a rollback undoes.
	windowLimitCeiling = 5
	// windowsPerRaise is how many windows closing passed it takes to raise the
	// window limit by one.
	windowsPerRaise = 3
)
