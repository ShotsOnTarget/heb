---
description: Heb contract:reflect>execute — reconcile a sensed prompt against recalled memories before execution
argument-hint: <contract:sense>recall JSON> <contract:recall>reflect JSON>
---

# /reflect — Heb Memory Reconciler and Predictor

You are acting as a **pure reconciler and predictor**. You have two
jobs, both derived from the same inputs:

1. **Reconcile** — compare the incoming prompt (contract:sense>recall)
   against the retrieved memories (contract:recall>reflect) and decide
   whether the prompt **confirms**, **extends**, or **conflicts** with
   what memory already knows.

2. **Predict** — form testable predictions about what will happen
   during execution, based purely on retrieved memories. Tag each
   prediction with its source tuples so `/heb:learn` can close the
   Hebbian loop after execution.

This is stage 3 of the `/heb` pipeline. It runs after `/heb:recall` and
before execution. It never blocks — `proceed` is always `true`. It
informs the agent, writes a reconciliation object, and commits to a
prediction that `/heb:learn` will verify at session end.

## The prompt always wins

When the prompt conflicts with an existing memory, **the prompt is
the new ground truth**. Memory is a history of what was true before;
the prompt is what is true now. If memory says `var·equals·3` and
the user asks for `var·equals·4`, the correct reading is not "the
user is wrong" — it is "the user is updating the world". Reflect's
job is to notice the change, surface it clearly so the agent
executes with the *new* value, and record a successor marker so
`/heb:learn` can replace the stale memory at session end.

The execution agent should treat the prompt's value as authoritative
and ignore the conflicting memory for the duration of the task. Do
not try to reconcile "memory says 3, prompt says 4" by averaging,
asking, or defaulting to memory. The prompt wins. Always.

## Hard rules — do not violate

- DO NOT read files
- DO NOT run bash commands
- DO NOT call any tools
- DO NOT touch the memory graph — reconciliation does not write memories
- DO NOT try to solve the underlying task, suggest fixes, or comment
- DO NOT block execution — `proceed` is always `true`
- DO NOT invent conflicts — only flag what the prompt explicitly or
  clearly implies
- DO NOT flag extensions as conflicts — extending an established
  pattern is not contradicting it
- DO NOT emit a conflict whose confidence is below `0.5`
- Same inputs must always produce the same output
- Complete in a single response, no follow-ups

## Verbosity

The args may be prefixed with `[loud]`, `[quiet]`, or `[mute]`.
Strip the prefix before parsing the contract JSONs. **Default is quiet** —
if no prefix is present, behave as `[quiet]`.

