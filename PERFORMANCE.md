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

### Record: v0.3.0 → v0.4.0

Purely additive by signature — nothing removed, nothing changed shape. `OpenWithOptions`,
`Forensics`, `StorageStats`, `ShouldCompact`, `Tx.As`/`Tx.Attributed`, and the `Signer` /
`Verifier` / `ActorTransactor` / `StorageReporter` interfaces are new; every call rohy makes
compiles and passes unchanged.

**It is not free.** Same machine, same session, `-benchtime=3x -count=3`, medians:

| Path | v0.3.0 | v0.4.0 | |
| --- | ---: | ---: | --- |
| `Compact()`, 10k | **45.1 ms** | **101.7 ms** | **2.3× slower** |
| `Compact()` allocations | 90 119 | 410 140 | **4.6×** |
| Open, uncompacted 100k | 921 ms | 965 ms | within noise |
| Open allocations, 100k | 4 210 352 | 5 310 577 | **+26%** |
| Open, compacted 10k | 49.9 ms | 58.5 ms | overlapping spreads |

The two timing rows that look like regressions are not resolvable — the spreads overlap, and §12
warns that differences under ~25% are not resolvable on this rig. **The allocation rows are.**
They are stable to the digit across every run, and they are the finding: v0.4.0 allocates 4.6× as
much to compact and 26% more to open.

Compaction is not on an interactive path — it runs after bulk ingest and before shutdown (§6) —
so the cost lands where it is least felt. It is recorded rather than acted on, because the number
to watch is the ALLOCATION count, and the next upgrade should compare against these figures rather
than against the older ones in §12.

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
| `TestCorrelationProjectionBudget`                       | The correlation projection's per-event ceiling (§16)                |
| `TestCorrelationProjectionIsFlatInFieldCount`           | Projection cost depends on the vocabulary, never on the payload     |
| `TestSlotsAreBoundedToTheirDomain`                      | Every per-slot bound still fits the widest legitimate value         |
| `TestDatasetCacheIsInvalidatedByAWrite`                 | A prepared dataset never outlives the store version it was built on |
| `TestVerifyImageDetectsAnEditedRecord`                  | The image check actually detects a one-bit edit to a record (§18)   |
| `TestVerifyImageReportsNoImageRatherThanPassingOrFailing`| An unverifiable case never reads as a verified one (§18)            |
| `TestInMemoryStoreReportsForensicsUnavailableNotClean`   | A backend with no forensic machinery says so, rather than looking clean |
| `TestDefaultPostureClaimsNothingItHasNotGot`            | No integrity guarantee is reported that has not been configured      |
| `TestMultiFileIngestSealsOnceNotPerFile`                | Sealing is once per ingest request, never per file (§18)             |
| `TestCancelledIngestIsStillSealed`                      | A cancelled ingest still seals what it wrote (§18)                   |
| `TestUnknownFingerprintIsNotExported`                   | A lost fingerprint history defaults to the caveat, never to "kept"   |
| `TestReObservingDoesNotRetractAnExport`                 | Reading the posture cannot retract a recorded export                 |
| `TestWindowsIdentityHashIsFrozen`                       | A literal digest: identity cannot change without a deliberate act    |
| `TestUnspecifiedAddressIsAbsentNotAKey`                 | 0.0.0.0 is a sentinel, not a key every local logon shares            |
| `TestAddressSpellingsUnifyIntoOneKey`                   | `::ffff:`v4 and v4 are one correlation key, not two                  |
| `TestGuardDoesNotDisturbACleanIngest`                   | Panic containment costs the happy path nothing                       |

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
  tell. The one deliberate exception is the **bounded** correlation projection (§16), which
  is fixed-width by construction precisely so this rule still holds.

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

---

## 16. The correlation projection: buying a field back

§14 moved `ParsedFields` out of the node record because it scales with the source data. That
was right, and it immediately blocked the thing v0.2.0 exists to do: field, temporal and
lineage correlation all need to compare *field values* across events, and the whole point of
the cold store is that reading it per event is the expensive path.

