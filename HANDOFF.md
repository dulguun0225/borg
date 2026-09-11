# Current handoff

## Request and scope

The owner asked to make the repository usable when switching between Claude and
GPT in Codex, after asking what the last Claude session was doing. The owner also
requires minimizing usage without compromising quality. The Codex coordinator
stays on Astra; subagents may use any available model. Delegate when useful and
choose model and effort by task difficulty, risk, reliability, and expected total
usage while preserving required checks. This work changes repository
instructions only. Resuming the earlier implementation pass has not yet been
requested.

## Starting point

- Base commit: `535c98c001fff2e50097c0beaedda22d4c532503`
  (`weekly limit mid session`). The working tree was clean before this task.
- `6febd96` describes M9's external report workflow as finished, with remaining
  claims stated or assigned to M12.
- `0fe86f2` (`mid progress`) and `535c98c` contain broad code, test, and claim
  corrections. Reading their diffs suggests a pass aligning implementation with
  design claims. This is an inference from git, not a recovered Claude transcript.
- No exact pending-review list or last successful full test run was recovered.
  Treat these commits as unfinished checkpoints, not evidence of passing checks.

## Current changes

- `AGENTS.md` directs Codex to the shared rules and the design directory's rules.
- `CLAUDE.md` explains native delegation in both assistants and the handoff protocol.
  The Codex coordinator uses `gpt-6-astra`; subagent model and effort follow the task.
  Small tasks stay local; focused delegation avoids duplicated work and retains
  required independent reviews.
- The instruction setup is complete. This file retains the known state of the
  interrupted implementation for the next assistant; git records commit status.

## Verification

- `git diff --check`: passed.
- `bash tools/consistency-commands.sh`: passed, with zero prose violations in
  this edit. It also reported existing prose-length violations elsewhere.
- `go run ./cmd/tracecheck` from `factory/`: passed.
- The usage-policy revision was performed locally because it is a small edit
  whose context was already available. A fresh top-level session has not been
  launched to test instruction loading.
- No application code changed. The application test suite has not been run here.

## When implementation resumes

Read the current diff and the two checkpoint commits, then identify the next
bounded package or finding before editing. Use `factory/README.md` for setup and
checks, and `end-goal/claims.txt` with the package's `doc.go` for its design claims.
Recover any available review findings before assuming which package comes next.
Establish the test baseline; the previous session's result is unknown.
