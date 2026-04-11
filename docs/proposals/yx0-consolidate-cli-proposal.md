# yx0 — `heb consolidate` CLI Absorption Proposal

**Status:** DRAFT, awaiting review
**Issue:** dreadfall-0-yx0
**Purpose:** Move all mechanical Contract 4 → memory-delta processing out of `.claude/commands/consolidate.md` and into `heb consolidate`. The slash command becomes a 20-line pipe-and-print wrapper. Hebbian constants are exposed as CLI flags with sensible defaults.

---

## 1. Current state

`consolidate.md` is 300 lines. It does seven things the LLM should not be doing:

1. Checking a significance threshold on Contract 4
2. Iterating lessons, splitting U+00B7 tuples, filtering by confidence
3. Computing `delta_new = confidence × 0.72` and `delta_reinforce = confidence × 0.08`
4. Enumerating all active tuple pairs and computing edge deltas (`0.06` / `0.03`)
5. Computing entanglement signals from surprise touches
6. Prefixing universal candidates with `[universal_candidate]`
7. Rendering the `CONSOLIDATE` display block and forwarding the payload to the CLI

The CLI already handles SQLite writes (memories, events, provenance, edges, episode). What's missing is the Contract 4 → delta translation. Moving that translation into Go collapses `consolidate.md` to a single heredoc call.

---

## 2. Target CLI surface

### 2.1 Invocation

```
heb consolidate < contract4.json           # default: Contract 4 mode
heb consolidate --raw < payload.json       # explicit-payload mode (debug/test)
```

Contract 4 JSON on stdin (the output of `/learn`). No positional args.

The CLI chooses mode by **explicit flag**, not shape detection — shape sniffing is fragile. Default is Contract 4 mode. `--raw` switches to the explicit-payload shape (memories/edges arrays pre-computed) and skips the translator entirely.

**Post-migration:** `--raw` stays permanently as a debug/test interface. It is useful for writing targeted store tests without going through the full Contract 4 translation.

### 2.2 Flags with defaults

```
--new-gain            float   0.72    delta for newly created memories = confidence × new-gain
--reinforce-gain      float   0.08    delta for existing memories      = confidence × reinforce-gain
--co-activation-boost float   0.06    edge delta when both tuples written this session
--entanglement-scale  float   0.05    signal = -(peak_intensity × entanglement-scale)
--entanglement-min    float  -0.08    most negative signal allowed (lower bound)
--entanglement-max    float  -0.02    least negative signal allowed (upper bound, closest to zero)
--min-confidence      float   0.50    drop lessons below this confidence
--format              string  both    "both" | "human" | "json"
```

All defaults match the current markdown exactly. The flags exist so future experiments can tune constants without a rebuild. Default invocation from the slash command uses none of them.

### 2.3 Input — Contract 4

The shape is defined in `.claude/commands/learn.md`. Fields the CLI consumes:

```json
{
  "session_id": "...",
  "bead_id": null,
  "project": "...",
  "intent": "act | understand",
  "tokens": [],
  "memory_loaded": { "memories_loaded": 0, "git_refs": 0, "was_cold_start": true },
  "implementation": {
    "files_touched": [],
    "files_read": [],
    "surprise_touches": []
  },
  "correction_count": 0,
  "peak_intensity": 0.0,
  "completed": true,
  "decisions": [],
  "lessons": [
    { "observation": "subject·predicate·object", "scope": "project | universal_candidate", "confidence": 0.0, "evidence": "..." }
  ]
}
```

Fields the CLI ignores (but stores verbatim in the episode blob): `raw_prompt`, `patterns_used`, `corrections[].*`, `decisions[].*` beyond existence checks.

**No `retrieved_tuples`.** Contract 4 does not carry retrieved tuples, and yx0 does not consume them. Edges strengthen only when tuples are written together in the same session — retrieval alone produces nothing the Hebbian rule can act on. See §3.3 and §7.1.

### 2.4 Output

Two blocks on stdout separated by a blank line, identical spirit to the current `CONSOLIDATE` display block.

**Block 1 — human-readable**

