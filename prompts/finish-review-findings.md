The unattended loop over ./review-findings.md has handled every block and the file
is gone. Run the rest of the consistency pass once over everything the run changed.
The run started at the commit that added the findings file:
`START=$(git log --diff-filter=A -1 --format=%H -- review-findings.md)`, and
`git diff --name-only $START..HEAD -- end-goal/` lists the files.

1. The cold-read check — one subagent per changed section directory, or per
   changed file outside one, dispatched exactly as end-goal/CLAUDE.md instructs,
   given nothing but the verbatim instruction with the target and the fields
   filled in. At most six subagents at a time; the next batch starts when the
   previous finishes.

2. The read-through end-goal/CLAUDE.md ends with. Dispatch it to one subagent given
   that instruction verbatim and nothing else, told to report whether the identity
   survives and, where it does not, the sentence that breaks it.

A failure either check reports is repaired in one commit per changed file, each run
through `bash tools/consistency-commands.sh` first — `docs: <imperative summary>`,
body naming the check that reported it, with the Co-Authored-By trailer. Triage the
coined and bent lists as end-goal/CLAUDE.md says: a settled term is not a failure.

Then `git push` and stop.