So a **fixed, bounded projection** of those fields comes back into the record —
`consts.CorrelationSlots`, ten named slots at v0.2.0 (thirteen since the network vocabulary
below) stored **by position** (`ck`) with a version (`ckv`). Everything else stays cold.

### The measurement that forced the design

The first cut had twelve slots and no per-slot bounds. Measured:

| | B/event |
|---|---:|
| First attempt (12 slots, unbounded) | **819** |
| Pre-measurement estimate | 200 |

The estimate was an estimate, not a measurement, and it was wrong by 4×. Two changes fixed
it, both of which the number is what justified:

1. **Store by position, not by name.** A `map[string]string` pays for every key on every
   event, forever. Ten positional entries in a slice pay for none — worth **~138 B/event** on
   its own.
2. **Bound each slot to its own domain**, not to a single global limit. A logon id needs 18
   bytes; a process command line would take hundreds, so it is not a slot at all. Two slots
   whose value could not be bounded honestly were dropped (12 → 10).

| | B/event |
|---|---:|
| Typical (a 4688 process-creation event) | **71** |
| The format's worst case (all ten slots at their bound; 381 at thirteen, below) | **324** |
| Ceiling asserted by `TestCorrelationProjectionBudget` | 400 |
| _(baseline node record, for scale)_ | _483_ |

The typical figure is what matters for capacity — the worst case requires an event carrying
every slot at maximum width, which no real record does. The **ceiling is asserted against the
worst case anyway**, because a regression arrives as a *new slot*, not as a busier event.
Even at worst case the extrapolation is 309 MB at 1M events, against the ~1.6 kB/event floor
§14 established.

### Rules

- **The slot list is append-only.** Slots are stored by position, so reordering or removing
  one silently reinterprets every already-stamped event. Add at the end, bump
  `consts.CorrelationKeyVersion`, and let the backfill re-stamp.
- **Every new slot needs a bound that fits its widest legitimate value.** Too tight is worse
  than the memory it saves: a truncated value becomes a *wrong* correlation key, and two
  unrelated events correlate. `TestSlotsAreBoundedToTheirDomain` checks each one against the
  widest real value of its kind (a full `uint64` id, the longest canonical IPv6 address).
- **Absent is not empty.** A field that was not present projects to nothing, so it can never
  match another absent field. `-`, `S-1-0-0`, `null`, `N/A` and a zero identifier all mean
  absent. Without this a field matcher becomes a false-positive engine, correlating every
  event that happens to be missing the same field.
- **Projection cost is a function of the vocabulary, never of the payload.**
  `TestCorrelationProjectionIsFlatInFieldCount` pins it: an event with 200 parsed fields costs
  the same as one with 6.

### Backfilling

An existing case has events stamped with an older version, or none. Re-stamping means reading
the payload cold store per event, which is exactly the expensive path §14 was built to avoid
— so it is **opt-in and resumable**, never a startup step, and reports status
(`CorrelationKeyStatus`) so the UI can say *why* a rule found nothing.

An event whose payload cannot be read is **left unstamped rather than stamped as empty**.
Stamping it current would mark it permanently done at the wrong answer; leaving it means the
next run retries it.

---

### The network vocabulary, and what it actually cost (v0.3.0)

`CorrelationKeyVersion` 3 added `dst_ip`, `dst_port` and `protocol` so IDS, firewall and capture
evidence can correlate with Windows events. The vocabulary was budgeted at **300–450 B** before
anyone did the arithmetic. Measured, it cost **+57 B**:

| | B/event |
|---|---:|
| worst case, ten slots (v2) | 324 |
| worst case, thirteen slots (v3) | **381** |
| ceiling, unchanged | 400 |
| typical 4688, unchanged | 71 |

Two things account for the gap between the estimate and the number, and both are worth
remembering the next time a slot is proposed:

- **`ip_address` was already the source-address slot.** Every alias it reads — `IpAddress`,
  `ClientAddress`, `SourceNetworkAddress`, `SourceIp` — names the party that *initiated*, and so
  do Suricata's `src_ip`, nginx's `remote_addr` and iptables' `SRC=`. A separate `src_ip` slot
  would have been 39 bytes of duplicate. It is an alias instead (`CorrelationSlotAliases`), which
  costs nothing on the wire.
