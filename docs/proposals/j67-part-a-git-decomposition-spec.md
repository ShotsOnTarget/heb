# j67 Part A — Formal Git Decomposition Spec

**Status:** DRAFT, awaiting review
**Issue:** dreadfall-0-j67
**Purpose:** Extract the loose natural-language rules in `.claude/commands/recall.md` for git log token decomposition into a precise specification that both the Go implementation and the current markdown can be verified against. Test fixtures in this document become the Go unit tests.

---

## 1. Input

```
tokens: []string          // Contract 2 tokens array, in order
cwd:    string            // repo root
```

Output is a flat `[]GitRef` where each ref is:

```go
type GitRef struct {
    Hash    string   // short hash
    Subject string   // commit subject line
    Age     string   // relative, from git log --format=%cr
}
```

The pass returns at most `GitRefCap` results, deduplicated by full commit hash. Default `GitRefCap = 10`, trimmed to `3` at the budget step (out of scope for this spec).

---

## 2. Algorithm overview

```
for each token in tokens (in order):
    refs = lookupLiteral(token)
    if len(refs) == 0:
        components = decompose(token)
        refs = lookupDecomposed(components)
    append refs to results
    dedupe results by hash
    if len(results) >= GitRefCap: stop
return results
```

---

## 3. Literal lookup — `lookupLiteral(token)`

Two-step cascade. Both steps run via `exec.Command`, never via shell interpolation.

### Step L1 — file grep + git log on matched files

```
files := grep -rl <token> . --include=*.gd
if len(files) > 0:
    take up to 5 files (alphabetical order)
    return gitLog(files)
```

`gitLog(files)` runs:

```
git log --oneline --format=%h\t%s\t%cr -10 --all -- <file1> <file2> ...
```

Parse stdout line by line, tab-separated into `(hash, subject, age)`.

### Step L2 — fallback message grep

```
if Step L1 produced 0 refs:
    return gitLog(--grep=<token>)
```

`gitLog(--grep=X)` runs:

```
git log --oneline --format=%h\t%s\t%cr -10 --all --grep=<X>
```

If L1 produced >0 refs, **do not** run L2. Literal lookup short-circuits.

---

## 4. Token decomposition — `decompose(token)`

Runs only when literal lookup returned 0 refs.

### 4.1 Split

Split on `_` into a component array. Preserve order.

```
"drone_stats"     → ["drone", "stats"]
"PlayerController" → ["PlayerController"]      // no underscore, single component
"drone_I"         → ["drone", "I"]
"cost"            → ["cost"]                   // no underscore, single component
```

### 4.2 Filter — length

Drop any component with `len(component) < MinComponentLen`. Default `MinComponentLen = 2`. Configurable via `--min-component-len` flag.

Single-character components cannot be searched meaningfully — `grep -rl "I" .` returns the entire codebase. This is a practical search constraint, not a linguistic judgement.

```
"drone_I"        → filter → ["drone"]          // "I" is 1 char, dropped
"level_3"        → filter → ["level"]          // "3" is 1 char, dropped
"ui_map"         → filter → ["ui", "map"]      // both >= 2, kept
"my_profile"     → filter → ["my", "profile"]  // both >= 2, kept
```

**No stopword filter.** A previous draft of this spec had a `Stopwords` map that dropped components like `the`, `and`, `for`, `data`, `info`, `item`, `list`. That filter was removed: it applied linguistic judgements before the repo had a chance to say what was meaningful. A word like `my` looks like noise in English but is the entire signal in a project with a `MyProfile` feature. A word like `the` looks noisy but might be the dominant token in a project whose code references `the_thing` heavily.

The noise cap (§4.3.1) already handles the real problem empirically. If a component matches too many commits it is eliminated because the repo says so, not because the spec assumed so. Remove the pre-emption, trust the repo.

### 4.3 Filter — empty result

If filtering leaves 0 components, decomposition yields nothing and the token produces no git refs.

### 4.4 Single-component result

If filtering leaves exactly 1 component, it is the winner. Skip to §5 with that component.

### 4.5 Multi-component selection

For each surviving component, run the literal lookup cascade (§3 Steps L1 then L2) against that component as if it were a token, but **count the results without emitting them yet**.

Let `counts[i] = len(refs)` for component `i`.

#### 4.5.1 Noise cap

Default `NoiseCap = 10`. Any component with `counts[i] > NoiseCap` is marked **noisy** and is not eligible as the winner, **unless** all other components also have `counts = 0` — in which case the noisy component is used as a last resort, taking the first `NoiseCap` results.

