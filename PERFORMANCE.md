# rohy Performance Guide

A developer guide for keeping rohy fast, written from measurements taken against
rohy's own workload rather than from general advice.

rohy sits on GrapheneDB, whose `API_REFERENCE.md` ships an excellent performance
guide. **This document does not replace it — it records where rohy's measured
behaviour agrees with it, where it does not, and why.** Where the two conflict,
the numbers in here won.

---

## 1. The one rule

> **Measure rohy's paths. Upstream benchmark ratios describe upstream's
> fixtures, not ours.**

Every rule below exists because a measurement produced a surprise. The most
expensive mistake made so far — a change that made cold start **10× slower** —
came from adopting a documented optimisation on the strength of the
documentation's numbers rather than rohy's. See §9.

Two corollaries, both learned the hard way:

- **A performance claim is only as good as the paths your benchmark set
  covers.** "No regression on any measured path" is worthless if the regression
  is on an unmeasured one. Before claiming a change is neutral, ask which paths
  the benchmarks _do not_ touch.
- **Benchmark the backend the user runs.** rohy runs on the disk backend.
  In-memory benchmarks understate write-path changes badly, because commit count
  only costs on disk.

---

## 2. rohy's cost model

Know what actually dominates before optimising anything.

| Layer                           | What dominates                                                                                        | What does _not_                                                                    |
| ------------------------------- | ----------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| **Event queries**               | Decoding matched event records — now the scalar fields only; the raw record moved to the payload cold store (§14). | Locating candidate ids. The index lookup was never the bottleneck.                 |
| **Ingest / graph build (disk)** | The number of durable commits. Every commit must reach the write-ahead log before it returns.         | Property encoding. Ingest is write-bound, not CPU-bound.                           |
| **Store open**                  | Loading the property index. Cost scales with _distinct values_, not entry count.                      | Loading the graph itself — linear and cheap (~131 ms at 100k events) — and the raw record, which is no longer resident. |
| **Ordering / paging**           | Decoding a minimal per-node view to sort.                                                             | Re-querying: the id order is cached against filter + store version.                |

The single most useful consequence: **in rohy, making a lookup asymptotically
faster usually buys nothing, because the lookup is not the cost.** Reducing how
many records get decoded, or how many commits get made, usually buys a lot.

---

## 3. Reads

### Do

- **Ask for ids, not records**, when you only need ids — `QueryNodeIDs` over
  `QueryNodes`, `BFSIDs` over `BFS`. Records copy property blobs; ids do not.
- **Use `Degree(id, nil)`** where an untyped count will do. The typed form is
  ~488× dearer because it must inspect every incident edge's labels.
- **Pass `nil` edge types to `EdgesOf`.** `consts.EdgeRelation` is the _only_
  edge label rohy writes, so filtering by it selects the same set while forcing
  label inspection. ⚠️ If a second edge type is ever introduced, every one of
  these must filter again.
- **Use `EdgeExists` / `IsConnected`** for boolean questions, instead of
  materialising an edge list or a path and testing its length.
- **Scope `FindPatterns`** — it is the most expensive path in the engine, and
  scope size drives cost directly.
- **Prefer ascending order.** Ascending over an already-ascending candidate set
  skips sorting; descending costs a linear reverse.

### Don't

- **Don't add a per-row query to decorate a list.** The events list is
  virtualised and can render hundreds of rows; anything per-row must be answered
  from one batched call (`RelationsAdjacency` is the pattern to copy).
- **Don't optimise a read path you have not ranked.** Rank against the benchmark
  set first.

---

## 4. Writes

### Choosing the call

This mirrors the upstream decision table, resolved for rohy's actual shapes:

