---
description: Heb remember — learn from a session and consolidate into memory in one step
argument-hint: (no arguments — reads the current session)
---

# /remember — Learn + Consolidate in one step

Runs Phase B of the Heb pipeline (learn then consolidate) as a single
uninterruptible flow. Both steps run inline — consolidate is a single
fast bash call, no subagent needed.

## Hard rules

- DO NOT stop between learn and consolidate
- DO NOT wait for user input between steps
- Both steps run in a single response

## Verbosity

Check `heb config get verbosity`. Apply the same rules as other
sub-skills:

- **`[loud]`** — show learn and consolidate output
- **quiet (default)** — 1-sentence summary (e.g. "Learned 3 lessons,
  consolidated 3 new memories.")
- **mute** — emit nothing

## Steps

### Step 1 — Learn (inline)

Invoke the `heb:learn` skill with no arguments. It reads the current
conversation and emits a contract:learn>consolidate JSON object.

```
Skill: heb:learn
```

Capture the contract:learn>consolidate JSON. **This is intermediate data.**
Immediately continue to Step 2.

### Step 2 — Consolidate (inline)

Immediately after Step 1, pipe the contract:learn>consolidate JSON directly
into `heb consolidate`. No session write needed — just pipe it.

```bash
heb consolidate --format json <<'JSON'
<the contract:learn>consolidate JSON, verbatim>
JSON
```

Then close the session:

```bash
heb session close <session_id>
```

## Done when

- `/heb:learn` produced a contract:learn>consolidate JSON
- `heb consolidate` wrote that JSON to the memory graph
- `heb session close` closed the session
- Both steps ran in a single response with no pause between them
