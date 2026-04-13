---
description: Heb contract:consolidate>memory — consolidate a learn object into the memory graph
---

# /consolidate

Pipes contract:learn>consolidate JSON (from `/heb:learn`) into `heb consolidate`. The CLI
runs the full Hebbian translation — threshold check, memory deltas,
edge co-activation, entanglement signals, episode write — inside a
single SQLite transaction. The slash command is a pipe.

## Hard rules

- DO NOT re-read the conversation — trust contract:learn>consolidate completely
- DO NOT re-derive lessons, thresholds, or deltas — the CLI owns the math
- DO NOT write to `.heb/memory.db` or any episode file directly
- DO NOT call `heb consolidate` more than once per invocation
- DO NOT parse or reshape the CLI output — print it verbatim
- If contract:learn>consolidate is malformed or the CLI exits non-zero, print both
  stdout and stderr exactly as-is and stop

## Bash simplicity rule

Every bash call must be a single simple command. The one exception is
the `heb consolidate` heredoc — that's what this whole command is.

## Input

A contract:learn>consolidate JSON object (the output of `/heb:learn`).

If the contract:learn>consolidate is not available in conversation context (e.g.
after compaction), recover it from session state:

```bash
heb session read <session_id> learn
```

## Action

Run the CLI with the contract:learn>consolidate on stdin via heredoc:

```bash
heb consolidate <<'JSON'
<contract:learn>consolidate JSON, on one or more lines, verbatim>
JSON
```

Defaults match the original markdown spec exactly — no flags needed.
If you want to tune constants for debugging:

```
--new-gain               float   0.72
--reinforce-gain         float   0.08
--co-activation-boost    float   0.06
--entanglement-scale     float   0.05
--entanglement-min       float  -0.08
--entanglement-max       float  -0.02
--predict-confirm-gain   float   0.04    prediction_confirmed delta
--predict-contradict-gain float -0.06   prediction_contradicted delta
--min-confidence         float   0.50
--format                 string  both    "both" | "human" | "json"
--raw                    bool    false   read an explicit Payload instead of contract:learn>consolidate
```

## Verbosity

The orchestrator may pass `[loud]`, `[quiet]`, or `[mute]` as context.
**Default is quiet** — if no verbosity signal is present, behave as
`[quiet]`.

- **`[loud]`** — run `heb consolidate` without `--format` flag and
  print stdout verbatim (human-readable + JSON)
- **`[quiet]` or no signal** — pass `--format json` to
  `heb consolidate`. Do not display the JSON stdout. **Do relay the
  stderr output verbatim** — it now includes the actual memory tuples
  that were learned, reinforced, and skipped, not just counts.
- **`[mute]`** — pass `--format json`. Emit nothing.

## Output

Print the CLI's stdout and stderr verbatim. No preamble, no commentary,
no summarising. Stdout carries the `CONSOLIDATE` display block and/or
the contract:consolidate>memory JSON result (depending on verbosity);
stderr carries a one-line summary.

## Session persistence

After `heb consolidate` completes successfully, close the session:

```bash
heb session close <session_id>
```

## Done when

- `heb consolidate` was invoked once with the contract:learn>consolidate on stdin
- The CLI's stdout and stderr were printed verbatim
- The session was closed via `heb session close`
- No additional writes were made to `.heb/` from the slash command layer
