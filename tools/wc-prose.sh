#!/usr/bin/env bash
# Word count per prose block, in the shape end-goal/CLAUDE.md's prose-form check
# counts, for a file given as an argument. A working aid, not part of the pass.
set -u
cd "$(dirname "$0")/.."
exec python3 tools/wc-prose.py "$@"
