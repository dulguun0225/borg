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

What the field does not reach is the [rollback](../../06-releases/06-rollback.md), which
restores code and no effect, and the [window](../../08-operations/02-the-analysis-window.md),
which watches how often the software fails and how long it takes to answer and watches
nothing else. Severity says what the window is not protecting, and says it where a human
authors it rather than leaving it to be read off a number. What it costs: an owner who
authors `irreversible` buys a human at Implementation on every item in that area, and the
value goes stale the way a [_People_](../../11-screens/01-work-ops-factory-people.md)
declaration does, nothing the factory observes saying that an area has started moving
money.
