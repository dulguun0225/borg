# Bootstrap

Stage 0 — what runs before the factory can run itself, and the record of getting there. [_End goal_](../end-goal/README.md) is the state the repository is built toward; nothing here is part of that design, and nothing here has authority over it.

## Why it is kept

The name is the compiler's and so is the shape: a bootstrap compiler is built cheap and incomplete, correct only on the subset needed to compile the real one. The same applies here — what is built first has to take one item through one gate to one deploy, and it does not have to be good.

What does not transfer is that a compiler's output is inert. A stage-2 compiler that miscompiles itself is a file to delete with stage 1 still on disk; the factory's output is what does the running, so a factory that deploys a broken version of itself may lose the ability to roll itself back — the rollback is executed by what just broke. So stage 0 is never discarded. It is the permanent recovery path, the way a distribution keeps its bootstrap chain long after it needs it.

## The plan

`end-goal/` is the intent. It goes through the factory's own stages, performed by hand: the interview, the cut, then a spec, an implementation plan, tasks, and an implementation per item. The chain repeats rather than running once — every item takes it again — which is why each pass has to stay cheap to repeat rather than ceremonious.

It ends where the factory can adopt a codebase it did not build, because by then the hand-built factory is exactly such a codebase. Self-hosting needs nothing designed for it: adoption pointed at the factory itself is the whole of it. What that milestone requires is the five things [_Adopting an existing codebase_](../end-goal/deferred.md#adopting-an-existing-codebase) says a first run does not have — a deploy record for what is already running, declared meaning on the interfaces, a design system a machine can read, a build to start a control from, and history behind the learned parameters. All five apply to the factory adopting itself, so that list is the requirement set for this phase ending.

The handoff is gradual and not a switch. Adoption creates the records; it does not make an agent good enough to write factory code. What covers the gap is already designed — the backstop duties (11, 12) are a human authoring a stage instead of an agent, only where it falls short and shrinking as it improves — so the milestone is the factory running its own pipeline with humans backstopping the stages it cannot do yet, and the backstop shrinking from there.

## What this holds, and what it does not

**A decision goes to the file that owns the subject.** Anything settled about what the factory is folds into `end-goal/` immediately, with its reason and its cost. Anything genuinely unsettled about it is a question in [_Open_](../end-goal/open.md), phrased as the question and what turns on it.

Neither of those is here. This file holds the plan, the constraints the cut has to apply, and where the work has got to — process rather than design. A list of answers kept outside the sections that own them is what retired `next.md` on 2026-08-14, and this directory is close enough to that shape to say so out loud. What the split costs is two places to look while an interview is running.

## Constraints on the cut

**The reconciler can never be an item.** It compares what is actually running against what the factory recorded, and a reconciler the factory deployed would be inside the trust domain it exists to check — which a factory building itself makes worse rather than better, since what is being shipped now is the checker. It stays hand-built and hand-deployed permanently. [_The reconciler_](../end-goal/how-humans-do-it/08-operations.md#the-reconciler) sets out what it is and why it is outside.

## The interview, in ten sittings

What the interview has to reach is enough to cut, which is the document's own end condition — it asks until there is enough to cut the intent and author a spec per item, not until the design is exhaustive. For this intent that means what the factory is made of: the cut produces one item per service the work changes, and the document describes services as a property of a customer's software rather than of the factory itself.

So a sitting settles records rather than sections. Records are the nodes of the one graph the product is built around, and the cut's whole input follows from them — who writes a record is a component boundary, who reads it is a dependency edge. Dividing by section would re-cover the interlocking claims that are the tree's value, each sitting reopening what an earlier one settled.

Per record: who writes it and at which event, who reads it, and the fields that decide who owns it. Not an exhaustive field list, and not the score's formula, the window's test, storage, wire formats, or how a declaration is derived per toolchain — none of those move a boundary, so they are the spec stage's.

| # | Sitting | Records |
|---|---|---|
| 1 | The traceability records | intent · item · candidate · release record · deploy record · the number, a field of the release · current release, a fact of the production deploy record |
| 2 | Before the cut | constraint, standing and per-item · design system · interview question · the cut decision · declared item order · partial intent · area · project |
| 3 | Artifacts of an item | spec · implementation plan · tasks · implementation · acceptance criterion · criterion encoding · a screen's state flow |
| 4 | Decisions and policy | gate decision · gate policy's seven parameters · pin · decision-log record · actor · policy version |
| 5 | The score | the factor vector · authorship prior · score version · the held-out sample · the trust number |
| 6 | Where software runs | service · environment record · master branch · candidate branch · candidate environment · merge queue · build and commit |
| 7 | Contracts | contract · contract version · consumer declaration · predicate catalog · deprecation list · the producer's own observables · store schema |
| 8 | The watch | control · health signal · watch window and its four exits · K · recovery target · explicit health threshold |
| 9 | Undo and trouble | rollback · revert item · incident · page event · the reconciler's mismatch and the hold it sets |
| 10 | The substrate | fleet entry · provider account · credential reference · People declaration · duty · the deploy-target seam |

Sitting 1 goes first, naming what every other one references, and sitting 10 last, nothing reading the substrate. The eight between are taken in whatever order a sitting allows. What makes that safe is the tree's own habit — six forward references are deliberate, each with a link forward at its early use — so a sitting may name a record a later one defines rather than waiting for it.

The interview ends, and the cut can run, when every record above has a named writer and named readers, and no record is written by two components with no seam declared between them.

## Where we are

Sitting 1 is running. Its questions are asked and unanswered.
