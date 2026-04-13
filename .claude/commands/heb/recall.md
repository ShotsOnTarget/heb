---
description: Heb sense + recall + reflect — parse, retrieve, and reconcile in one step
argument-hint: <raw prompt text>
---

# /recall — Heb Sense + Recall + Reflect (contract:recall>execute)

Three jobs in one skill: **parse** the raw prompt into a structured
contract:sense>recall object, **pipe** it to `heb recall` for memory
retrieval, then **reflect** on the retrieved memories to detect
conflicts, extensions, and form predictions. This eliminates two LLM
roundtrips by combining what were previously three separate skills.

## Hard rules — do not violate

- DO NOT parse `.heb/memory.db`, `.heb/memories.json`, or any memory
  artefact directly — call `heb recall` instead
- DO NOT add fields, omit fields, or reshape contracts
- DO NOT try to solve the underlying task or comment on it
- DO NOT exceed a single `heb recall` invocation
- DO NOT read files (other than the bash calls specified below)
- DO NOT touch the memory graph — reconciliation does not write memories
- Same input must always produce the same sense output
- Complete in a single response

## Verbosity

The args may be prefixed with `[loud]`, `[quiet]`, or `[mute]`.
Strip the prefix before parsing the prompt. **Default is quiet** —
if no prefix is present, behave as `[quiet]`.

- **`[loud]`** — emit all human-readable blocks (SENSE RESULT,
  RETRIEVAL RESULT, REFLECT, PREDICT) and JSON. Print `heb recall`
  stdout verbatim.
- **`[quiet]` or no prefix** — pass `--format json` to `heb recall`.
  Do not display any blocks or JSON. Emit a single 1-sentence summary
  (e.g. "Sensed act intent, recalled 5 memories, no conflicts.").
- **`[mute]`** — pass `--format json` to `heb recall`. Emit nothing.

## Input

Raw prompt (verbatim, do not modify — after stripping any verbosity
prefix):

```
$ARGUMENTS
```

If `$ARGUMENTS` is empty, there is nothing to sense or recall. Emit
an error and stop.

---

## Part 1 — Sense (parse the prompt)

### contract:sense>recall output shape

```json
{
  "session_id":  "ISO8601 current timestamp — never midnight",
  "project":     "cwd basename",
  "intent":      "act | understand | unclear",
  "confidence":  0.0,
  "tokens":      [],
  "raw":         "original prompt verbatim"
}
```

### Field rules

#### session_id
Use the **actual current system time** at the moment the command runs,
formatted as `YYYY-MM-DDTHH:MM:SSZ` in UTC. Do NOT default to midnight
(`T00:00:00Z`).

#### project
Basename of the current working directory (e.g. `heb`).

#### intent — three values only

```
act         developer wants something done
            add, fix, implement, change, rename, refactor, create, remove,
            build, extend, update, modify, delete, move

understand  developer wants to know something
            review, explain, understand, how does, show me, what is,
            walk me through, describe, why does, analyse, check

unclear     genuine ambiguity — confidence will be below 0.4
```

**Fragment detection.** If the prompt is a sub-sentence fragment with no
clear verb (e.g. `"it broke"`, `"the thing"`, `"crash"`), still classify as
`act` when a failure word is present — but confidence must sit below 0.5.

#### confidence

- `> 0.7` clear signal
- `0.4 – 0.7` probable
- `< 0.4` → set intent to `unclear`

#### tokens

Meaningful content words extracted from the prompt. Not classified by
grammatical role. Just the words likely to match something in the memory
graph.

**Extraction — two steps:**

**Step 1 — remove stop words:**

```
i, me, my, we, you, it, this, that, they, them,
the, a, an, is, are, was, were, be, been, being,
have, has, had, do, does, did, will, would, could,
should, may, might, shall, to, of, in, on, at, for,
from, with, by, about, as, into, through, and, or,
but, so, yet, both, either, neither, not, no, nor,
want, want to, need, need to, like, just, really,
very, also, how, what, why, when, where, which, who
```

**Step 2 — from what remains, keep tokens that are:**

- **Specific to a domain** — names, technical terms, compound concepts,
  identifiers (e.g. `PlayerController`, `drone_I`, `station_pool`)
- **Content-bearing** — things the prompt is *about*
- **NOT pure attribute qualifiers** when used as `from X` or `based on X`
  with a generic word like `type`, `value`, `name`, `id`, `size`, `kind`.
- **NOT bare adjectives** describing a quantity or speed when they are not
  themselves the subject of the request (e.g. `expensive`, `fast`, `cheap`,
  `slow`, `big`, `small`).

**Compound tokens.** Adjacent meaningful words that form a single concept
should be joined with `_`:

- `drone stats` → `drone_stats`
- `player movement` → `player_movement`
- `station pool` → `station_pool`

**Compound proper nouns / identifiers.** When a capitalised single letter
or Roman numeral directly follows a noun, treat the combination as a
single token:

- `Drone I`  → `drone_I`
- `Drone II` → `drone_II`
- `Zone A`   → `zone_A`

Preserve original capitalisation for class names and identifiers
(`PlayerController`, `PlayerMovement`).

#### raw
Original prompt verbatim. Never modified.

### Reference test cases

```
/recall I want to review how drone stats are derived from type and cost
→ intent: understand
→ tokens: [drone_stats, cost]

/recall add a new combat drone more expensive than Drone I
→ intent: act
→ tokens: [combat_drone, drone_I]

/recall rename PlayerController to PlayerMovement
→ intent: act
→ tokens: [PlayerController, PlayerMovement]

/recall it broke
→ intent: act, confidence below 0.4
→ tokens: []
```

