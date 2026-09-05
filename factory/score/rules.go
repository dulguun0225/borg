package score

// LearningVersion names the published rules by which a supplied value moves. It
// moves when a rule changes, the way [FormulaVersion] moves when the formula
// does, and a version naming it is readable against the rules that were in force.
const LearningVersion = "outcomes-1"

// Rules is the published rules, in the words the score version stores and an
// owner disagreeing with a moved value reads. A learned value nobody can argue
// with is a value nobody will trust, which is the score's own rule about its
// number applied to the seven numbers it supplies.
//
// Every rule is a function of the whole graph and never a step from the value in
// force, so two passes over one store produce one table and a pass that runs twice
// moves nothing twice. TestLearningIsIdempotent is what holds that true.
const Rules = `Each value the score supplies starts where the formula was calibrated and moves as outcomes arrive.
An outcome is read off records that already exist: a human's verdict on a decision, a analysis window's
exit and the read it closed on, a rollback and the releases it swept, an incident against a release,
and the attempts a stage took.

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
                      It rises one band per 3 held-out firings at that row whose items turned out well,
                      ceilinged at 0.90, and nothing else raises it: a gated change a human approved is
                      not evidence the gate was unnecessary, because the human's own scrutiny is part
                      of why it turned out well. A gated change that turned out well is consistent with
                      three things at once — that it was never risky, that the human caught what would
                      have made it risky, and that its author was more careful for knowing a human
                      would read it — and no record tells those apart. The sample is the only thing
                      that produces one that does, which is why it is the only evidence for a rise.
                      A fall outranks a rise: it names a number the score is known to have got wrong.

  attempt limit       per stage, both ways, once 3 items have reported at that stage. One above the
                      highest attempt at which the stage produced work that got past it, floored at 2
                      and ceilinged at 6 — so a stage that has needed a third attempt gets a fourth,
                      and a stage nothing has ever needed more than one attempt at keeps one retry and
                      not two. The floor is the design's own reasoning about a reply the protocol
                      refused, which one retry is what covers.

  item-size target    per area. Halved per stall in that area — an item whose attempts at a stage
                      reached the limit above and which has no release, which is work spent and thrown
                      away — floored at 50. One way only: the other end of a bad size is cost per
                      feature and rework rate, and cost per feature needs features counted, which
                      nothing here does. Nothing reads the value either, until a decomposition sizes something.

  analysis window size   per service, and the coarser of two numbers. What harm asks for is the starting
                      size halved per miss — a window closed passed or timed out over a release an
                      incident was later raised against, which is the crossing the health monitor could
                      have seen and did not — floored at 0.002. What the traffic reaches is the finest
                      size on that same lattice whose units to clean this service's newest closed
                      window's read supplies inside the cap in force, and never coarser than the
                      starting size. The size in force is the coarser of the two, because a size finer
                      than the traffic can resolve rules nothing out: the window ends at the cap every
                      time, protects nothing, and holds the next deploy for the whole cap. The two are
                      different questions — what is worth catching, and whether anything can be caught
                      — and only the first is about harm.

  window confidence   per service. Each false pass — a miss whose window closed passed rather
                      than timing out — halves the distance from the confidence in force to
                      one, ceilinged at 0.999. One way only, and the reason is arithmetic: the units a
                      window needs grow as the log of one over one-minus-confidence, where they grow as
                      the inverse square of the size, so tightening this costs about a doubling where
                      two halvings of the size cost sixteen times. It does not compound.

  analysis window cap    per service, both ways. Twice the longest a window of that service took to close
                      on evidence — passed or failed, the two exits that read the quantity rather than the
                      clock — floored at 60 and ceilinged at a week. A cap under what a window actually
                      needed closes unresolved one that would have resolved; a cap far above it holds
                      the next deploy for nothing.

  window limit        per service. Folded over that service's own history in order: from 1, every 3
                      windows closing without failing a release raise it by one, ceilinged at 5, and a rollback that
                      swept a release lowers it by one and resets the count, floored at 1. Windows
                      closing without failing a release are one-sided evidence — a service that has never rolled
                      back has seen the throughput a higher limit gives and none of the rollback size
                      it causes — which is why the rise is slow and the fall is immediate.

  allowed predicate   nothing. No outcome teaches which kinds of assertion a consumer contract may draw
  kinds               from, so the score supplies none and the value in force is the kinds this
                      factory can decide, what an owner authored, and what a safeguard adds.

The held-out sample is how the threshold gets evidence its own decisions did not select. One firing in
ten that the score would have gated is held out instead: the item auto-passes every gate the score
would have gated from that firing onward, its close event says the sample and not the threshold passed
it, and its release is watched to the cap rather than stopping where the boundary would allow. It
reaches no row a safeguard reached, because a human a safeguard added at a gate is a human an owner
added and no mechanism in the design removes one. What the sample cannot have on this substrate is the strategy that
keeps a control: every deploy here moves a process rather than traffic, so a held-out release is
watched by the same confounded comparison as every other one and the longest watch available is all
the sample gets.

Two of the seven move one way, and each says above why its loose end is not observable here. What that
costs is that those two ratchet: an install tightens them and only an owner authoring over the value
loosens anything. Neither compounds — the confidence's cost grows as a log, and nothing reads the
item-size target yet — which is what makes a ratchet on those two tolerable where a ratchet on the
window's size was not.
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
	// the limit moves off its starting value at all. One item that got past a stage
	// first time is not grounds for supplying a limit the whole factory reads.
	attemptLimitEvidence = 3
	// itemSizeFloor is as small as the target goes. Below this an item is smaller
	// than the overhead of shipping one.
	itemSizeFloor = 50
	// windowSizeFloor is as fine as the size goes. The traffic a comparison needs
	// at this size is more than any install the design describes receives, so
	// below it the window would be watching for something it can never rule out.
	windowSizeFloor = 0.002
	// windowConfidenceCeiling is as sure as the comparison is asked to be.
	windowConfidenceCeiling = 0.999
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
	// windowsPerRaise is how many windows closing without failing a release it takes to raise the
	// window limit by one.
	windowsPerRaise = 3
)