- **Canonicalization paid for a quarter of the rest.** Addresses now normalize to their RFC 5952
  form, and `netip.Addr.String()` emits at most 39 characters, so slot 8's bound came *down* from
  45. The 45 was sized for the longest uncanonicalized IPv6 literal, which can no longer be
  stored.

🔒 The rule the budget test states — *shrink the vocabulary, never raise the ceiling* — held, and
was the reason the cheap design was found. The first sketch priced a maximalist vocabulary
(src_ip, dst_ip, both ports, protocol, method, status, host, uri) at over 400 B and concluded the
budget had to move. The ceiling refusing to move is what forced the question "which of these is
actually a join key?", and the answer was four of them.

⚠️ **Two correctness bugs came out of the same change**, both live in v0.2.0 for Windows-only
cases:

- `::ffff:10.0.0.5` and `10.0.0.5` were different correlation keys, so any rule joining across a
  v4-mapped spelling silently matched nothing.
- `0.0.0.0` on a 4624 means "not a network logon". It is not in `CorrelationAbsentValues`, so
  **every local logon in a case shared one `ip_address` key** — a false-positive generator of
  exactly the kind `bucketByKey`'s own comment warns about, in a spelling that list did not cover.

Neither was found by a benchmark. Both were found by asking what a *second* source would have to
write to match the first.

---

## 17. Prepare once, match many

Every correlation algorithm needs the same three things from the event set: a chronological
ordering, a partition by scope, and the correlation keys. The v0.1.0 build re-derived all of
it **per rule** — the sort, in particular, ran once per enabled rule over the same events.

`autograph.Dataset` derives it **once per build**, driven by the union of what the enabled
rules actually require (`Requirements`). Measured over 50 000 events, mixed algorithms
(`BenchmarkBuild` vs `BenchmarkBuildPreparingPerRule`, `-benchtime=20x -count=3`):

| Rules | Prepared once | Prepared per rule | Allocated (once → per rule) |
| ----- | ------------: | ----------------: | --------------------------: |
| 1     |       28.0 ms |           26.8 ms |           2.1 MB → 2.1 MB   |
| 5     |       86.0 ms |          154.6 ms |           4.9 MB → 13.3 MB  |
| 20    |    **282 ms** |       **505 ms**  |   **16.2 MB → 56.1 MB**     |

Times are medians of three runs; the spread is roughly ±10%, so read the ratios, not the
digits. Allocation figures are stable to the byte.

At one rule the two are the same within noise — there is one prepare either way, and nothing
to amortise. The gap opens from two rules on, reaching **~1.8× faster and 3.5× less allocated
at twenty**, which is the shape that matters: the built-in library is thirty-five rules and a
real run enables most of them.

Divide each row by its rule count and the falling per-rule cost *is* the shared sort. If
preparation ever moves back inside the per-rule loop, that curve goes flat — which is what
the benchmark is for.

### Rules

- **Requirements-driven, not "prepare everything".** A dataset builds only the indexes some
  enabled rule asked for. A library of pure-sequence rules pays for no correlation-key
  bucketing at all.
- **A prepared dataset is immutable and single-use per store version.** The dry-run cache
  (capacity 1) keys on the filter *and* the requirements fingerprint, and re-reads
  `Store.Version()` **after** the query rather than before — reading it first would let a
  write that lands mid-query be cached as if it had been included.
- **Cache invalidation is the whole risk here.** A stale dataset does not fail loudly; it
  produces a *plausible* graph from events that no longer match the case.
  `TestDatasetCacheIsInvalidatedByAWrite` and `TestBuildInvalidatesTheCache` pin it.

---

## 18. Verifying the image: why it is a button and not a startup step

The engine rohy stores evidence in can re-derive every record hash and check the compacted
image against the digest written with it. That closes a real gap: the structural validation
which always runs at open catches a malformed or internally inconsistent file, and does **not**
catch an edit to the records in a file that stays coherent — which is exactly what a deliberate
edit looks like.