| Situation                                  | Use                   | rohy example                                                     |
| ------------------------------------------ | --------------------- | ---------------------------------------------------------------- |
| Many nodes, no edges                       | `AddNodes`            | `InsertEvents`                                                   |
| Many edges, endpoints already exist        | `AddEdges`            | `InsertRelations`                                                |
| One record, latency-sensitive              | `AddNode` / `AddEdge` | `InsertRelation` (manual UI relation), live capture              |
| Nodes **and** their edges together         | `Begin` / `Commit`    | _(none today — ingest makes only nodes, graph build only edges)_ |
| Several related entities deleted           | `Begin` / `Commit`    | `DeleteGraphRelations`                                           |
| A multi-step edit that must not half-apply | `Begin` / `Commit`    | `IncrementDedupCounts`                                           |

`Begin` and the slice APIs commit through the same path and **cost the same**.
Choose `Begin` for _semantics_ — atomicity across a node/edge boundary or a
multi-step edit — never in the hope of speed.

### Rules

- **Batch, and chunk the batch.** A batch buffers in memory until commit. Chunk
  size bounds that memory _and_ is the granularity at which long work notices
  cancellation. See `consts.RelationBatchSize`, `consts.EventBatchSize`.
- **Register index properties per entity, not per key** —
  `IndexNodeProperties(id, map)`, never a call per key.
- **Use `UpdateNodeIndexed` / `UpdateEdgeIndexed`, never update-then-reindex.**
  The default reindex policy is _keep_, so a plain update leaves index entries
  **stale** — the old value still matches. This has already caused one real bug:
  `graph_id` is indexed, so a relation moved between graphs stayed findable
  under its old graph.
- **Index registration is not part of a transaction.** A `Tx` buffers record
  writes only. Register index entries _after_ commit, and know what a crash in
  that gap leaves behind. For graph builds the answer is the idempotent rebuild;
  if you add a path where it is not, say so in a comment.
- **`Compact()` between phases of work, never per write.** It takes an exclusive
  lock.

---

## 5. Indexing

### The rule

> Index a key when it removes a scan from a path you have **measured**, and you
> can afford the write and memory cost on every mutation touching it.

Storing a value in the property blob is free. Indexing it is not: roughly 93–163
B per entry, and index loading is what dominates store open.

### What rohy indexes today, and why

Node keys: `event_id`, `timestamp`, `provider`, `channel`, `user`, `computer`,
`hash_normalized`, `search_blob`, `source_type`. Edge keys: `relation_type`,
`created_by`, `graph_id`.

All are matched with `Equal` (O(1) postings lookup) except:

- **`timestamp`** — range filters. See §9 for why it is _not_ declared ordered.
- **`search_blob`** — `Contains` only.

### Recorded deviation: `search_blob`

Upstream §19.4 says _do not index a key you only ever use with `Contains`_ — no
index can help, and you pay write and memory cost for nothing.

**rohy indexes it anyway, and must.** The residual filter matches against the
**indexed value**; the property blob is opaque to the storage layer. Removing
the index would not make search slower — it would stop search working. The
upstream advice assumes an alternative that does not exist here.

### Before adding an indexed key

1. Which query drives from it? If none, don't index it.
2. Is it selective? A low-cardinality key matching half the graph will be
   ignored by the planner anyway.
3. Is it `Contains`-only? Then it is a functional requirement, not an
   optimisation — say so in the comment.
4. **Re-run the open benchmarks.** A near-unique key is the expensive kind.

---

## 6. Lifecycle: open, close, compact

Store open is the slowest thing rohy does. Everything here is about keeping it
off the path to first paint.

- **`OpenLazy` + `Warm` is load-bearing, not decoration.** `OpenLazy` costs ~180
  ns; the real open costs 58 ms (10k) to 770 ms (100k). It moves the entire cost
  off first paint. **Never replace a lazy open with an eager one for
  convenience.**
- **Bind anything that must happen on open to `ensure()`**, the single funnel
  every accessor passes through. Multiple open paths exist (`Open`, `OpenLazy`,
  `OpenInMemory`) and the failure mode is a path that forgets.
- **Compaction speeds up the _next_ open by ~38%** (58 ms → 36 ms at 10k).
  Compact after bulk ingest and before shutdown, not per write.
