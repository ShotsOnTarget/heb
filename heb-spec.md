  # Heb — Specification

---

## What Heb Is

Heb is persistent weighted memory for AI coding agents. It learns automatically from every session. It gets smarter the more it is used. It never asks the developer to write anything down.

The name comes from Donald Hebb, the neuroscientist who in 1949 proposed: neurons that fire together wire together. That principle is the entire architecture.

---

## The Problem

Every AI coding session starts from zero. The agent does not remember your preferences. It does not remember what worked last time. It does not remember what it got wrong. Every session the developer carries the context the agent should be carrying.

Developers compensate by writing CLAUDE.md files, .cursorrules, AGENTS.md. The AI does not reliably read them. Nothing accumulates. Heb fixes this.

---

## The Core Principle

One rule. Applied consistently. Without exceptions.

**Things that keep being confirmed get stronger.**
**Things that keep being contradicted get weaker.**
**Things that stop being relevant fade.**

The weight of a memory is the truth about that memory. Trust the weight. Never impose a classification the process has not earned.

---

## What Heb Remembers

**Developer preferences** — how this developer likes to build things. These emerge from corrections. Every time the developer says no, do it this way, Heb learns.

**Project patterns** — how this codebase works. What conventions have been established. What the code is for, not where it lives. These emerge from accepted work. Every accepted output strengthens the pattern.

**Mistake patterns** — what goes wrong. What approaches keep failing. What areas produce unexpected corrections. These emerge from correction clusters over time.

**Connected systems** — when a session reveals that multiple concepts interact as a coherent whole, that relationship is itself worth remembering. Not detected algorithmically. Observed by the agent during the work and written as a memory like any other.

---

## What Heb Is Not

Not a linter. Not a style enforcer. Not a documentation system. Not a file index — it remembers what code does, not where it lives. Not a replacement for Claude Code.

It does not classify memories by language, type, or category. It does not impose structure the Hebbian process has not formed naturally. It does not use arbitrary labels for what the weight and provenance already express.

---

## The Memory Model

Every memory is a weighted tuple:

```
SUBJECT · PREDICATE · OBJECT
```

Examples:

```
drone_cost    · expressed_as  · threshold_delta
drone_consts  · defined_as    · typed_dictionary
error_style   · prefer        · explicit_types
randomisation · prefer        · shuffled_pool
drone_system  · interacts_with · station_and_card_systems
```

Memories encode concepts not locations. The concept persists when files move or are renamed.

The weight is the single truth. It encodes how many times confirmed, how consistently, how strongly stated. Weight changes only from events. Never from time passing. A correct two-year-old memory is as strong as a correct two-day-old memory.

Every memory also carries provenance — which project or projects it came from, which sessions, which Beads. Provenance is metadata. It tells where a memory was formed. It does not classify what the memory is.

All memories are the same kind of thing. No special types. One structure, uniform treatment, different weights.

---

## Where Memories Live

```
.heb/memories.json        formed in this project
                          most relevant to current work
                          read first during retrieval

~/.heb/memories.json      confirmed across multiple projects
                          always available to every project
                          ! weight memories always surface
```

A memory graduates from project to parent when it has been confirmed across multiple projects at high weight and the developer approves. Graduation is a single event — the memory moves up, weight updates to `!`, it now surfaces in every future project.

No special names for either store. No somatic. No germline. Just where the memory lives reflecting how universal it has become.

---

## The Weight Events

Every weight change has a reason. Nothing changes silently.

```
created                  first observation
session_reinforced       applied and accepted
session_corrected        approach was wrong
explicit_correction      high intensity, larger delta
prediction_confirmed     prediction matched reality, small positive delta
prediction_contradicted  prediction wrong, negative delta to source memory
code_confirmed           daemon verified against codebase
bead_attached            provenance only, no weight change
graduated                moved to parent store, weight set to !
superseded               replaced by successor
retracted                explicitly wrong
archived                 weight below retrieval threshold
```

The event log is the permanent audit trail.

---

## The Edges

Memories that activate together strengthen their connection. When two memories are both retrieved and both contribute to accepted work the edge between them strengthens. When retrieved together repeatedly the edge triggers spreading activation — retrieving one surfaces the other automatically.

When a session reveals that multiple systems interact the agent writes a synthesis memory during /learn. The synthesis memory connects to its related memories via edges. Spreading activation then surfaces the whole connected picture when any part of it is queried.

Edges are not imposed. They form from experience. The cluster is what strong mutual edges feel like from the outside. No labels needed.

Edge strength changes from events only. Edges strengthen from co-activation. They weaken when connected memories are superseded or retracted.

---

## The Pipeline

One command. Everything else automatic.

```
/think [prompt]

1. /sense        parse the prompt
2. /recall       retrieve relevant memory
3. /reflect      detect conflicts, predict execution outcome
4. execute       act with full context loaded
5. /learn        extract what the session taught
6. /consolidate  write it to the graph
```

The developer notices the agent is smarter than last time. They do not see the pipeline.

---

## Prediction

Before executing, the agent commits to what it expects — what files
will be touched, what approach will work, what the outcome will be.
This prediction is formed purely from retrieved memories. No file
reads, no tool calls. It is the memory graph's model of reality
expressed as a testable statement.

Prediction happens inside `/reflect`, after conflict detection. The
same inputs that detect conflicts also generate predictions. Each
prediction element is tagged with which memory tuples informed it.

After execution, `/learn` reconciles each prediction element against
what actually happened. The reconciliation produces the richest
learning signal in the system:

- **Matched** predictions confirm their source memories via
  `prediction_confirmed` events — small positive weight delta.
- **Wrong** predictions weaken their source memories via
  `prediction_contradicted` events — negative weight delta. The
  memory that caused a bad prediction should surface less
  confidently next time.
- **Missed** predictions (something happened that was not predicted)
  create new high-confidence lessons — these are precisely
  identified gaps in the memory graph.

This closes the Hebbian loop for prediction. Memories that generate
accurate predictions strengthen. Memories that mislead weaken. Over
time the graph self-corrects toward accuracy, not just accumulation.

Cold start sessions produce honest low-confidence predictions rather
than fabricated ones. Cold start mismatches do not penalise anything
— there was nothing to be wrong about.

---

## The CLI

Used primarily by agents. Occasionally inspected by developers.

```
heb init        initialise memory in project
heb recall      retrieve context for a query
heb consolidate write session learning to graph
heb scan        verify memories, seed from history
heb status      graph health and statistics
heb list        browse active memories
heb graduate    approve a memory for the parent store
heb history     view lineage of a memory
heb export      backup
```

JSON in, JSON out. Agent reads stdout. Human summary on stderr. No LLM inside the CLI — pure Go, fast, deterministic. The intelligence stays in Claude Code where it is already running.

---

## The Slash Commands

```
/sense        parse prompt
/recall       retrieve memory context
/reflect      detect conflicts, predict execution outcome
/learn        extract session lessons
/consolidate  write to graph via heb CLI
/think        run the full pipeline
```

Slash commands call the CLI for mechanical operations. The CLI never calls slash commands. One direction only.

---

## The Contracts

Five interfaces between pipeline stages. Each independently testable.

```
contract:developer>sense        raw prompt in
contract:sense>recall           structured query out
contract:recall>reflect         memory context injected
contract:reflect>execute        reconciliation before execution
contract:learn>consolidate      session record out
contract:consolidate>memory     weight updates written
```

The session record captures what was learned, what was corrected, what files were touched including files outside the expected task scope, what Bead was active, and any synthesis observations about connected systems.

---

## What The Agent Does With Memory

Hard constraints — `!` weight — are never violated. They override the prompt itself if necessary. The agent surfaces the conflict explicitly.

Parent store memories with `!` weight are injected unconditionally before every session. They anchor reasoning to established principles and do not recede as the session grows.

Project memories tell the agent what has worked in this codebase. Applied as working knowledge, checked against current code when weight is lower.

Edge-activated memories arrive alongside directly matched memories. When a cluster of strongly connected memories surfaces together the agent reads them as a coherent picture. The edges did the work.

The weight tells the agent how confident to be. It calibrates accordingly.

---

## How Memory Updates

New learning that contradicts established memory creates a successor — not an overwrite. The old memory moves to prior status, stops being reinforced, and moves toward archival. The history of having known something differently is preserved.

The new memory starts weak. It earns strength through confirmation. The transition is gradual and proportional to evidence.

A memory reinforced many times resists supersession. Deeply confirmed memories are structurally stable. Strong evidence is required to displace them.

The developer's prompt is sufficient confirmation for an update. They said it — that is the signal.

---

## How Mistakes Are Remembered

Failure memories are regular memories whose content describes an approach that was corrected. They start at low weight. They surface when directly relevant and signal low confidence in that direction. The weight communicates everything. No special handling needed.

---

## The Beads Integration

Beads is the task plane. Heb is the learning plane.

Beads adds task intent, multi-session continuity, and project history to what Heb can retrieve. Every memory created or reinforced in a session is tagged with the active Bead ID as provenance. When working on a Bead, recall surfaces memories from that Bead's history and its dependencies — the Beads dependency graph becomes a memory retrieval graph.

Beads without Heb is an audit trail with no learning. Heb without Beads is learning with no task context. Neither is complete without the other.

---

## The Scan Daemon

A background process with two jobs.

It verifies existing memories against the codebase. Confirmed memories gain a small weight increase via `code_confirmed`. Contradicted memories are flagged for review. Verification is concept-level — the daemon checks whether the idea the memory describes is still present in the code, not whether specific lines exist in specific files.

It seeds memory from project history. Git commits and Beads history generate the same Hebbian input events that sessions generate. Patterns consistent enough to represent intentional decisions get seeded at low weight, pending session confirmation. A new developer on an established project does not start cold.

---

## The Philosophy

**Let the weights form naturally from experience, trust them completely, and never impose a classification the process has not earned.**

---

## Relationship To Related Projects

```
Beads    task plane      what to do and why
Heb      learning plane  what was learned
Git      truth plane     what actually changed
CAPA     quality plane   what went wrong and what was done
```

---

## Current State

**Working:**

```
/sense /recall /reflect /think /learn /consolidate
edge co-activation and spreading activation
episode short term retrieval
bead id attachment
synthesis memory extraction
prediction and prediction reconciliation
```

**Not yet built:**

```
heb CLI in Go (prediction_confirmed/prediction_contradicted events)
Dolt backend
heb scan daemon
graduation flow
```

**Validated:**

Two sessions on dreadfall-0. Mining drone task. Memory written in session one retrieved correctly in session two. Agent applied cost formula without being told. Correction count dropped. The loop works.

---

## Build Order

```
1   slash commands          done
2   heb CLI in Go           heb consolidate and heb recall first
3   Dolt backend            version-controlled memory
4   scan daemon             passive reinforcement and seeding
5   graduation flow         cross-project promotion
6   Beads integration       dependency graph retrieval
7   collective pool         optional, shared domain knowledge
```
