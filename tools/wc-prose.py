#!/usr/bin/env python3
"""Word count per prose block of a Markdown file, in the shape the prose-form
check of end-goal/CLAUDE.md counts, plus the same count over the file at HEAD."""
import re, subprocess, sys

ITEM = re.compile(r'([-*+] |\d+\. )')


def blocks(text):
    out, cur, start, last, fence = [], [], 0, 0, False
    for i, s in enumerate(text.split('\n'), 1):
        if s.startswith('```'):
            fence = not fence
            continue
        if fence or s.startswith('#') or s.lstrip().startswith('|'):
            continue
        s = re.sub(r'^\s*> ?', '', s)
        if not s.strip() or ITEM.match(s.lstrip()):
            if cur:
                out.append((start, last, ' '.join(cur)))
                cur = []
        if s.strip():
            if not cur:
                start = i
            cur.append(s.strip())
            last = i
    if cur:
        out.append((start, last, ' '.join(cur)))
    return out


for p in sys.argv[1:]:
    was = subprocess.run(['git', 'show', 'HEAD:' + p], capture_output=True, text=True)
    head = sum(len(b[2].split()) for b in blocks(was.stdout)) if was.returncode == 0 else 0
    now = blocks(open(p, encoding='utf-8').read())
    print('%s: HEAD %d, now %d' % (p, head, sum(len(b[2].split()) for b in now)))
    for s, e, b in now:
        print('  line %-5d %d words' % (s, len(b.split())))
