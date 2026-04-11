# j67 Part B — `heb recall` CLI Absorption Proposal

**Status:** DRAFT, awaiting review
**Issue:** dreadfall-0-j67
**Depends on:** j67 Part A (git decomposition spec) approved first
**Purpose:** Move all mechanical work in `.claude/commands/recall.md` into `heb recall` so the slash command becomes a ~20-line pipe-and-print wrapper. The LLM stops running bash loops; it just invokes the CLI and shows the result.

---

## 1. Current state

`recall.md` is 323 lines. It does five things the LLM should not be doing:

1. Building a heredoc payload from Contract 2 and calling `heb recall` (already in CLI — kept)
2. Running `grep -rl`, `git log`, and `bd list --json` via multiple bash calls per token
3. Decomposing compound tokens and running a fallback grep cascade (see Part A spec)
4. Trimming output to a 300-token budget using a priority order
5. Assembling a two-block output (human-readable block + Contract 3 JSON)

Steps 2–5 are all mechanical. They move into Go. The slash command just reads Contract 2 from context, calls `heb recall`, prints whatever it gets back.

**CAPA removed.** The original `recall.md` has a pass that greps `/code/capa/wiki` for token matches. This pass is deleted — it was approximating "root-cause retrieval" with keyword search over a legacy wiki, and the memory graph now serves that role directly (high-weight correction lessons surface via normal recall). Actual CAPA methodology — root-cause extraction — moves into `/learn` under a separate bead (dreadfall-0-fng).

---

## 2. Target CLI surface

### 2.1 Invocation

```
heb recall < contract2.json
```

Contract 2 JSON on stdin. That's it. No positional args.

### 2.2 Flags with defaults

```
--token-budget       int     300   total output budget in tokens (~4 chars/token)
--git-results        int     3     max git refs after trimming
--git-cap            int     10    max git refs before trimming (cap from Part A §1)
--beads-results      int     2     max beads results after trimming
--capa-results       int     1     max capa results after trimming
--memory-limit       int     15    how many memories to ask the SQLite store for
--min-component-len  int     2     decomposition filter (Part A §4.2) — minimum component length
--git-noise-cap      int     10    decomposition noise cap (Part A §4.6.1)
--file-glob          string  *.gd  file grep pattern for decomposition
--no-external        bool    false skip git/beads entirely, memories only
--format             string  both  "both" | "human" | "json"
```

Defaults match the current markdown behavior exactly (minus CAPA, removed). The `--no-external` flag is a convenience for tests — not required for parity. `--format` lets the slash command request `both` (current behavior) and lets future callers request only one block; notably, future automation that only needs the structured output can use `--format=json` to skip the human block.

### 2.3 Output — matches recall.md §2 output format verbatim

Two blocks on stdout, in this order, separated by a single blank line:

**Block 1 — human-readable**

```
RETRIEVAL RESULT
───────────────────────────────
session_id:  <from input>
project:     <from input>
budget used: <N> / <token_budget> tokens

MEMORIES (<count> entries)
  [match <score>] <tuple>·+<weight>
  [edge  <score>] <tuple>·+<weight>
  ...

GIT (<count> commits)
  <short_hash>  <subject>
  ...

BEADS (<count> tasks)
  <id>  <title>  [<status>]
  ...
───────────────────────────────
```

**Block 2 — Contract 3 JSON**

```json
{
  "session_id": "...",
  "project": "...",
  "token_budget": 300,
  "tokens_used": 0,
  "memories": [
    {
      "tuple": "...",
      "weight": 0.0,
      "source": "match",
      "relevance": 0.0
    }
  ],
  "git_refs": [
    { "hash": "...", "message": "...", "age": "..." }
  ],
  "beads": [
    { "id": "...", "title": "...", "status": "..." }
  ]
}
```

Field rules identical to current recall.md, minus the `capa` array which is removed entirely. The Contract 3 JSON no longer carries a `capa` field — downstream consumers (`/reflect`, `/learn`) must not reference it.

Stderr gets a one-line summary: `recall: N memories, M git, K beads, T tokens used`.

---

## 3. New Go structure

