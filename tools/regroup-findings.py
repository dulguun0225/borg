#!/usr/bin/env python3
"""Regroup review-findings.md (usage: python3 tools/regroup-findings.py review-findings.md) by the end-goal file each finding names first.

Each `##` block becomes one owning file; each entry keeps its text and gains a
`**Raised by:**` line naming the discipline that wrote it. Entries that cite one
another under "Also reached separately by" land in the same block, adjacent.
Blocks are ordered structurally: the record and component inventories first,
then the document in path order.
"""
import re
import sys
from pathlib import Path

PATH = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent / "review-findings.md")
text = PATH.read_text(encoding="utf-8")

pre, *blocks = re.split(r"(?m)^(?=## )", text)
entries = []  # dicts: disc, head, title, body(lines), file
for b in blocks:
    disc = b.split("\n", 1)[0][3:].strip()
    for e in re.split(r"(?m)^(?=### )", b)[1:]:
        lines = e.rstrip("\n").split("\n")
        head = lines[0]
        where = next(l for l in lines[1:] if l.startswith("**Where:**"))
        m = re.search(r"`((?:end-goal/)?[^`]*\.md)`", where)
        assert m, head
        f = m.group(1).removeprefix("end-goal/")
        entries.append(dict(disc=disc, head=head, title=head[4:].strip(), lines=lines, file=f))

# every entry survives the regrouping


def norm(s):
    return re.sub(r"[^a-z0-9]+", " ", s.lower()).strip()


by_disc = {}
for i, e in enumerate(entries):
    by_disc.setdefault(norm(e["disc"]), []).append(i)

# union-find over cross-references
parent = list(range(len(entries)))


def find(x):
    while parent[x] != x:
        parent[x] = parent[parent[x]]
        x = parent[x]
    return x


def union(a, b):
    ra, rb = find(a), find(b)
    if ra != rb:
        parent[max(ra, rb)] = min(ra, rb)


unresolved = []
for i, e in enumerate(entries):
    also = [l for l in e["lines"] if l.startswith("**Also reached separately by:**")]
    if not also:
        continue
    for disc, title in re.findall(r"([A-Z][A-Za-z ]+?) — [\"“](.+?)[\"”]", also[0]):
        cands = by_disc.get(norm(disc), [])
        nt = norm(title)
        hit = [j for j in cands if norm(entries[j]["title"]) == nt]
        if not hit:
            hit = [j for j in cands if norm(entries[j]["title"]).startswith(nt[:40]) or nt.startswith(norm(entries[j]["title"])[:40])]
        if len(hit) == 1:
            union(i, hit[0])
        else:
            unresolved.append((e["head"], disc, title, len(hit)))

for u in unresolved:
    print("unresolved:", u, file=sys.stderr)


def cluster(f):
    """The section a file belongs to: the unit one loop session handles."""
    if "/" not in f:
        return f
    parts = f.split("/")
    if parts[0] == "what-the-factory-does":
        return "what-the-factory-does/"
    sec = "/".join(parts[:2]) + "/"
    if sec == "how-the-factory-works/02-intent-into-items/" and parts[2] == "01-intake":
        return sec + "01-intake/"
    if sec == "how-the-factory-works/08-operations/" and parts[2] == "07-pages.md":
        return f
    return sec


def rank(f):
    order = ["records.md", "components.md", "deferred.md", "glossary.md", "what-humans-do.md",
             "what-the-factory-does/", "how-the-factory-works/", "open.md", "README.md"]
    for k, o in enumerate(order):
        if f == o or (o.endswith("/") and f.startswith(o)):
            return (k, f)
    return (len(order), f)


# a joined group lands in the cluster of its best-ranked file
groups = {}
for i in range(len(entries)):
    groups.setdefault(find(i), []).append(i)
clusters = {}
for members in groups.values():
    f = min((cluster(entries[i]["file"]) for i in members), key=rank)
    clusters.setdefault(f, []).append(members)

out = [pre.rstrip("\n"), "",
       "Regrouped after the run: each `##` heading below is the `end-goal/` file or section directory a finding names first, so one loop session handles every finding on a section. `**Raised by:**` names the discipline that wrote each entry; entries that reached one another separately sit adjacent under one heading.", ""]
for f in sorted(clusters, key=rank):
    out.append(f"## {f}")
    out.append("")
    for members in clusters[f]:
        for i in members:
            e = entries[i]
            out.append(e["head"])
            out.append(f"**Raised by:** {e['disc']}")
            out.extend(e["lines"][1:])
            out.append("")
PATH.write_text("\n".join(out).rstrip("\n") + "\n", encoding="utf-8")
print(f"{len(entries)} entries, {len(clusters)} blocks, {sum(len(g) > 1 for g in groups.values())} joined groups, {len(unresolved)} unresolved refs")
