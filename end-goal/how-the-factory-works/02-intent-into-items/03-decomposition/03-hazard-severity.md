# Hazard severity

An [area](02-what-an-item-names.md) carries its **hazard severity**: the worst harm the
software in it can do while it behaves as a release makes it behave, graded by what the
factory can do about that harm afterwards. It is one of three values. `negligible` is an
area whose software answers requests and writes its own store, and does nothing outside
itself that outlasts a traffic shift. `recoverable` is one whose software acts outside
itself where a later [item](../../01-one-pipeline.md) corrects what it did, such as an
amount posted wrong or a message published to a consumer that a compensating message
retracts. `irreversible` is one whose software does what nothing the factory ships
afterwards corrects: money paid out, a message delivered to a person, data erased, an
actuator driven, a disclosure made.

The value in force for an item is the highest named anywhere on its area chain up to the
project. So declaring a finer area never lowers it, which is the invariant the chain
already keeps for a [safeguard](../../09-gate-policy/02-one-shape-across-all-of-them.md)
and the reason no safeguard is needed on the field. It is authored outright with nothing
supplied, for the reason the [_service level
objective_](../../08-operations/05-service-level-objectives.md) is: nothing the factory
observes says what harm the software can do. Where no area in a chain names one, the value
in force is `negligible`. [_Factory_](../../11-screens/01-work-ops-factory-people.md)
reports how many declared areas name none, so an install reading every area as a marketing
page is visible rather than silent.

Three things read it, and each states its own rule where it acts. At
[_Implementation_](../../03-gates/07-what-particular-gates-decide/05-implementation/README.md) an
`irreversible` value is [resolved rather than
weighed](../../04-risk-score/01-factors-at-least.md), a human deciding whatever the formula
returns. The [_rollout strategy_](../../03-gates/02-the-rollout-strategy.md) bounds how much
of production such a release may take before the comparison has cleared any of it. And the
vector a gate firing writes names the value in force, so the impact half of the
[score](../../04-risk-score/README.md) has one input a human can argue with rather than a
term nothing supplies.

An area graded `irreversible` also names its **hazardous operation**: the call in the
software that does what nothing afterwards corrects, the payout, the send, the erasure, the
actuator call. Beside the name the owner authors a **bound**: the count of that operation
the service may perform per period, the period authored with the count. The grade is not
written without the two; the write at Factory refuses it. Both are authored outright with
nothing supplied, for the reason the grade is: nothing the factory observes says which
operation carries the harm or how much of it a period may hold. The bound is where the
magnitude of the harm enters, in the operation's own unit. A second grade for magnitude,
kept apart from recoverability the way MIL-STD-882 and ISO 26262 keep severity apart from
controllability, is refused. What such a grade would move is how much of the operation a
release may perform before it is stopped, which the bound already carries, and an ordinal
the factory could not act on would sit beside a count it can. What the refusal costs is
that two `irreversible` areas are treated alike everywhere but at the bound, so a duplicate
email and a wrong dose differ only by what the owner authored there.

Two things read the operation, and both are stated where they act. At
[_Spec_](../../03-gates/07-what-particular-gates-decide/02-spec/01-the-record.md) the bound
is a criterion the factory derives, its provenance
[hazard-derived](../../03-gates/07-what-particular-gates-decide/02-spec/01-the-record.md)
and naming the area: once the count of the operation in the current period has reached the
bound, the service refuses the operation and emits a [failure
record](../../08-operations/01-the-health-monitor.md) naming it. The count is kept in the
service's own store, so every instance on every target reads one count. That is the
protection at the point of the action, and it is what makes a release's exposure between
deploy and the window's close a number the owner set rather than whatever the rollout share
and the cap allowed. The software also emits a count of the operation per interval, which
[_The health monitor_](../../08-operations/01-the-health-monitor.md) keeps beside the three
quantities and the [window](../../08-operations/02-the-analysis-window.md) reads as a
fourth. A release performing the operation at a rate its control does not is a crossing,
and `passed` is not reached until that rate is ruled unchanged, which on an operation
performed rarely is never inside the cap. What that costs is a criterion in every
`irreversible` area's service, a series per named operation, and a window that runs to its
cap on such a service, narrowing no prior and raising no window limit, which is the
direction the grade asks for.

What the field does not reach is the [rollback](../../06-releases/06-rollback.md), which
restores code and no effect, and a harm that moves no count: a wrong amount paid at the
usual rate is inside the bound and flat against the control, and only the human at
Implementation and the rollout schedule stand between it and production. Severity says what
the window is not protecting, and says it where a human authors it rather than leaving it
to be read off a number. What it costs: an owner who authors `irreversible` buys a human at
Implementation on every item in that area, and the value goes stale the way a
[_People_](../../11-screens/01-work-ops-factory-people.md) declaration does, nothing the
factory observes saying that an area has started moving money.