```
CONSOLIDATE
───────────────────────────────
session:   <session_id>
project:   <project>
threshold: met | not met — <reason>

MEMORIES (<N> total)
  added:      <N> new entries
  reinforced: <N> updated
  skipped:    <N> (below confidence or malformed tuple)

EDGES
  sent:       <N> deltas
  updated:    <N>

ENTANGLEMENT
  signals:    <N> weight reductions applied
  (— if none)

EPISODE
  written:    episodes/episode-<session_id>.json     (on first write)
  skipped:    already present                        (on subsequent re-run)

ERRORS (<N>)
  (— if none)
───────────────────────────────
```

Exactly one of `written:` / `skipped:` appears, never both. This tells the developer whether a re-run hit the idempotency guard.

**Block 2 — JSON result**

```json
{
  "session_id": "...",
  "threshold_met": true,
  "applied": [
    {
      "id": "...",
      "subject": "...",
      "predicate": "...",
      "object": "...",
      "event": "created | session_reinforced | entanglement_signal",
      "new_weight": 0.68,
      "was_new": true
    }
  ],
  "edges_updated": 5,
  "entanglement_signals": 0,
  "episode_written": true,
  "skipped": [
    { "tuple": "...", "reason": "below confidence 0.42 < 0.50" }
  ],
  "errors": []
}
```

Stderr gets a one-line summary: `consolidate: Np new, Nr reinforced, Ne edges, Nt entanglement, episode=<bool>`.

**Error shape is identical to success shape.** On any error — parse failure, SQLite rollback, malformed Contract 4 — the CLI exits non-zero and prints the same top-level JSON keys on stdout with `applied: []`, `edges_updated: 0`, `entanglement_signals: 0`, `episode_written: false`, and the `errors` array populated. Details also echo to stderr. This way any caller (the slash command, tests, future automation) can always parse stdout as the same `Result` type regardless of outcome. The slash command renders both streams.

---

## 3. Core algorithm

### 3.1 Significance threshold

Skip memory deltas if none of these are true:

```
correction_count > 0
completed == false
peak_intensity > 0.3
len(decisions) > 0
len(files_touched) > 0
len(lessons) > 0
```

If threshold not met: skip both the memories pass and the edges pass (edges are derived from written lessons — no lessons, no edges). Still write the episode. Set `threshold_met: false` and `reason: "<which condition failed>"`.

### 3.2 Memory deltas

For each lesson with `confidence >= min-confidence`:

```
parts = split(lesson.observation, "·")
if len(parts) != 3:
    add to skipped with reason "malformed tuple"
    continue

delta_new       = lesson.confidence * new-gain
delta_reinforce = lesson.confidence * reinforce-gain

reason = "lesson confidence " + fmt("%.2f", lesson.confidence)
if lesson.scope == "universal_candidate":
    reason = "[universal_candidate] " + reason

append memoryDelta{
    subject:         parts[0],
    predicate:       parts[1],
    object:          parts[2],
    event:           "session_reinforced",
    delta_new:       delta_new,
    delta_reinforce: delta_reinforce,
    reason:          reason,
}
```

The existing `ApplyMemoryEvent` in `store/memory.go` already handles new-vs-existing routing and the event kind override to `"created"` on insert.

### 3.3 Edge deltas

Hebbian rule applied honestly: edges strengthen only when both tuples were **written** in the same session — i.e. both sides of the connection produced confirmed output that the developer validated. Retrieval alone does not count; the system retrieving memories together without external confirmation is just talking to itself.

```
written = tuples from lessons passing threshold
active  = written

if len(active) < 2: skip edges

for every unordered pair (a, b) in active with a != b:
    emit edgeDelta{a: split(a), b: split(b), delta: co-activation-boost}   // 0.06
```

Store canonicalizes `(a,b)` by smaller ID wins (already done in `UpdateEdge`).

**Why no passive co-retrieval.** An earlier draft strengthened edges when tuples were retrieved together without being written. That is circular — it lets the system's own retrieval patterns shape its graph with no external signal. The cost of removing it is that understand-only sessions contribute nothing to edge formation. That is the correct behaviour: understand sessions produce no confirmed knowledge, so they should move no weights. Clusters form from act sessions, where the Hebbian rule actually applies.

### 3.4 Entanglement signals