This is the only mechanism for eliminating high-frequency components. It replaces what the stopword list was trying to do in the previous draft.

#### 4.5.2 Zero-result components

Any component with `counts[i] == 0` is eligible but last-priority. A zero-result component can only win if every non-zero component is also zero (i.e. the whole decomposition returned nothing).

#### 4.5.3 Specificity preference (primary rule)

Among eligible, non-noisy, non-zero components, the **winner is the component with the smallest `counts[i]`**.

#### 4.5.4 First-component bias (tiebreak rule)

When two or more eligible components have exactly the same `counts[i]` equal to the minimum, the earlier component (lower index in the split order) wins. If there is a strict minimum — one component with a count strictly less than all others — it wins outright, regardless of how close the next-smallest is.

**No tie window.** A previous draft of this spec used a ±2 window to treat near-ties as ties and apply first-component bias across them. That was removed: it produced counterintuitive results like `[enemy:4, spawn:2, system:9]` selecting `enemy` over the strictly-more-specific `spawn`. Strict-tie-only is simpler, more intuitive, and needs no magic constant.

**Tiebreak examples:**

```
counts = [3, 4, 3]        → component 0 wins   (indices 0 and 2 tied at 3, first wins)
counts = [5, 3, 4]        → component 1 wins   (strict min at 3)
counts = [5, 3, 5]        → component 1 wins   (strict min at 3)
counts = [5, 3, 4, 2]     → component 3 wins   (strict min at 2)
counts = [3, 3, 3]        → component 0 wins   (three-way tie, first wins)
counts = [5, 4, 3, 2]     → component 3 wins   (strict min at 2)
counts = [3, 2, 4, 2]     → component 1 wins   (indices 1 and 3 tied at 2, first wins)
counts = [4, 2, 9]        → component 1 wins   (strict min at 2; "spawn" in Example 5)
```

**Formal statement:**

```
Let M = min(counts) over eligible components.
Let T = {i : counts[i] == M and eligible}.
Winner = min(T)   // smallest index in T
```

Equivalent to: "find the minimum count, collect only components exactly at that minimum, take the first."

---

## 5. Emit refs for winning component

Once a winning component is selected, run the literal lookup cascade **again** for that component (not cached from §4.5 counting) and use those refs as the emitted result for the original token. The reason for re-running: counting in §4.5 only needed the count; emitting requires the full ref records. Implementations may cache if they wish, but the spec treats it as a fresh call for clarity.

---

## 6. Worked examples (Go test fixtures)

Each example lists: input token, what each step produced, and the expected winning refs.

### Example 1 — literal file match

```
token: "PlayerController"
L1: grep -rl PlayerController . --include=*.gd → ["game/player_controller.gd"]
    git log ... -- game/player_controller.gd → 3 refs
L2: not run
decompose: not run
winner refs: the 3 refs from L1
```

### Example 2 — literal message grep fallback

```
token: "refactor_auth"
L1: grep -rl refactor_auth . --include=*.gd → []
L2: git log --grep=refactor_auth → 2 refs
decompose: not run
winner refs: the 2 refs from L2
```

### Example 3 — decomposition, specificity wins

```
token: "drone_stats"
L1: grep -rl drone_stats . → []
L2: git log --grep=drone_stats → 0 refs
decompose:
  split:   ["drone", "stats"]
  length:  both >= 2, kept
  counts:  [drone: 8, stats: 14]
  noise:   stats (14 > 10) → ineligible
  winner:  drone (only eligible)
winner refs: git log refs for "drone" (up to 10)
```

### Example 4 — decomposition, strict minimum

```
token: "player_combat"
L1: 0, L2: 0
decompose:
  split:   ["player", "combat"]
  counts:  [player: 5, combat: 6]
  noise:   neither > 10
  min:     5
  T:       {0}   (only player has count 5)
  winner:  index 0 → "player"
winner refs: refs for "player"
```

### Example 5 — decomposition, strict minimum wins over earlier components

```
token: "enemy_spawn_system"
L1: 0, L2: 0
decompose:
  split:   ["enemy", "spawn", "system"]
  counts:  [enemy: 4, spawn: 2, system: 9]
  noise:   none
  min:     2
  T:       {1}   (only spawn has count 2; 4 and 9 are not at the minimum)
  winner:  index 1 → "spawn"
winner refs: refs for "spawn"
```

