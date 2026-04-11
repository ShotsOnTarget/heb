---
description: Heb contract:learn>consolidate — learn from a completed task session and emit a structured handoff object
argument-hint: (no arguments — reads the current session)
---

# /learn — Heb Session Learner (contract:learn>consolidate)

You are acting as a **pure learner**. Your only job is to read the
**entire current conversation** from start to finish and emit a single
contract:learn>consolidate JSON object. That is all.

The weight is the single truth. Extract what the session actually
demonstrated. Do not classify beyond what is necessary. Do not impose
structure the session did not produce. Do not invent lessons. Report
what the agent observed and what the developer confirmed or corrected.

## Hard rules — do not violate

- DO NOT propose new work, fixes, or follow-ups
- DO NOT touch the memory graph yourself
- DO NOT add fields, omit fields, or reshape the contract
- DO NOT re-derive `tokens` or `intent` — copy from contract:sense>recall verbatim
- DO NOT invent decisions or corrections that did not happen
- DO NOT write lessons below `0.50` confidence
- DO NOT write lessons the session did not earn
- DO NOT reference `germline`, `somatic`, `signals`, `primary_noun`, or `tier` — these no longer exist
- Complete in a single response, no follow-ups

## What you read

The **full current conversation**, from the very first message to the
most recent. Every message, every question, every answer, every file
read or edited, every decision, every correction.

The session almost always begins with a `SENSE RESULT` block (contract:sense>recall)
and a `RETRIEVAL RESULT` block (contract:recall>reflect). Pull structured fields
from these directly — do not re-derive them.

### Recovering contracts from session state

If compaction has removed the `SENSE RESULT` or `RETRIEVAL RESULT`
blocks from conversation context, recover them from durable session
state. First find the active session:

```bash
heb session list
```

Then read whichever contracts are missing:

```bash
heb session read <session_id> sense
heb session read <session_id> recall
heb session read <session_id> reflect
```

This is the primary recovery mechanism for the interrupted-session and
compaction failure modes. The contracts in the database are identical
to what was originally emitted — use them as-is.

## contract:learn>consolidate output shape

```json
{
  "session_id":     "from /sense output at session start",
  "bead_id":        "active bead id or null",
  "project":        "project name",
  "timestamp_end":  "ISO8601 current time UTC — never midnight",
  "raw_prompt":     "original prompt verbatim",
  "intent":         "act | understand | unclear",
  "tokens":         ["from /sense output"],

  "memory_loaded": {
    "memories_loaded": 0,
    "git_refs":        0,
    "was_cold_start":  true
  },

  "recalled_via_edges": [],

  "implementation": {
    "files_touched":    [],
    "files_read":       [],
    "surprise_touches": [],
    "approach":         "one sentence past tense",
    "patterns_used":    []
  },

  "decisions": [
    {
      "question":  "what the agent asked",
      "answer":    "what the developer answered",
      "weight":    "high | medium | low"
    }
  ],

  "corrections": [
    {
      "what":       "what the agent did",
      "correction": "what the developer said instead",
      "intensity":  0.0
    }
  ],

  "correction_count": 0,
  "peak_intensity":   0.0,
  "completed":        true,

  "lessons": [
    {
      "observation": "subject·predicate·object",
      "scope":       "project | universal_candidate",
      "confidence":  0.0,
      "evidence":    "what in the session supports this",
      "source":      "session | prediction"
    }
  ]
}
```

## Field rules

### session_id
Find the `SENSE RESULT` block at session start. Copy `session_id`
verbatim.

### bead_id
Scan for `bd update`, `bd close`, `bd show` commands. Take the first
bead id found. `null` if none.

### project
Basename of the current working directory (e.g. `dreadfall-0`). Pull
from the contract:sense>recall output if present.

### timestamp_end
**Actual current system time** at the moment `/learn` runs, formatted
as `YYYY-MM-DDTHH:MM:SSZ` in UTC. Do NOT default to midnight.

### raw_prompt
The original developer prompt that opened this task, verbatim.

### intent
Copy from contract:sense>recall. Do not re-derive.

### tokens
Copy from the contract:sense>recall JSON output of `/sense`. Do not re-derive.

### memory_loaded
Scan for the `RETRIEVAL RESULT` block.

- `memories_loaded` — total count of memories listed under PROJECT
  MEMORIES and UNIVERSAL MEMORIES combined
- `git_refs` — count of git commits listed
- `was_cold_start` — `true` if `memories_loaded == 0` and no
  `episode_refs` were surfaced

If no retrieval block exists at all, treat it as a cold start with all
counts `0` and `was_cold_start: true`.

### recalled_via_edges
Scan the `RETRIEVAL RESULT` block for memories where `source` is
`"edge"`. These are memories that entered context via spreading
activation, not direct token match. Extract their tuples as strings
in `"subject·predicate·object"` format.

If reading from the JSON block, use the `tuple` field directly for
entries where `"source": "edge"`.

If reading from the human-readable block, look for lines tagged
`[edge ...]` and strip the weight suffix (the `·+N.NN` at the end).

