# /remember — Learn + Consolidate in one step

Runs Phase B of the Heb pipeline (learn then consolidate) as a single
uninterruptible flow. This exists because Phase B must never stop
between learn and consolidate — `/learn` output is intermediate data,
not a result.

## Hard rules

- DO NOT stop between learn and consolidate
- DO NOT wait for user input between steps
- Both steps run in a single response

## Steps

### Step 1 — Learn

Invoke the `learn` skill with no arguments. It reads the current
conversation and emits a contract:learn>consolidate JSON object.

```
Skill: learn
```

Capture the contract:learn>consolidate JSON from the output. **This is intermediate
data.** Immediately continue to Step 2.

### Step 2 — Consolidate

Immediately after Step 1 emits its JSON, invoke the `consolidate`
skill with the contract:learn>consolidate JSON as its argument.

```
Skill: consolidate
args: <the contract:learn>consolidate JSON from step 1, on a single line>
```

Print the consolidate output verbatim.

## Done when

- `/learn` produced a contract:learn>consolidate JSON
- `/consolidate` wrote that JSON to the memory graph
- Both steps ran in a single response with no pause between them