There are two ways to ask for it, and the measurement picked the second decisively.

| | Route | When it runs |
| --- | --- | --- |
| `VerifyOnOpen` | an engine option set at open | every launch |
| `VerifyImage` | `disk.VerifyCSRDigest` + `VerifyCSRRoots`, package-level, storeless, keyless | when the analyst presses the button |

### Measured

Ryzen 9 5980HS, medians of three runs, on a **compacted** store (there must be an image to
verify). `-benchtime 5x` at 10k, `3x` at 100k.

| Events | Open, default | Open, verified | Ratio | `VerifyImage` alone |
| -----: | ------------: | -------------: | ----: | ------------------: |
| 10k    |         59 ms |         117 ms | 2.0×  |               65 ms |
| 100k   |        500 ms |    **1117 ms** | 2.2×  |          **626 ms** |

| Events | Allocated, default | Allocated, verified | Delta | `VerifyImage` alone |
| -----: | -----------------: | ------------------: | ----: | ------------------: |
| 10k    |            39.2 MB |             84.0 MB | +44.8 MB |            44.8 MB |
| 100k   |             366 MB |             815 MB  | **+449 MB** |         **449 MB** |

### The number that settles it

**`VerifyOnOpen` is `VerifyImage` executed during open, and nothing else.** Read the deltas
against the standalone column — at 100k the overhead is +617 ms against a standalone cost of
626 ms, and the allocation delta is +449 MB against 449 MB, matching to three significant
figures and to the allocation count (+5 100 090 vs 5 100 111).

So the option buys no evidence the button does not. What it buys is automaticity, and it
charges **+617 ms and +449 MB of allocation churn on every launch of a 100k case** for it.

🔒 **Verification is on demand, and `VerifyOnOpen` stays off by default — permanently, not
provisionally.** This is not a threshold that a faster machine could clear later; the two
routes do identical work, so the tax is pure. It sits in the deep integrity check beside
`VerifyIndexes`, which is where work proportional to the whole store already lives. Upstream's
own reason for offering the package-level form is the same sentence §12a already makes:

> a check that makes every open slower gets disabled, and a disabled check protects nothing

🔒 **Off is a reported state.** `WithVerifyOnOpen` remains available for a strict per-case
posture, and `ForensicStatus` reports which posture is in force, because a verification that is
off without saying so is worse than one that was never offered.

⚠️ **Verification is allocation-heavy, not just slow** — 449 MB of churn at 100k, ~4.5 kB per
event. It is the wrong thing to run concurrently with a graph build, and the maintenance
binding's single-pass guard is what keeps it from happening.

### Sealing at the ingest boundary

Compaction is what computes the snapshot roots, so until it runs nothing in a case can be proven.
rohy used to compact in exactly one place — clean shutdown — which meant a force-quit or crashed
session left roots for nothing it had ingested.

The question that decides the placement is not what `BenchmarkCompact` measures. Compacting a fresh
store says nothing about the case that matters: **an analyst has a large case and ingests one more
log file into it.**

Ryzen 9 5980HS, medians of three. "Increment" is an already-compacted store of that size plus 500
events — a realistic single small log file — compacted again.

| Events | Compact from scratch | Compact after +500 | Allocated (scratch → +500) |
| -----: | -------------------: | -----------------: | -------------------------: |
| 10k    |                88 ms |              73 ms |        49.1 MB → 47.1 MB   |
| 100k   |              1149 ms |        **1325 ms** |    **600 MB → 566 MB**     |

**Compaction is proportional to the CASE, not to the ingest.** Adding 500 events to a 100k case
costs the same ~1.3 s as compacting all 100k from nothing, because it rewrites the whole image
either way. The allocation figures are the stable ones and they say it plainly: 566 MB against
600 MB, 4.02M allocations against 4.10M — within 6%.

🔒 **So sealing happens once per ingest REQUEST, never per file and never per chunk.** A twelve-file
ingest that sealed per file would pay ~16 s on a 100k case and discard eleven of the twelve results,
because only the last image's roots survive. `TestMultiFileIngestSealsOnceNotPerFile` pins it.

