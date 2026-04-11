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

- `/heb:learn` produced a contract:learn>consolidate JSON
- `heb consolidate` wrote that JSON to the memory graph
- All session work (code + memories) was committed to git (or skipped if clean)
- `heb session close` closed the session
- All steps ran in a single response with no pause between them
