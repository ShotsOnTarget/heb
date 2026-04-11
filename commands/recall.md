---
description: Heb contract:recall>reflect — recall context for a sensed prompt
argument-hint: <contract:sense>recall JSON>
---

# /recall — Heb Context Recaller (contract:recall>reflect)

Takes a contract:sense>recall JSON object and returns a contract:recall>reflect
retrieval bundle. All retrieval — memories, git, beads, decomposition, budget
trim, rendering — is delegated to `heb recall`. The slash command is
a pipe. Do not parse, rerank, or reshape the output.

## Hard rules — do not violate

- DO NOT parse `.heb/memory.db`, `.heb/memories.json`, or any memory
  artefact directly — call `heb recall` instead
- DO NOT add fields, omit fields, or reshape contract:recall>reflect
- DO NOT try to solve the underlying task or comment on it
- DO NOT exceed a single `heb recall` invocation
- Complete in a single response

## Input

contract:sense>recall JSON (from `/sense`):

```
$ARGUMENTS
```

If `$ARGUMENTS` is empty — i.e. `/recall` was invoked without an
explicit contract:sense>recall JSON — first try reading the sense contract
from session state:

```bash
heb session read <session_id> sense
```

If no session_id is available, fall back to reading the most recent
`SENSE RESULT` / contract:sense>recall JSON block from conversation
context. If neither exists, pass `{}` on stdin — `heb recall` will
emit an empty contract:recall>reflect.

## Invocation

One call. Heredoc is the only permitted form for piping stdin.

```bash
heb recall <<'JSON'
<contract:sense>recall JSON here, on any number of lines>
JSON
```

Print the stdout verbatim. Do not touch stderr. Do not re-rank. Do
not add decay. Do not comment.

## Session persistence

After `heb recall` completes, persist the recall contract to durable
session state:

```bash
heb session write <session_id> recall <<'JSON'
<the contract:recall>reflect JSON output from heb recall>
JSON
```

## Done when

- `heb recall` was called exactly once
- stdout was printed verbatim
- the recall contract was written to session state
- no other bash calls were issued (besides session write)