🔒 **It is a deferred call, not a trailing one.** The cancellation path returns from inside the file
loop, so written as a trailing statement it was skipped there — a cancelled ingest left everything
it had already written unaccounted for. `TestCancelledIngestIsStillSealed` found that, and is what
stops a future early return reintroducing it.

⚠️ **The wait is announced** (`case:sealing` / `case:sealed`). Over a second of silence after
"ingest complete" is indistinguishable from a hang — the same reason the backfill reports progress.

⚠️ **A live continuous capture seals when it stops, not per batch.** It reaches the same deferred
call, which is the correct behaviour by construction rather than by a special case: per-batch
sealing on an open-ended stream would be unbounded waste at the worst possible ratio.

### Three costs measured for free along the way

- **Compaction is not optional overhead.** It computes the snapshot roots unconditionally, so
  every store already pays for them whether or not anything reads them. Until the first
  compaction there is no image, and nothing in the case can be proven.
- **A missing image is not a failed check.** `VerifyImage` reports `no_image` (never compacted)
  and `absent` (an old image digests never covered) as distinct from match and mismatch, so an
  unverifiable case cannot read as a verified one.
- **Proof verification is asymmetric in the useful direction.** Upstream measures building an
  inclusion proof at ~8 ms per 10k nodes against ~5 µs to verify one — about 1500×. A recipient
  checking evidence pays almost nothing.

### Proving one event (v0.3.0)

`ProveEvent` is the drawer's _Save proof…_. It covers graphene's `ExportNodeProof`, which
recomputes every node leaf because graphene stores no leaf hashes, plus the payload read and
`eventproof.Assemble`, which verifies its own document before returning it. Ryzen 9 5980HS,
compacted store, median of three runs of `-benchtime 5x`:

| Events | Time    | Allocated | Allocations |
| -----: | ------: | --------: | ----------: |
| 10k    | 11.7 ms |    5.1 MB |      50 k   |
| 100k   |   94 ms |     50 MB |     500 k   |

Cost is linear, at about 500 bytes and five allocations per event in the case. All of it is
graphene's leaf recomputation; the proof itself is a few kilobytes.

The proof runs on a click, and the drawer shows a busy state for it, so nothing is optimised. If
it ever matters, the fix belongs upstream: storing leaf hashes, which graphene's `ProveNode`
comment prices at about 6% more disk, would make the cost O(log n).

```
go test ./backend/graphene/ -run XXX -bench ProveEvent -benchtime 5x -count 3
```

### Closing an unchanged case (v0.3.0)

Every compaction writes a new fingerprint, even over identical content, so rohy no longer seals
a case with nothing new: after an ingest that wrote nothing, and on close. It asks
`Store.Unsealed()` first, which checks the delta and delete counts and, when they are zero, walks
the write-ahead log for any record. A compacted log still holds graphene's 50-byte header, so its
size cannot answer the question. Same machine, sealed store reopened, median of three runs of
`-benchtime 2s`:

| Events | `Unsealed()` | Allocated | Allocations | What it replaces (above) |
| -----: | -----------: | --------: | ----------: | -----------------------: |
| 10k    |       0.4 ms |    1.6 KB |          15 | a full compaction        |
| 100k   |       1.1 ms |    1.6 KB |          15 | ~1.3 s, 566 MB           |

Allocation is flat. The time grew from 10k to 100k and varied about twofold between runs (0.63
to 1.44 ms at 100k); the cause was not profiled. Either way it is about a thousandth of the
compaction it replaces, paid once per close or ingest, so it is not worth reducing.

```
go test ./backend/graphene/ -run XXX -bench Unsealed -benchtime 2s -count 3
```

### Accounting for one event (v0.3.0)

`EventCustody` is the drawer's _Account for this event_ inside the store: graphene's `CustodyFor`
(one inclusion proof, and the ledgers rohy does not keep), then `ProveEvent` for the record and raw
layers. Same machine, compacted store, median of three runs of `-benchtime 5x`, with `ProveEvent`
from the same run beside it:

| Events | `EventCustody` | Allocated | Allocations | `ProveEvent` |
| -----: | -------------: | --------: | ----------: | -----------: |
| 10k    |        20.6 ms |   10.2 MB |       100 k |       9.8 ms |
| 100k   |         184 ms |    101 MB |       1.0 M |        87 ms |

Two proofs, so twice `ProveEvent`, and linear for the same reason. The rest of the report (the
history walk, the wording) is not measurable beside it. It runs on a click with a busy state, so
nothing is optimised; reusing `CustodyFor`'s inclusion proof for the record check would halve it,
at the cost of a second path that could disagree with the proof export.

```
go test ./backend/graphene/ -run XXX -bench EventCustody -benchtime 5x -count 3
```

### Redacting one event (v0.3.0)

Two costs, both on a click. The **preview** is what the dialog waits for before it can say what
would go: every event's record is read for the raw hash, which is not indexed, and the whole
payload log is walked for unreferenced copies. The **redaction** does that work again, then the
five steps, then the seal, which rewrites the whole image. Same machine, compacted store with a raw
record per event, median of three runs of `-benchtime 3x`:

| Events | Preview (`Resolve`) | Allocated | Redaction, seal included | Allocated |
| -----: | ------------------: | --------: | -----------------------: | --------: |
| 10k    |             50.2 ms |   13.7 MB |                   205 ms |   57.4 MB |
| 100k   |              597 ms |    127 MB |                   2.07 s |    537 MB |

Linear in both. At 100k the seal is most of the redaction (the ~1.3 s and ~566 MB above), and the
preview's two passes are most of the rest. Nothing is optimised: the dialog shows a working state
for the preview and the redaction, and a redaction is rare. Indexing `hash_raw` would make the
sibling search a lookup, at an index entry per event paid on every open, for a question asked once
per redaction; the payload walk has no index to use at all.

```
go test ./backend/redaction/ -run XXX -bench 'Resolve|Redact' -benchtime 3x -count 3
```

### Signing and attribution (v0.3.0)

Item 41 changed three costs: every write rohy can put in a commit became an attributed
transaction instead of the engine's batch insert; every case keeps an audit log, which records each
seal; and a signed case signs each commit and seal. Each is measured against what it replaced, in
one binary, so no other build is needed. Same machine, `-benchtime 20x`, three runs:

| One ingest batch (512 events) | Time | Allocated | Allocations |
| --- | ---: | ---: | ---: |
| Engine batch insert, actor 0 (before) | 2.6–3.2 ms | 1.10 MB | 5,648 |
| Attributed transaction | 2.4–4.2 ms | 1.20 MB | 5,663 |
| Attributed and signed | 2.4–4.7 ms | 1.20 MB | 5,667 |

| One seal of a 10k-event case | Time | Allocated | Allocations |
| --- | ---: | ---: | ---: |
| No audit log (before) | 20–35 ms | 14.27 MB | 49,306 |
| Audit log | 22–26 ms | 14.27 MB | 49,311 |
| Audit log, signed | 24–26 ms | 14.27 MB | 49,317 |

The three are inside each other's run-to-run spread: a batch is one durable commit whichever way
it is written, and the fsync is the cost. The transaction copies each node once more (+9% bytes);
signing adds a handful of allocations per commit, flat. The audit entry is one more synced append
per seal.

**Unlocking a key** is argon2id at 64 MiB: 60–78 ms and 67 MB allocated, once per session. That is
the point of it — an offline guess at a stolen key file pays the same per try.

The three loops that wrote one relation at a time — a snapshot restore, the legacy graph migration,
the tolerant graph clear — now commit once each, so turning each write into a transaction did not
turn each relation into an fsync.

```
go test ./backend/graphene/ -run XXX -bench 'InsertShape|SealShape' -benchtime 20x -count 3
go test ./backend/keyfile/  -run XXX -bench Unlock -benchtime 10x -count 3
```

## 19. Checkpoints: what a publish and a check cost (item 42)

A checkpoint commits to the head of every history except the events themselves: the image root,
its attestation, the audit log, the removal and grant ledgers. Reading them once is what both an
integrity check and a held-digest check do. A publish adds the engine's audit entry and the
checkpoint record, and each is synced.