- **Close is cheap** (~0.5–0.9 ms) — no need to defer or background it.
- **Open scales with the property index**, roughly linearly once no key is
  declared ordered. Watch it when adding keys.

---

## 7. Diagnostics

**Do not guess — ask the planner.**

```go
plan, _ := g.ExplainNodeQuery(q)   // or ExplainEdgeQuery
```

`store.QueryPlan` is a struct (`Driver`, `DriverKey`, `Candidates`, `Residuals`,
`Results`) with a one-line `String()`. **Assert on the typed fields in tests,
not on substrings.**

| What you see                       | Meaning                    | Action                                                                 |
| ---------------------------------- | -------------------------- | ---------------------------------------------------------------------- |
| `driver=scan`                      | Nothing bounded the query  | Index a key you filter on                                              |
| `driver=labels`, huge `candidates` | The label is unselective   | Add a selective property filter                                        |
| `residual=k:set~<big>`             | A filter built a large set | Usually a range on an undeclared key — **but see §9 before declaring** |
| `candidates` ≫ `results`           | Weak driver                | A different key would drive better                                     |
| `candidates` ≈ `results`           | Driver is working          | Look at record materialisation or call volume instead                  |

rohy's time-range plan today is `driver=labels ... residual=timestamp:set~N`,
which the table above would tell you to fix by declaring the key. **That is the
one case where we deliberately do not follow it** — §9.

`VerifyIndexes()` checks index _structure_, not cost and not value correctness
(indexed values are caller-encoded and opaque). It is a correctness tool, not a
performance one.

---

## 8. Benchmarking

### Running

```
go test ./backend/graphene/ -run XXX -bench . -benchmem -count=5
```

Use `-count=5` and read the spread — the first run of a fixture includes setup
and is not representative. For open benchmarks use `-benchtime=Nx`, since each
iteration seeds a store.

### Coverage groups

| File                               | Covers                                                                                                   |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `graphene_bench_test.go`           | Unfiltered paging: first page, scroll, count                                                             |
| `graphene_adoption_bench_test.go`  | Time-range queries, relation writes (memory **and** disk), graph clear, adjacency                        |
| `graphene_lifecycle_bench_test.go` | Open (compacted/uncompacted, 10k–100k), close, compact, warm, lazy open, plus index-attribution controls |

### The coverage obligation

When you report a performance result, **state which paths the benchmarks covered
and which they did not.** The M2 regression (§9) was reported as "no regression
on any measured path", which was true and thoroughly misleading: no benchmark
covered store open, which is exactly where the regression was.

### Attributing a cost

When a cost is surprising, isolate it with controls rather than reasoning about
it. The open-cost investigation used four: no index at all, all keys undeclared,
one key removed at a time, and low-cardinality keys only. The first hypothesis
(that one expensive key was responsible) was wrong, and only the controls showed
it.

---

## 9. Case study: the ordered-key reversal

**Read this before declaring an ordered property.** It is the most instructive
thing in this document, and the change it describes looks correct from every
angle except the measured one.

**The reasoning, which was sound.** rohy runs range filters on `timestamp`.
Upstream says a declared ordered key answers ranges by binary search instead of
scanning every entry under the key, quoting 9.4 ms → 1.2 ms wide and 8.6 ms → 17
µs narrow. rohy's timestamp index value is fixed-width UTC, so byte order
already equals chronological order — the precondition is satisfied. The query
plan even said `residual=timestamp:set~N`, which the diagnostic table reads as
"declare it". Everything pointed the same way.

**What it cost.** Open, at 100 000 events:

| Configuration                | Open        |
| ---------------------------- | ----------- |
| `timestamp` declared ordered | **~7.98 s** |
| Undeclared                   | **~0.77 s** |

A declaration is **runtime state that does not survive a reopen**. It must
therefore re-absorb every already-registered entry on _every_ open, and for a
near-unique key that absorption dominates startup and grows faster than linearly
— 76× the time for 10× the data, while allocations grew only 10×.