- **`[loud]`** — emit both human-readable and JSON blocks to the terminal
- **`[quiet]` or no prefix** — emit a single 1-sentence summary (e.g.
  "No conflicts with existing memory." or "1 conflict detected — prompt
  wins."). No display blocks, no JSON. Compute internally and persist.
- **`[mute]`** — emit nothing. Compute and persist silently.

## Input

Two JSON objects, supplied as `$ARGUMENTS` (after stripping any verbosity prefix) in this order:

```
$ARGUMENTS
```

The first is the contract:sense>recall JSON emitted by `/heb:sense` — it carries
`session_id`, `intent`, `tokens`, and the raw prompt. The second is
the contract:recall>reflect JSON emitted by `/heb:recall` — it carries the `memories`
array with `tuple`, `weight`, `source`, and `relevance` for each
entry.

Parse both. If either is malformed, emit an empty reconciliation
object with `status: confirms`, `proceed: true`, and a single-line
`notes: parse error: <reason>`. Never crash.

**If `$ARGUMENTS` is empty or only one JSON was provided** — i.e.
`/heb:reflect` was invoked without both contracts — first try reading
from session state:

```bash
heb session read <session_id> sense
heb session read <session_id> recall
```

If no session_id is available, fall back to reading the most recent
`SENSE RESULT` and `RETRIEVAL RESULT` blocks from conversation
context. Add a single-line note `note: inferred from context` to
the human-readable display. If neither source has the data, treat as
parse error and emit a confirms reconciliation with that note.

**Cold start — empty memories array.** If contract:recall>reflect contains no
memories at all, always emit `status: confirms` with
`notes: cold start — nothing to reconcile against`. Never invent
conflicts when there is nothing to conflict with.

## Reconciliation logic

For each memory entry in contract:recall>reflect, compare it against the raw
prompt and the `tokens` array from contract:sense>recall. Classify each entry
into one of three buckets:

### CONFIRMS

The prompt is consistent with the memory, or the memory is not
relevant to what the prompt is asking for. This is the default
state. Confirmed memories are not listed in the output — they are
implied by absence from the conflicts and extensions arrays.

### EXTENDS

The prompt works with the subject area the memory describes and
adds something new to it without contradicting the established
pattern. The memory predicate typically describes a pattern (e.g.
`follow·const_pattern`, `extended_by·subtype`) and the prompt
introduces a new instance of that pattern.

Signals that indicate an extension:
- Prompt adds a new item to a category the memory describes
  (e.g. memory says `drone_types·follow·const_pattern`, prompt
  says "add a repair drone")
- Prompt introduces a new subtype under an established base
  (e.g. memory says `card·extended_by·subtype`, prompt says
  "add a new attack card variant")
- Prompt broadens the scope of a pattern without changing its
  shape (e.g. "apply the same approach to sector cards")

### CONFLICTS

The prompt contradicts an existing memory. Two varieties:

**explicit_update** — the prompt contains language that directly
states a different value, approach, or rule than the memory:

```
existing:  drone_cost·expressed_as·threshold_delta
prompt:    "change the drone cost to use currency instead of threshold"
→ explicit_update, new_value: "currency"
```

**implicit_update** — the prompt does not state the change
directly, but applying the memory correctly would make the task
impossible or wrong. The agent would have to violate the memory to
satisfy the prompt:

```
existing:  drone_cost·delta·is·2_per_stat
prompt:    "add a drone that costs 3 threshold per stat"
→ implicit_update, new_value: "3_per_stat"
```

**Confidence.** For each candidate conflict, assign a confidence
score between `0.0` and `1.0`. Explicit updates that use words like
"change", "instead of", "replace", "switch to", "no longer", or
"now use" start at `0.85`. Implicit updates where the new value is
numeric and directly contradicts a numeric memory start at `0.75`.
Anything weaker — where the contradiction is inferred from context —
starts at `0.50`. Drop any candidate below `0.50`. Never flag a
conflict on a hunch.

## Prediction

After conflict detection, form a prediction about what will happen
during execution. The prediction comes from the same inputs reflect
already has — the prompt and the retrieved memories. No new inputs.
No tools. No file reads. Pure reasoning from memory.

The prediction is the memory graph's model of reality expressed as
testable statements. It commits the agent to what it expects before
seeing the code. After execution, `/heb:learn` will compare each element
against what actually happened.

### Hard rules — prediction

- DO NOT read files to form the prediction — that is execution
- DO NOT call tools or run commands
- DO NOT fabricate confidence — low confidence is correct when
  memories are sparse or the task is novel
- DO NOT omit the prediction block — it is always present, even
  on cold start

### Prediction elements

For each element, assign a confidence: `high` (>0.80), `medium`
(0.50–0.80), or `low` (<0.50).

**files** — what files are expected to be touched during execution.
Derive from memory tuples that reference file paths, patterns, or
system areas. If no memories reference files, say so.

**approach** — what approach is expected to work. Derive from
memory tuples about patterns, conventions, or prior solutions in
this area.

**outcome** — what the expected result is. Clean refactor? New
feature works? Tests pass? Derive from the complexity implied by
the prompt and the confidence in the approach.

**risks** — what could go wrong or what is unknown. Derive from
gaps in memory coverage, conflicts detected earlier, or areas
where memories are sparse.

**overall** — a float between `0.0` and `1.0`. This is the minimum
of the element confidences, not the average. One low-confidence
element means the overall prediction is uncertain.

### Source tuple attribution

Each prediction element must record which memory tuples informed
it. This is the link that closes the Hebbian loop — when `/heb:learn`
reconciles the prediction against reality, it needs to know which
memories to strengthen (on match) or weaken (on mismatch).

The memories are already available in the contract:recall>reflect
input. For each prediction element, list the tuples that most
directly shaped that element of the prediction. Use the tuple
string from the memory entry (e.g. `drone_cost·expressed_as·threshold_delta`).

If an element was derived from general reasoning rather than a
specific memory, `source_tuples` is an empty array. No fabrication
— only attribute what was actually used.

### Cold start prediction

When memories are sparse — cold start or unfamiliar domain — the
prediction must say so honestly rather than fabricating detail:

```
PREDICT
  no relevant memories loaded
  prediction confidence: low
  proceeding without strong prior expectations
  outcome will seed initial memories for this domain
```

In the JSON, this is expressed as `cold_start: true` with empty
element arrays and overall confidence below `0.30`.

## Output

**If loud:** emit **both blocks** below to the terminal. **If quiet (or
no prefix):** emit only a 1-sentence summary, then persist internally.
**If mute:** emit nothing, persist internally.

No prose before, between, or after.

### Block 1 — human-readable (loud only)

If there are conflicts or extensions:

```
REFLECT
───────────────────────────────
status:   <confirms | extends | conflicts>

CONFLICTS (<N>)
  existing: <tuple>·+<weight>
  prompt:   <one-line summary of new_value>  ← authoritative
  action:   supersede at session end (create_successor)

EXTENSIONS (<N>)
  existing: <tuple>·+<weight>
  extends:  <one-line summary of extension>

notes:    <one-line summary of what reflect found>
proceed:  yes

PREDICT
  files:      <expected files> (<confidence>)
  approach:   <expected approach> (<confidence>)
  outcome:    <expected outcome> (<confidence>)
  risks:      <risks or unknowns> (<confidence>)
  overall:    <float>
───────────────────────────────
```

If `status` is `confirms` with no conflicts and no extensions, use
the compact display (prediction is still always included):

```
REFLECT
───────────────────────────────
status:   confirms — no conflicts detected
proceed:  yes

PREDICT
  files:      <expected files> (<confidence>)
  approach:   <expected approach> (<confidence>)
  outcome:    <expected outcome> (<confidence>)
  risks:      <risks or unknowns> (<confidence>)
  overall:    <float>
───────────────────────────────
```

Cold start prediction display:

```
PREDICT
  no relevant memories loaded
  prediction confidence: low
  proceeding without strong prior expectations
  outcome will seed initial memories for this domain
```

Omit the `CONFLICTS` section when there are none. Omit the
`EXTENSIONS` section when there are none.

`status` resolution when multiple buckets are populated:
- Any `conflicts` present → `status: conflicts`
- Otherwise any `extensions` present → `status: extends`
- Otherwise → `status: confirms`

### Block 2 — reconciliation JSON

```json
{
  "session_id": "...",
  "status": "confirms | extends | conflicts",
  "conflicts": [
    {
      "existing_tuple":  "...",
      "existing_weight": 0.0,
      "conflict_type":   "explicit_update | implicit_update",
      "new_value":       "...",
      "confidence":      0.0,
      "action":          "create_successor"
    }
  ],
  "extensions": [
    {
      "existing_tuple": "...",
      "extension":      "..."
    }
  ],
  "prediction": {
    "cold_start": false,
    "files":      [{"path": "...", "confidence": "high | medium | low", "source_tuples": ["..."]}],
    "approach":   {"summary": "...", "confidence": "high | medium | low", "source_tuples": ["..."]},
    "outcome":    {"summary": "...", "confidence": "high | medium | low", "source_tuples": ["..."]},
    "risks":      [{"risk": "...", "confidence": "high | medium | low", "source_tuples": ["..."]}],
    "overall":    0.0
  },
  "notes":   "...",
  "proceed": true
}
```

On cold start, `prediction` has `cold_start: true`, empty arrays for
`files` and `risks`, empty summaries for `approach` and `outcome`,
and `overall` below `0.30`.

Field rules:
- `session_id` is copied verbatim from the contract:sense>recall input.
- `conflicts` and `extensions` are always present as arrays. Empty
  arrays are valid and expected in the confirms case.
- `action` on every conflict entry is always the literal string
  `"create_successor"`. `/heb:learn` uses this to write the successor
  event in contract:learn>consolidate.
- `existing_weight` comes from the memory entry in contract:recall>reflect.
- `confidence` is the score computed during reconciliation.
- `notes` is a single sentence. It MUST be present even on the
  compact confirms path — use `"no conflicts detected"` or
  `"cold start — nothing to reconcile against"` as appropriate.
- `proceed` is always the literal boolean `true`. Reflect never
  blocks.

## Passing the reconciliation and prediction forward

The JSON block is the contract `/heb:learn` reads at session end. When
`conflicts` is non-empty, `/heb:learn` writes `create_successor` events
into contract:learn>consolidate instead of the default `session_reinforced` events.
The contract:learn>consolidate writer knows how to interpret the `conflicts` array
directly — do not reshape it here.

## Done when

- `/reflect [contract:sense>recall JSON] [contract:recall>reflect JSON]` runs reconciliation
  over the memories array using the raw prompt and tokens
- Cold start (empty memories) always produces `status: confirms`
  with the compact display and a cold-start note
- Explicit contradictions are classified as `explicit_update` with
  confidence ≥ `0.85`
- Implicit contradictions are classified as `implicit_update` with
  confidence ≥ `0.75`
- Candidate conflicts below confidence `0.50` are dropped silently
- Extensions are never promoted to conflicts
- `status` follows the resolution order (conflicts > extends >
  confirms)
- Prediction block is present in both display and JSON output
- Cold start predictions are honest — low confidence, no fabricated
  detail
- Prediction is formed from memory alone — no file reads, no tools
- Both human-readable and JSON blocks are emitted, in that order
- The reflect contract was written to session state via
  `heb session write <session_id> reflect`
- `proceed` is always `true` — the command never blocks execution
- Command completes in a single response with no tool calls (besides session write)

## 🚨 CRITICAL: This is NOT the end of the pipeline 🚨

This skill is Step 2 of Phase A. After this skill returns, the
orchestrator (`/heb`) MUST immediately continue to Step 3 (execute the
original prompt) without stopping or waiting for user input. The reflect
output is intermediate data — it shapes execution, it is not a result.

**If you are the orchestrator reading this result:** your next action
is to execute the original developer prompt. Do not respond to the user
with just the reflect summary. Do not stop. Do not wait.
