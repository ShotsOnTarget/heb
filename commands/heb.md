---
description: Heb task pipeline — recall then execute; remember gated on user confirmation
argument-hint: <raw prompt text>
---

# /heb — Heb Task Pipeline

You are running the Heb task pipeline for a developer prompt.
The pipeline has two phases:

- **Phase A — auto** runs in one continuous flow: recall → execute
- **Phase B — gated** runs only on explicit user confirmation in a later turn: remember

Phase A prepares context and produces the actual answer. Phase B
commits the session to memory. **The split exists because memory must
never be written from an unconfirmed session.** Silence is not
acceptance. Absence of confirmation means no commit.

## 🚨 CRITICAL: HALT AFTER EXECUTE 🚨

```
Phase A (auto):
  Step 1 recall → Step 2 execute → HALT

Phase B (gated, not in this invocation):
  Step 3 remember
```

- **DO NOT STOP between steps 1–2.** Intermediate outputs in Phase A
  are INPUT to the next step, not results.
- **DO NOT RUN step 3 in this invocation.** It runs only when
  the user explicitly asks to commit the session (e.g. "commit that",
  "save it", "/learn", "looks good, remember it", "/remember", or a
  later `/heb remember`-style command).
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

- DO NOT execute the prompt before `/heb:recall` has run
- DO NOT run `/heb:remember`, `/heb:learn`, or `/heb:consolidate` in
  this invocation — Phase B is explicitly user-gated
- DO NOT write anything to `.heb/` during Phase A
- DO NOT modify, paraphrase, summarise, or "improve" the developer's prompt
- DO NOT skip any Phase A step, even if the prompt looks trivial
- DO NOT pause, stop, or wait for user input between Phase A steps —
  recall and execute both run in one continuous response
- DO NOT invent context — only use what `/heb:recall` actually returned
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

## Step 0 — Verbosity check (run FIRST, before anything else)

Before starting Phase A, read the verbosity config:

```bash
heb config get verbosity
```

The result is one of: `loud`, `quiet`, `mute`. Apply the rules below
for the entire pipeline run (Phase A and Phase B):

### loud

Everything as-is. All display blocks emitted. All human-readable
output from sub-skills and the Go binary shown to the user. This is
the original behavior — change nothing.

### quiet (default)

- **No display blocks, no JSON on the terminal.** Suppress
  `RETRIEVAL RESULT`, `REFLECT`, `PREDICT`, `LEARN`,
  `CONSOLIDATE` blocks and all contract JSON. The user never sees
  pipeline internals.
- **Contracts are internal working memory.** The agent computes them,
  persists them to session state via bash commands, and passes them
  between steps — but never displays them as text output.
- **Each sub-skill emits a 1-sentence summary** of what it did. That
  single sentence is the only terminal output from recall and
  remember. Example: "Sensed act intent, recalled 5 memories, no
  conflicts." or "Learned 3 lessons, consolidated 3 new memories."
- **After Step 2 (execute):** the execute step produces its normal
  output (code edits, explanations, etc.), then emits the acceptance
  prompt. Quiet mode only silences the pipeline scaffolding, not the
  actual work.

### mute

- **Emit NOTHING from the pipeline to the terminal.** No display
  blocks, no JSON, no summaries, no single-sentence lines. Complete
  silence from the pipeline.
- **The execute step itself** still produces its normal output — mute
  only silences the pipeline, not the work product.
- **Do NOT emit the acceptance prompt.** The session is still
  committable — the user knows they can say "commit" — but do not
  print the banner.

### Applying verbosity to sub-skills

When invoking sub-skills (`/heb:recall`, `/heb:remember`),
**prepend the verbosity level as a prefix** in the skill args so each
skill knows whether to suppress its display blocks:

- In `loud` mode: prepend `[loud] ` to the skill args
- In `quiet` or `mute` mode: no prefix needed — **sub-skills default
  to quiet**. You may optionally prepend `[quiet] ` or `[mute] ` but
  it is not required.

Sub-skills default to quiet because agents commonly forget to pass
the prefix. The safe default is silence. Only `[loud]` needs to be
explicitly passed.

**In addition**, in `quiet` or `mute` mode: do not display the JSON
output from sub-skills to the user either. Capture it silently and
pass it to the next step. The user should see nothing from the
pipeline scaffolding — only the execute step's actual work product.

---

## Phase A — runs automatically in this invocation

### Step 1 — Recall (DO NOT STOP AFTER THIS STEP)

Invoke the `heb:recall` skill with the original prompt verbatim. This
skill handles sensing (parsing the prompt into tokens and intent),
retrieval (querying the memory graph), and reflection (reconciling
memories against the prompt and forming predictions) — all in one step.