**What it bought.** Measured on a 20k-event store:

| Query              | Declared     | Undeclared   |
| ------------------ | ------------ | ------------ |
| Narrow time range  | 1.63–1.85 ms | 1.71–1.91 ms |
| Wide time range    | 10.6–12.4 ms | 10.1–12.4 ms |
| Count over a range | ~0.55 µs     | ~0.55 µs     |

Nothing. Because (§2) rohy's range queries are dominated by decoding matched
records, not by finding them. The optimisation improved a step that was never
the bottleneck, and charged every launch for it.

**The lessons.**

1. An optimisation's cost and its benefit often land on **different paths**.
   Benchmark both.
2. "Runtime state re-established on open" is a cost multiplied by every launch,
   forever.
3. Upstream ratios are measured on fixtures whose cost model may differ from
   yours. Here, upstream's range queries were index-bound; rohy's are
   hydration-bound. Same call, opposite conclusion.

**Current state:** no ordered properties are declared.
`TestNoOrderedPropertiesDeclared` asserts it, so re-adding one is a conscious
act rather than a plausible-looking optimisation. If record hydration ever gets
much cheaper, or ranges run over a far larger store, re-measure — **and measure
open, not just the query.**

---

## 10. Upgrading GrapheneDB

The v0.2.0 → v0.3.0 upgrade produced one trap worth generalising.

**A changed signature can hide a changed contract.** `GetNodes` went from two
return values to three. The compiler caught the arity — but the _contract_ also
changed: v0.2.0 returned a slice positionally aligned with the requested ids and
errored on the first missing node; v0.3.0 returns a compacted found-set plus a
separate missing-id list, and does not error.

The obvious fix (add a third variable, discard `missing`) compiles, passes every
existing test, and silently turns index/store divergence into a short page in
the events list.

**Upgrade procedure:**

1. **Move the version alone.** No capability adoption in the same step, or no
   regression is attributable.
2. Diff the API by **signature**, not by name. Names alone reported this upgrade
   as "purely additive"; it was not.
3. Check **non-API behaviour** too — default policies especially. The new
   reindex policy defaults to _keep_, which happened to match the old implicit
   behaviour; had it defaulted to _purge_, existing code would have silently
   started losing index entries.
4. For each compile break, ask **"did the contract change, or just the shape?"**
   Preserve the old contract unless changing it is a deliberate, separate
   decision.
5. Re-run the **full** benchmark set — including lifecycle — before and after.

---

## 11. Checklist for a persistence-touching change

**Correctness**

- [ ] Does any indexed value change? If so, use the `*Indexed` update form.
- [ ] Does the write path call `bumpVersion()`? The ordering cache must never
      survive a write.
- [ ] If a `Tx` is used: are index registrations placed after commit, and is the
      crash gap documented?
- [ ] Does a partial failure leave a state some other code would mistake for a
      complete one?

**Performance**

- [ ] Which benchmark covers this path? If none, add one before claiming an
      improvement.
- [ ] Does it change what happens on **open**? Re-run the lifecycle benchmarks.
- [ ] Does it add per-row work to a virtualised list?
- [ ] Is a new indexed key justified by a query that drives from it?
- [ ] If it adds records-where-ids-would-do, is that deliberate?

**Reporting**

- [ ] State which paths the benchmarks covered and which they did not.
- [ ] Report disk numbers for write-path claims, not in-memory ones.
- [ ] If a result contradicts upstream guidance, record the deviation and the
      numbers.

---

## 12. Measured reference

⚠️ **Ratios, not promises.** Measured on one machine (AMD Ryzen 9 5980HS,
Windows, 16 logical cores) against synthetic fixtures. Differences under ~25%
are not resolvable here. Use these to spot order-of-magnitude changes, not to
validate micro-optimisations. Re-measure locally before drawing conclusions.

