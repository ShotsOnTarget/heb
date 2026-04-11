---
description: Heb task pipeline — sense, recall, reflect, execute; learn and consolidate gated on user confirmation
argument-hint: <raw prompt text>
---

# /heb — Heb Task Pipeline

You are running the Heb task pipeline for a developer prompt.
The pipeline has two phases:

- **Phase A — auto** runs in one continuous flow: sense → recall → reflect → execute
- **Phase B — gated** runs only on explicit user confirmation in a later turn: learn → consolidate

Phase A prepares context and produces the actual answer. Phase B
commits the session to memory. **The split exists because memory must
never be written from an unconfirmed session.** Silence is not
acceptance. Absence of confirmation means no commit.

## 🚨 CRITICAL: HALT AFTER EXECUTE 🚨

```
Phase A (auto):
  Step 1 sense → Step 2 recall → Step 3 reflect → Step 4 execute → HALT

Phase B (gated, not in this invocation):
  Step 5 learn → Step 6 consolidate
```

- **DO NOT STOP between steps 1–4.** Intermediate outputs in Phase A
  are INPUT to the next step, not results.
- **DO NOT RUN steps 5 or 6 in this invocation.** They run only when
  the user explicitly asks to commit the session (e.g. "commit that",
  "save it", "/learn", "looks good, remember it", or a later
  `/heb learn`-style command).
- **DO NOT ASSUME silence means confirmation.** If the user's next
  message is unrelated or does not explicitly accept, the session is
  not committed and the memory graph stays as-is.
- **DO NOT WRITE** to `.heb/memories.json`, `.heb/edges.json`,
  `.heb/events.json`, or any episode file during this invocation.
  Phase A is pure read + response.
- **Session state IS allowed** — `heb session start/write/read` calls
  are the exception. Each step persists its contract to the session
  database so it survives compaction and conversation boundaries.

## Hard rules — do not violate

- DO NOT execute the prompt before `/sense`, `/recall`, and `/reflect`
  have all run
- DO NOT run `/learn` or `/consolidate` in this invocation — Phase B
  is explicitly user-gated
- DO NOT write anything to `.heb/` during Phase A
- DO NOT modify, paraphrase, summarise, or "improve" the developer's prompt
- DO NOT skip any Phase A step, even if the prompt looks trivial
- DO NOT pause, stop, or wait for user input between Phase A steps —
  sense, recall, reflect and execute all run in one continuous response
- DO NOT invent context — only use what `/recall` actually returned
- The retrieved context **shapes how** you work; it does NOT replace the
  prompt and it does NOT add scope
- If any Phase A step fails, stop and report — do not improvise

## Input

```
$ARGUMENTS
```

This is the original developer prompt. It is the source of truth for
what must be done. Treat it as immutable.

---

## Phase A — runs automatically in this invocation

### Step 1 — Sense (DO NOT STOP AFTER THIS STEP)

Invoke the `sense` skill (contract:sense>recall) with the original prompt verbatim.

```
Skill: sense
args: <the original prompt, exactly as typed>
```

The sensor will emit a `PARSE RESULT` block and contract:sense>recall JSON. Capture
the JSON object. **This is intermediate data, not a result.** Immediately
continue to Step 2 in the same response. Do not stop. Do not wait. Do not
acknowledge the sense output to the user — just feed it into Step 2.

If the sensor returns malformed JSON, stop and report the sense failure.
Otherwise, proceed to Step 2 immediately and without pause.

---

### Step 2 — Recall (DO NOT STOP AFTER THIS STEP)

Immediately after Step 1 emits its JSON, invoke the `recall` skill
(contract:recall>reflect) with that contract:sense>recall JSON as its argument. Do this in the
same response, with no pause, no commentary, no waiting.

```
Skill: recall
args: <the contract:sense>recall JSON from step 1, on a single line>
```

The recaller will emit a `RETRIEVAL RESULT` block and contract:recall>reflect JSON.
Capture it. **This is intermediate data, not a result.** Immediately
continue to Step 3 in the same response.

If retrieval fails entirely (not cold-start — actual failure), stop and
report. Cold start (`memory_tuples: []`) is a valid state and you must
proceed to Step 3 normally.

---

### Step 3 — Reflect (DO NOT STOP AFTER THIS STEP)

Immediately after Step 2 emits its JSON, invoke the `reflect` skill with
the contract:sense>recall parse and the contract:recall>reflect retrieval bundle as input.

```
Skill: reflect
args: <contract:sense>recall JSON> <contract:recall>reflect JSON>
```

`/reflect` takes the raw retrieval result and reconciles it against the
prompt — surfacing confirmations, extensions, and any conflicts between
existing memories and what the prompt now asks for. It also produces a
prediction block committing to what the agent expects before seeing any
code. Both the reconciliation and prediction are intermediate data that
`/learn` will consume at session end.

Capture the reflect output. **This is intermediate data, not a result.**
Immediately continue to Step 4.

If `/reflect` is not yet available, skip straight to Step 4 and apply
the contract:recall>reflect retrieval directly — do not stall.

---

### Step 4 — Execute the original prompt, THEN HALT

Immediately after Step 3, act on the developer's original prompt. This
is the step that produces the real code / edits / answers.

The original prompt must be executed **exactly as typed** in `$ARGUMENTS`.
Do not rewrite it. Do not narrow it. Do not expand it. Do not turn it
into a clarifying question unless it is genuinely ambiguous in a way that
no amount of retrieved context could resolve.

While executing, apply the retrieved context as **active working memory**
according to these rules:

#### Applying retrieved context