| | 10k events | 100k events |
|---|---|---|
| `Histories` + `Holds` + `Current` (1,000 audit entries) | 1.8 ms, 0.80 MB, 3,093 allocs | 2.2 ms, 0.79 MB, 3,093 allocs |
| `ReadyCheckpointChain` + `PublishCheckpoint` | 17 ms, 0.59 MB | 22 ms, 0.59 MB |

Neither moves with the number of events. They grow with the audit log, which holds rohy's
actions, never one entry per event. A publish is a click; the read runs inside each integrity
check, under `sealMu`, and is small beside it.

A publish on a case with unsealed changes seals first, and then costs what a seal costs (§18).

```
go test ./backend/graphene/ -run XXX -bench 'Histories|PublishCheckpoint' -benchtime 20x
```

## 20. The integration path: ordering, the build's memory, repair (item 34)

**Writing chunks in file order cost nothing measurable.** A file ingest now hands its chunks to
the sink in file order through a bounded window (LLD §7.1). `TestBoundedMemoryStreaming` measured
the live heap of the same two synthetic files before and after:

| | 44,340 records | 177,360 records |
|---|---|---|
| Before (handed over as workers finished) | 18 MiB | 23 MiB |
| After (in file order) | 17 MiB | 25 MiB |

The ceiling is 96 MiB, and the large file stays within 2× the small one. While the oldest chunk
is stuck, at most 12 chunks are parsed ahead of it (`TestTheReorderWindowBoundsWhatIsParsedAhead`);
before, all of them were.

**A build's memory grows with the case, not with its rules.** `TestTheBuildStaysBounded` builds
the 20 synthetic rules (12 sequence, 4 field, 3 temporal, 1 lineage) over distinct synthetic
events, on disk, with GOGC pinned at 10. Three runs:

| | Peak live heap | Relations | Build |
|---|---|---|---|
| 25,000 events, 20 rules | 26.6–27.0 MiB | 5,621 | 0.5 s |
| 100,000 events, heaviest single rule | 87.9–89.1 MiB | 500–1,000 | 1.3 s |
| 100,000 events, 20 rules | 106.6–111.8 MiB | 22,499 | 2.2 s |

Twenty rules cost about 1.25× the heaviest one, because the dataset is prepared once and shared.
Four times the events cost about 4.1× the memory: linear, as the dataset holds every matching
event. The test fails above 2× the heaviest rule, above 6× between the sizes, or above 224 MiB,
which is twice the worst run.

**The repair now checks every index key.** `RepairRelationIndex` runs before every build. It used to
ask only whether `graph_id` listed each relation; it now asks of all four keys, so a relation
missing from its rule's index is found (LLD §12). On a healthy in-memory case:

| | 10,000 relations | 50,000 relations |
|---|---|---|
| Before, `graph_id` only | 25–26 ms, 3.6 MB | 122–130 ms, 17 MB |
| After, every key | 26–32 ms, 10 MB | 143–194 ms, 50 MB |

It is a step of a build, and a build writing 22,000 relations takes 2.2 s, so the increase is not
worth a cleverer walk. The memory is transient and goes when the repair returns.

**Where a build's time goes.** `go run ./tools/profile -events 100000` on this machine:

| Phase | Time | Peak `HeapAlloc` |
|---|---|---|
| ingest, 100,504 records, not deduplicated | 6.9 s | 271 MiB |
| build, 20 rules | 1.9 s | 316 MiB |
| open, up to the first query | 1.3 s | 231 MiB |

The peaks include garbage not yet collected: the GC is not pinned here. In the build's CPU profile,
about 40% is decoding the node records the dataset is read from (`encoding/json`), and 15% is file
I/O. That decode is the first place to look if a build is ever too slow; nothing is changed for it
here.

```
go test -tags=integration ./backend/integration/ -run TestTheBuildStaysBounded -count=1 -v
go test ./backend/graphene/ -run XXX -bench RepairRelationIndexHealthy -benchtime 10x
go run ./tools/profile -events 100000 && go tool pprof -top profiles/cpu-build-100000.pprof
```