**Queries** — 20 000 events, in-memory, ~1.6 KB `RawXML` each:

| Path                            | Time     | Allocations       |
| ------------------------------- | -------- | ----------------- |
| First page (`EventBatchSize`)   | ~15 ms   | ~2.98 MB / ~14.3k |
| Scroll (paged, same filter)     | ~14.8 ms | ~2.92 MB / ~14k   |
| Count with search filter        | ~0.42 µs | 129 B / 5         |
| Narrow time range (~100 of 20k) | ~1.7 ms  | ~0.55 MB / ~2.2k  |
| Wide time range (~half)         | ~11 ms   | ~2.9 MB / ~12.5k  |
| Count over a time range         | ~0.55 µs | 304 B / 8         |

**Relation writes** — 1 000 relations:

| Path                             | Disk         | In-memory |
| -------------------------------- | ------------ | --------- |
| Batched (`InsertRelations`)      | **~23.6 ms** | ~2.9 ms   |
| One at a time (`InsertRelation`) | ~29.5 ms     | ~3.3 ms   |

Batching wins ~20% on disk. The upstream table cites −53 to −64% for node
batching; rohy sees less because every relation also registers index entries,
and that per-entity pass is unchanged and now dominates. It is already in the
recommended shape.

**Lifecycle** — disk backend, no ordered declarations:

| Path                          | Time        |
| ----------------------------- | ----------- |
| Open, 10k, uncompacted        | ~58 ms      |
| Open, 10k, compacted          | ~36 ms      |
| Open, 100k, uncompacted       | ~0.77 s     |
| Close                         | ~0.5–0.9 ms |
| Compact, 10k                  | ~59–71 ms   |
| Warm (background open), 10k   | ~69 ms      |
| `OpenLazy` alone (pre-window) | **~180 ns** |

**Open-cost attribution** — 100k events, showing where open time goes:

| Configuration                            | Open    |
| ---------------------------------------- | ------- |
| No property index at all                 | ~131 ms |
| Low-cardinality keys only                | ~0.55 s |
| All 9 keys, none declared _(current)_    | ~0.77 s |
| All 9 keys, `timestamp` declared ordered | ~7.98 s |

**Store memory** — resident bytes per event, disk backend, after ingest:

| RawXML per event  | Resident per event |
| ----------------- | ------------------ |
| 64 B              | ~1 682 B           |
| 512 B _(typical)_ | **~2 052 B**       |
| 4 096 B           | ~7 796 B           |

Extrapolating the typical figure: **1M events ≈ 1.9 GB, 10M events ≈ 19.1 GB.**

Two things follow, and both matter for capacity planning:

- The **fixed floor is ~1.7 kB per event** — index plus topology, independent of
  payload. Indexing fewer keys is the only lever on it.
- The store **retains node property blobs in memory, RawXML included.** Memory
  grows with record size (4.6× for a 64× larger record), even though RawXML is
  read only when an analyst opens a single event.

⚠️ **This is the binding constraint on the "100 GB+ EVTX" target.** A corpus
that size is far more than 10M events and does not fit in memory under the
current schema. Closing that gap means not holding RawXML resident — a schema
change, not a tuning change.

> **Fixture warning, learned the hard way.** The first version of this
> measurement used NUL bytes as RawXML filler and reported ~4 356 B/event with
> an apparent 14× record sensitivity. JSON escapes each NUL to a six-character
> sequence, so the figure described the fixture, not the store. **Synthetic
> payloads must be representative of real data**, or the measurement measures
> the generator.

---

## 12a. Consistency and recovery

Performance work created the one durable correctness hazard in this layer, so it
belongs here rather than in a separate document.

### The commit/index gap

A record commit and its index registration are **two separate steps** — index
registration is not part of a commit. A crash between them does not simply lose
the relation. It leaves an edge that is:

- **committed and visible to adjacency**, which reads incident edges directly;
- **invisible to every graph-scoped query**, which resolves through `graph_id`;
- **unremovable by the graph clear**, which finds targets through that same
  index.