| Context source                              | How to apply                                                                                          |
|---------------------------------------------|-------------------------------------------------------------------------------------------------------|
| **Hard constraints** (tuples starting `!`)  | NEVER violate, under any circumstance. Override the prompt itself if there is a conflict — and say so.|
| **High-weight tuples** (weight > 0.80)      | Treat as strong preferences. Follow unless the prompt explicitly requires otherwise.                  |
| **Other tuples**                            | Treat as defaults. Follow unless a higher-priority signal contradicts.                                |
| **Episode refs**                            | Read recent session lessons and corrections before repeating similar work.                            |
| **Git refs**                                | READ those commits' touched files BEFORE writing any new code in the same area.                       |
| **Beads refs**                              | CHECK these tasks before creating anything new — the work may already exist or be in progress.        |
| **CAPA refs**                               | Read the linked corrective actions before repeating a known-broken pattern.                           |

#### Conflict resolution

If two pieces of retrieved context conflict, priority order is:

1. Hard constraints (`!`) — always win
2. The developer's explicit prompt instructions
3. High-weight tuples (> 0.80)
4. Lower-weight tuples
5. Episode refs
6. Git / beads / CAPA references

If a hard constraint conflicts with the prompt, surface the conflict
explicitly and ask the developer how to proceed. Never silently violate
a hard constraint.

#### Cold start

If `memories` is empty and there are no external refs, just execute
the prompt normally. Cold start is a valid state. Do not stall waiting
for memory that does not exist yet.

#### After execution — HALT

Once execution is complete — the task is done, tests pass, or the
question has been answered — **stop the pipeline**. Do NOT proceed to
Step 5 or Step 6 in this invocation.

End the response with a short acceptance prompt so the user knows how
to commit or discard:

```
───────────────────────────────
Session ready for commit. Memory has NOT been written.

To commit this session to memory, reply with one of:
  • "commit" / "save it" / "looks good, remember it"
  • /learn followed by /consolidate
  • any clear acceptance signal

To discard, say nothing or move on to a new topic. The memory graph
will stay as-is.
───────────────────────────────
```

That is the end of this invocation. The assistant must stop here.

---

## Phase B — gated, runs in a later invocation only

Steps 5 and 6 are documented here so the pipeline shape is visible in
one file, but **they do not run as part of `/heb`**. They run when the
user, in a later turn, explicitly asks to commit the session.

### Step 5 — Learn (gated)

When the user confirms, invoke the `learn` skill (contract:learn>consolidate) with no
arguments — it reads the current conversation, including the halted
Phase A output. If compaction has removed the earlier contracts from
context, `/learn` will recover them from session state using
`heb session read <session_id> <step>`.

```
Skill: learn
```

The learner emits a `LEARN` display block and contract:learn>consolidate JSON
capturing lessons, decisions, corrections, reinforcements, and files
touched. **This is intermediate data, not a result.** Immediately
continue to Step 6 in the same response.

### Step 6 — Consolidate (gated)

Immediately after Step 5 emits its JSON, invoke the `consolidate`
skill (contract:consolidate>memory) with that contract:learn>consolidate JSON as its argument.

```
Skill: consolidate
args: <the contract:learn>consolidate JSON from step 5, on a single line>
```

The consolidator writes to `.heb/memories.json`, `.heb/edges.json`,
`.heb/events.json`, and the episode record, then emits a `CONSOLIDATE`
display block.

Once Phase B runs, the pipeline is done.

---

## Step ordering — non-negotiable

```
Phase A (auto, this invocation):
  sense → recall → reflect → execute → HALT

Phase B (gated, later invocation, only on user confirmation):
  learn → consolidate
```

Never:
- execute before sensing, recalling, and reflecting
- run learn or consolidate in the same invocation as execute
- write to `.heb/` during Phase A
- treat silence or unrelated follow-up as acceptance
- merge or reorder Phase A steps "to save time"

The whole point of the split is that retrieved context is available
**before** the agent starts touching the codebase, and memory is
written **only** after the developer has had a chance to confirm the
session was correct. Skipping the halt defeats the acceptance
principle.

---

## Done when (Phase A)

- Step 1 completed: `/sense` produced a contract:sense>recall JSON for the prompt
- Step 2 completed: `/recall` produced a contract:recall>reflect JSON from that input
- Step 3 completed: `/reflect` produced a reconciliation (or was
  skipped with an explicit note if not yet available)
- Step 4 completed: the original prompt was executed verbatim, with
  retrieved tuples and refs actively shaping the approach
- The acceptance prompt was emitted at the end of Step 4
- **No writes were made to `.heb/` during this invocation**
- Hard constraints respected, conflicts surfaced explicitly if any
- The assistant stopped and did NOT run `/learn` or `/consolidate`

## Done when (Phase B, later invocation)

- User explicitly confirmed acceptance in a subsequent turn
- `/learn` produced a contract:learn>consolidate JSON from the confirmed session
- `/consolidate` wrote contract:learn>consolidate to the memory graph and emitted the
  `CONSOLIDATE` display block

---

Example:

```
/heb review how drone stats are derived from type and cost
```

→ runs `/sense "review how drone stats are derived from type and cost"`
→ runs `/recall <contract:sense>recall JSON>`
→ runs `/reflect <contract:sense>recall> <contract:recall>reflect>`
→ executes the original prompt with retrieved tuples and refs as live
  working memory
→ emits the acceptance prompt and STOPS

Later turn (only if the user confirms):

```
commit
```

→ runs `/learn` on the confirmed session
→ runs `/consolidate <contract:learn>consolidate JSON>` to write the memory graph
