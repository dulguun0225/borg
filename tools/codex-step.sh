#!/usr/bin/env bash
# Runs one Codex worker session on a prompt file and returns at once; the
# session runs detached so a caller's own limits cannot end it mid-edit.
#
#   tools/codex-step.sh <prompt-file> <out-dir> [effort] [session-id]
#
# The prompt is read from the file, with MILESTONE and STEPNUM already
# substituted by the caller. The worker's last message lands in
# <out-dir>/<prompt-basename>.out.md and its whole log in <prompt-basename>.log;
# the log's last line is `exit <code>` once the session ends, which is what a
# caller polls for. A session id resumes that session with the same model and
# sandbox, which `codex exec resume` does not carry over on its own.
set -eu
prompt=$1; out=$2; effort=${3:-high}; session=${4:-}
model=${CODEX_WORKER_MODEL:-gpt-5.6-luna}
name=$(basename "$prompt" .md)
mkdir -p "$out"
rm -f "$out/$name.out.md"
if [ -n "$session" ]; then
  cmd="codex exec resume $session -c model='\"$model\"' -c model_reasoning_effort='\"$effort\"' -c sandbox_mode='\"danger-full-access\"' -c approval_policy='\"never\"'"
else
  cmd="codex exec -m $model -c model_reasoning_effort='\"$effort\"' -s danger-full-access -c approval_policy='\"never\"'"
fi
cd "$(dirname "$0")/.."
setsid nohup bash -c "$cmd -o '$out/$name.out.md' - < '$prompt' > '$out/$name.log' 2>&1; echo \"exit \$?\" >> '$out/$name.log'" > /dev/null 2>&1 < /dev/null &
disown
echo "$out/$name.log"