```
heb/cmd/heb/recall.go          // unchanged entry: reads stdin, calls store.Recall
                                //                  then delegates to internal/retrieve
heb/internal/retrieve/
    retrieve.go                 // Run(input Contract2, cfg Config) (Result, error)
    git.go                      // Pass: git refs with decomposition
    beads.go                    // Pass: bd list --json filter
    budget.go                   // Token trimming with priority order
    format.go                   // Block 1 + Block 2 rendering
    decompose.go                // Spec from j67 Part A
    decompose_test.go           // All 11 fixtures from Part A §6
```

`retrieve.Run` is the single public entry point. It is a pure function of `(input, cfg, fs/exec adapters)` so it can be unit-tested without hitting the real filesystem.

---

## 4. Shell-out strategy

All external commands run via `exec.Command`, **never** via `bash -c` or shell interpolation. This eliminates quoting bugs and command injection risk.

```go
// correct
cmd := exec.Command("grep", "-rl", token, ".", "--include="+fileGlob)

// wrong — never do this
cmd := exec.Command("bash", "-c", "grep -rl "+token+" . --include="+fileGlob)
```

All shell-outs are wrapped behind an interface so tests can inject fakes:

```go
type Runner interface {
    Run(name string, args ...string) (stdout []byte, stderr []byte, err error)
}
```

Default implementation wraps `exec.Command`. Test implementation returns canned fixtures keyed by `(name, args)`.

**Failures are silent.** Any non-zero exit from `git`, `grep`, or `bd` produces zero results for that source. The caller never sees the error. This matches the current markdown behavior ("return nothing for that source — never block").

---

## 5. Token budget trim algorithm

Precise restatement of recall.md §3. Defaults: budget = 300, ~4 chars/token.

**Phase 1 — measure.** Render the human-readable block with all current content. Count characters. `tokens_used = ceil(chars / 4)`.

**Phase 2 — if over budget, trim in this order until under budget:**

```
priority  description                                       action
────────  ────────────────────────────────────────────────  ─────────────────────────
1         hard constraints (tuple.subject starts with "!")  NEVER trim
2         match memories score >= 1.0                       trim last, lowest first
3         match memories score < 1.0                        trim from lowest up
4         edge memories (source == "edge")                  trim first, lowest first
5         git refs                                          trim to most recent 3
6         beads refs                                        trim to top 2
```

Processing order: start at priority 6, trim one entry, remeasure, repeat until under budget or nothing left to trim. When priority 6 is exhausted, move to priority 5, and so on up to priority 2. Priority 1 is never touched.

**Hard floor:** if priority 2 is exhausted and the block is still over budget, stop trimming and emit what remains. Budget is a guideline, not a hard cap. This matches the current markdown's silent behavior.

---

## 6. Passes — implementation notes

### 6.1 Memories (already in CLI)

Current `heb recall` already calls `store.Recall(tokens, limit)`. No change needed beyond making `limit` come from `--memory-limit` instead of being hard-coded to 15.

### 6.2 Git

Implements the j67 Part A spec end-to-end. The input is `Contract2.tokens`, the output is `[]GitRef` capped at `--git-cap`.

### 6.3 Beads

```go
out, _ := runner.Run("bd", "list", "--json")
// parse JSON array
// filter: title (case-insensitive) contains any token
// score: number of distinct tokens matched
// sort by score desc, take top --beads-results
```

If `bd` isn't installed, the exec call fails silently and beads returns `[]`. Matches markdown.

### 6.4 ~~CAPA~~ (removed)

The CAPA grep pass is deleted. It was grepping `/code/capa/wiki` for token matches to surface "known root causes" — a keyword search over a legacy wiki. The memory graph replaces this directly: corrections become high-weight lessons via `/learn` → `/consolidate`, and those lessons surface through normal memory recall.

Root-cause extraction (the real CAPA methodology) moves to `/learn` under a separate bead: **dreadfall-0-fng**. See that issue for the `/learn` enhancement proposal.

---

## 7. Slash command rewrite

`.claude/commands/recall.md` drops from 323 lines to roughly this:

```markdown
---
description: Heb Contract 3 — recall context for a sensed prompt
argument-hint: <Contract 2 JSON>
---

# /recall

Takes a Contract 2 JSON object and returns Contract 3. All retrieval
is delegated to `heb recall`. The slash command is a pipe.

## Hard rules

- DO NOT read `.heb/memory.db` directly — call `heb recall`
- DO NOT add fields, omit fields, or reshape Contract 3
- DO NOT try to solve the underlying task
- Complete in a single response

## Input

Contract 2 JSON:

```
$ARGUMENTS
```

If `$ARGUMENTS` is empty, read the most recent `SENSE RESULT` JSON
block from conversation context. If none, emit a parse error Contract 3.

## Invocation

One call. Heredoc is the only permitted stdin-piped form.

```bash
heb recall <<'JSON'
<Contract 2 JSON here, on any number of lines>
JSON
```

Print the output verbatim. No re-ranking. No decay. No commentary.

## Done when

- `heb recall` was called exactly once
- Output was printed verbatim
- No other bash calls were issued
```

That's ~25 lines including YAML frontmatter, code fences, and blank lines. From 323 → ~25 is a **92% reduction** in markdown.

---

## 8. Testing strategy

Three layers:

**Layer 1 — decomposition unit tests.** All 11 fixtures from Part A §6, each asserting the winning component (not the refs). Runner mocked.

**Layer 2 — pass integration tests.** For each pass (git, beads, budget), a table-driven test with a mocked `Runner` that returns canned stdout for specific `(name, args)` tuples. Verifies the pass behaves correctly in isolation.

**Layer 3 — end-to-end test.** A test fixture Contract 2 JSON, a mocked Runner with a realistic set of `bd`/`git` responses, assertion on the full Block 1 + Block 2 output. One happy path, one cold start, one all-external-failing case.

Total target: ~20 test cases. All deterministic, no real filesystem or subprocess calls.

---

## 9. Migration plan

1. Land j67 Part A spec, get reviewer approval (this document).
2. Land j67 Part B proposal (this document), get approval.
3. Implement `internal/retrieve` with TDD: decompose first, then passes, then budget, then format, then e2e.
4. Extend `cmd/heb/recall.go` to route through `retrieve.Run` when external refs are requested. Keep the current memory-only path as a fallback for `--no-external`.
5. Rebuild and install `heb.exe`.
6. Rewrite `.claude/commands/recall.md` to the ~25-line shape above.
7. Run `/heb` end-to-end on a sample prompt, diff the new output against the current output. They should match modulo ordering ties.
8. Commit.

Single bead closed at the end: j67.

---

## 10. Risks

- **Slash command / CLI divergence during testing.** If the Part A spec is wrong and the Go implementation matches the spec but the markdown doesn't, we get a silent mismatch. Mitigation: review Part A before writing Go.
- **Platform drift.** `grep`, `git`, and `bd` behave slightly differently on Windows/msys vs Linux. Mitigation: the Runner interface makes this testable; any platform-specific fixup goes in one place.
- **Budget trim producing empty output.** If a cold-start session has 0 memories and all externals fail, the block is still "under budget" and emits mostly `no matches` lines. That's fine — matches current markdown.

---

## 11. Acceptance criteria

Implementation of j67 is complete when:

1. `heb recall < contract2.json` emits Block 1 + Block 2 that is **semantically equivalent** to the current markdown output — same entries in the same priority order, modulo wall-clock age strings and same-score ordering ties (git commit order within equal timestamps, beads score ties, edge memories at equal weight).
2. All 11 Part A fixtures pass as Go unit tests.
3. The slash command `recall.md` is ≤30 lines of non-frontmatter content.
4. `heb recall --help` documents every flag with its default.
5. The `/heb` Phase A pipeline still runs end-to-end against the current `.heb/memory.db`.
6. All existing behavior from `.claude/commands/recall.md` sections on retrieval passes, budget, cold start, and output format is implemented in Go.
7. Running `heb recall --no-external` produces output identical to running `heb recall` with all external commands (`git`, `bd`, `grep`) returning exit code 1. The `--no-external` flag is not a code path shortcut — it is equivalent to all externals failing silently. Verified by a test that compares the two output paths on the same input.