Note: `spawn` wins here because it is the strictly most specific component. Under the previous ±2 window design, `enemy` would have won via first-component bias, which was counterintuitive — the more specific component should always win when there is a strict minimum. The strict-tie-only rule (see §4.6.4) resolves this.

### Example 6 — decomposition, noise cap eliminates generic components

```
token: "item_data_cache"
L1: 0, L2: 0
decompose:
  split:   ["item", "data", "cache"]
  length:  all >= 2, kept
  counts:  [item: 34, data: 41, cache: 2]
  noise:   item (34 > 10) → ineligible
           data (41 > 10) → ineligible
  winner:  cache (only eligible, count 2)
winner refs: refs for "cache"
```

The result is the same as the previous stopword-based draft, but the mechanism is correct: the repo said `item` and `data` were noisy because they matched too many commits. The spec did not pre-assume it.

### Example 7 — decomposition, all components pass length filter

```
token: "ui_id_map"
L1: 0, L2: 0
decompose:
  split:   ["ui", "id", "map"]
  length:  "ui"=2 kept, "id"=2 kept, "map"=3 kept
  counts:  [ui: 8, id: 5, map: 3]
  noise:   none (nothing > 10)
  min:     3
  T:       {2}   (only map has count 3; strict tie rule from §4.5.4)
  winner:  index 2 → "map"
winner refs: refs for "map"
```

Under the previous draft, all three components would have been dropped by the length filter (< 4) and this token would have contributed zero refs. Under the new rules (`len >= 2`), all three survive and the noise cap plus strict-tie rule pick the most specific component.

### Example 8 — decomposition, all components zero-count

```
token: "flux_capacitor"
L1: 0, L2: 0
decompose:
  split:   ["flux", "capacitor"]
  counts:  [flux: 0, capacitor: 0]
  min:     0
  T:       {0, 1}   (both strictly tied at 0)
  winner:  index 0 → "flux"   (first-component bias)
winner refs: refs for "flux" (which is 0) → none
```

### Example 9 — all components noisy, fallback

```
token: "game_state"
L1: 0, L2: 0
decompose:
  split:   ["game", "state"]
  counts:  [game: 42, state: 30]
  noise:   both > 10 → both marked noisy
  fallback: nothing else → use noisy set, strict-tie rule still applies
           min of noisy = 30 (state)
           T = {1}  (only state at the minimum)
           winner: "state"
winner refs: first 10 refs for "state"
```

The strict-tie rule applies identically in the noisy-fallback case. No special-casing.

### Example 10 — single-component token, not compound

```
token: "cost"
L1: 0, L2: 2 refs
decompose: not run (literal succeeded)
winner refs: 2 refs from L2
```

### Example 11 — noise cap handles common words

```
token: "the_thing"
L1: 0, L2: 0
decompose:
  split:   ["the", "thing"]
  length:  "the"=3 kept, "thing"=5 kept   (both >= 2)
  counts:  [the: 47, thing: 1]
  noise:   the (47 > 10) → ineligible
  winner:  thing (only eligible)
winner refs: refs for "thing"
```

Under the previous draft `the` would have been dropped by a stopword list. Under the new rules the noise cap eliminates it empirically: the repo said `the` was noisy because it matched 47 commits. Same result, correct mechanism. A different repo where `the` is a meaningful identifier would produce a different outcome — as it should.

### Example 12 — non-default file glob

Verifies the `--file-glob` flag is wired through to the L1 grep invocation. Same algorithm, different file extension.

```
token: "PlayerController"
flag:  --file-glob=*.cs
L1:    grep -rl PlayerController . --include=*.cs
       → ["Scripts/PlayerController.cs"]
       git log ... -- Scripts/PlayerController.cs → 2 refs
L2:    not run (L1 succeeded)
decompose: not run
winner refs: 2 refs from L1
```

This is the fixture that catches the common regression where the flag is parsed but never forwarded to the `exec.Command` argument list.

### Example 13 — component preserved by absence of stopword filter

```
token: "my_profile"
L1: 0, L2: 0
decompose:
  split:   ["my", "profile"]
  length:  "my"=2 kept, "profile"=7 kept
  counts:  [my: 3, profile: 6]
  noise:   neither > 10
  min:     3
  T:       {0}   (only "my" has count 3)
  winner:  index 0 → "my"
winner refs: refs for "my"
```

`my` wins because it is the strictly more specific component in this repo. A stopword list would have eliminated it pre-emptively and forced `profile` to win by default. The noise cap correctly preserves it — the repo said `my` matched only 3 commits, which is more specific than `profile` at 6. This is the canonical case for why stopwords are the wrong mechanism.