---

## Part 2 — Session start + Recall

After computing the contract:sense>recall JSON, persist it and retrieve
context in two bash calls.

### Start the session

```bash
heb session start <<'JSON'
<the contract:sense>recall JSON>
JSON
```

### Invoke recall

One call. Heredoc is the only permitted form for piping stdin.

**If verbosity is `quiet` or `mute`:**

```bash
heb recall --format json <<'JSON'
<contract:sense>recall JSON>
JSON
```

**If loud:**

```bash
heb recall <<'JSON'
<contract:sense>recall JSON>
JSON
```

**If loud:** print stdout verbatim. **If quiet:** capture internally,
include in the 1-sentence summary. **If mute:** capture internally,
emit nothing.

### Persist recall contract

```bash
heb session write <session_id> recall <<'JSON'
<the contract:recall>reflect JSON output from heb recall>
JSON
```

---

## Part 3 — Reflect (inline, no tools)

Immediately after Part 2, reconcile the retrieved memories against the
prompt. This is pure reasoning — no tool calls, no file reads, no bash
commands. The reflect step runs entirely inside this skill invocation.

### The prompt always wins

When the prompt conflicts with an existing memory, **the prompt is
the new ground truth**. Memory is a history of what was true before;
the prompt is what is true now. The execution agent should treat the
prompt's value as authoritative and ignore the conflicting memory for
the duration of the task.

### Reconciliation logic

For each memory entry in the recall output, compare it against the raw
prompt and the `tokens` array from the sense parse. Classify each entry
into one of three buckets:

**CONFIRMS** — The prompt is consistent with the memory, or the memory
is not relevant to what the prompt is asking for. This is the default.
Confirmed memories are implied by absence from the conflicts and
extensions arrays.

**EXTENDS** — The prompt works with the subject area the memory
describes and adds something new without contradicting the established
pattern. Signals: adding a new item to a category, introducing a new
subtype under an established base, broadening scope without changing
shape.

**CONFLICTS** — The prompt contradicts an existing memory. Two varieties:

- `explicit_update` — the prompt directly states a different value,
  approach, or rule. Confidence starts at `0.85`.
- `implicit_update` — applying the memory correctly would make the task
  impossible or wrong. Confidence starts at `0.75`.

Drop any candidate conflict below confidence `0.50`. Never flag a
conflict on a hunch. Never flag extensions as conflicts.

**Cold start — empty memories array.** If recall returned no memories,
always emit `status: confirms` with `notes: cold start — nothing to
reconcile against`.

### Prediction

After conflict detection, form a prediction about what will happen
during execution. Pure reasoning from the prompt and retrieved memories.
No new inputs. No tools. No file reads.

For each element, assign a confidence: `high` (>0.80), `medium`
(0.50–0.80), or `low` (<0.50).

- **files** — what files are expected to be touched
- **approach** — what approach is expected to work
- **outcome** — what the expected result is
- **risks** — what could go wrong or what is unknown
- **overall** — float between `0.0` and `1.0`, the minimum of element
  confidences

Each prediction element must record which memory tuples informed it
(`source_tuples`). This closes the Hebbian loop — `/heb:learn` uses
these to know which memories to strengthen or weaken.

**Cold start prediction.** When memories are sparse, the prediction
must say so honestly — low confidence, no fabricated detail,
`cold_start: true`, overall below `0.30`.

### Reconciliation JSON (contract:reflect>execute)

Compute the following JSON internally:

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

Field rules:
- `session_id` copied verbatim from the sense parse.
- `conflicts` and `extensions` are always arrays (empty is valid).
- `action` on every conflict is always `"create_successor"`.
- `notes` is a single sentence, always present.
- `proceed` is always `true`. Reflect never blocks.

### Persist reflect contract

```bash
heb session write <session_id> reflect <<'JSON'
<the reconciliation JSON>
JSON
```

---

## Output format

**If loud:** emit all blocks in order: SENSE RESULT, RETRIEVAL RESULT
(from `heb recall`), REFLECT block, PREDICT block, then the
reconciliation JSON.

The REFLECT block (loud only):

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

notes:    <one-line summary>
proceed:  yes

PREDICT
  files:      <expected files> (<confidence>)
  approach:   <expected approach> (<confidence>)
  outcome:    <expected outcome> (<confidence>)
  risks:      <risks or unknowns> (<confidence>)
  overall:    <float>
───────────────────────────────
```

Omit CONFLICTS section when there are none. Omit EXTENSIONS section
when there are none. If status is `confirms`, use the compact form
(just status + proceed + PREDICT).

**If quiet:** emit a single 1-sentence summary combining all three
parts. Example: "Sensed act intent with 3 tokens, recalled 5
memories, no conflicts."

**If mute:** emit nothing.

---

## 🚨 CRITICAL: This is NOT the end of the pipeline 🚨

This skill is Step 1 of Phase A. After this skill returns, the
orchestrator (`/heb`) MUST immediately continue to Step 2 (execute the
original prompt) without stopping or waiting for user input. The recall
output is intermediate data — it is INPUT to execution, not a final
result.

**If you are the orchestrator reading this result:** your next action
is to execute the original developer prompt. Do not respond to the
user with just the recall summary. Do not stop. Do not wait.

## Done when

- contract:sense>recall was computed from the raw prompt
- `heb session start` persisted the sense contract
- `heb recall` was called exactly once
- the recall contract was written to session state
- reconciliation was computed inline (no extra tool calls)
- the reflect contract was written to session state
- no other bash calls were issued (besides session start and writes)