Only if:
- `len(surprise_touches) > 0`
- `correction_count > 0`

Compute base signal:

```
signal = -(peak_intensity * entanglement-scale)   // default = peak × -0.05
// clamp into [entanglement-min, entanglement-max]
if signal < entanglement-min:  signal = entanglement-min   // -0.08
if signal > entanglement-max:  signal = entanglement-max   // -0.02
```

Both bounds are negative. `--entanglement-min` is the most negative value allowed; `--entanglement-max` is the least negative (closest to zero). Standard min/max semantics: the clamp keeps `signal` within `[min, max]`.

**Active set.** Apply entanglement signals to the **written** set only (the same tuples §3.3 emitted edges for). An earlier draft proposed `union(written, retrieved)` on the grounds that established memories are more likely to be the source of the concentration pattern — but with `retrieved_tuples` removed from Contract 4 entirely (see §3.3), there is nothing to match against. Entanglement, like edges, acts on tuples that actually fired in this session.

**Tuple matching.** For each written tuple, split it on `·` to get its three parts, then check if any part appears as a case-insensitive substring of any `surprise_touches` file path (full path string, no path segmentation):

```go
func matchesSurpriseTouch(tuple string, touch string) bool {
    parts := strings.Split(tuple, "·")
    touchLower := strings.ToLower(touch)
    for _, part := range parts {
        if strings.Contains(touchLower, strings.ToLower(part)) {
            return true
        }
    }
    return false
}
```

On match, emit an **additional** `memoryDelta` entry:

```
memoryDelta{
    subject, predicate, object from the matched tuple,
    event:           "entanglement_signal",
    delta_new:       signal,
    delta_reinforce: signal,
    reason:          "surprise touch on " + <first matching file>,
}
```

**This is appended, not merged**, with the reinforcement delta already emitted for the same tuple in §3.2. When both signals hit the same tuple, the store applies them as two sequential events in the same transaction: first `session_reinforced` (positive), then `entanglement_signal` (negative). The net weight change is `reinforce_delta + signal`, which is typically a small positive reinforcement with a slight entanglement drag. This interaction must be covered explicitly in the tests — see §6.

### 3.5 Episode

Always included. The payload is the full Contract 4 JSON verbatim, nested under `episode.payload`. The CLI's existing `WriteEpisode` is idempotent on `session_id`.

---

## 4. New Go structure

```
heb/cmd/heb/consolidate.go           // unchanged entry: reads stdin, dispatches
heb/internal/consolidate/
    consolidate.go                   // Run(in Contract4, cfg Config) (Result, error)
    threshold.go                     // significance check
    memories.go                      // lesson -> delta
    edges.go                         // pair enumeration
    entanglement.go                  // surprise-touch signals
    format.go                        // Block 1 + Block 2 rendering
    consolidate_test.go              // table-driven tests
```

`consolidate.Run` is a pure function of `(Contract4, Config)` — it produces the explicit payload (the current shape the CLI already accepts) plus the display blocks. The existing `cmd/heb/consolidate.go` then applies the payload to the store in its existing transaction.

This means the refactor is additive: the existing explicit-payload code path stays, we just add a Contract 4 → explicit-payload translator in front of it.

---

## 5. Slash command rewrite

`.claude/commands/consolidate.md` drops from 300 lines to roughly this:

```markdown
---
description: Heb Contract 5 — consolidate a learn object into the memory graph
---

# /consolidate

Pipes Contract 4 JSON (from /learn) into `heb consolidate`. The CLI
computes all deltas. The slash command is a pipe.

## Hard rules

- DO NOT re-read the conversation
- DO NOT re-derive lessons
- DO NOT write to `.heb/memory.db` directly
- DO NOT call `heb consolidate` more than once
- Complete in a single response

## Input

Contract 4 JSON from `/learn`, available in conversation context
immediately after `/learn` runs. Read it directly from context.

If Contract 4 is not present: stop and report `CONSOLIDATE FAILED —
no Contract 4 in context`. Do not proceed.

## Invocation

One heredoc, no flags. Defaults are correct.

```bash
heb consolidate <<'JSON'
<Contract 4 JSON here, on any number of lines>
JSON
```

Print the CLI output verbatim. If the CLI exits non-zero, surface
stderr in the response so the user sees what failed.

## Done when

- `heb consolidate` was called exactly once
- Output was printed verbatim
- No direct writes to `.heb/memory.db`
```

