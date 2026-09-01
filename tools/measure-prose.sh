#!/usr/bin/env bash
# Read-only: print the prose-form measurements for end-goal/.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 tools/measure-prose.py