Example: `[edge 0.37] all_cards·sideways_play·grants_1_of_any_stat·+0.68`
yields `"all_cards·sideways_play·grants_1_of_any_stat"`.

Empty array if no edge-sourced memories or no retrieval block.

### implementation.files_read
Every file the agent opened, read, searched, or grep'd during
execution. Not just files modified. Read the full transcript and
collect every file path that appeared in a read, search, view, or bash
operation. Deduplicate alphabetically. Repo-relative paths.

### implementation.files_touched
Files the agent actually edited or created. Subset of `files_read`.

### implementation.surprise_touches
Files in `files_read` that have no obvious connection to the `tokens`
from contract:sense>recall. A file is a surprise touch if none of the session
tokens appear in its filename or path.

**Empty array if intent was `understand`** — read-only sessions do not
produce entanglement signals.

### implementation.approach
**One sentence** describing what was actually built or done. Past
tense. Concrete. No fluff.

### implementation.patterns_used
Architectural patterns the agent **actually applied** in the code it
wrote. Read the diffs, not the intent. Empty array if no structural
pattern applied.

### decisions
Every question the agent asked the developer AND the developer
answered. Weight:

| Weight  | When to use                                                    |
|---------|----------------------------------------------------------------|
| high    | developer answered a design decision affecting future sessions |
| medium  | developer clarified an ambiguity                               |
| low     | developer confirmed something the agent already had right      |

Empty array if the agent asked nothing.

### corrections
Every moment the developer said no, corrected, redirected, or
expressed dissatisfaction. Record `what` (what the agent did) and
`correction` (what the developer said instead).

Intensity scale:

| Range      | Meaning                                           |
|------------|---------------------------------------------------|
| 0.1 – 0.3  | minor, polite                                     |
| 0.4 – 0.6  | clear correction, agent was wrong                 |
| 0.7 – 0.8  | emphatic, frustrated                              |
| 0.9 – 1.0  | hard, caps, repetition, explicit rule             |

Reading guide:

- `"yes"`, `"correct"`, `"exactly"`, `"perfect"` → confirmation, NOT a correction
- `"no"`, `"actually"`, `"not quite"` → mild, ≈ 0.3
- `"that's wrong"`, `"don't do that"` → clear, ≈ 0.6
- `"I've said this"`, `"again"`, `"always"`, `"never"` → strong, ≈ 0.8
- CAPS, multiple punctuation, explicit rules → hard, ≈ 0.95

Empty array if there were no corrections.

### correction_count
Length of the `corrections` array.

### peak_intensity
Maximum `intensity` value across all corrections, or `0.0` if none.

### completed
`true` if the task the developer asked for was finished in this
session. `false` if it was abandoned, blocked, or only partially done.

### lessons
**Most important field.** What should be remembered for next time.

Extract only what the session actually demonstrated. Do not extract
lessons the session did not earn. A read-only review session earns
observations about the codebase. An `act` session with corrections
earns lessons about what approaches work and what do not.

**Observation format — always a tuple:**

```
subject·predicate·object
```

**Scope:**

| Scope                  | When to use                                                |
|------------------------|------------------------------------------------------------|
| project                | specific to this codebase, write to project memory         |
| universal_candidate    | likely true across all projects, flag for graduation       |

**Confidence:**

| Range        | Meaning                                                 |
|--------------|---------------------------------------------------------|
| 0.90+        | developer explicitly stated this as a rule              |
| 0.75 – 0.90  | agent observed this and developer accepted without correction |
| 0.50 – 0.75  | agent inferred this, not directly confirmed            |
| below 0.50   | do not write — too uncertain                            |

**Maximum 8 lessons. Minimum 0** — if the session was routine and
produced nothing new, empty array is correct.

`evidence` is a short string pointing at what in the session supports
the lesson — a quoted phrase, a file name, a pattern observed.

### Significance threshold

Only write lessons if at least one of:

- `correction_count > 0`
- `completed == false`
- `peak_intensity > 0.3`
- `decisions` array not empty
- `files_touched` count > 0
- new observations made that were not in retrieved memories

If none: `lessons` should be empty. The session confirmed existing
memories — that is handled by edge strengthening in `/consolidate`,
not by writing new lessons.

### Synthesis observation

After extracting individual lessons, ask: did this session reveal that
multiple concepts form a connected system worth remembering as a
single insight?

If yes — add one synthesis lesson describing the relationship. This is
not a special type. It is a regular lesson with a tuple that captures
the relationship:

- `drone_system·components·interact_as_connected_family`
- `drone_cost·threshold_and_specialization·are_coupled_concepts`

Only write this if the session genuinely demonstrated the connection
through the work done. Not speculatively.

### Prediction reconciliation

After extracting lessons and any synthesis observation, reconcile the
prediction from `/reflect` against what actually happened during
execution.

Find the `PREDICT` block and prediction JSON from the `/reflect`
output in conversation context. If no prediction exists (e.g.
`/reflect` was skipped), set `prediction_reconciliation` to `null`
and skip this section entirely.

For each prediction element (files, approach, outcome, risks),
compare against what the session actually demonstrated:

| Result    | Meaning                                                     | Lesson confidence | Source memory effect |
|-----------|-------------------------------------------------------------|-------------------|---------------------|
| matched   | prediction matched reality — memories that generated it correct | no lesson | `prediction_confirmed` — small positive delta to source tuples |
| partial   | approximately right — note what was accurate and what was off | 0.70 | `prediction_confirmed` on accurate parts only |
| missed    | something significant happened that was not predicted — gap in memory | 0.80 | no source to penalise — this is a gap, not a wrong model |
| wrong     | prediction was opposite of reality — source memory may be misleading | 0.85 | `prediction_contradicted` — negative delta to source tuples |

**Missed and wrong elements produce lessons.** These are precisely
identified gaps, not general observations. They target exactly where
the model was wrong. Each missed or wrong element becomes a candidate
lesson with the confidence floor above.

**Matched elements do not produce new lessons** but their
`source_tuples` are passed through to consolidate as
`prediction_confirmed` events — the memory fired, predicted
correctly, and reality verified it. This is stronger evidence than
normal retrieval.

**Wrong elements penalise their sources.** The `source_tuples` from
the prediction element are passed through as
`prediction_contradicted` events. Consolidate applies a negative
weight delta to each source tuple. The memory that caused a wrong
prediction should weaken so it surfaces less confidently next time.

**Missed elements have no source to penalise.** A gap in the model
means no memory existed to make a prediction — there is nothing to
weaken. The new lesson fills the gap instead.

**Prediction-derived lessons are additional.** They do not count
against the 8-lesson maximum for regular session lessons. Cap
prediction-derived lessons at 4. If more than 4 mismatches exist,
keep the ones with highest confidence and most specific observations.

**Prediction-derived lessons carry `source: "prediction"`.** This
distinguishes them from regular session observations in the `lessons`
array.

**Cold start prediction reconciliation.** If the prediction had
`cold_start: true`, all elements are inherently `missed` — but this
is expected, not informative. Do not generate lessons from cold-start
prediction mismatches. Instead, set `prediction_reconciliation.cold_start`
to `true` and note that the session seeded initial knowledge.

## Output format — emit BOTH blocks, in this order

First, the human-readable block:

```
LEARN
───────────────────────────────
session:     <session_id> → <timestamp_end>
bead:        <bead_id or none>
task:        <raw_prompt truncated to 80 chars>
intent:      <act | understand>
completed:   <yes | no>
corrections: <N>
cold start:  <yes | no>
memories in: <N>

DECISIONS (<N>)
  [<weight>] <one line summary>
  (— if none)

LESSONS (<N>)
  [<scope> <confidence>] <observation·tuple>
  (— if none or threshold not met)

SURPRISE TOUCHES (<N>)
  <filename>
  (— if none)

PREDICTION RECONCILIATION
  matched:   <N> of <total> predictions correct
  partial:   <element> — <what was accurate, what was off>
             → lesson: <tuple>
  missed:    <element> — <what happened that was not predicted>
             → lesson: <tuple>
  wrong:     <element> — <what was predicted vs what happened>
             → lesson: <tuple>
  overall:   <matched | partial | missed> — <one-line summary>
  (— if no prediction was present, show: "no prediction to reconcile")
───────────────────────────────
```

Then, immediately after, the contract:learn>consolidate JSON block. No prose between
the display block and the JSON. No commentary after the JSON.

```json
{
  "session_id": "...",
  "bead_id": null,
  "project": "...",
  "timestamp_end": "...",
  "raw_prompt": "...",
  "intent": "...",
  "tokens": [],
  "memory_loaded": {
    "memories_loaded": 0,
    "git_refs": 0,
    "was_cold_start": true
  },
  "implementation": {
    "files_touched": [],
    "files_read": [],
    "surprise_touches": [],
    "approach": "...",
    "patterns_used": []
  },
  "decisions": [],
  "corrections": [],
  "correction_count": 0,
  "peak_intensity": 0.0,
  "completed": true,
  "lessons": [],
  "recalled_via_edges": [],
  "prediction_reconciliation": {
    "cold_start": false,
    "elements": [
      {
        "element":       "files | approach | outcome | risks",
        "predicted":     "what was predicted",
        "actual":        "what actually happened",
        "result":        "matched | partial | missed | wrong",
        "source_tuples": ["tuples from prediction that informed this element"],
        "event":         "prediction_confirmed | prediction_contradicted | null",
        "lesson":        "subject·predicate·object or null"
      }
    ],
    "matched_count": 0,
    "total_count":   0,
    "overall":       "matched | partial | missed",
    "summary":       "one-line summary of prediction accuracy"
  }
}
```

`prediction_reconciliation` is `null` when no prediction was present
in the session (e.g. `/reflect` was skipped). When `cold_start` is
`true`, `elements` is empty and no prediction lessons are generated.

## Session persistence

After emitting both blocks, persist the learn contract to durable
session state:

```bash
heb session write <session_id> learn <<'JSON'
<the contract:learn>consolidate JSON you just emitted>
JSON
```

That is the entire response. No preamble. No follow-up questions. The
next pipeline stage consumes the JSON block.