That's ~30 lines including frontmatter. From 300 → ~30 is a **90% reduction**.

---

## 6. Testing strategy

Three layers, table-driven:

**Layer 1 — unit.** Each of `threshold`, `memories`, `edges`, `entanglement` has a table test with fixtures covering:
- Threshold met vs. not met (each of the 6 conditions)
- Lessons at, just below, and just above `min-confidence`
- Malformed tuples (wrong number of parts, empty strings)
- Universal candidate reason prefix
- Edge pair enumeration: every unordered pair from the written set emits one `co-activation-boost` delta; `len(written) < 2` skips the pass entirely
- Entanglement signal with/without surprise_touches, with/without corrections, clamp at both bounds
- **Interaction test: reinforcement + entanglement on the same tuple.** Lesson with `confidence=0.80` teaches tuple `A`; `surprise_touches` contains a path that matches `A`; `peak_intensity=0.60`. Expected events on `A` in order: `session_reinforced: +0.064` (= 0.80 × 0.08), then `entanglement_signal: -0.03` (= 0.60 × 0.05, within bounds). Net delta: `+0.034`. Verify final weight = `previous_weight + 0.034`. This is the most important edge case — a session that both teaches something new AND triggers an entanglement warning on the same tuple.

**Layer 2 — integration.** One golden Contract 4 input, one golden explicit-payload output. If the translator output matches, the rest of the pipeline is already proven.

**Layer 3 — end-to-end.** Feed a Contract 4 into `heb consolidate` against a temporary SQLite DB, assert resulting memory weights, edge counts, and episode row.

Target: ~20 test cases.

---

## 7. Resolved questions

All six open questions were resolved during review. Recording the decisions for posterity.

1. **`retrieved_tuples` in Contract 4 — not needed; passive co-retrieval removed.** The Hebbian rule is *neurons that fire together wire together*. The key word is **together** — both sides of the connection must fire. Retrieval fires one side (the memory surfaces in context), but if nothing in the world confirms it — no correction accepted, no code written, no developer validation — the other side did not fire. Retrieval alone is the system talking to itself; strengthening edges from it is circular reasoning.

   Therefore: edges strengthen only when tuples are **written** in the same session, i.e. emitted as lessons by `/learn` and accepted by the threshold. No `passive-boost`. No `retrieved_tuples` in Contract 4. The edge pass needs only the written set, which is already in the `lessons` array.

   **Verification was performed** against `.heb/memory.db` (the episode payload carries `memory_loaded` counts only, no tuple list) and `learn.md` (no mention of `retrieved_tuples`). The earlier concern that this required a `learn.md` precondition is resolved by removing passive co-retrieval entirely. The blocker disappears.

   **Cost.** Understand-only sessions contribute nothing to edge formation. That is the correct behaviour: understand sessions produce no confirmed knowledge, and a memory graph that learns from unconfirmed retrieval loops is not learning, it is hallucinating structure. Clusters form from act sessions — where the developer actually confirms what the agent produced — which is exactly where Hebb's rule applies.

   **Simplification cascade.** One correct decision collapses three things at once:
   - **`/learn`** — no longer needs to extract `retrieved_tuples` from the `RETRIEVAL RESULT` block. One less field to fabricate or get wrong.
   - **yx0 §3.3** — edge algorithm halves in complexity. No union, no boost selection, no passive path. Every pair in the written set gets one `co-activation-boost` delta.
   - **yx0 §3.4** — entanglement algorithm clarifies. Written-only matches the philosophical statement (edges and entanglement both act on what actually fired) exactly.

   Three simultaneous simplifications from one decision is a strong signal the decision is correct.

2. **Surprise-touch tuple matching — simple substring.** No path segmentation. Split the tuple on `·`, lowercase both the parts and the full file path, check if any part is a substring of the full path. Signal magnitude is ≤0.08, false positives are acceptable. See §3.4 for the exact `matchesSurpriseTouch` function.

