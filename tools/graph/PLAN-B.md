# Plan B — deterministic graphify over `end-goal/`, zero LLM tokens for markdown

Shelved 2026-08-17. Plan A is running graphify as-is over the repo; this plan activates
if graphify's LLM extraction of the markdown proves too wasteful again. It sits here by
the owner's instruction of the same date, an exception to the rule that the repository
holds no plan file — it describes the tool this directory will hold, and it is deleted
when that tool exists or the approach is refused.

## Context

The owner wants the project queryable as a knowledge graph, likes graphify's query
experience (`graphify query/path/explain`, communities, god nodes, HTML viz), but found
its markdown ingestion too wasteful: docs are routed to LLM "semantic extraction"
subagents on every change. Code, by contrast, graphify already extracts
**deterministically and free** (AST pass, no LLM, no key).

The insight: graphify's pipeline is split at a documented JSON contract.
`.graphify_ast.json` (deterministic) and `.graphify_semantic.json` (LLM) merge into
`.graphify_extract.json`, which `graphify.build.build_from_json()` turns into
`graphify-out/graph.json`; everything the owner likes runs off that file. The schema is
in `~/.claude/skills/graphify/references/extraction-spec.md` (nodes/edges/hyperedges,
node-ID rule `{path_stem}_{entity}` lowercased `[a-z0-9_]`, relations
`references|cites|conceptually_related_to|…`, confidence `EXTRACTED` score 1.0 for
explicit relations).

And the relations in `end-goal/` are fully explicit and mechanically checked (the
2026-08-17 relinking sweep plus the coverage check in `end-goal/CLAUDE.md`): glossary
lines = term nodes with defining sections, markdown links = reference edges, headings =
section structure, bare `(1)`–`(12)` = duty citations. Nothing needs inferring, so
nothing needs an LLM. We emit the extraction JSON ourselves — every edge honestly
`EXTRACTED` at 1.0 — and drive graphify's own library for build/cluster/export. Rebuild
cost: milliseconds, zero tokens, every time the docs change. When code lands in the
monorepo, the same build step runs graphify's free AST extractor over it and merges
both into one graph.

## What gets built

Two small stdlib-only python files in this directory, plus a gitignore line.

### 1. `tools/graph/extract_docs.py` — deterministic markdown extractor

Walks `end-goal/**/*.md` (skipping `CLAUDE.md`) and emits graphify extraction JSON to
`graphify-out/.graphify_docs.json`. Reuse the parsing already proven in the repo: the
glossary-line regex and term matcher from the coverage check in `end-goal/CLAUDE.md`
(verification block), and the link/anchor extraction from the existing consistency-pass
snippets.

**Nodes** (schema per extraction-spec: `id`, `label`, `file_type`, `source_file`
absolute, `source_location` = line number):

- One per heading (`#`/`##`/`###`): `file_type:"document"`, id = graphify's rule
  applied to path + anchor slug, e.g.
  `end_goal_how_humans_do_it_08_operations_the_watch_window`.
- One per glossary term: `file_type:"concept"`, id `end_goal_glossary_<term_slug>`,
  from each `- **term** — … [Name](target)` line.
- One per duty (1–12): `file_type:"concept"`, from `what-humans-do.md`'s numbered
  list.

**Edges** (all `confidence:"EXTRACTED"`, `confidence_score:1.0`, `weight:1.0`,
`source_file`/`source_location` set):

- `references`: section → target section, one per markdown link, resolved file+anchor
  → node (anchor-less link → the target file's `#` node). Resolution logic = the
  consistency pass's link checker, kept identical.
- `references`: glossary term → its defining section (the line's last link).
- `conceptually_related_to`: section → term, for each distinctive term occurrence
  (multi-word terms plus `K`, same matcher and same SKIP pairs as the coverage check)
  — this is what makes "what touches the watch window" a one-hop query.
- `references`: subsection → parent section (heading nesting is explicit structure).
- `cites`: section → duty node, one per bare `(n)` reference.

`hyperedges: []`, `input_tokens/output_tokens: 0` — the report's token line then
honestly reads zero.

### 2. `tools/graph/build.py` — build/cluster/export driver, no LLM anywhere

- Runs `extract_docs.py`; if the repo has code files (future), also runs graphify's
  own free AST pass (`graphify.extract.extract` over `graphify.detect.detect` code
  results) and merges, mirroring the skill's Part C merge.
- Calls the graphify library directly: `build_from_json(extraction, root=<repo>,
  directed=True)` → `cluster(G)` → `god_nodes/surprising_connections` →
  `to_json(G, communities, 'graphify-out/graph.json')` → `generate(...)` for
  `GRAPH_REPORT.md`.
- Community labels deterministically: each community named by its two highest-degree
  node labels (no LLM labeling step).
- Handles the `to_json` shrink-guard (#479): since our build is deterministic and
  full, delete the stale `graphify-out/graph.json` before writing (equivalent to
  `--force`), so legitimate shrinks (a section deleted) don't wedge the rebuild.
- Runs `graphify.diagnostics.diagnose_extraction` and prints the health line
  (dangling/missing/self-loop edges — expect none, since every edge endpoint is a node
  we ourselves emitted).
- Interpreter: resolve the same way the skill does (`graphify-out/.graphify_python`,
  falling back to `python3` + `import graphify`); `graphify` is installed at
  `~/.local/bin/graphify`.

### 3. Repo integration

- `.gitignore` (new file at repo root): `graphify-out/` — derived output is never
  source, the repo's derive-don't-maintain pattern.
- One short paragraph in the root `README.md` (hard-wrapped, per house style) naming
  `tools/graph/` and what it derives; queries then run as plain
  `graphify query "<question>"` from the repo root, and `graphify export html` gives
  the interactive view. `graphify path` / `explain` work unchanged.
- Nothing under `end-goal/` changes; the extractor reads the conventions the coverage
  check already enforces, so the consistency pass is what keeps the graph's inputs
  sound.

## Update story (the thing graphify got wrong for md)

No cache, no manifest, no incremental logic: docs re-extract in milliseconds, so every
update is a full deterministic rebuild — `python3 tools/graph/build.py`. When the
monorepo grows code, code stays free too (AST pass); the only place LLM extraction
would ever re-enter is if the owner someday wants inferred edges, and that stays
opt-out by construction.

## Verification

1. Build once; sanity-check counts against known ground truth: ~100 term nodes
   (glossary lines), ~300 section nodes, `references` edge count ≥ the markdown link
   count in `end-goal/`, zero health warnings, `input_tokens: 0` in the report.
2. Idempotence: run the build twice, `diff graphify-out/graph.json` runs clean.
3. Three live queries, answers eyeballed against `end-goal/`:
   - `graphify query "what depends on the watch window"`
   - `graphify path "the cut" "rollback"`
   - `graphify explain "restore floor"`
4. Regression probe: temporarily remove one link in one file, rebuild, confirm the
   corresponding edge disappears (the graph tracks source exactly); restore.
5. `graphify export html`, open, confirm communities look like the real neighborhoods
   in `end-goal/` (operations, contracts, intake…).
