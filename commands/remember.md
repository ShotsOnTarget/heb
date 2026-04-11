---
description: Heb remember — learn from a session and consolidate into memory in one step
argument-hint: (no arguments — reads the current session)
---

# /remember — Learn + Consolidate in one step

Runs Phase B of the Heb pipeline (learn then consolidate then git
commit) as a single uninterruptible flow. All steps run inline — no
sub-skills are invoked.

## Hard rules

- DO NOT invoke `heb:learn`, `heb:consolidate`, or any other skill
- DO NOT stop between learn, consolidate, and git commit
- DO NOT wait for user input between steps
- All steps run in a single response

## Verbosity

Check `heb config get verbosity`. Apply the same rules as other
sub-skills:

- **`[loud]`** — show learn and consolidate output
- **quiet (default)** — 1-sentence summary (e.g. "Learned 3 lessons,
  consolidated 3 new memories.")
- **mute** — emit nothing

## Steps

### Step 1 — Learn (inline, no skill invocation)

Read the **entire current conversation** from start to finish and
produce a contract:learn>consolidate JSON object. This step runs
inline — do NOT invoke `heb:learn` or any other skill.

#### Recovering contracts from session state

If compaction has removed the sense/recall/reflect contracts from
conversation context, recover them from durable session state:

```bash
heb session list
heb session read <session_id> sense
heb session read <session_id> recall
heb session read <session_id> reflect
```

#### contract:learn>consolidate output shape

```json
{
  "session_id":     "from sense contract",
  "bead_id":        "active bead id or null",
  "project":        "project name",
  "timestamp_end":  "ISO8601 current time UTC — never midnight",
  "raw_prompt":     "original prompt verbatim",
  "intent":         "act | understand | unclear",
  "tokens":         ["from sense contract"],

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
  ],

  "prediction_reconciliation": null
}
```

#### Field rules

**session_id** — copy from sense contract verbatim.

**bead_id** — scan for `bd update`, `bd close`, `bd show` commands.
Take the first bead id found. `null` if none.

**project** — from sense contract.

**timestamp_end** — actual current system time, ISO8601 UTC, never
midnight.

**raw_prompt** — original developer prompt verbatim.

**intent, tokens** — copy from sense contract. Do not re-derive.

**memory_loaded** — from recall contract: `memories_loaded` is the
count of memories, `git_refs` is the count of git commits,
`was_cold_start` is true if both are zero.

**recalled_via_edges** — from recall contract, tuples where
`"source": "edge"`.

**implementation.files_read** — every file the agent opened, read,
searched, or grep'd during execution. Deduplicate alphabetically.
Repo-relative paths.

**implementation.files_touched** — files the agent actually edited or
created. Subset of `files_read`.

**implementation.surprise_touches** — files in `files_read` with no
obvious connection to the tokens. Empty if intent was `understand`.

**implementation.approach** — one sentence, past tense, concrete.

**implementation.patterns_used** — architectural patterns actually
applied. Empty if none.

**decisions** — every question the agent asked AND the developer
answered. Weight: `high` (design decision), `medium` (clarification),
`low` (confirmation).

**corrections** — every developer correction. Intensity: `0.1–0.3`
(minor), `0.4–0.6` (clear), `0.7–0.8` (emphatic), `0.9–1.0`
(hard/caps/repetition).

**correction_count** — length of corrections array.

**peak_intensity** — max intensity, or `0.0`.

**completed** — `true` if the task was finished.

**lessons** — what should be remembered. Tuple format:
`subject·predicate·object`. Max 8. Min 0. Confidence must be ≥ 0.50.

Only write lessons if at least one of: corrections exist, task
incomplete, peak intensity > 0.3, decisions exist, files touched > 0,
or new observations not in retrieved memories.

Scope: `project` (codebase-specific) or `universal_candidate`
(cross-project).

**prediction_reconciliation** — reconcile reflect predictions against
what actually happened. For each element (files, approach, outcome,
risks): `matched`, `partial`, `missed`, or `wrong`. Matched elements
confirm source tuples. Wrong elements contradict source tuples.
Missed/wrong elements produce lessons (max 4, `source: "prediction"`).
Set to `null` if no prediction exists. Cap prediction lessons at 4.

#### Persist learn contract

```bash
heb session write <session_id> learn <<'JSON'
<the contract:learn>consolidate JSON>
JSON
```

**This is intermediate data.** Immediately continue to Step 2.

### Step 2 — Consolidate (inline)

Immediately after Step 1, pipe the contract:learn>consolidate JSON
directly into `heb consolidate`.

```bash
heb consolidate --format json <<'JSON'
<the contract:learn>consolidate JSON, verbatim>
JSON
```

### Step 3 — Git commit (inline)

After consolidation, commit the files that were changed by the agent
while executing the prompt, plus any `.heb/` memory files written by
consolidation. Only stage files the agent touched — not unrelated
working-tree changes that existed before the session.

1. Stage the specific files that were created or modified during
   execution and consolidation. Use `git add <file1> <file2> ...`
   with explicit paths — never `git add -A` or `git add .`.

2. Commit with a message summarising the session's work:

```bash
git commit -m "<summary of what was done in the session>

Memories: <count> learned, <count> consolidated

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

The commit message subject should describe the actual task, not the
pipeline mechanics. Example: `feat: add git commit step to remember`
not `heb: remember session 2026-04-11T15:30:00Z`.

If there are no changes to commit, skip silently — this is not an error.

Then close the session:

```bash
heb session close <session_id>
```

## Done when

- Learn contract was computed inline from the conversation
- `heb consolidate` wrote lessons to the memory graph
- All session work (code + memories) was committed to git (or skipped if clean)
- `heb session close` closed the session
- All steps ran in a single response with no pause between them
- No sub-skills were invoked (no `heb:learn`, no `heb:consolidate` skill)
