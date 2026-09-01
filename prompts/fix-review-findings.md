Run `git status` first. If the working tree has uncommitted changes, a previous
iteration was interrupted: finish that work and commit it if it is coherent,
otherwise `git restore` it. Then proceed.

The findings you handle are appended below this prompt: every `###` entry under one
`##` discipline heading of ./review-findings.md, verbatim. Do not read that file
whole — it is large and the rest of it is not yours — except as step 1 says.

1. Run `grep -n '^##' review-findings.md`. Where a heading under another discipline
   is the same subject as one of your entries, read that entry too and handle it
   with yours in the same edit: two agents reaching one finding separately is
   recorded with the finding wherever it lands. Use no other entry.

2. Sort the entries before opening any file. Decide each from the entry's text where
   the text decides it; where it does not, read the file that owns the subject and
   decide from that. Each gets one of two dispositions:

   - **Escalate it** when fixing it needs a decision only the owner can make — a
     product choice the document does not already imply, a trade-off between two
     things the document protects. Append the entry verbatim to ./requires-human.md
     under its original heading, then add one paragraph: what decision is needed
     and what turns on it. Remove it with
     `python3 tools/drop-finding.py "<its ### heading>"` and commit both files as
     one commit — `docs: escalate <summary>`. Do all escalations first; they read
     nothing but the entry. Any helper script or scratch text written to do this
     goes under /tmp, never into the repository.
   - **Fix it** otherwise. Read end-goal/CLAUDE.md once before the first edit and
     follow it. Start from the files the entry's **Where:** line names and the file
     that owns the subject, and open further files only where the edit needs them —
     a section it links to, a rule it must agree with, a term it must use as the
     document does. Read what the fix needs and nothing for orientation. Answer
     the finding with a
     mechanism — a rule, a record, a check, a bound the design states — never a
     sentence acknowledging the problem. A fix you attempted and could not verify
     becomes an escalation.

   Never refuse a finding and never drop one; refusal is the owner's. When in doubt
   between the two dispositions, escalate.

   Before committing a fix, run `bash tools/consistency-commands.sh` and fix
   anything it reports: its prose-form check reads the working tree against `HEAD`,
   so it gates only when run before the commit. Then remove the entry with
   `python3 tools/drop-finding.py "<its ### heading>"` (this also drops an emptied
   `##` heading and deletes the file after the last entry), and commit everything
   that finding changed as one commit — `docs: <imperative summary>`, body saying
   what the finding was and how it was resolved, with the Co-Authored-By trailer.
   One commit per finding, so the owner can read each on its own.

3. `git push`.

Do not run the cold-read check or the read-through: neither depends on which
session made an edit, so the loop runs both once over the whole run after the last
block, from prompts/finish-review-findings.md. Dispatch no subagent.

Minimizing the number of edits to ./end-goal is not a goal. Make ./end-goal as
correct as possible.

Handle the entries below and any you joined to them under step 1, then stop.

---
