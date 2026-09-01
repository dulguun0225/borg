"""Measure prose form across end-goal/: sentence, paragraph and em-dash counts."""
import glob
import os
import re
import statistics

files = [p for p in sorted(glob.glob('end-goal/**/*.md', recursive=True))
         if os.path.basename(p) != 'CLAUDE.md']


def prose_lines(path):
    out, fence = [], False
    for line in open(path, encoding='utf-8'):
        s = line.rstrip('\n')
        if s.startswith('```'):
            fence = not fence
            continue
        if fence or s.lstrip().startswith('|') or s.startswith('#'):
            continue
        out.append(s)
    return out


total_words = 0
sents, paras, em = [], [], 0
per_file = {}
for p in files:
    blocks, cur = [], []
    for s in prose_lines(p):
        if not s.strip():
            if cur:
                blocks.append(' '.join(cur))
                cur = []
        else:
            cur.append(s.strip())
    if cur:
        blocks.append(' '.join(cur))
    fw = 0
    for b in blocks:
        w = len(b.split())
        total_words += w
        fw += w
        paras.append((w, p))
        em += b.count('—')
        for s in re.split(r'(?<=[.?!])\s+(?=[A-Z“"`*\[(])', b):
            if s.split():
                sents.append((len(s.split()), p, s))
    per_file[p] = fw

sents.sort(reverse=True)
paras.sort(reverse=True)
n = len(sents)
print('files', len(files), 'words', total_words, 'sentences', n, 'paragraphs', len(paras))
print('sentence median', statistics.median(x[0] for x in sents),
      'mean', round(statistics.mean(x[0] for x in sents), 1))
for k in (40, 45, 50, 55, 60, 80, 100):
    c = sum(1 for x in sents if x[0] > k)
    print(f'  sentences >{k}: {c} ({100 * c / n:.1f}%)')
print('longest sentence', sents[0][0], sents[0][1])
for k in (150, 200, 250, 300, 400):
    print(f'  paragraphs >{k}:', sum(1 for x in paras if x[0] > k))
print('longest paragraphs', [(w, os.path.basename(p)) for w, p in paras[:6]])
print('em dashes', em, '= 1 per', round(total_words / em, 1), 'words')
big = sorted(per_file.items(), key=lambda kv: -kv[1])[:12]
print('largest files:')
for p, w in big:
    print(f'  {w:5d}  {p}')
for k in (800, 1200, 1500, 2000, 2500):
    print(f'  files >{k} words:', sum(1 for w in per_file.values() if w > k))