3. **Backward compat — `--raw` flag, permanent.** Keep the explicit-payload path permanently behind `--raw`. Useful for writing store tests without going through the Contract 4 translator. Shape sniffing would be fragile — flag is explicit. See §2.1.

4. **Skipped tuples — JSON only.** Block 1 is the human glance view. Skipped tuples are implementation detail surfaced only when debugging. Developers read the JSON block if they need to dig in.

5. **Episode idempotency — show both forms.** Exactly one of `written:` / `skipped:` appears in Block 1 so the developer can tell at a glance whether a re-run hit the guard. See §2.4.

6. **ErrorOut — same JSON shape, always.** CLI exits non-zero with the same top-level keys on stdout (`applied: []`, `edges_updated: 0`, `entanglement_signals: 0`, `episode_written: false`, `errors: [...]`) plus details on stderr. Callers can always parse stdout as the same `Result` type regardless of outcome. See §2.4.

---

## 8. Migration plan

No `learn.md` precondition — `retrieved_tuples` is not consumed (see §3.3 / §7.1). The migration is purely additive inside `heb/`.

1. Implement `internal/consolidate` with TDD: threshold → memories → edges → entanglement → format → e2e. Lessons, edge pair enumeration on the written set, entanglement clamps, and the reinforcement+entanglement interaction test are the critical fixtures.
2. Extend `cmd/heb/consolidate.go` to accept Contract 4 by default and `--raw` for the explicit-payload shape.
3. Rebuild and install `heb.exe`.
4. Rewrite `.claude/commands/consolidate.md` to the ~30-line shape above.
5. Run `/heb` end-to-end: Phase A (sense → recall → reflect → execute → HALT), then accept, then Phase B (learn → consolidate). Compare the written memory deltas against a known-good run.
6. Commit.
7. Close yx0.

---

## 9. Risks

- **Behaviour change: no passive co-retrieval.** Earlier drafts of the recall→consolidate math strengthened edges when tuples were retrieved together without being written. yx0 removes that path entirely on Hebbian-correctness grounds (§7.1). The consequence is that understand-only sessions contribute nothing to edge formation. Mitigation: this is intentional — understand sessions do not produce confirmed knowledge — but note it for anyone comparing edge counts before and after yx0 lands.
- **Math drift.** Hard-coded constants in Go could drift from the markdown if the markdown is updated independently. Mitigation: flags expose all constants, and the proposal says the slash command passes no flags — so the CLI defaults are the single source of truth. If someone wants to change the new-gain, they change the CLI default (one place).
- **Surprise-touch matching.** Substring matching is crude. Could over-trigger. Acceptable for now — the signals are small (max 0.08) and get absorbed by normal reinforcement over time.
- **Episode size.** Contract 4 can get large. The episode table stores it verbatim. Acceptable — episodes are rarely queried, and SQLite handles multi-KB rows fine.

---

## 10. Acceptance criteria

Implementation of yx0 is complete when:

1. `heb consolidate < contract4.json` applies memory/edge/episode deltas semantically equivalent to the current markdown math on a fixed test input, with the one intentional deviation that passive co-retrieval edges are no longer emitted (§3.3 / §7.1).
2. `heb consolidate --raw < payload.json` still accepts the explicit-payload shape for debug/test use.
3. All unit/integration/e2e tests pass, including the reinforcement+entanglement interaction test (§6).
4. The slash command `consolidate.md` is ≤35 lines of non-frontmatter content.
5. `heb consolidate --help` documents every flag with its default.
6. Hebbian constants in the current markdown are exposed as flags with the defaults specified in §2.2, using `--entanglement-min` / `--entanglement-max` (standard min/max semantics, not floor/ceil). `--passive-boost` does not exist.
7. Contract 4 significance threshold, malformed-tuple skipping, universal-candidate reason prefix, and entanglement clamping all match the current markdown behaviour.
8. Edges and entanglement signals both act on the written set only — no consumption of `retrieved_tuples` (which is not in Contract 4).
9. On any error, the CLI exits non-zero with the full `Result`-shaped JSON on stdout (errors populated, applied empty) plus details on stderr.
10. The `/heb` Phase B pipeline (learn → consolidate) still runs end-to-end against `.heb/memory.db`.
