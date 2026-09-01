#!/usr/bin/env python3
"""Remove one triaged entry from review-findings.md.

Usage: python3 tools/drop-finding.py "<start of the ### heading>"

The argument must match the start of exactly one `###` heading. The entry — the
heading and everything up to the next `##`/`###` heading or the end of the file —
is removed; its `##` discipline heading goes with it when no entry remains under
it; the file is deleted when no `###` entry remains at all. Matching zero or two
headings fails without writing, so a mistyped prefix cannot remove the wrong entry.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

PATH = Path(__file__).resolve().parent.parent / "review-findings.md"


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    prefix = sys.argv[1].strip()
    if not prefix.startswith("### "):
        prefix = "### " + prefix
    lines = PATH.read_text(encoding="utf-8").split("\n")

    heads = [i for i, l in enumerate(lines) if l.startswith("### ") and l.startswith(prefix)]
    if len(heads) != 1:
        print(f"error: {len(heads)} headings start with {prefix!r}; need exactly 1", file=sys.stderr)
        return 1
    start = heads[0]
    end = next(
        (i for i in range(start + 1, len(lines)) if re.match(r"^##+ ", lines[i])),
        len(lines),
    )
    del lines[start:end]

    # A `##` heading with nothing but blank lines before the next heading or the end.
    i = 0
    while i < len(lines):
        if lines[i].startswith("## "):
            j = i + 1
            while j < len(lines) and not lines[j].strip():
                j += 1
            if j == len(lines) or lines[j].startswith("## "):
                del lines[i:j]
                continue
        i += 1

    text = re.sub(r"\n{3,}", "\n\n", "\n".join(lines)).rstrip("\n") + "\n"
    remaining = sum(1 for l in text.split("\n") if l.startswith("### "))
    if remaining == 0:
        PATH.unlink()
        print("dropped; no entry remains, file deleted")
    else:
        PATH.write_text(text, encoding="utf-8")
        print(f"dropped; {remaining} entries remain")
    return 0


if __name__ == "__main__":
    sys.exit(main())
