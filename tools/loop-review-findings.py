#!/usr/bin/env python3
"""Run `claude -p` repeatedly until the findings file is deleted.

One `##` discipline block per invocation — its entries, verbatim, appended to
the prompt — so each iteration gets a fresh context and the session never reads
the findings file whole. Paths are resolved against the repository root, so the
script runs from anywhere.
"""

from __future__ import annotations

import argparse
import datetime
import hashlib
import re
import subprocess
import sys
import time
import zoneinfo
from pathlib import Path

# What a session-limit or rate-limit refusal looks like in claude's output.
# These clear on their own when the window resets, so the loop waits and
# retries instead of counting them as failures.
LIMIT_PATTERN = re.compile(
    r"usage limit|session limit|rate limit|limit reached|overloaded", re.I
)

# The refusal names when the window resets, e.g.
# "session limit · resets 5:40am (Asia/Ulaanbaatar)" or "resets 9am (UTC)".
RESET_PATTERN = re.compile(
    r"resets\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*\(([^)]+)\)", re.I
)

REPO_ROOT = Path(__file__).resolve().parent.parent

# The rtk PreToolUse hook may rewrite `git ...` to `rtk git ...` before the
# permission check, so every command pattern appears in both spellings.
_GIT = ["status", "diff", "log", "add", "commit", "push", "restore", "rev-parse"]
_CMDS = [
    "bash tools/consistency-commands.sh",
    "python3 tools/drop-finding.py:*",
    "grep:*",
    "graphify query:*",
    "graphify explain:*",
    "graphify path:*",
]
ALLOWED_TOOLS = ",".join(
    ["Read", "Grep", "Glob", "Edit", "Write", "Task", "Agent"]
    + [f"Bash(git {sub}:*)" for sub in _GIT]
    + [f"Bash(rtk git {sub}:*)" for sub in _GIT]
    + [f"Bash({c})" for c in _CMDS]
    + [f"Bash(rtk {c})" for c in _CMDS]
)


def digest(path: Path) -> str | None:
    try:
        return hashlib.sha256(path.read_bytes()).hexdigest()
    except FileNotFoundError:
        return None


def stamp() -> str:
    return datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")


def seconds_until_reset(output: str, now: datetime.datetime | None = None) -> int | None:
    """Seconds until the reset time the refusal names, or None if it names none.

    The time is a clock time in a named zone with no date, so it is taken as the
    next such time at or after now. A minute of slack covers the clock skew
    between the message and the window actually clearing.
    """
    m = RESET_PATTERN.search(output)
    if not m:
        return None
    hour, minute, ampm, zone = int(m.group(1)), int(m.group(2) or 0), m.group(3), m.group(4)
    if ampm:
        hour = hour % 12 + (12 if ampm.lower() == "pm" else 0)
    try:
        tz = zoneinfo.ZoneInfo(zone.strip())
    except (zoneinfo.ZoneInfoNotFoundError, ValueError):
        return None
    now = now or datetime.datetime.now(tz)
    now = now.astimezone(tz)
    reset = now.replace(hour=hour, minute=minute, second=0, microsecond=0)
    if reset <= now:
        reset += datetime.timedelta(days=1)
    return int((reset - now).total_seconds()) + 60


