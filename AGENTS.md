# Repository instructions

Read [CLAUDE.md](CLAUDE.md) in full before working. It is the shared source of
repository rules for Claude Code and Codex; this file is Codex's entry point.
Follow its **Working across assistants** section when using the role definitions
in `.claude/agents/` with Codex. Its **Delegation and usage** section minimizes
total usage while preserving quality: work locally when sufficient, delegate
when useful, and retain required checks. The Codex coordinator uses `gpt-6-astra`;
subagents may use any available model. Choose their model and effort by task
difficulty, risk, reliability, and expected total usage as described there.

Before working under `end-goal/`, also read
[end-goal/CLAUDE.md](end-goal/CLAUDE.md). Its rules apply throughout that directory.
Before changing code, read [factory/README.md](factory/README.md) and the affected
package's `doc.go` or screen's `README.md`, as the shared rules require.

On a new or resumed task, read [HANDOFF.md](HANDOFF.md) if present, then check
`git status --short`, the current diff, and recent commits. Update the handoff
before yielding unfinished work. Keep shared rules in `CLAUDE.md` so the two
assistants do not acquire separate copies that can disagree.