```
Skill: heb:recall
args: [quiet] <the original prompt, exactly as typed>
       ^^^^^^ only if verbosity is quiet; use [mute] if mute; omit if loud
```

The recaller will parse the prompt, start a session, retrieve context,
reconcile memories, form predictions, and persist all contracts. Capture
the output. **This is intermediate data, not a result.** Immediately
continue to Step 2 in the same response.

🚨 **When the Skill tool returns from `heb:recall`, you are NOT done.**
The recall result is input to execution. Your very next action MUST be
to execute the original developer prompt. Do not emit a response to the
user. Do not stop. Do not wait.

If retrieval fails entirely (not cold-start — actual failure), stop and
report. Cold start (`memory_tuples: []`) is a valid state and you must
proceed to Step 2 normally.

---

### Step 2 — Execute the original prompt, THEN HALT

Immediately after Step 1, act on the developer's original prompt. This
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

#### Reflect reconciliation

If `/heb:recall` detected conflicts, the prompt wins. Execute with the
prompt's values, not the conflicting memory's values. The conflict was
already recorded — `/heb:remember` will handle creating successor
memories at commit time.

If `/heb:recall` detected extensions, note them as additional context
but do not expand scope beyond what the prompt asks for.

#### Cold start

If `memories` is empty and there are no external refs, just execute
the prompt normally. Cold start is a valid state. Do not stall waiting
for memory that does not exist yet.

#### After execution — HALT

Once execution is complete — the task is done, tests pass, or the
question has been answered — **stop the pipeline**. Do NOT proceed to
Step 3 in this invocation.

End the response with a short acceptance prompt so the user knows how
to commit or discard:

```
───────────────────────────────
Session ready for commit. Memory has NOT been written.

To commit this session to memory, reply with one of:
  • "commit" / "save it" / "looks good, remember it"
  • /remember
  • any clear acceptance signal

To discard, say nothing or move on to a new topic. The memory graph
will stay as-is.
───────────────────────────────
```

That is the end of this invocation. The assistant must stop here.

---

## Phase B — gated, runs in a later invocation only

Step 3 is documented here so the pipeline shape is visible in one
file, but **it does not run as part of `/heb`**. It runs when the
user, in a later turn, explicitly asks to commit the session.

### Step 3 — Remember (inline, gated)

When the user confirms, invoke the `heb:remember` skill. It runs
learn, consolidate, and git commit as a single uninterruptible flow
— all inline, no sub-skills invoked:

```
Skill: heb:remember
```

`/heb:remember` reads the current conversation, extracts lessons
inline, pipes them to `heb consolidate`, commits agent-touched files
to git, and closes the session. If compaction has removed earlier
contracts from context, it recovers them from session state using
`heb session read`.

Once Phase B runs, the pipeline is done.

---

## Step ordering — non-negotiable

```
Phase A (auto, this invocation):
  recall → execute → HALT

Phase B (gated, later invocation, only on user confirmation):
  remember
```

Never:
- execute before recall has run
- run remember in the same invocation as execute
- write to `.heb/` during Phase A
- treat silence or unrelated follow-up as acceptance
- skip the recall step "to save time"

The whole point of the split is that retrieved context is available
**before** the agent starts touching the codebase, and memory is
written **only** after the developer has had a chance to confirm the
session was correct. Skipping the halt defeats the acceptance
principle.

---

## Done when (Phase A)

- Step 1 completed: `/heb:recall` sensed the prompt, retrieved context,
  and reconciled memories — producing sense, recall, and reflect contracts
- Step 2 completed: the original prompt was executed verbatim, with
  retrieved tuples and refs actively shaping the approach
- The acceptance prompt was emitted at the end of Step 2
- **No writes were made to `.heb/` during this invocation**
- Hard constraints respected, conflicts surfaced explicitly if any
- The assistant stopped and did NOT run `/heb:remember`

## Done when (Phase B, later invocation)

- User explicitly confirmed acceptance in a subsequent turn
- `/heb:remember` ran learn + consolidate in a single flow
- `heb session close` closed the session

---

Example:

```
/heb review how drone stats are derived from type and cost
```

→ runs `/recall "review how drone stats are derived from type and cost"`
  (senses, retrieves, and reconciles in one step)
→ executes the original prompt with retrieved tuples and refs as live
  working memory
→ emits the acceptance prompt and STOPS

Later turn (only if the user confirms):

```
commit
```

→ runs `/heb:remember` (learn + consolidate + session close)