So an event shows a relation belonging to no graph, permanently, and the next
build adds a second copy alongside it.

### What does and does not detect it

| Tool                  | On this failure                                                                                                                                    |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `VerifyIndexes`       | **Passes.** An entry that was never written is not structural damage — the edge and its adjacency agree.                                           |
| `RebuildIndexes`      | **Cannot help.** It re-derives only what the storage layer can see; indexed values are this package's encoding and are opaque to it.               |
| `RepairRelationIndex` | **Fixes it.** Walks edges (independent of the index), compares against what the index reports per graph, re-registers what is missing. Idempotent. |

### Rules

- **Any new batched write path inherits this gap.** Register index entries after
  commit, and state in a comment what a crash in the gap leaves and how it
  recovers.
- **Recovery belongs before the destructive step.** The graph build repairs once
  per run _before_ clearing, so a previous run's leftovers are folded into the
  rebuild rather than duplicated. A repair that ran afterwards would be useless.
- **Surface a non-zero repair count** (`Result.RepairedRelations`) rather than
  absorbing it — it means a previous run did not finish.
- **`VerifyIndexes` is a test and recovery tool, not a startup step.** It is
  proportional to the whole store; running it on every open taxes every launch
  to re-prove something almost always true.

### Scope of the crash claim

A unit test cannot kill the process mid-write. The tests reproduce the **state**
a crash produces — records present, index entries absent — and prove recovery
from it. That is narrower than "survives a crash". Say the narrower thing.

---

## 13. Decisions pinned by tests

These tests exist to make a decision hard to reverse by accident. If one fails,
the decision is being changed — check that it is on purpose.

| Test                                                    | Pins                                                                |
| ------------------------------------------------------- | ------------------------------------------------------------------- |
| `TestNoOrderedPropertiesDeclared`                       | No ordered declaration (§9)                                         |
| `TestTimeRangeQueryPlanShape`                           | The accepted plan shape; never a full scan                          |
| `TestTimeRangeResultsUnchangedUnderByteWiseComparison`  | Timestamp encoding orders correctly at year/month/digit boundaries  |
| `TestUndatedEventsExcludedFromTimeRange`                | A zero timestamp sorts before every real record                     |
| `TestUpdateRelationLeavesNoStaleIndexEntry`             | Atomic indexed update; no stale `graph_id`                          |
| `TestInsertRelationsBatchRoundTrip`                     | Batched writes register index entries too                           |
| `TestDeleteGraphRelationsIsAllOrNothing`                | Transactional clear touches no nodes                                |
| `TestIncrementDedupCountsBatched`                       | A missing id skips rather than aborting the pass                    |
| `TestIndexesVerifyAfterEveryWritePath`                  | Every write path leaves the index structurally truthful             |
| `TestReopenAfterUncleanShutdownReplaysWAL`              | An uncompacted close still replays into a consistent store          |
| `TestCommitIndexGapLeavesEdgesInvisibleToGraphQueries`  | The exact shape of the gap (§12a) — not merely "relation missing"   |
| `TestRepairOrphanedRelationsRecoversFromCommitIndexGap` | Repair works and is idempotent                                      |
| `TestRunRecoversFromAnInterruptedPreviousBuild`         | A crashed build reruns without duplicating relations                |
| `TestStoreMemoryPerEvent`                               | Per-event resident cost, with a regression ceiling                  |
| `TestStoreMemoryGrowsWithRecordSize`                    | Memory grows far slower than the record; the fixed floor stays flat |

---

---

## 14. The payload cold store

**Rule: nothing that scales with the source data belongs in the node record.**

The graph database loads its records into memory when the store opens, property blobs
included. Anything in an event's blob is resident for **every event in the case**, for the
whole session — so a field's cost is not "per read", it is "per event, always".

The raw record and parsed fields were 70% of an event's resident cost while being read in
exactly one place: the detail drawer. They now live in `backend/payload`, an append-only log,
with a fixed `(offset, length)` reference in the event.