---

## 7. Open questions for reviewer

1. **~~Stopword list completeness~~ — resolved by removal.** Previous drafts proposed a stopword map. Removed: it applied linguistic judgements the repo itself was better placed to make. The noise cap (§4.5.1) does the work empirically. No stopword list exists anywhere in the spec or implementation.

2. **`--include=*.gd` hard-coding.** Current markdown hard-codes Godot. Should the Go CLI accept `--file-pattern` (default `*.gd`) or stay hard-coded? I recommend a flag: `--file-glob=*.gd` with that default, because `heb` is meant to be project-agnostic even if `dreadfall-0` is a Godot project.

3. **NoiseCap = 10.** Markdown uses "> 10 results". Flag: `--git-noise-cap=10`. Accept?

4. **~~Tie window = ±2~~ — resolved.** Previous drafts proposed a ±2 window around the minimum for first-component bias. Removed in favor of strict-tie-only (§4.6.4). Example 5 exposed the ±2 window as too permissive (it let `enemy:4` beat `spawn:2` which was counterintuitive). Strict tie is simpler and eliminates the magic constant.

5. **Zero-result handling in tiebreak.** When all eligible counts are 0, spec says the first-index component wins (Example 8). Alternative: skip zero-count components entirely and return nothing. I prefer the current rule because it's deterministic and the caller sees "token tried, nothing found" rather than "token silently skipped".

6. **Re-running lookup in §5.** The spec says "count in §4.5, emit fresh in §5." A caching implementation is allowed but not required. Is that acceptable or should the spec mandate caching?

7. **Order of token processing.** Spec processes `tokens` in order and stops at `GitRefCap`. Alternative: process all tokens, merge, then trim. I prefer in-order-with-early-stop because it's predictable and matches the markdown's "first token" bias.

---

## 8. What this spec does NOT cover

- The 300-token budget trim (separate section of recall.md, covered in j67 Part B).
- The bd list pass (trivial, covered in j67 Part B). The CAPA grep pass was removed from j67 Part B — see bead dreadfall-0-fng for the replacement work on the `/learn` side.
- How these refs are assembled into the Contract 3 JSON (j67 Part B).

### 8.1 Required: null-separator git log output

Git log output parsing **must** use null separators, not tabs. This is a hard requirement, not a recommendation. Commit subjects occasionally contain literal tab characters and will silently corrupt tab-separated parsing.

```
git log --format=%H%x00%s%x00%cr%x00 -z --all ...
```

Implementation: split stdout on `\x00`, group into threes (hash, subject, age), trim the trailing empty string produced by `-z`. The hash is the full SHA; truncate to 7 characters for display.

### 8.2 File-glob asymmetry (L1 vs L2)

The `--file-glob` flag (default `*.gd`) applies only to the L1 file grep step. The L2 message grep (`git log --grep=...`) is always language-agnostic because it searches commit messages, not file contents. This asymmetry is intentional:

- L1 is trying to find files that currently contain the token. Source-only filtering keeps results meaningful (a token appearing in a `.json` config or a `.md` doc is usually noise).
- L2 is trying to find commits whose message mentions the token. Commit messages are language-independent by nature — a developer writing "fix PlayerController crash" doesn't care which language `PlayerController` is in.

A project using mixed languages (e.g. Godot + Rust FFI) can set `--file-glob=*.{gd,rs}` to cover both source trees in L1; L2 needs no configuration.

---

## 9. Acceptance criteria for implementation

The Go implementation is correct iff:

1. All 13 worked examples in §6 produce the expected winner when run against a fixture git repo with the specified commit counts.
2. The algorithm is a pure function of `(tokens, repo_state)` — no hidden state, no randomness.
3. Running the slash command `/recall` on the same input produces the same ref list (modulo ordering within equal-age commits, which git itself may not guarantee).
4. All flags expose their defaults via `heb recall --help`.
5. Git log output parsing uses null separators per §8.1. A test with a commit subject containing a literal tab character passes.
6. Example 12 passes with `--file-glob=*.cs`, verifying the flag is actually forwarded to the L1 `exec.Command` invocation.
7. No `Stopwords` map, slice, or set exists in the implementation. The only components-filtering mechanism is the length filter (§4.2) and the noise cap (§4.5.1).
8. Example 13 (`my_profile`) passes and selects `my` as the winner, verifying the absence of a stopword filter.
