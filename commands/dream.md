---
description: Heb dream — explore the memory graph through structured and random dreaming
---

# /dream

Explores the memory graph by turning the sense organ inward. Instead of
parsing a developer prompt, dreams take memories as input and use them to
drive retrieval against the rest of the graph. The graph explores itself.

Two modes run in sequence:
1. **Structured dreaming** — associative chains from seed memories
2. **Random dreaming** — distant pair exploration for non-obvious connections

Dreams write weak memories with speculative predicates and tentative edges.
They never write strong memories directly. The Hebbian process determines
what survives through future session reinforcement.

## Arguments

Optional flags (passed as $ARGUMENTS):
- `--seeds N` — number of seed memories for structured dreaming (default 3)
- `--hops N` — chain depth per seed (default 2)
- `--random-pairs N` — number of random pairs to explore (default 5)
- `--temperature T` — LLM creativity dial (default 1.2)

Parse these from $ARGUMENTS if provided, otherwise use defaults.

## Hard rules

- DO NOT write memories directly to `.heb/memory.db` — use `heb consolidate --raw`
- DO NOT ask the developer questions during the dream — dreams are autonomous
- DO NOT exceed confidence caps: 0.45 for structured, 0.35 for random
- Every memory written MUST have a speculative predicate (may_follow, may_apply, may_connect, may_share, etc.)
- Every bash call must be a single simple command

## Step 1: Seed selection

```bash
heb dream seeds --limit <seeds>
```

Parse the JSON output. These are the starting points for structured dreaming.
If no seeds returned (empty graph), output "Nothing to dream about yet." and stop.

## Step 2: Structured dreaming

For each seed, up to `--hops` deep:

### 2a. Tokenize the seed

Split the seed tuple on `·` (U+00B7). Extract the meaningful components
as dream tokens. Example: `drone_cost·expressed_as·threshold_delta` yields
tokens `["drone_cost", "expressed_as", "threshold_delta"]`.

### 2b. Recall against dream tokens

```bash
heb recall --no-external --format json <<'JSON'
{"tokens": ["token1", "token2", "token3"]}
JSON
```

### 2c. Filter results

From the recall results, identify memories that:
- Surfaced via `"source": "match"` (not already edged)
- Are NOT the seed itself
- Have NOT been previously dreamt with this seed (check `heb dream pairs --exists`)

If no unconnected memories surfaced, this chain ends. Move to next seed.

### 2d. LLM simulation — structured pass

For each candidate (up to 3 per hop), reason at elevated temperature about
whether the seed and candidate share a non-obvious connection.

Use this framing:

```
DREAM SIMULATION (structured)

SEED MEMORY: {seed_tuple}
SURFACED MEMORY: {candidate_tuple}

These memories share tokens but have no direct edge in the graph.
If both are true simultaneously, what follows? Does the combination
imply something the graph doesn't explicitly encode?

Be generative. Follow associations even when they seem distant.
The plausibility filter will catch what is truly incoherent.
Prefer surfacing a weak connection over discarding a possible insight.

Reply with EXACTLY one of:
DISCARD — no plausible connection worth encoding
CONNECT subject·predicate·object — a speculative memory in tuple format
  confidence: 0.0-1.0 (will be capped at 0.45)
  reasoning: one sentence explaining the connection
```

If the LLM produces DISCARD, skip silently.
If CONNECT, cap confidence at 0.45 and write via `heb consolidate --raw`.
Build a Payload with the speculative memory (event: "dream_edge", delta_new: 0.10,
delta_reinforce: 0.05) and an edge between seed and candidate (delta: 0.02).
The Payload uses the same format as `/consolidate` — see consolidate types.go.

```bash
heb consolidate --raw --format json <<'JSON'
{
  "project": "<project>",
  "memories": [{
    "subject": "...", "predicate": "may_...", "object": "...",
    "event": "dream_edge", "delta_new": 0.10, "delta_reinforce": 0.05,
    "reason": "dream chain: seed_id -> candidate_id"
  }],
  "edges": [{
    "a": {"subject": "...", "predicate": "...", "object": "..."},
    "b": {"subject": "...", "predicate": "...", "object": "..."},
    "delta": 0.02
  }]
}
JSON
```

### 2e. Chain forward

Use the highest-scoring unconnected recall result as the next seed.
Repeat from 2a for the configured number of hops.

## Step 3: Random dreaming

```bash
heb dream random-pairs --limit <random-pairs>
```

For each pair, run the LLM simulation:

```
DREAM SIMULATION (random)

MEMORY A: {a_tuple}
MEMORY B: {b_tuple}

These memories share NO tokens and have NO edges between them.
Most random pairs have no plausible connection — that is expected.
Discard silently if no connection exists.

But when a connection survives the plausibility check, surface it
even if it seems surprising. That surprise is the point.

Reply with EXACTLY one of:
DISCARD — no plausible connection (expected for most pairs)
CONNECT subject·predicate·object — a speculative memory
  confidence: 0.0-1.0 (will be capped at 0.35)
  reasoning: one sentence
```

If CONNECT, cap confidence at 0.35 and write via `heb consolidate --raw`
using the same Payload format as structured dreaming (delta_new: 0.10,
delta_reinforce: 0.05, event: "dream_edge", edge delta: 0.02).

## Step 4: Summary

After all dreaming completes, output:

```
DREAM COMPLETE
──────────────────────────────
seeds explored:     N
hops taken:         N
random pairs tried: N
memories formed:    N (N structured, N random)
edges written:      N
pairs discarded:    N
──────────────────────────────
```

Then run `heb status` to show the updated dream health.

## Predicate conventions

Dream memories MUST use speculative predicates:
- `may_follow` — X may follow the same pattern as Y
- `may_apply` — rule X may apply to context Y
- `may_connect` — X and Y may be related
- `may_share` — X and Y may share a property
- `may_conflict` — X and Y may be in tension
- `may_depend_on` — X may depend on Y

Never use definitive predicates (is, follows, applies) — those are for
confirmed memories only.

## Done when

- All seeds explored with structured dreaming
- All random pairs evaluated
- Results written via `heb consolidate --raw`
- Summary printed
- `heb status` shows updated dream counts