| | Before | After |
|---|---:|---:|
| 64 B record | 1682 B/event | 1624 B/event |
| 4096 B record | 7796 B/event | 1610 B/event |
| **Scaling with record size** | **4.6×** | **1.0× — flat** |

The headline is the shape, not the percentage: **resident cost no longer depends on record
size.** `TestStoreMemoryIsFlatInRecordSize` guards it, because this regresses invisibly —
nothing looks wrong until a case gets large.

### Rules that follow

- **Hydrate on single-event reads only.** `GetEvent` pays for the payload; list queries never
  do. `TestListQueriesDoNotHydratePayload` pins it, because a regression here would leave the
  rows looking perfectly correct.
- **Write the payload before the record that references it.** A crash between the two leaves
  orphaned bytes — waste, reclaimed by rebuilding. The reverse order leaves a record pointing
  at a payload that was never written, which cannot be recovered. Waste is the acceptable
  failure.
- **A zero reference means "no payload".** Events that never had a raw record (catalogue
  rows) cost nothing here and need no special case at the call site.
- **Before adding a field to `Event`, ask whether it scales with the input.** If it does, it
  belongs in the payload log, not the node record — and the decomposition below is how to
  tell.

### How to decompose a memory cost

Do not guess which part dominates. Build the same fixture four ways — with and without the
index, with and without the payload — and subtract. Measured at a 2 KB record:

| Component | B/event |
|---|---:|
| Topology | 503 |
| Property index (9 keys) | 867 |
| Raw record + parsed fields | 3137 |

An early guess that the index dominated was **wrong**: that is only true at tiny payloads.
Measuring first is what kept the effort off trimming index keys, which would have cost query
capability for a fifth of the benefit.

---

## 15. Perceived performance

Being fast and *feeling* fast are different problems. §1–§14 are about the first; this is
about the second — covering the unavoidable async gaps (a query, a payload fetch) with motion
so the app reads as "arriving" rather than "waiting". Borrowed from mobile OSes and games,
which animate a transition over the moment content is loading.

The trap to avoid is a **deceptive** version of this, and two rules keep it honest:

1. **The reveal plays for the same duration every time.** A content-arrival transition
   (`lib/motion.js` `reveal()`) animates the element as it mounts, for a fixed duration,
   whether the data took 5 ms or 500 ms. It never holds data back — it animates pixels that
   are already there — so it adds **zero real latency**. And because the duration is constant,
   the animation never becomes a *tell* that "this time it was slow", which is exactly what a
   duration scaled to load time would leak. `TestRevealIsAConsistentArrival` pins that two
   calls are identical.

2. **A skeleton appears only past a threshold.** `SKELETON_DELAY` (140 ms, just above the
   ~100 ms that reads as instant) gates the shimmer via `afterDelay()`. A fast load cancels
   the timer before it fires, so it reveals cleanly with no flash; only a genuinely slow load
   shows a placeholder. Flashing a skeleton for a few frames reads as a glitch — worse than
   the reveal alone.

### Rules

- **Reveal on arrival, always.** Wrap content that replaces a loading state in
  `in:fly={reveal()}`. It is the consistent motion the whole effect depends on.
- **Skeleton, never spinner, for slow loads** — and shape it like the content (see
  `Skeleton.svelte`, and the events-list skeleton rows that mirror the real row grid and
  height, so the reveal lands with no reflow jump).
- **Gate the skeleton with `afterDelay`.** Never show it unconditionally; a fast load must
  skip it entirely.
- **Do not add latency to make motion "fit".** The reveal covers the gap for free; the data
  path stays as fast as §1–§14 make it. Perceived speed is layered on top of real speed, never
  traded for it.
- **Reduced motion still wins.** `reveal()` collapses to an instant, motionless appearance;
  the skeleton stays (it is information) but its shimmer stills. Same one-place rule as §14.
