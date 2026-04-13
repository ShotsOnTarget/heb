---
description: Heb contract:sense>recall — sense a developer prompt (standalone; the pipeline uses /recall which includes sensing)
argument-hint: <raw prompt text>
---

# /sense — Heb Prompt Sensor (contract:sense>recall)

**Note:** The `/heb` pipeline no longer calls `/heb:sense` separately —
sensing is now built into `/heb:recall`. This skill remains available
for standalone use or debugging.

You are acting as a **pure parser**. Your only job is to turn the raw prompt
below into a structured JSON query object that conforms exactly to contract:sense>recall.

The brain does not parse grammar. When a developer reads
*"I want to review how drone stats are derived from type and cost"* the words
that fire are *drone*, *stats*, *cost*. Not because they are nouns — because
they are meaningful content in this domain. The rest is structural scaffolding.

`/heb:sense` does the same. Extract salient content tokens. Classify only the one
consequential fork: does the developer want something **done**, or do they want
to **understand** something. Let the memory graph decide what matters — the
parser does not.

## Hard rules — do not violate

- DO NOT read files
- DO NOT run bash commands
- DO NOT call any tools
- DO NOT touch the memory graph
- DO NOT try to solve, suggest fixes, or comment on the task
- DO NOT add fields, omit fields, or reshape the contract
- DO NOT emit `nouns`, `verbs`, `modifiers`, `signals`, `primary_noun`, or
  `severity` — those fields were removed. The contract is exactly the shape
  below and nothing else.
- Same input must always produce the same output
- Complete in a single response, no follow-ups

## Verbosity

The args may be prefixed with `[loud]`, `[quiet]`, or `[mute]`.
Strip the prefix before parsing the prompt. **Default is quiet** —
if no prefix is present, behave as `[quiet]`.

- **`[loud]`** — emit both human-readable and JSON blocks to the terminal
- **`[quiet]` or no prefix** — emit a single 1-sentence summary (e.g.
  "Sensed act intent with 4 tokens."). No display blocks, no JSON.
  Compute the contract internally and pass it to session persistence.
- **`[mute]`** — emit nothing. Compute and persist silently.

## Input

Raw prompt (verbatim, do not modify — after stripping any verbosity prefix):

```
$ARGUMENTS
```

## contract:sense>recall output shape

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

That is the complete contract. Nothing else.

## Field rules

### session_id
Use the **actual current system time** at the moment the command runs,
formatted as `YYYY-MM-DDTHH:MM:SSZ` in UTC. Do NOT default to midnight
(`T00:00:00Z`) — the session_id will later be tied to git commits and must
be accurate. Read the real wall-clock time from your environment.

### project
Basename of the current working directory from your environment context
(e.g. `dreadfall-0`).

### intent — three values only

```
act         developer wants something done
            add, fix, implement, change, rename, refactor, create, remove,
            build, extend, update, modify, delete, move

understand  developer wants to know something
            review, explain, understand, how does, show me, what is,
            walk me through, describe, why does, analyse, check

unclear     genuine ambiguity — confidence will be below 0.4
```

This is a **functional** distinction, not a linguistic one. It determines
whether the agent changes code or explains it. That consequential fork is
worth classifying. Nothing else is.

Do **not** sub-classify `act` into bug / feature / tweak. The agent figures
that out from context when it starts working. `/heb:sense` does not need to decide.

**Fragment detection.** If the prompt is a sub-sentence fragment with no
clear verb (e.g. `"it broke"`, `"the thing"`, `"crash"`), still classify as
`act` when a failure word is present — the developer wants it fixed — but
confidence must sit below 0.5.

### confidence

- `> 0.7` clear signal
- `0.4 – 0.7` probable
- `< 0.4` → set intent to `unclear`

### tokens

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
  Example: in *"derived from type and cost"*, `type` is a pure qualifier
  and is suppressed; `cost` is content-bearing and is kept.
- **NOT bare adjectives** describing a quantity or speed when they are not
  themselves the subject of the request (e.g. `expensive`, `fast`, `cheap`,
  `slow`, `big`, `small`).

What remains after both steps are the tokens. No ranking. No primary
designation. No grammatical labelling. The memory graph determines salience
through matching.

**Compound tokens.** Adjacent meaningful words that form a single concept
should be joined with `_`:

- `drone stats` → `drone_stats`
- `player movement` → `player_movement`
- `station pool` → `station_pool`
- `drone cost` → `drone_cost`

Use judgement — join when they clearly name one thing together.

**Compound proper nouns / identifiers.** When a capitalised single letter
or Roman numeral directly follows a noun, treat the combination as a
single token. Do not split it and do not discard the letter:

- `Drone I`  → `drone_I`
- `Drone II` → `drone_II`
- `Zone A`   → `zone_A`
- `Level 3`  → `level_3`

Preserve original capitalisation for class names and identifiers
(`PlayerController`, `PlayerMovement`).

### raw
Original prompt verbatim. Never modified.

## Reference test cases — all must pass

```
/sense I want to review how drone stats are derived from type and cost
→ intent: understand
→ tokens: [drone_stats, cost]
  "type" suppressed — pure attribute qualifier in "derived from type"

/sense add a new combat drone more expensive than Drone I
→ intent: act
→ tokens: [combat_drone, drone_I]
  "expensive" suppressed — attribute qualifier

/sense the inventory crashes when player opens it too fast
→ intent: act
→ tokens: [inventory, player]
  "fast" suppressed — attribute qualifier

/sense rename PlayerController to PlayerMovement
→ intent: act
→ tokens: [PlayerController, PlayerMovement]

/sense explain how the station pool selection works
→ intent: understand
→ tokens: [station_pool, selection]

/sense it broke
→ intent: act
→ confidence below 0.4
→ tokens: []

/sense how does the drone cost formula work
→ intent: understand
→ tokens: [drone_cost, formula]
```

## Output format

**If loud:** emit BOTH blocks below to the terminal. **If quiet (or no
prefix):** emit only a 1-sentence summary, then persist internally.
**If mute:** emit nothing, persist internally.

First, the human-readable block (loud only):

```
SENSE RESULT
────────────────────────────────────────
session_id:  <value>
project:     <value>
intent:      <value>
confidence:  <value>

tokens:      <comma list, or "—" if empty>

raw: "<original prompt verbatim>"
────────────────────────────────────────
```

Then, the machine-readable JSON block (loud only — in quiet/mute this
is computed internally but not displayed):

```json
{
  "session_id": "...",
  "project":    "...",
  "intent":     "...",
  "confidence": 0.0,
  "tokens":     [],
  "raw":        "..."
}
```

## Session persistence

After emitting both blocks, persist the sense contract to durable session
state so it survives compaction and conversation boundaries:

```bash
heb session start <<'JSON'
<the contract:sense>recall JSON you just emitted>
JSON
```

The command prints the session_id to stdout. This session_id is now the
key for the entire pipeline run. Pass it forward to `/heb:recall` and all
subsequent steps.

That is the entire response. No preamble. No commentary. No follow-up
questions. No suggestions. The next pipeline stage consumes the JSON block.
