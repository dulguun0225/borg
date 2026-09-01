#!/usr/bin/env bash
# Runs the verification commands of the consistency pass: the bash fence in
# end-goal/CLAUDE.md, executed from the repository root. The fence is read at
# run time, so this script cannot drift from it. The cold-read check and the
# read-through are judgments, not commands, and stay the session's job.
set -u
cd "$(dirname "$0")/.."
awk '/^```bash$/{f=1;next} /^```$/{f=0} f' end-goal/CLAUDE.md | bash