def first_block(path: Path) -> str | None:
    """The first `##` heading and every `###` entry under it, or None if no entry remains."""
    text = path.read_text(encoding="utf-8")
    blocks = re.split(r"(?m)^(?=## )", text)[1:]
    for b in blocks:
        if "\n### " in b:
            return b.rstrip("\n") + "\n"
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--findings", type=Path, default=Path("review-findings.md"))
    parser.add_argument(
        "--prompt", type=Path, default=Path("prompts/fix-review-findings.md")
    )
    parser.add_argument(
        "--finish-prompt",
        type=Path,
        default=Path("prompts/finish-review-findings.md"),
        help="prompt run once after the findings file is gone: the cold-read "
        "check and the read-through over the whole run",
    )
    parser.add_argument("--max-iterations", type=int, default=60)
    parser.add_argument(
        "--max-failures",
        type=int,
        default=2,
        help="consecutive failed or stalled iterations tolerated before stopping",
    )
    parser.add_argument(
        "--iteration-timeout",
        type=int,
        default=21600,
        help="seconds one claude invocation may run before it is killed",
    )
    parser.add_argument(
        "--limit-wait",
        type=int,
        default=1800,
        help="seconds to sleep after a limit refusal that names no reset time",
    )
    parser.add_argument(
        "--max-limit-waits",
        type=int,
        default=16,
        help="limit-refusal sleeps tolerated over the whole run before stopping",
    )
    parser.add_argument("--model", default="fable")
    parser.add_argument(
        "--effort",
        default="high",
        help="effort level passed to claude (low, medium, high, xhigh, max)",
    )
    parser.add_argument("--allowed-tools", default=ALLOWED_TOOLS)
    parser.add_argument(
        "--claude-bin", default="claude", help="path to the claude executable"
    )
    args = parser.parse_args()

    findings = args.findings if args.findings.is_absolute() else REPO_ROOT / args.findings
    prompt_path = args.prompt if args.prompt.is_absolute() else REPO_ROOT / args.prompt
    prompt_head = prompt_path.read_text()
    finish_path = (
        args.finish_prompt
        if args.finish_prompt.is_absolute()
        else REPO_ROOT / args.finish_prompt
    )
    finish_prompt = finish_path.read_text()

    def run_claude(prompt: str) -> tuple[str | None, str]:
        """Run one headless session; return (failure reason or None, output)."""
        try:
            result = subprocess.run(
                [
                    args.claude_bin,
                    "-p",
                    prompt,
                    "--model",
                    args.model,
                    "--effort",
                    args.effort,
                    "--allowedTools",
                    args.allowed_tools,
                ],
                cwd=REPO_ROOT,
                timeout=args.iteration_timeout,
                capture_output=True,
                text=True,
            )
            output = result.stdout + result.stderr
            print(output, flush=True)
            if result.returncode != 0:
                return f"claude exited with {result.returncode}", output
            return None, output
        except subprocess.TimeoutExpired as exc:
            output = (exc.stdout or b"").decode(errors="replace") + (
                exc.stderr or b""
            ).decode(errors="replace")
            print(output, flush=True)
            return f"claude exceeded {args.iteration_timeout}s and was killed", output

    iteration = 0
    failures = 0
    limit_waits = 0
    while findings.exists():
        block = first_block(findings)
        if block is None:
            # Only the preamble is left: the file's own rule says it goes.
            subprocess.run(["git", "rm", "-q", str(findings)], cwd=REPO_ROOT, check=True)
            subprocess.run(
                ["git", "commit", "-q", "-m", "review: findings file emptied by triage"],
                cwd=REPO_ROOT,
                check=True,
            )
            subprocess.run(["git", "push", "-q"], cwd=REPO_ROOT, check=False)
            break

        iteration += 1
        if iteration > args.max_iterations:
            print(
                f"stop: reached max-iterations={args.max_iterations} with "
                f"{findings} still present",
                file=sys.stderr,
            )
            return 1

        before = digest(findings)
        heading = block.split("\n", 1)[0]
        entries = block.count("\n### ")
        print(f"=== iteration {iteration} [{stamp()}] {heading} ({entries} entries) ===", flush=True)

        failed, output = run_claude(prompt_head + "\n" + block)

        # Stall guard: an unchanged file means the iteration made no progress.
        if failed is None and digest(findings) == before:
            failed = f"no change to {findings}"

        # A limit refusal clears on its own: wait for the window, retry, and
        # count the attempt against neither max-iterations nor max-failures.
        if failed is not None and LIMIT_PATTERN.search(output):
            limit_waits += 1
            iteration -= 1
            if limit_waits > args.max_limit_waits:
                print(
                    f"stop: hit a usage limit {limit_waits} times "
                    f"(max-limit-waits={args.max_limit_waits})",
                    file=sys.stderr,
                )
                return 1
            wait = seconds_until_reset(output)
            why = "until the reset it names" if wait is not None else "no reset time named"
            wait = args.limit_wait if wait is None else wait
            print(
                f"usage limit hit ({limit_waits}/{args.max_limit_waits}); "
                f"sleeping {wait}s ({why}) [{stamp()}]",
                flush=True,
            )
            time.sleep(wait)
            continue

        if failed is not None:
            failures += 1
            print(
                f"iteration {iteration} failed ({failures}/{args.max_failures}): {failed}",
                file=sys.stderr,
                flush=True,
            )
            if failures >= args.max_failures:
                print("stop: too many consecutive failures", file=sys.stderr)
                return 1
        else:
            failures = 0

    print(f"done: {findings} removed after {iteration} iterations [{stamp()}]")

    # The cold-read check and the read-through depend on which sections changed,
    # not on which session changed them, so they run once over the whole run.
    print(f"=== finish [{stamp()}] {finish_path.name} ===", flush=True)
    failed, _ = run_claude(finish_prompt)
    if failed is not None:
        print(f"finish failed: {failed}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
