# What stands for a rollback

A production deploy record also names, per [target](../../05-environments/01-records-and-one-long-lived-branch.md), how many instances of [the release a
rollback of this one would return to](../../08-operations/03-overlapping-windows.md) the
[deployer](../../08-operations/01-the-health-monitor.md) is keeping there: the capacity that release had, times [the fraction its owner
authored](../../08-operations/03-overlapping-windows.md). The deployer writes it when the
deploy starts, which is when the window opens over it, and it is the only fact about the
rollback path that exists before the rollback does. Without it those kept instances are an
assertion. [_Drift detection_](../../08-operations/08-drift-detection.md) reads what runs
against what the record names, and a count no record holds is one nothing can be read
against. What it costs is a field the deployer computes from a capacity it has to ask the
[platform](../../03-gates/02-the-rollout-strategy.md) for, and a record that is wrong the
moment the platform reclaims an instance,
which is the case the detector's own pass is what catches.

**Instance-hours** is the same fact carried into a duration. Three sets of instances run for spans this record already dates — the release's own, the [control](../../08-operations/01-the-health-monitor.md)'s, both named on it, and the kept instances above. The dates are when the deploy started and the window opened over it, and when a kept or a control fleet is torn down. Instance-hours per release sums that count across that span for the three together, a recorded fact of fields and timestamps the record already carries and not a value computed apart from them. Where the [service record](../../02-intent-into-items/03-decomposition/README.md) carries an instance-hour rate, an owner-authored figure in currency per instance-hour, in force at a given write, the deployer writes the converted amount for that span beside the dates, fixed at the write and never repriced by a rate corrected later. [_Factory_](../../11-screens/01-work-ops-factory-people.md) reports it beside [environment-hours](../../05-environments/02-an-environment-per-candidate/README.md), per service and per item.

At deploy the deployer also records on the deploy record a digest over the resolved value set of the service's configuration, beside the build's digest, computed through the resolver that seam 3 of [_Deferred, but not designed out_](../../../deferred.md) names. The configuration version so named is restored with the release at a rollback. What it costs is one field on the record and a resolver that can hash what it returns.
