# rohy v0.2.0 — Engineering Plan

> **TEMPORARY WORKING DOCUMENT.** This file is deliberately not referenced by `README.md`,
> `LLD.md`, `RULES.md`, `PERFORMANCE.md`, `.gitignore`, CI, or any source file. It exists only
> for the duration of v0.2.0 development and is deleted when the work lands. Do not link it.

**Legend**

| Mark | Meaning |
|---|---|
| ✅ | Already exists in `HEAD` and is fit for purpose — do not rebuild |
| ⚠️ | Exists but partial, or exists with a caveat that v0.2.0 must resolve |
| ❌ | Does not exist — net-new work |
| 🔴 | Breaking change (format version bump, interface change, or on-disk shape change) |
| 🟡 | Gated on a measurement — the number decides, not the design |
| 🔒 | Invariant that must not be weakened |

Audit basis: `HEAD` = `2ddd411`, read-only git only. 285 tracked files, 10,429 non-test Go
lines, 380 Go test/benchmark functions (28 benchmarks), 15 frontend test files.

---

## Status — audit of 2026-11-03

Audited against `fef2ebf` plus the working tree, after the v0.3.0 work in §15. Scoring: done = 1,
partial = ½, not started = 0, per numbered §13 item. The CI additions are **deferred** (CI stays
minimal for now) and are not counted: benchmark smoke (#34c), the `extension` job (#35b),
`rohyctl` in the build matrix (#35c), the integration CI step.

```
P1 Foundations       ████████████████████ 100%  7/7    (#5 was ½ until D2 was fixed, §15)
P2 Algorithms        ████████████████████ 100%  5/5    (#9 was ½ until D1 was fixed, §15)
P3 Pipeline/caching  ████████████████████ 100%  3/3
P4 Editor            ███████████████████░  94%  8½/9   #23 raw editor polish: only live duration check
P5 Graph             ████████████████████ 100%  7/7    carve-outs: thumbnail, canvas region/arrow, export layers
P6 Integrity & DX    ████████████████████ 100%  4/4
P7 Tooling           ████████░░░░░░░░░░░░  40%  2/5    #32b extension, #33 QueryAPI, #34 integration+stress ⬜; #36 docs ½
P8 Forensic          █████████████░░░░░░░  64%  4½/7   #37 ✅ (§15.5), #38 ✅ (§15.4), #39 ✅ (§15.6), #40 ✅ (§15.7); #43 ½ (detectors 16/17 missing); #41–#42 ⬜
───────────────────────────────────────────────────────
Overall              █████████████████░░░  87%  41/47
Definition of done   ████████████████░░░░  82%  9/11   the "integration path" ✅ was not true; proof gate ✅ via #38; redaction ✅ via #40
```

Not numbered, not started: §7.4 editor → canvas step highlight; §7.3 (b) diff against the file on
disk, (c) diff in the rename dialog; §7.1 completion filtered by algorithm.

**#37 landed 2026-11-04** (§15.5): every seal is recorded, the chain is checked, an automatic
seal over an unchanged case no longer moves the fingerprint, and an unreadable history is kept
rather than overwritten. **Upstream is not a blocker for anything left:** every API #38–#42 needs exists in
graphene v0.4.0 (`disk.ProveNode`, `ExportNodeProof`, package-level `VerifyExportedProof`,
`CustodyFor*`, `RedactNodeProperties`, `PublishCheckpoint`, `Anchor`, `signing.*`). #40 needed
only rohy's own payload tombstone, which it built first.

**#38 landed 2026-11-04** (§15.4): event proofs, exported from the drawer and checked with
`rohyctl verify-proof`.

**#39 landed 2026-11-04** (§15.6): a custody report per event in the drawer — gaps, never a
verdict — that compares a fingerprint the analyst kept against the current image whatever the
event's state.

**#40 landed 2026-11-05** (§15.7): attributed redaction. The content goes from everywhere rohy
put it: the record, the payload log in place, sibling copies, rule-made relations, the sidecar
copies, then a seal. The removal is recorded with its reason, and a journal keeps the content
from being ingested again. **Next:** #41 signing and attribution (§14.6).

---

## 0. Build log — where we actually are

**Phase 1 complete, and Phase 2's algorithms pulled forward with it.**
`go vet` clean · **backend 15/15 packages green** · **race detector clean** on graphene, evtx,
api, graphbuild, autograph · **frontend 221/221 green**.

| # | Item | State |
|---|---|---|
| 1 | `consts` vocabulary: slots, classes, per-slot bounds, algorithm descriptors, `RuleFormatVersion=2`, error codes, provenance bounds | ✅ |
| 2 | Correlation extraction + wired into all three normalizers + parity tests | ✅ |
| 3 | `Event.CorrKeys`/`CorrKeyVersion`, `Relation` provenance, `rule_id` edge index, `RelationsByRule` | ✅ |
| 4 | `BackfillCorrelationKeys` + `CorrelationKeyStatus` — opt-in, resumable, idempotent | ✅ |
| 5 | `rules` v2: 7 new fields, algorithm-aware validation, `RequiredFormatVersion`, `AppliesTo`, served algorithm descriptors, schema golden | ✅ |
| 6 | `autograph` `Dataset`/`Prepare`, interface change, `sequence` refactored, registry parity test | ✅ |
| — | **`field`, `temporal`, `lineage` algorithms** — pulled forward from Phase 2 (see D-4) | ✅ |
| — | JS validator mirror + consts synced to v2; test schema now reads the Go golden | ✅ |
| — | **v0.1.0 compatibility gate** — all 30 built-ins produce edges identical to an independent reference implementation of the old matcher | ✅ |
| 7 | `RULES.md` rewritten for v2 — §3 split by algorithm, §5 version history + "declare the minimum", §7 the four algorithms with the anchoring and PID-reuse notes, **§8 split per algorithm**, new §9 on correlation fields | ✅ |
| — | **Algorithms explainer page** (new — see §7.7) — route, animated player, trace model, 21 tests | ✅ |
| — | **5 v2 built-in rules** (2 field, 2 temporal, 1 lineage) + behaviour tests | ✅ |
| — | **Benchmarks — the one-sort win measured, not asserted** (see below) | ✅ |
| — | Transport extracted to a pure reducer + 21 tests (closes the gap that produced 4 UI bugs) | ✅ |

### The shared-dataset claim, measured

`BenchmarkBuild` runs a mixed rule set over 50k events preparing **once**;
`BenchmarkBuildPreparingPerRule` is the v0.1.0 shape kept as the comparison, so the win is a
number rather than a comment. Ryzen 9 5980HS, `-benchtime=5x`:

| Rules | Shared prepare | Per-rule prepare | Speed-up | Allocated (shared → per-rule) |
|---:|---:|---:|---:|---|
| 1 | 18.5 ms | 18.0 ms | — | 2.1 MB → 2.1 MB |
| 5 | 58.4 ms | 123.7 ms | **2.1×** | 4.9 MB → 13.3 MB |
| 20 | 222 ms | 480 ms | **2.2×** | 16.2 MB → **56.1 MB** |

Per **rule**, which is the shape that matters: 18.5 → 11.7 → 11.1 ms as the count rises,
against 18.0 → 24.7 → 24.0 ms flat. The falling curve *is* the shared sort. At one rule the two
are identical, exactly as they should be — there is one prepare either way. Allocation is the
starker number: 3.5× less garbage at twenty rules, because nineteen redundant copies of the
partitioned case are no longer built and thrown away.

🟡 If preparation ever moves back inside the per-rule loop, the shared curve goes flat and this
benchmark is where it shows.

**Built-ins:** all 30 now declare `channels` (Security / System / PowerShell-Operational /
Defender-Operational / TerminalServices-Operational / WMI-Activity), which is what will make
the missing-channel detector work on a fresh case. All 30 stay at `format_version: 1`.

### Deviations from the plan as written — and why

**D-1 🔴 Extraction moved from `backend/evtx` to `backend/graphene`.** §4.2 put it in `evtx`.
That is a layering violation: the backfill (§4.3) lives in `graphene`, `graphene` cannot import
`evtx` (the dependency runs the other way), so the backfill could not have reached the
extractor and a second implementation would have been forced — guaranteed to disagree with the
first eventually. It now sits in `backend/graphene/correlate.go` beside
`ComputeNormalizedHash`, which is exactly the argument that function's own comment makes about
identity. All three normalizers call one line, as planned.

**D-2 🟡 Budget breached on first measurement; vocabulary shrunk, ceiling re-derived.**
The 200 B/event figure in §3.1 was an estimate written before any of this existed. First run:
**819 B/event** worst case (12 slots × a shared 64-byte cap) against a 483 B baseline node
record. Two fixes, both correct independently of the number:

- **Per-slot bounds** (`consts.CorrelationSlotMaxLen`) — identifiers 18, image 40, account 32,
  address 45, service 40. A 64-byte cap on a process id was never anything but slack.
- **Two slots dropped**, 12 → 10. `parent_process_name` is redundant (a lineage edge joins the
  parent's creation event to the child's, so the parent's image is already on the other
  endpoint as `process_name`); `session_id` is subsumed by `logon_id`, correlates strictly less
  precisely, and Session 0 alone would have bucketed every service event on a host together.

Measured after: **324 B worst case, 66 B on a representative 4688** — roughly **+8% typical**
against the 867 B/event baseline, to enable the entire correlation feature set. Ceiling set to
400 and enforced. The by-name comparison is pinned in the same test: it would cost **+138
B/event of pure key names**, which is the positional decision paying for itself.

The budget is measured against the **encoded node record**, not the heap. The existing heap
tests carry ~1000 B/event of GC noise — comfortably more than the whole feature's allowance —
so they could not have failed for the reason the budget exists. The properties blob is
literally what the graph holds resident per node.

**D-3 ✅ New requirement found and implemented: a rule must declare the MINIMUM version it
needs, not the build's current one.** `Spec.validate()` defaulted an omitted `format_version`
to `consts.RuleFormatVersion`, which is now 2. Left alone, every newly authored rule would
declare 2 — and be refused by v0.1.0 — even when it uses nothing beyond v1. All 30 built-ins
declare `1` explicitly so they were unaffected, but new user rules would not have been.
`Spec.RequiredFormatVersion()` now returns 2 only when the algorithm needs it; the omitted-
version default, the schema's `format_version` default, and the validator all read it.

**D-4 🔴 The three new algorithms were pulled forward from Phase 2 into Phase 1, because the
registry parity test proved the split was wrong.** The plan put the algorithm *names* in Phase
1 (validation, schema, editor descriptor) and the *implementations* in Phase 2. The parity test
written in Phase 1 item 6 immediately failed:

> `consts.Algorithms accepts "field" at load but no implementation is registered — a rule
> using it would build an empty graph and look like a rule that found nothing`

That is exactly the silent failure the test exists to prevent, and it is not a test being
fussy: between the two phases, a user could author a `field` rule, watch it validate cleanly,
run it, and get an empty graph with nothing anywhere saying why. The two alternatives were both
worse — leaving the suite red across a phase boundary, or adding an `Implemented bool` flag
that exists only to let a half-finished state look green and would be deleted two steps later.

**The vocabulary is the contract between the validator and the engine, so a name enters it when
its implementation does.** `field`, `temporal` and `lineage` are implemented, registered and
tested. Remaining Phase 2 work: new built-in rules using them, and the benchmarks.

**D-6 🔴 `logon_id` could not correlate a session with what it did — found by writing the
built-in rules, which is what they were for.** Windows names the same session differently
depending on the event's point of view: a `4624` records the session it created as
`TargetLogonId`, while everything that session then does — `4688`, `4672`, `4634` — records it
as `SubjectLogonId`. Sourcing `logon_id` from `TargetLogonId` alone populated it on the logon
and left it **empty** on every event the logon produced, so those events were excluded for
having no value and the single most valuable correlation ("what did this session do") matched
nothing — silently, which is the exact failure mode the design exists to prevent.

`logon_id` now means "the logon session this event **concerns**", sourced
`TargetLogonId → SubjectLogonId → LogonId`; `subject_logon_id` remains the explicit actor
session for events that genuinely distinguish the two. `CorrelationKeyVersion` → 2, so anything
projected under recipe 1 is detectably stale and the backfill re-reads it.

Worth noting how it was caught: not by a test, but by trying to write a real rule against real
event shapes. The unit tests were all internally consistent with the wrong model.

**D-5 ⚠️ The JS validator mirror's test schema was a hand-written copy; it now reads the Go
golden.** Syncing the mirror to v2 broke 20 fixture cases — the shared fixture working as
designed. But the deeper problem was the test's local `SCHEMA` literal: fine while the format
was uniform, actively harmful once fields became algorithm-dependent, because the stale copy is
what makes the mirror *agree* with a backend it no longer matches. `validate.test.js` now reads
`backend/rules/testdata/schema-golden.json`, so the golden does double duty and a format change
lands on both sides in one edit.

---

## 1. Repository Audit Summary

### 1.1 Shape of the system

Single-process Wails desktop app. Go backend + Svelte frontend in one binary, no network, no
daemon. Data lives in `rohy-data/` beside the launch directory.

```
main.go / app.go          wiring + lifecycle only
backend/api               6 bound structs, zero business logic ✅
backend/evtx              ingest: parse → normalize → hash → dedup → batch → persist ✅
backend/dbsource          SQLite reader, two documented schemas, no sniffing ✅
backend/capture           durable per-channel live bookmarks ✅
backend/graphene          THE ONLY package that opens the DB ✅
backend/payload           append-only cold store for RawXML + ParsedFields ✅
backend/rules             format, validation, registry, schema descriptor, format/save/import ✅
backend/autograph         correlation engine — pure, writes nothing ✅
backend/graphbuild        the one place rules + events + engine + persistence meet ✅
backend/graphreg          named-graph registry sidecar ✅
backend/layout            canvas positions sidecar ✅
backend/findings          analyst annotations sidecar ✅
backend/utils, consts     primitives + single source of truth for domain vocabulary ✅
frontend/src              routes / components / stores / lib, one api door ✅
```

Layering is enforced by import direction and is genuinely clean. Three consequences that the
v0.2.0 plan leans on heavily:

1. 🔒 `graphene` is the only DB opener. Every new persistence need routes through it.
2. 🔒 `autograph` cannot write — it returns unpersisted `Relation` values. **This is what makes
   the rule testbench a near-free deliverable.**
3. 🔒 `api` holds no business logic, so new bindings are argument adapters, not features.

### 1.2 Data model as it stands

Two entities, one node label (`NODE_EVENT`), one edge label (`EDGE_RELATION`).

`Event` (`backend/graphene/schema.go`) — `id`, `event_id`, `timestamp`, `provider`, `channel`,
`computer`, `user`, `hash_raw`, `hash_normalized`, `source_type`, `source_identifier`,
`deduplication_count`, `source_counts`, `payload Ref`. `RawXML` and `ParsedFields` are tagged
`json:"-"` and live in the cold store.

`Relation` — `id`, `from`, `to`, `graph_id`, `relation_type`, `relation_label`,
`confidence_score`, `created_by`, `created_at`.

Node secondary index: 9 keys. Edge secondary index: 3 keys (`relation_type`, `created_by`,
`graph_id`).

### 1.3 Rule engine as it stands

| Piece | State |
|---|---|
| One file = one rule = one graph | ✅ `rules` + `graphbuild` |
| `Spec` fields: `format_version`, `name`, `description`, `relation_type`, `algorithm`, `sequence`, `labels` | ✅ |
| Located validation (`code`/`field`/`index`/`line`/`col`) | ✅ `validate.go`, one validator, three consumers |
| Format-as-data descriptor served to the UI | ✅ `schema.go` + reflection test |
| Byte-level formatter preserving field order + unknown fields | ✅ `format.go` |
| Registry: builtin `go:embed` + user dir + enabled-state sidecar | ✅ `registry.go` |
| Import (file/folder), save, delete, per-path repair | ✅ `import.go`, `save.go` |
| 30 built-in rules | ✅ `backend/rules/builtin/*.json` |
| Algorithms implemented | ⚠️ **one** — `sequence` |
| Algorithm registry as extension point | ⚠️ exists (`autograph.registry`) but the accepted-name list is hardcoded in `validate.go` |

### 1.4 Editor as it stands (this is the biggest audit surprise)

A large amount of the stated v0.2.0 "editor" scope **already shipped in v0.1.0**:

| v0.2.0 deliverable | Actual state |
|---|---|
| Raw editor improvements | ✅ `RawEditorPanel.svelte`, `highlight.js`, `complete.js` (Ctrl+Space), `CodeArea.svelte` |
| Guided editor MVP | ✅ `GuidedEditorPanel.svelte` + `SequenceBuilder.svelte`, generated from the served schema |
| Unified validation service | ✅ `rules.ValidateSource` (authority) + `lib/rules/validate.js` (latency mirror), kept in step by the shared `testdata/validation-cases.json` fixture |
| Rule formatter | ✅ `rules.Pretty` / `Minify`, byte-level |
| Import pipeline | ✅ `ImportRuleFiles` / `ImportRuleFolder` |
| Export pipeline | ⚠️ single-rule download exists (`RuleEditorDialog.svelte:97` → `lib/export.js:download`); no bundle/multi-rule export |
| Rule diffing | ⚠️ `lib/rules/diff.js` + `RuleDiffView.svelte` exist; raw-text only, no guided/field-level view |
| Undo/redo | ✅ `lib/rules/history.js`, one history shared across both modes, coalesced typing |
| Keyboard shortcuts | ✅ `lib/shortcuts.js` — single definition drives handler **and** help dialog |
| Dark/light theme | ✅ `stores/theme.js`, `lib/consts/theme.js` |
| Rule testbench | ❌ |
| Correlation algorithm selector | ⚠️ the control would generate itself, but the enum has exactly one value |
| Shared selection editor ↔ graph | ❌ `stores/selection.js` covers event detail + graph queue only |

### 1.5 Graph / timeline as it stands

✅ Multiple named graphs, per-graph layout sidecar, manual + rule-generated edges, connect
mode, box select, fit-to-content, virtualized canvas, backend density histogram with lanes,
zoom/pan/scrub/range-select, shared selection between timeline and canvas.

⚠️ "Auto-layout" is a **fixed grid** computed in the frontend
(`GraphCanvas.svelte:426-434`) — column/row arithmetic, no graph-structural awareness.

❌ Snapshot/restore, annotation layers, replay, heatmap, cluster grouping, relationship
inspector.

### 1.6 Testing, CI, performance as they stand

✅ CI (`.github/workflows/ci.yml`): vet, full backend suite, race detector on the four
concurrent packages, frontend suite, then a 4-target build matrix that deletes
`frontend/dist` first. Release workflow gates on all six artefacts arriving.

✅ 28 benchmarks, a memory test, a scale test, race on/off variants, consistency tests,
crash/idempotency tests, a reflection test, a lifecycle test.

✅ `PERFORMANCE.md` records a measured cost model and one optimisation that made cold start
10× worse before it was measured.

❌ Schema regression golden, integration-tagged end-to-end suite, rule-lint CI job, rule
caching layer, dedicated stress/profiling harness.

---

## 2. Architectural Gaps

Ordered by how much they constrain v0.2.0.

### GAP-1 🔴🟡 — Correlation fields are not reachable at correlation time

**This is the gap that decides the shape of the release.**

Field correlation, temporal correlation and process lineage all need EventData: `TargetLogonId`,
`SubjectLogonId`, `ProcessId`, `NewProcessId`, `ParentProcessId`, `NewProcessName`,
`TargetUserName`, `IpAddress`.

Those live in `Event.ParsedFields`, which is:

- tagged `json:"-"` — **not in the node record** (`schema.go:36-37`);
- stored in the payload cold store, deliberately, because it is ~70% of an event's resident
  cost (3137 → 867 B/event, `PERFORMANCE.md`);
- readable only one reference at a time — `payload.Store.Read(Ref)` takes a single `Ref`, does
  one `ReadAt`, and there is no batch form;
- never hydrated by `QueryEvents` — only `GetEvent` hydrates (`graphene_query.go:252-257`).

So a naive field-correlation build over 100k events costs 100k random reads plus 100k JSON
decodes of multi-kilobyte blobs — re-incurring, per build, exactly the cost the cold store was
created to remove. Not acceptable, and not fixable by making the lookup faster: per
`PERFORMANCE.md`, decoding is the cost, not locating.

### ✅ REVERSED LATER — the format stayed at version 1

The plan called for `RuleFormatVersion = 2`, it was implemented, and it was then **removed
again** after the question "why maintain two versions?" was put to it. The audit reasoning below
is left as written, because the correction is more useful than a tidy plan.

**What the bump was for:** stopping a v0.1.0 build from reading a `field` rule, matching on the
event-ID sequence alone, and producing a graph that is *wrong* rather than *absent*.

**Why it was not needed:** v0.1.0 already refuses unknown algorithm names —

```go
if a := trimmed(s.Algorithm); a != "" && a != consts.AlgoSequence { /* unknown algorithm */ }
```

— so a `field` rule is rejected by an old build with or without the bump, and with a *better*
message: it names the missing matcher instead of a version number. For all three new algorithms
the version was **redundant**.

**The one case where it was not** — and which the implementation had missed. `match_scope` was
given `AppliesTo: all algorithms`, including `sequence`, while `RequiredFormatVersion()` only
consulted the algorithm. Probed directly:

```
{"format_version":1, "algorithm":"sequence", "match_scope":"global", ...}
  → valid=true, required version = 1
```

A v0.1.0 build accepts that, ignores `match_scope` as unknown, scopes by computer, and silently
produces a narrower graph. The exact failure the versioning existed to prevent, unguarded by it.

**Resolution.** `match_scope` now belongs to `field`, `temporal` and `lineage` only. Global scope
is meaningless without `match_fields` and a `sequence` rule has none — `RULES.md` §7 already
advised against the combination. With it gone, every v0.2.0 field is reachable only through a
v2-only *algorithm name*, which old builds refuse by name, so nothing is left for a version to
protect. Neither old nor new builds read it for `sequence`, so there is no divergence at all.

**Deleted:** `RuleFormatVersion=2`, `AlgorithmDescriptor.MinFormatVersion`,
`Field.RequiresFormatVersion`, `Spec.RequiredFormatVersion()`, `RuleErrAlgorithmNeedsFormat`,
the JS `formatVersionShortfall`/`requiredFormatVersion`, and the editor's "set format_version to
2" affordance. All 35 built-ins now declare `1`.

🔒 The `format_version` **mechanism** is untouched and still refuses a from-the-future file. What
changed is only the judgement that v0.2.0 was not a breaking change. `RULES.md` §5 records the
reasoning and states the one thing that *would* force a bump: a field that changes what an
**existing** algorithm matches.

### GAP-2 🔴 — The rule format cannot express any matcher but a sequence

`Spec` is fixed: `sequence` + `labels`. There is nowhere to say "match on `logon_id`",
"within 5 minutes", or "these are the process-creation event ids". `RULES.md` §7 reserves the
axes but nothing is half-implemented, which is correct — and it means v0.2.0 must add the
fields, and per §5 **a new `algorithm` value is a breaking change that bumps
`format_version` to 2.**

### GAP-3 — Algorithm names are hardcoded in the validator, not derived from the registry

`autograph.registry` is the stated extension point (`autograph.go:41-43`), but
`validate.go:179` checks `a != consts.AlgoSequence`. Nothing prevents an algorithm being
registered and rejected at load, or accepted at load and unregistered. `rules` cannot import
`autograph` (the dependency runs the other way), so this needs a neutral vocabulary.

### GAP-4 — Every rule re-sorts the same dataset

`graphbuild` reads the dataset once ✅, then hands the same slice to each rule, and
`sequenceAlgorithm.Generate` re-filters undated, re-groups by scope, and re-sorts every scope
**per rule** (`sequence.go:30-53`). At 20 rules that is 20 sorts of the same data. The
`Algorithm` interface takes `[]*graphene.Event`, so there is no seam to share preparation.

### GAP-5 — A relation cannot say why it exists

`Relation` carries `created_by` and `confidence_score` and nothing else. It does not record
which rule produced it, which algorithm, which occurrence it belongs to, which step of the
sequence it is, or what the match was based on. A "Graphene DB relationship inspector" has
nothing to inspect, the editor cannot link an edge back to a rule step, and rebuild-orphan
detection is impossible.

### GAP-6 — Correlation establishes far less than it appears to

`RULES.md` §8 states it honestly: a sequence match proves a temporally ordered pairing on one
host and nothing more — not the same user, account, or logon session. This is the actual
analytical limitation of v0.1.0, and GAP-1/GAP-2 are what it takes to retire it.

### GAP-7 — Layout is a grid, and it lives in the wrong layer

`autoLayout()` is 8 lines of column arithmetic in a Svelte component. Structure-aware profiles
(sequence, lineage, resource, temporal) need graph traversal, are worth unit-testing, and want
to be reusable by snapshots and future export — none of which a component method can offer.

### GAP-8 — No maintenance/integrity surface, despite the primitives existing

`VerifyIndexes`, `RebuildIndexes`, `RepairRelationIndex` and `AuditFindings` all exist and are
well-reasoned. None is reachable from the UI, none is composed into a single report, and there
is no detector for the two failure modes analysts actually hit: a rule that cannot fire because
its channel was never ingested, and a rule that is inert because its event ids are absent.

### GAP-9 — No developer surface outside the GUI

`rules` depends only on `consts`, so a CLI is cheap — but no `cmd/` exists, no `main` package
other than the Wails app, and no editor tooling.

### GAP-10 ⚠️ — Sharp edges that new work must not trip over

Carried forward verbatim from `LLD.md` §23 because v0.2.0 touches all of them:

- `orderKey()` must cover every field that changes what matches. Miss it → wrong results.
- 🔒 `EdgesOf(..., nil)` assumes **one** edge label. Introducing a second edge type breaks
  `RelationsOf` and the adjacency reasoning. → v0.2.0 will not introduce one (see §5.4).
- A rule's id is a slug of its name; renaming replaces.
- `payloads.log` never shrinks.
- Cross-source dedup depends on two normalizers hashing identically — a third normalizer, or a
  change to the field list, breaks it silently. 🔒 `FindingsHashVersion` must be bumped in the
  same change if the recipe ever moves.

### GAP-11 🔴 — The evidence store cannot vouch for its own contents

Found after the v0.2.0 audit, and it is the largest gap remaining. See **§14** for the plan.

`graphene_store.go:73` calls `graphene.Open(dir)` — the engine's historical default: commits
unsigned, image unverified. No rohy file imports `graphene/disk`, `graphene/signing` or
`graphene/merkle`. Every graphene call rohy makes is storage, index or query.

Unused, and all of it already paid for by a dependency rohy ships:

| Available | rohy today |
|---|---|
| Ed25519-signed commits, `RequireSignedCommits` | unsigned — and unsigned commits can **never** be attributed retrospectively |
| `VerifyOnOpen` — re-derives record hashes | off; only structural validation runs |
| Merkle snapshot roots (`Compact()` computes them **unconditionally**) | computed and discarded |
| `ProveNode` / `ExportNodeProof` — third-party-verifiable inclusion proof, no store, no key | nothing |
| `RedactNodeProperties` + ledger + redaction proofs | only `DeleteNode`, which is unattributed |
| `CustodyFor` — chain of custody per entity | nothing |
| `PublishCheckpoint` / `VerifyAgainstAnchor` | nothing |
| `Audit`, `Retention` | off; every pre-compaction actor and signature is discarded at compaction |

Also unused, and relevant to §13 item 33: `BFSIDs`, `BFS`/`DFS`, `ProvenanceChain`,
`ShortestPath`, `FindPatterns` (subgraph matching), `HasCycle`, `InducedSubgraph`.

**Why this is a 🔴 and not a nice-to-have.** rohy's entire analytical thesis is that inferred ≠
asserted — `RULES.md` §8 is careful to the point of pedantry about what a match establishes, and
Phase 4 built a whole provenance surface so an analyst can check a rule's claim. One layer down,
the tool cannot establish that the event under that claim is the event that was ingested. The
honesty discipline stops exactly where the evidence starts.

Two things make it urgent rather than merely open:

- **Signing cannot be retrofitted.** Commits already written are unsigned forever. Every case
  ingested before this lands is permanently unattributable.
- **Roots are already being computed and thrown away.** `Compact()` produces them whether or not
  anyone asks. The keyless half of the machinery — which is where nearly all the value is — costs
  nothing beyond calling it.

---

## 3. Proposed v0.2.0 Architecture

### 3.1 The one load-bearing decision: the correlation key projection

Resolve GAP-1 with a **bounded, positional projection of correlation-relevant fields, computed
at normalize time and stored in the node record.**

```go
// backend/consts — the vocabulary. Order is the wire format; append only. 🔒  ✅ SHIPPED
var CorrelationSlots = []string{
    "logon_id",            // TargetLogonId / LogonId       — identifier, ≤18
    "subject_logon_id",    // SubjectLogonId                — identifier, ≤18
    "process_id",          // verbatim ProcessId            — identifier, ≤18
    "new_process_id",      // NewProcessId                  — identifier, ≤18
    "parent_process_id",   // ParentProcessId/CreatorProcessId — identifier, ≤18
    "process_name",        // basename, lowercased          — image,      ≤40
    "target_user",         //                               — plain,      ≤32
    "subject_user",        //                               — plain,      ≤32
    "ip_address",          // the initiator; alias src_ip   — address,    ≤39 (RFC 5952)
    "service_name",        //                               — plain,      ≤40
    "dst_ip",              // v0.3.0 — the responder        — address,    ≤39
    "dst_port",            // v0.3.0                        — port,       ≤5
    "protocol",            // v0.3.0 — IANA keyword         — protocol,   ≤10
}
const CorrelationSlotCount   = 13  // was 10 at v0.2.0
const CorrelationValueMaxLen = 40  // the ceiling the per-slot bounds sit under (was 45)
const CorrelationKeyVersion  = 3   // bump when the vocabulary or extraction changes (§15)
var  CorrelationSlotMaxLen   = map[int]int{ /* per-slot, sized to its domain — see D-2 */ }
```

Values are canonicalized on the way in so two spellings of one thing compare equal:
identifiers to lowercase `0x`-hex from either decimal or hex input (a PID written `420` by one
provider and `0x1A4` by another must correlate); images to lowercased basename across both
path separators (a case is routinely analysed on a different platform from the one it was
captured on); `-`, the null SID, `null`, `n/a` treated as **absent**, never as a value —
bucketing every event carrying a hyphen together is precisely how a field matcher becomes a
false-positive engine. Zero is absent for **identifiers only**, since zero is a real value
elsewhere.

```go
// backend/graphene/schema.go — added to Event
// CorrKeys is the positional projection of CorrelationSlots. Values are stored by POSITION,
// not by name, so the key vocabulary costs zero bytes per event. Trailing empties are
// trimmed. CorrKeyVersion records which extraction recipe produced them, so a vocabulary
// change is a detectable, resumable backfill rather than a silent wrong answer.
CorrKeys       []string `json:"ck,omitempty"`
CorrKeyVersion int      `json:"ckv,omitempty"`
```

Why positional and not `map[string]string`: a map repeats every key name on every event
forever. Twelve names averaging 15 bytes is ~180 B/event of pure vocabulary — comparable to the
entire budget. A slice costs the values and two bytes of framing per slot.

**Budget ✅ measured** — estimated at ≤200 B/event, measured at 819, fixed to **324 B/event
worst case and 66 B typical**; ceiling enforced at 400 by `TestCorrelationProjectionBudget`,
which fails the build when exceeded. See **D-2** for what changed and why. The response to a
breach is to shrink the vocabulary, never to raise the ceiling — this mirrors the project's
existing discipline: `PERFORMANCE.md` exists because a textbook optimisation was measured and
reverted.

**Not indexed.** No `CorrelationSlots` value gets a secondary-index entry in v0.2.0. Every
indexed key costs memory on every event forever, `logon_id` is near-unique (the same shape
that made a declared `timestamp` key cost 8.0 s vs 0.8 s at open), and correlation reads the
whole matching set once per build and builds in-memory maps from it — the index would
accelerate only ad-hoc filtering, which is not in scope. Revisit **with a measurement** if
slot-based event filtering is ever a deliverable.

**Backfill.** Existing cases have `CorrKeyVersion == 0`. `graphene.BackfillCorrelationKeys`
pays the cold-store read once per event, batched and progress-reported, updating nodes in
bounded transactions. It is:

- **opt-in** — a maintenance action, never a startup step, for exactly the reason
  `VerifyIndexes` is not one: the cost is proportional to the whole store;
- **resumable** — skips events already at the current `CorrKeyVersion`;
- **idempotent** — a second run does nothing;
- **honest** — a build over a store with un-backfilled events reports how many, and the field/
  temporal/lineage algorithms count them as `SkippedNoKeys` rather than treating an absent
  projection as an absent field.

### 3.2 The prepared dataset

Resolve GAP-4 by giving the engine a preparation seam.

```go
// backend/autograph
type Dataset struct {
    Events         []*graphene.Event            // chronological, undated already removed
    ScopeOrder     []string                     // sorted — determinism for the match cap
    ByScope        map[string][]*graphene.Event // each slice chronological (inherited, not re-sorted)
    ByKey          map[KeySpec]map[string][]*graphene.Event
    Lifetimes      map[string]map[string][]ProcInterval // computer → pid → intervals
    SkippedUndated int
    SkippedNoKeys  int
}

func Prepare(events []*graphene.Event, need Requirements) *Dataset   // pure, deterministic
```

- `Requirements` is the **union of what the selected rules actually ask for** — which slot
  tuples to bucket, whether process lifetimes are needed. A build of sequence-only rules pays
  nothing extra.
- The global chronological sort happens **once**. Scope groups and key buckets are built by
  stable single passes, so every bucket is chronological for free — no per-bucket sort at all.
- 🔒 `Dataset` is **immutable after construction**. No lazy memoization: a mutable shared
  dataset would be a race the moment rules are ever evaluated concurrently, and the purity of
  `autograph` is what makes the whole engine testable.

🔴 `Algorithm.Generate(spec *rules.Spec, events []*graphene.Event)` becomes
`Generate(spec *rules.Spec, ds *Dataset)`. Internal interface, one existing implementation.

### 3.3 The algorithm vocabulary

Resolve GAP-3 without inverting the dependency (`autograph → rules`, so `rules` cannot see the
registry):

```go
// backend/consts
const (
    AlgoSequence = "sequence"
    AlgoField    = "field"
    AlgoTemporal = "temporal"
    AlgoLineage  = "lineage"
    DefaultAlgorithm = AlgoSequence
)
type AlgorithmDescriptor struct {
    Name          string
    MinFormatVer  int      // sequence:1, everything else:2
    RequiresSeq   bool
    Fields        []string // which prefixed fields apply
    Summary       string
}
var Algorithms = []AlgorithmDescriptor{ ... }
```

`rules` validates against `consts.Algorithms`. `autograph` registers implementations under the
same names. **A test in `autograph` asserts `registry` keys and `consts.Algorithms` names are
exactly equal in both directions** — so an algorithm accepted at load but not implemented, or
implemented but rejected at load, fails the build rather than half-running.

### 3.4 Rule format version 2

Per `RULES.md` §5, new `algorithm` values are breaking → `RuleFormatVersion = 2`. 🔴

**Flat, prefixed top-level fields — not nested objects.** Reasoning, because the alternative
looks tidier and is the wrong call here:

- `scanPositions` (`validate.go:301`) addresses exactly two shapes: `field` and `field[i]`.
  Nested objects would require extending it to dotted paths.
- `ValidationError.{Field,Index}` is the guided form's addressing scheme, and the form's
  controls are keyed the same way.
- `complete.js` builds the raw editor's completion list from the flat descriptor.

Nesting buys no expressiveness and costs a change to the position scanner, the error
addressing, the form, and the completion list simultaneously. Flat fields cost one entry each
in `Describe()`.

```jsonc
{
  "format_version": 2,
  "name": "Failed Logons Then Success, Same Logon Session",
  "algorithm": "field",
  "sequence": ["4625", "4625", "4624"],
  "labels": ["", "then succeeds"],

  "match_fields": ["logon_id"],      // field
  "match_scope":  "computer",        // field | temporal | sequence  (default "computer")
  "window_within": "5m",             // temporal — between consecutive matched steps
  "window_total":  "30m",            // temporal — first→last, optional
  "lineage_create_ids": ["4688"],    // lineage
  "lineage_depth": 0,                // lineage — 0 = direct edges only
  "channels": ["Security"]           // metadata, ALL algorithms — non-breaking (§5)
}
```

`channels` is an optional field with a safe default, so per `RULES.md` §5 it does **not**
require the version bump — it would have been legal in v1. It is what makes the missing-channel
detector possible (§12).

### 3.5 Relation provenance

```go
// backend/graphene — added to Relation
RuleID     string   `json:"rule_id,omitempty"`
Algorithm  string   `json:"algorithm,omitempty"`
MatchID    string   `json:"match_id,omitempty"`   // groups every edge of one occurrence
StepIndex  int      `json:"step_index,omitempty"` // which consecutive pair this is
Basis      []string `json:"basis,omitempty"`      // ≤4 entries, ≤64 chars: WHY these two joined
RelVersion int      `json:"rel_v,omitempty"`      // 0 = written before provenance existed
```

`Basis` is the deliverable's actual value: `["logon_id=0x3e7"]`, `["Δt=42s ≤ 5m"]`,
`["ppid=4180", "image=powershell.exe"]`. It is what turns "inferred ≠ asserted" from a colour
into an auditable claim.

**Edge index gains `rule_id` only.** `match_id` is near-unique and would bloat the property
index and slow open for the same reason a declared timestamp key did — and nothing queries by
it; the inspector reaches it through the edges it already has. `rule_id` is low-cardinality and
is needed to find a rule's edges without knowing its graph id (orphan detection, inertness,
inspector). Edge index goes from 3 keys to 4.

🔒 Still **one node label and one edge label.** Process lineage is modelled as
event → event edges (a creation event points at the creation event of its child), not as
`Process` nodes. Introducing a second edge type would break `EdgesOf(..., nil)` and the
`RelationsOf` reasoning across the whole query layer (`GAP-10`). The event-to-event model is
also the more honest one: the evidence is the record, not an inferred entity.

### 3.6 New packages

```
backend/caseintegrity   composed integrity report; reads, never repairs without being asked ❌
backend/graphlayout     pure layout profiles: relations + metadata in, positions out ❌
backend/snapshot        graph snapshot sidecar ❌
backend/annotate        graph annotation-layer sidecar ❌
cmd/rohyctl             CLI: validate, format, explain ✅
tools/vscode-rohy       editor extension + generated JSON Schema ❌
tools/genschema         go run target emitting the JSON Schema from rules.Describe() ✅
```

All sit at or below the existing layers. `graphlayout`, `caseintegrity`, `snapshot` and
`annotate` are pure or sidecar-only; none opens the database.

### 3.7 Dependency graph after v0.2.0

```
app.go / main.go
  └── backend/api
        ├── evtx, dbsource                (ingest)
        ├── graphbuild ──► autograph ──► rules ──► consts
        │        └──────► graphene ──► payload, utils
        ├── caseintegrity ──► graphene, rules, findings, graphreg, layout
        ├── graphlayout   ──► graphene (types only), consts
        ├── snapshot, annotate, graphreg, layout, findings, capture   (sidecars)
        └── rules
cmd/rohyctl ──► rules, consts, version          (never graphene — no DB in the CLI) 🔒
```

---

## 4. Module-by-Module Plan

### 4.1 `backend/consts`

1. Add `CorrelationSlots`, `CorrelationSlotCount`, `CorrelationValueMaxLen`,
   `CorrelationKeyVersion`.
2. Add `AlgoField`, `AlgoTemporal`, `AlgoLineage`, `AlgorithmDescriptor`, `Algorithms`.
3. Bump `RuleFormatVersion` to `2`. 🔴
4. Add rule error codes: `RuleErrMatchFieldsRequired`, `RuleErrUnknownMatchField`,
   `RuleErrBadDuration`, `RuleErrSequenceNotAllowed`, `RuleErrLineageIDsRequired`,
   `RuleErrFieldNotForAlgorithm`, plus message templates.
5. Add `RelationSchemaVersion`, `PropRuleID`, `MaxBasisEntries`, `MaxBasisLen`.
6. Add integrity codes: `IntegrityChannelMissing`, `IntegrityRuleInert`,
   `IntegrityDanglingRelation`, `IntegrityOrphanGraph`, `IntegrityOrphanLayout`,
   `IntegrityPayloadTail`, `IntegrityKeysStale`.

**Data flow:** none — vocabulary only. **Integration:** everything reads from here; no other
package may introduce a domain string.

### 4.2 `backend/evtx` (+ `dbsource`)

1. Add `backend/evtx/correlate.go`: `ExtractCorrelationKeys(parsed map[string]string) []string`
   — pure, positional, case-normalizing, length-capped, alias-aware
   (`TargetLogonId`/`SubjectLogonId`/`LogonId`; `NewProcessId`/`ProcessId`; …).
2. Call it in `normalizeRecord` (`evtx_normalize.go:46`), in `evtx_xml_normalize.go`, and in
   `dbsource` — all three producers, so a `.evtx` file and the live channel project identically.
3. 🔒 **Do not touch `ComputeNormalizedHash`.** Correlation keys must not enter event identity:
   the same event read from a file and from the live log must still collapse, and changing the
   recipe would orphan every existing finding.
4. Test: an XML-path/file-path parity test asserting both normalizers project the same slots
   for the same event — mirroring the existing hash-parity discipline.

### 4.3 `backend/graphene`

1. `schema.go`: add `CorrKeys`, `CorrKeyVersion` to `Event`; `RuleID`, `Algorithm`, `MatchID`,
   `StepIndex`, `Basis`, `RelVersion` to `Relation`; add `consts.PropRuleID` to
   `Relation.indexValues()`.
2. `graphene_order.go`: add `CorrKeys []string \`json:"ck"\`` to `eventSortView` **only if**
   slot-based filtering ships; otherwise leave it out so ordering stays cheap. Current plan:
   **leave it out.** ⚠️ If any filter field is added, `orderKey()` must be updated in the same
   commit — this is the step the LLD says people forget.
3. New `graphene_correlate.go`:
   - `BackfillCorrelationKeys(ctx, progress func(done, total int)) (BackfillResult, error)` —
     resumable, idempotent, bounded transactions, `bumpVersion()` on completion.
   - `CorrelationKeyStatus() (total, current, stale int, err error)` for the integrity report.
4. New `graphene_heatmap.go`: `RelationHeatmap(graphID uint64, buckets int, groupBy string)`,
   reusing the timeline's bucketing so the two can never disagree.
5. New `graphene_integrity.go`: `DanglingRelations()`, `RelationsByRule(ruleID)`,
   `GraphIDsInStore()`, `PayloadTailBytes()`.
6. 🔒 Every new write path calls `bumpVersion()` via `defer`, matching every existing one.

### 4.4 `backend/rules`

1. `rules.go`: extend `Spec` with `MatchFields []string`, `MatchScope string`,
   `WindowWithin string`, `WindowTotal string`, `LineageCreateIDs []string`,
   `LineageDepth int`, `Channels []string`. Extend `normalize()` to trim and default each.
2. `validate.go`: make `problems()` **algorithm-aware**. Resolve the algorithm first, look it
   up in `consts.Algorithms`, then dispatch:
   - unknown algorithm → single error, stop (as the future-format check already does);
   - algorithm requires `format_version >= 2` and the file declares 1 → explicit error naming
     both, not a generic "unknown algorithm";
   - `sequence` required for `sequence`/`field`/`temporal`; **not** required for `lineage`, and
     present-but-unused fields are a warning, never an error (unknown fields are ignored per
     §3, so a field belonging to a different algorithm must be equally tolerant);
   - `match_fields` non-empty and every entry in `consts.CorrelationSlots` for `field`;
   - `window_within` parses as a Go duration, is > 0, and `window_total >= window_within`;
   - `lineage_create_ids` non-empty and no blank entries.
3. `schema.go`: add `AppliesTo []string` to `Field`; describe the seven new fields; add the
   `MinFormatVersion` per algorithm to `Schema`. The reflection test already fails the build if
   a `Spec` field is undescribed — it now also asserts every `AppliesTo` names a real algorithm.
4. `format.go`: no change 🔒 — it works on bytes and already preserves unknown fields and order.
   Add fixture cases for v2 files to `testdata/format-cases.json`.
5. `import.go` / `save.go`: no logic change; add v2 fixtures.
6. `builtin/*.json`: add `"channels"` to all 30 files (no version bump needed). Add ~6 new
   built-ins demonstrating the new algorithms, at `format_version: 2`. ⚠️ Existing 30 stay at
   v1 so they keep loading on a v0.1.0 build.

### 4.5 `backend/autograph`

1. `dataset.go` — `Requirements`, `Prepare`, `Dataset`, `ProcInterval`, `KeySpec`.
2. 🔴 `Algorithm.Generate(spec, ds)`; `Generate(spec, ds)` dispatches through the registry.
3. `sequence.go` — read `ds.ByScope` instead of re-filtering/grouping/sorting. Behaviour
   unchanged; the existing tests must pass untouched. 🔒
4. `field.go`, `temporal.go`, `lineage.go` — §6.
5. `provenance.go` — `MatchID` derivation. **Deterministic, never random:** the digest of
   `ruleID | scope | first node id | last node id`. Same inputs → same match ids, which is what
   keeps a rebuild byte-identical and makes diffing two builds meaningful.
   _As built (2026-11-03 audit): `m-<first>-<last>-<len>` in `autograph/sequence.go`, with no rule
   id or scope — deterministic, which is the property that matters, and unique per rule because
   relations are looked up through the `rule_id` index first._
6. Per-algorithm match caps replacing the single package-level `maxMatches` var, still seeded
   from consts, still counting-past-the-cap so `Truncated`/`Dropped` stay exact. 🔒

### 4.6 `backend/graphbuild`

1. Compute `Requirements` from the selected rules; call `autograph.Prepare` **once**; pass the
   `Dataset` to every rule.
2. Stamp `RuleID`, `Algorithm`, `RelVersion` on every relation at persist time, beside the
   existing `GraphID` and `CreatedAt`. 🔒 Provenance is stamped by the backend, never accepted
   from the caller — same rule `CreatedAt` already follows.
3. Extend `Result` with `SkippedNoKeys`, `UnresolvedParents`, `StaleCorrelationKeys`,
   `DatasetPrepareMs`.
4. Add `DryRun(ctx, spec *rules.Spec, filter, limit) (DryRunResult, error)` — prepares (or
   reuses the cached dataset), generates, returns matches and sample relations, **persists
   nothing.** This is the testbench, and it is small precisely because `autograph` cannot write.
5. Add the dataset cache (§11.3).
6. 🔒 Keep `RepairRelationIndex()` first, before anything is cleared. Keep the empty-graph
   discard and its two refusals.

### 4.7 `backend/api`

New/changed bindings, all argument-adapters with no logic:

| Struct | Method | Purpose |
|---|---|---|
| `BuildAPI` | `DryRunRule(source string, q EventQuery, limit int)` | testbench ❌ |
| `RulesAPI` | `ExportRules(ids []string)` | bundle export ❌ |
| `RulesAPI` | `RuleSchema()` | ✅ unchanged shape, richer content |
| `GraphAPI` | `ComputeLayout(graphID uint64, profile string)` | layout profiles ❌ |
| `GraphAPI` | `Clusters(graphID uint64)` | cluster grouping ❌ |
| `GraphAPI` | `RelationHeatmap(graphID uint64, buckets int, groupBy string)` | heatmap ❌ |
| `GraphAPI` | `InspectRelation(id uint64)` | relationship inspector ❌ |
| `GraphAPI` | `SaveSnapshot / ListSnapshots / RestoreSnapshot / DeleteSnapshot` | snapshots ❌ |
| `GraphAPI` | `ListLayers / SaveLayer / DeleteLayer / SaveAnnotation / DeleteAnnotation` | annotations ❌ |
| `SystemAPI` | `CheckCaseIntegrity(deep bool)` | integrity report ❌ |
| `SystemAPI` | `BackfillCorrelationKeys()` + `maintenance:progress` event | backfill ❌ |
| `QueryAPI` | `Describe()`, `Run(spec QuerySpec)` | read-only console ❌ |

New event channels (defined in `consts`, used on both sides): `maintenance:progress`,
`maintenance:complete`, `build:dryrun` (if streaming is wanted; otherwise the call returns
directly).

🔒 `QueryAPI` holds `*graphene.Store` but exposes **only** read methods. No mutation binding is
added to it, ever.

### 4.8 Frontend

| Path | Work |
|---|---|
| `lib/consts/index.js` | mirror the new algorithm names, slot names, error codes, integrity codes, layout profiles |
| `lib/rules/validate.js` | mirror the algorithm-aware validation; **first step is adding the cases to `backend/rules/testdata/validation-cases.json`** 🔒 |
| `lib/rules/complete.js` | filter completions by the current `algorithm` value |
| `lib/rules/diff.js` | add a field-level structural diff alongside the text diff |
| `stores/ruleEditor.js` | algorithm state, testbench results, dry-run call, export |
| `stores/selection.js` | add the shared rule/edge/step selection slice |
| `stores/graph.js` | layout profiles, clusters, snapshots, layers, replay state |
| `components/rules/` | `AlgorithmSelect.svelte`, `MatchFieldsPicker.svelte`, `WindowEditor.svelte`, `LineageEditor.svelte`, `TestbenchPanel.svelte` |
| `components/graph/` | `RelationInspector.svelte`, `LayerPanel.svelte`, `SnapshotPanel.svelte`, `ReplayControls.svelte`, `HeatmapOverlay.svelte`, `ClusterHull.svelte` |
| `routes/` | `MaintenanceView.svelte` (integrity + backfill + query console) |

---

## 5. Graphene DB Schema Plan

### 5.1 Node — `NODE_EVENT` (unchanged label 🔒)

| Field | JSON | Type | v0.1.0 | v0.2.0 | Indexed |
|---|---|---|:---:|:---:|:---:|
| id | — | uint64 | ✅ | ✅ | store-assigned |
| event_id | `event_id` | string | ✅ | ✅ | ✅ |
| timestamp | `timestamp` | time | ✅ | ✅ | ✅ (not declared-ordered 🔒) |
| provider | `provider` | string | ✅ | ✅ | ✅ |
| channel | `channel` | string | ✅ | ✅ | ✅ |
| computer | `computer` | string | ✅ | ✅ | ✅ |
| user | `user` | string | ✅ | ✅ | ✅ |
| hash_raw | `hash_raw` | string | ✅ | ✅ | ❌ |
| hash_normalized | `hash_normalized` | string | ✅ | ✅ | ✅ |
| source_type | `source_type` | string | ✅ | ✅ | ✅ |
| source_identifier | `source_identifier` | string | ✅ | ✅ | ❌ (post-hydration) |
| deduplication_count | `deduplication_count` | int | ✅ | ✅ | ❌ |
| source_counts | `source_counts` | map | ✅ | ✅ | ❌ |
| payload | `payload` | Ref | ✅ | ✅ | ❌ |
| search_blob | — | derived | ✅ | ✅ | ✅ |
| **corr_keys** | `ck` | `[]string` ≤13 (≤10 at v0.2.0) | ❌ | ✅ **shipped** | ❌ **deliberately** |
| **corr_key_version** | `ckv` | int | ❌ | ✅ **shipped** | ❌ |
| raw_xml | — | string | cold store | cold store 🔒 | never |
| parsed_fields | — | map | cold store | cold store 🔒 | never |

Node index stays at **9 keys**. No growth.

**`ck` positional encoding** — index → slot, per `consts.CorrelationSlots`:

```
0 logon_id            4 parent_process_id    8  ip_address      12 protocol
1 subject_logon_id    5 process_name         9  service_name
2 process_id          6 target_user          10 dst_ip
3 new_process_id      7 subject_user         11 dst_port
```

(Corrected 2026-11-03. The table first drafted here kept `parent_process_name` and `session_id`,
which were measured out before shipping — D-2 — and so had positions 6–11 wrong. Positions 10–12
were appended in v0.3.0, §15.)

🔒 Append-only. Reordering or reusing a slot silently reinterprets every stored event; a
vocabulary change bumps `CorrelationKeyVersion`, which makes the backfill re-run.

Normalization rules (applied at extraction, so comparison is a plain string equality):
lowercase hex for ids (`0x3E7` → `0x3e7`), basename-only lowercased for image paths
(`C:\Windows\System32\cmd.exe` → `cmd.exe`), `-` and `S-1-0-0` and `0x0` treated as **absent**
(they are Windows' "not applicable", and bucketing them together is the classic
false-positive generator), values truncated to each slot's own bound (≤40 bytes) on a rune
boundary — except addresses, ports and protocols, which are dropped rather than truncated when
they do not parse (§15).

### 5.2 Edge — `EDGE_RELATION` (unchanged label 🔒)

| Field | JSON | Type | v0.1.0 | v0.2.0 | Indexed |
|---|---|---|:---:|:---:|:---:|
| id / from / to | — | uint64 | ✅ | ✅ | adjacency |
| graph_id | `graph_id` | uint64 | ✅ | ✅ | ✅ |
| relation_type | `relation_type` | string | ✅ | ✅ | ✅ |
| relation_label | `relation_label` | string | ✅ | ✅ | ❌ |
| confidence_score | `confidence_score` | float | ✅ | ✅ | ❌ |
| created_by | `created_by` | string | ✅ | ✅ | ✅ |
| created_at | `created_at` | time | ✅ | ✅ | ❌ |
| **rule_id** | `rule_id` | string | ❌ | ✅ **new** | ✅ **new** |
| **algorithm** | `algorithm` | string | ❌ | ✅ **new** | ❌ (low value, derivable from rule) |
| **match_id** | `match_id` | string | ❌ | ✅ **new** | ❌ **near-unique** |
| **step_index** | `step_index` | int | ❌ | ✅ **new** | ❌ |
| **basis** | `basis` | `[]string` ≤4×64 | ❌ | ✅ **new** | ❌ |
| **rel_v** | `rel_v` | int | ❌ | ✅ **new** | ❌ |

Edge index: **3 → 4 keys**.

**Compatibility.** Every new field is `omitempty`, so a v0.1.0 edge decodes with zeros.
`rel_v == 0` means "written before provenance existed" and the inspector says exactly that,
rather than showing a blank field that reads as "the rule recorded nothing". Hand-drawn edges
(`created_by: user`) carry `rel_v` but no `rule_id` — correct, and distinguishable.

### 5.3 Sidecars

```
rohy-data/
├── db/                                    ✅ unchanged
│   └── payloads/payloads.log              ✅ unchanged
├── graphs/registry.json                   ✅ unchanged
├── layout/canvas-<id>.json                ✅ unchanged
├── rules/*.json + rules-state.json        ✅ unchanged
├── capture/positions.json                 ✅ unchanged
├── findings/findings.json                 ✅ unchanged
├── snapshots/<graphID>/<snapID>.json      ❌ new
├── annotations/<graphID>.json             ❌ new
└── maintenance/state.json                 ❌ new — backfill progress, last integrity run
```

**Snapshot document:**

```jsonc
{
  "snapshot_version": 1,
  "id": "snap-20260727-141133",
  "graph_id": 7,
  "graph_name": "Failed Logons Then Success",
  "rule_id": "failed-logons-then-successful-logon",
  "taken_at": "2026-07-27T14:11:33Z",
  "store_version": 412,
  "app_version": "0.2.0",
  "viewport": { "x": -120, "y": 40, "zoom": 0.85 },
  "nodes": [ { "id": 913, "hash": "a91f…", "x": 120, "y": 40 } ],
  "relations": [ { "id": 5501, "from_hash": "a91f…", "to_hash": "77c2…",
                   "relation_type": "correlation", "relation_label": "then succeeds",
                   "rule_id": "…", "match_id": "…", "step_index": 1, "basis": ["logon_id=0x3e7"] } ],
  "layers": ["layer-1"]
}
```

🔒 Endpoints are recorded by **`hash_normalized` as well as node id**, for exactly the reason
findings are hash-keyed: node ids are assignment-order and a re-ingest hands the same id to a
different event. A restore that matched on id alone would silently move a saved graph onto
unrelated records.

**Restore semantics** — never fabricate:

1. Layout + viewport are always re-applied.
2. A relation is re-applied only if an edge with the same id **and** matching endpoint hashes
   still exists.
3. A relation whose endpoints resolve by hash but whose edge is gone is offered as a
   **re-creatable** item the analyst confirms — it is re-inserted as `created_by: user` with
   `basis: ["restored from snapshot <id>"]`, because the tool asserting it now is not the same
   claim as the rule having inferred it then.
4. Anything unresolvable is **reported, never dropped silently** — count plus reason.

**Annotation document:**

```jsonc
{
  "annotations_version": 1,
  "graph_id": 7,
  "layers": [ { "id": "layer-1", "name": "Initial access", "colour": "#c2410c", "visible": true, "z": 0 } ],
  "items": [
    { "id": "a-1", "layer": "layer-1", "kind": "note",
      "anchor": { "kind": "event", "hash": "a91f…" }, "text": "first failure burst" },
    { "id": "a-2", "layer": "layer-1", "kind": "region",
      "anchor": { "kind": "world", "x": 0, "y": 0, "w": 600, "h": 400 }, "text": "lateral movement" }
  ]
}
```

Same discipline as `findings`: 🔒 opinion beside the evidence, plain readable JSON, hash-keyed
where it attaches to an event, bounded (≤ 200 items, ≤ 2000 chars per note, ≤ 16 layers).

### 5.4 Explicitly rejected schema changes

| Proposal | Why rejected |
|---|---|
| `NODE_PROCESS` node type | Breaks the "one node label" assumption throughout the query layer; a process is inferred, an event is evidence — modelling the inference as a node makes it look like the latter |
| A second edge label for lineage | 🔒 `EdgesOf(..., nil)` and `RelationsOf` assume one label (`LLD.md` §23); a second one silently changes adjacency semantics everywhere |
| Indexing `logon_id` / `process_id` | Near-unique keys; the measured cost of a near-unique indexed key is 8.0 s vs 0.8 s at open, and nothing in v0.2.0 queries by them |
| Indexing `match_id` | Same, and there is no query for it |
| Storing `ParsedFields` in the node record | Reverts the measured 3137 → 867 B/event win; the projection exists precisely to avoid this |
| Putting provenance in a sidecar | It is machine-derived, immutable, and rebuilt with the edge — it belongs with the edge, unlike findings |

---

## 6. Correlation Algorithm Specifications

Common contract for all four: 🔒 **pure, deterministic, writes nothing**, same inputs → same
output independent of map iteration or wall clock; drops undated events and counts them;
evaluates scopes in sorted order so the match cap always drops the same tail; counts past the
cap so `Truncated`/`Dropped` are exact.

### 6.1 `sequence` — existing ✅, refactored only

Unchanged semantics: greedy, earliest-anchored, non-overlapping, scoped by computer, steps need
not be adjacent, one edge per consecutive pair, confidence 1.0.

Changes: reads `ds.ByScope`; emits provenance. Existing tests must pass untouched. 🔒

`O(E)` per scope after the shared sort. **Per-rule sorting cost drops from N×(E log E) to
1×(E log E) for a build of N rules.**

### 6.2 `field` — new ❌

**Intent.** Retire `RULES.md` §8's central caveat for rules that opt in: a match no longer means
only "in this order on this host", it means "in this order on this host **and sharing these
field values**".

**Spec:** `sequence` (≥2), `match_fields` (≥1 slot names), `match_scope` (default `computer`).

**Evaluation:**

1. From `ds`, take the chronological, dated event slice.
2. For each event, read the slot values named by `match_fields`.
   **If any is absent → the event is excluded and counted in `SkippedNoKeys`.** 🔒 It is never
   bucketed under the empty string; doing so would correlate every event that lacks the field
   with every other, which is how a field matcher becomes a false-positive engine.
3. Bucket key = `scope value` + `0x1f` + each slot value joined by `0x1f` (the same unit
   separator the hashing already uses, for the same reason — it cannot occur in event text).
4. Buckets inherit chronological order from the global sort — **no per-bucket sort**.
5. Within each bucket, run the identical greedy non-overlapping match as `sequence`.
6. Emit one edge per consecutive pair. `confidence = 1.0` (exact equality).
   `Basis = ["logon_id=0x3e7"]` (one entry per matched field, value truncated to 64 chars).

**Persistence:** `RelationType` from the spec (default `correlation`), `Algorithm: "field"`,
`RuleID`, deterministic `MatchID`, `StepIndex`, `Basis`.

**Cost:** `O(E)` bucketing + `O(Σ b)` matching = `O(E)` after the shared sort. Memory: one map
of `E` entries per distinct `KeySpec`, built once in `Prepare` and shared by every rule that
asks for the same tuple.

**Reported:** `SkippedUndated`, `SkippedNoKeys`, `StaleCorrelationKeys` (events at an older
`CorrKeyVersion` — a build over a store that has not been backfilled says so loudly rather than
reporting a small match count as if it were the truth).

### 6.3 `temporal` — new ❌

**Spec:** `sequence` (≥2), `window_within` (required, Go duration), `window_total` (optional),
`match_scope` (default `computer`), and optionally `match_fields` — a temporal window composes
with field bucketing, and that combination is the most precise matcher rohy will have.

**Evaluation — a latest-anchored DP, not greedy-with-abort.** This matters:

> With a window constraint, the earliest subsequence is no longer necessarily a valid one.
> Rejecting it and abandoning the scan **misses matches**; restarting the scan from each failed
> anchor is `O(E²)`. Both are wrong.

Correct formulation, single pass, `O(E · S)` worst case and `O(E)` with an `event_id → steps`
map:

```
last[s]   = index of the most recent event completing step s, or -1
chain[s]  = the indices that satisfied steps 0..s for THAT completion

for i in chronological order:
    for each step s where events[i].EventID == seq[s], DESCENDING in s:
        if s == 0:
            last[0], chain[0] = i, [i]
        else if last[s-1] != -1 and t[i] - t[last[s-1]] <= within
                and (window_total == 0 or t[i] - t[chain[s-1][0]] <= window_total):
            last[s], chain[s] = i, chain[s-1] + [i]
        if s == len(seq)-1 and last[s] == i:
            emit edges along chain[s]
            reset last[]                     # non-overlapping
```

⚠️ **Corrected 2026-11-03 (defect D1).** This pseudocode first kept a single `parent[s]` per
step and walked it back. That is wrong whenever an event can satisfy several steps: the event
re-completes the earlier steps after a later one has read them, and the walk splices in the newer
completion. The shipped built-in `4625 ×3 → 4624` came out `F2 → F2 → F3 → S`, and `window_total`
was measured from the wrong anchor. Keeping the chain per completion fixes both;
`TestTemporalChainUsesEachStepsOwnEvent` and its two neighbours pin it.

Descending iteration over `s` is required so one event cannot advance two steps of the same
match in a single visit. Keeping only the **most recent** index per step is provably optimal:
a later `j` has a larger `t[j]`, so it satisfies `t[i] - t[j] <= within` for strictly more `i`.

**Semantic note that must go in `RULES.md`:** this is **latest-anchored** (it takes the
tightest chain), whereas `sequence` is earliest-anchored. With `within = ∞` the two would
therefore not always emit the same edges. They are different algorithms with different names,
so this is acceptable — but it must be documented, not discovered.

**Emit:** confidence 1.0. `Basis = ["Δt=42s ≤ 5m"]` per edge, plus the field basis when
`match_fields` is also set.

**Cost:** `O(E)` per scope. Memory `O(S)` per scope.

### 6.4 `lineage` — new ❌

**Intent.** Reconstruct process ancestry from process-creation records and link the events, not
inferred entities.

**Spec:** `lineage_create_ids` (default `["4688"]`), `lineage_depth` (default 0 = direct edges
only), `match_scope` (default `computer`), optional `lineage_image` filter (basename), **no
`sequence`**.

**The correctness problem this design exists to solve: PIDs are reused.** Linking a child to a
parent by PID alone, across a case spanning days, produces wrong ancestry — confidently. So:

**Evaluation:**

1. **Build the lifetime table** (in `Prepare`, shared across rules):
   for each `(computer, pid)`, an ordered list of intervals derived from creation events —
   interval `k` starts at the creation of that pid and ends at the next creation of the same
   pid on the same computer, or at its `4689` exit, or at `+∞`.
2. For each creation event `C` at time `t` with parent pid `P`:
   binary-search `lifetimes[computer][P]` for the interval containing `t`.
   - **Found** → emit `parentCreationEvent → C`, label `spawned`, confidence **1.0**,
     `Basis = ["ppid=4180", "image=powershell.exe"]`.
   - **Not found** → `UnresolvedParents++`. 🔒 **No edge is emitted and no guess is made.** The
     parent was almost certainly created before the ingest window; inventing a link to the
     nearest candidate is exactly the kind of confident wrongness rohy is built not to do.
3. If `lineage_depth > 0`, additionally emit ancestor edges up to that depth, with
   `confidence = 0.9` and `Basis = ["transitive depth=2"]`. ⚠️ Default stays 0: transitive
   edges are derivable by traversing the direct ones, and emitting them multiplies edge count
   without adding information.

**Confidence semantics — stated once, everywhere:** in rohy, `confidence_score` means **how
exact the match was**, never how likely the activity is malicious. Direct interval-resolved
parentage is exact (1.0); a transitive edge is a derived claim (0.9). Nothing in v0.2.0 emits a
probabilistic score.

**Cost:** `O(E log E)` to build lifetimes (sort per pid) + `O(C log L)` to resolve. Memory
bounded by distinct `(computer, pid)` pairs.

**Reported:** `SkippedUndated`, `SkippedNoKeys`, `UnresolvedParents`, `Truncated`, `Dropped`.

### 6.5 Unified inference pipeline

```
graphbuild.RunWithProgress
  │
  ├─ store.RepairRelationIndex()                     ✅ existing, still FIRST 🔒
  ├─ selectRules(req.RuleIDs)                        ✅ existing
  ├─ Requirements = ∪ over selected rules            ❌ new
  ├─ store.QueryEvents(filter, undated=exclude)      ✅ existing — ONE read for N rules 🔒
  ├─ cache lookup (filter orderKey, store version, requirements)   ❌ new
  ├─ autograph.Prepare(events, requirements)         ❌ new — ONE sort for N rules
  │     ├─ drop + count undated (second independent guard 🔒)
  │     ├─ global chronological sort, id tie-break
  │     ├─ ByScope        (stable pass, order inherited)
  │     ├─ ByKey[spec]    (only the tuples asked for)
  │     └─ Lifetimes      (only if a lineage rule was selected)
  │
  └─ for each rule:
        ├─ graphs.EnsureForRule(...)                 ✅ existing
        ├─ store.DeleteGraphRelations(graph.ID)      ✅ existing — one transaction 🔒
        ├─ autograph.Generate(spec, dataset)         🔴 signature change
        ├─ stamp GraphID, CreatedAt, RuleID, Algorithm, RelVersion   ❌ new (backend-stamped 🔒)
        ├─ InsertRelations in chunks of 512          ✅ existing
        ├─ discard empty graph + its layout          ✅ existing, two refusals kept 🔒
        └─ report Progress                           ✅ existing, per rule 🔒
```

---

## 7. Editor Enhancements

### 7.1 Correlation algorithm selector ❌ (the control is free; the schema is the work)

**UI flow.** Guided editor, Metadata group: a `Select` bound to `algorithm`, options and prose
read from `schema.fields[algorithm].enum` — the control already generates itself, it simply has
one option today. On change, the form shows only the fields whose `applies_to` contains the
selection, and shows an inline note when the choice will raise `format_version` to 2.

**Backend.** `RuleSchema()` (✅ exists) returns the richer descriptor. No new binding.

**Validation.** `algorithm` unknown → located error on that control. ~~Algorithm requires v2 while
the file declares v1 → an error that names both, with a one-click "set format_version to 2".~~
_Superseded by the format-version reversal (§2): the format stayed at 1 and nothing gates on it._
A field belonging to another algorithm → **warning, not error** 🔒 (the format ignores fields it
does not interpret; the editor must be no stricter than the loader).

**Raw editor.** `complete.js` filters by the current `algorithm` value; `highlight.js` needs no
change.

### 7.2 Rule testbench ❌ (the deliverable the architecture already paid for)

**UI flow.** A collapsible panel below the editor. "Run against current dataset" → shows:
matches, relations, elapsed, `skipped_undated`, `skipped_no_keys`, `unresolved_parents`,
`truncated`/`dropped`, and the first 20 matches as chains of event summaries with the basis on
each hop. "Open in graph" is deliberately absent: 🔒 **a dry run never persists anything**, and
offering a graph would blur that.

**Backend.** `BuildAPI.DryRunRule(source, query, limit)`:

1. `rules.ValidateSource(source)` → invalid returns the report, no run;
2. reuse or build the cached `Dataset` for `(filter, storeVersion, requirements)`;
3. `autograph.Generate(spec, dataset)`;
4. hydrate only the sampled events (≤ `limit` × sequence length);
5. return — **no `InsertRelations`, no `EnsureForRule`, no `DeleteGraphRelations`.**

This is ~80 lines because `autograph` already returns unpersisted relations. It is the single
clearest payoff of the existing layering, and worth saying so in the LLD.

**Validation behaviour.** The testbench is disabled while the buffer has errors, and shows the
authoritative backend report — not the local mirror — because a run against text the loader
would refuse would be a lie about what the rule does.

### 7.3 Rule diffing ⚠️ → complete

Today: `lib/rules/diff.js` + `RuleDiffView.svelte` diff raw text; `hasChanges` drives the
dirty guard.

Add: (a) **field-level structural diff** — a two-column view of `Spec` fields with per-field
added/removed/changed marks, and sequence steps aligned as a chain so an inserted step reads as
an insertion rather than as every subsequent step changing; (b) **diff against the saved
version on disk**, not only against the session's `original`; (c) a diff in the rename
confirmation, since a rename replaces identity (`save.go`) and the analyst should see exactly
what the replacement contains.

### 7.4 Shared selection state ❌

`selection.js` gains a third slice:

```js
correlation: { ruleId: '', matchId: '', stepIndex: -1, edgeIds: [], nodeIds: [] }
```

Two directions, both enabled by `Relation.RuleID` + `MatchID` + `StepIndex` (§5.2):

- **Canvas → editor**: selecting an edge reads its `rule_id`/`step_index`, opens the rule, and
  highlights that step of the sequence.
- **Editor → canvas**: focusing a step highlights every edge in the active graph with that
  `rule_id` and `step_index`, and dims the rest.

⚠️ Dependency: this feature is not startable until §5.2 lands.

### 7.5 Import / export ⚠️ → complete

Import ✅ unchanged. Export today is a single-rule download. Add `RulesAPI.ExportRules(ids)`
returning a manifest + each rule's **bytes exactly as stored** 🔒 (never a re-serialization —
the formatter's whole point is that field order and uninterpreted fields survive), which the
frontend zips or writes as a JSON bundle. A bundle re-imported must produce byte-identical
files; that round trip is a test.

### 7.7 Algorithms explainer page ✅ (added to scope, not in the original audit)

**Why it earns its place.** `RULES.md` §8 now states what each algorithm establishes, and it is
the authority — but it is a document, read before or after the work. The moment an author needs
it is *while choosing an algorithm in the editor*, which is exactly when they will not go and
open a file. v0.2.0 makes that choice consequential for the first time (§6), so the explanation
has to be somewhere the choice is made.

**What it deliberately does not do.** It does not restate the format. Field names, bounds and
defaults are served by the backend descriptor and rendered by the guided form; a second copy
would drift out of step with the loader. The page explains **behaviour** — what a matcher does,
and what a match therefore proves — which is the part a form cannot show.

**Anti-drift, the load-bearing decision.** A diagram explaining an algorithm is a *claim* about
that algorithm, and a hand-drawn one is a claim nothing checks — the engine changes, the
picture does not, and the page goes on teaching something untrue to the people least able to
notice. So the scenarios live in `lib/learn/algorithms.js` as data, built from the **same event
sets and expected outcomes as the Go tests**, and each names the test it mirrors:

| Scenario | Mirrors | Shows |
|---|---|---|
| `sequence` | `TestSequenceGreedyWithNoise` / `…NonOverlapping` | steps need not be adjacent; occurrences never overlap |
| `field` | `TestFieldCorrelationRequiresSharedValue` / `…ExcludesEventsWithNoValue` | the cross-session pairing sequence matching would make — **shown, then rejected**; the `-` event excluded rather than bucketed |
| `temporal` | `TestTemporalFindsTheMatchGreedyWouldMiss` | greedy anchoring on the first 4625 and failing the window, then the sweep finding the pair it walked past |
| `lineage` | `TestLineageDoesNotLinkAcrossPIDReuse` | the PID-reuse error drawn as a real candidate edge, then rejected by interval containment |

`algorithms.test.js` (21 tests) asserts those outcomes, that every implemented algorithm has a
scenario, that each states what a match establishes, and that no step references an event the
scenario does not define. A change to what an algorithm does now breaks this file rather than
silently rewriting what the page claims.

**Files:** `lib/learn/algorithms.js` (trace model + layout), `lib/learn/algorithms.test.js`,
`components/learn/ScenarioPlayer.svelte` (animated SVG + transport),
`routes/AlgorithmsView.svelte`, plus the four-step route recipe from `LLD.md` §22 —
`ROUTES.ALGORITHMS`, `NAV_KEYS[6]`, the `SHORTCUTS` entry, and the `App.svelte` switch. The nav
menu and the shortcuts help derive themselves, so the page is reachable from every route.

🔒 **Motion policy.** Movement is not decoration here: a chip sliding into its session lane *is*
the explanation of bucketing, and an edge drawing itself *is* the explanation of an edge being
emitted. Under `prefers-reduced-motion` the component does not shorten its animations — it
removes them and does not self-start, because every step is a **complete snapshot** rather than
a delta, which makes the substitution lossless. With motion allowed it plays once on arrival: a
diagram that sits still until clicked mostly does not get clicked.

**A11y:** the stage is `role="img"` with a per-step label (it is a picture, not a control), the
narration is an `aria-live` region so the walkthrough is usable without seeing it at all, and
stepping uses the standard roving-tabindex tablist pattern on the step pips. Build is clean of
a11y and unused-CSS warnings.

⚠️ **Not visually verified, and that cost something.** It compiles with zero warnings and its
logic module is fully tested, but this project has no jsdom or component-mounting test setup —
the 16 existing test files are all pure modules — so nothing asserts how it *behaves on
screen*. Three bugs got through to review as a result:

1. **Edges drew once per element lifetime.** A CSS `animation` fires when an element is
   created, and Svelte reuses a keyed element rather than recreating it — so an edge keyed on
   its endpoints animated the first time it appeared and never again, and an edge that changed
   meaning (lineage's candidate link becoming a rejection) changed colour without redrawing.
   Fixed by folding an `epoch` counter and the edge's state into the key.
2. **Replay looked broken.** `play()` reset to step 0 and then waited a full `STEP_MS` — and
   step 0 of every scenario is the static "here is the data" frame, so the diagram sat visibly
   unchanged for 3.2 seconds after the button was pressed. Fixed with a short `RESTART_MS`
   first beat.
3. **A bare `scenario;` statement** was the effect's only dependency — precisely the dead code
   a compiler may drop, which would have left tab switches not restarting at all. Now reads
   `scenario.id`.

Also added: chips pop on state change (keyed on `epoch:state`), which is the only motion in a
scenario where no chip ever changes lane, and the sweep line moves by `transform` rather than
by animating `x1`/`x2` — SVG geometry attributes whose CSS transitionability is engine-dependent.

That fix then introduced a fourth: `start()` did `epoch += 1`, and the restart path runs inside
`$effect`. `+=` **reads** the state as well as writing it, so the effect gained a dependency on
a value it also wrote — a self-triggering update loop that took the component down entirely.
Fixed structurally rather than with `untrack`: `epoch` is now only ever **assigned** from a
plain non-reactive counter, so it cannot be read inside the effect by construction.

🔒 Two lessons, both earned the hard way:

- **CSS animations are once-per-element**, so anything that must re-play needs its identity to
  change. Persist what should move (chips, via `transform` transitions); re-key what should
  re-draw (edges, chip bodies).
- **Inside `$effect`, `x += 1` on `$state` is a loop.** Write-only assignment is safe;
  read-modify-write is not. Keep the counter in a plain variable and assign the reactive one
  from it — the invariant then holds by construction rather than by remembering.

A **fifth** followed: the speed control renamed `timing` to `timingNow()` and missed the
`$state` initializer, so the page failed to render at all. `vite build` passed — it had to, since
an undefined identifier in a `<script>` block is legal JavaScript and could be a global, so the
compiler has no reason to complain and the failure only appears at runtime as a blank page.

✅ **Both gaps are now closed, neither with a new dependency:**

- **The transport is a pure reducer** (`lib/learn/player.js`, 21 tests). It performs no writes,
  so the update-loop class is impossible by construction rather than fixed.
- **Components are smoke-tested by SSR rendering** (`components/smoke.test.js`, 24 tests).
  Svelte 5 compiles for the server, so a component can be executed in plain Node — no jsdom, no
  testing-library. The script body runs, props are read, deriveds compute, markup is produced.
  Verified against the real bug: reintroducing it produces `ReferenceError: timing is not
  defined` instead of a green build.

  🔒 What it does NOT do: `$effect` never runs, no event fires, and nothing here can say the
  diagram is legible or the animation plays. It answers exactly one question — *does this
  component still execute?* — which is the question that had actually been failing five times.
  Looking at the page remains necessary.

  It paid for itself immediately by surfacing a `state_referenced_locally` warning the build had
  been printing unnoticed.

**A sixth, found from a symptom report of "sometimes stops prematurely":** the player's index and
the scenario it indexes into are updated at different times. Switching algorithm changes the
`scenario` prop and re-renders the template **before** the effect that resets the index runs —
render effects precede user effects — so for one frame a 4-step scenario could be asked for step
5 left over from a 7-step walkthrough still playing. `scenario.steps[5]` is `undefined`, the next
property access throws, and the component dies mid-render. Intermittent by construction: it only
happens when the switch lands late enough in a longer walkthrough.

Fixed with `clampStep()` — a pure function, tested against every scenario and every out-of-range
probe. Clamping centrally rather than guarding at each use, because four deriveds read the step
and each would otherwise need the same defence. Every template read of `player.index` now goes
through the clamped value too, so the pips, counter and aria label cannot disagree with the step
actually drawn.

🔒 The pattern worth carrying: **when two pieces of state are updated by different mechanisms,
the one that indexes into the other must tolerate being briefly out of range.** Effects run after
render, so "they are reset together" is never true for the frame in between.

### 7.6 Raw editor improvements ⚠️

Small, since the base is solid: bracket matching + auto-close, a problems gutter with
click-to-jump using the existing `line`/`col`, a format-on-save preference, and duration
validation feedback for `window_within` as you type.

---

## 8. Graph Enhancements

### 8.1 Auto-layout profiles ✅ — new package `backend/graphlayout`

Computed in **Go**, not in the component. Reasons: deterministic and unit-testable (a grid in a
Svelte method is neither), reusable by snapshots and any future export, and it keeps the canvas
a renderer. Pure: relations + node metadata in, positions out; opens nothing.

```go
func Compute(profile string, nodes []NodeInfo, rels []graphene.Relation, opts Options) (map[uint64]Point, error)
```

| Profile | Rule | Notes |
|---|---|---|
| `sequence` | x = topological rank following edge direction; y = chronological within rank | cycles broken by lowest node id, deterministically |
| `lineage` | tidy tree; roots = nodes with no incoming `spawned` edge | multiple roots laid out side by side |
| `resource` | column per distinct bucket value of a chosen slot; chronological within column | needs the correlation projection |
| `temporal` | x = timestamp linearly mapped; y = greedy lane assignment avoiding overlap | undated nodes get an explicit lane, never x=0 🔒 |

`GraphAPI.ComputeLayout(graphID, profile)` returns positions; the frontend applies them through
the existing `graph.applyLayout` and the existing `layout` sidecar persists them. ✅ No change to
the layout store.

**Built.** `backend/graphlayout` — 4 profile files + `clusters.go`, 54 tests. `GraphAPI` gains
`ComputeLayout`, `LayoutProfiles`, `LayoutFields`, `Clusters`, `ClusterModes` (13 tests).
Frontend: `lib/graph/autolayout.js` (17 tests) + `AutoLayoutPanel.svelte` in the graph aside.

🔒 **Computing is not applying.** `ComputeLayout` returns positions and writes nothing.
Node positions are the only thing on the canvas placed by hand and there is no undo for them, so
a profile is *previewed* — the nodes move, the previous positions are snapshotted, and nothing
reaches the sidecar until the analyst presses **Keep**. `TestComputeLayoutNeverWrites` pins it.
The snapshot is taken once, at the first preview, and deliberately not refreshed when the profile
changes: otherwise trying two profiles in a row would make "Put back" restore the first *preview*
rather than the hand placement.

🔒 **The undated tray.** The temporal profile's x axis is time, so any x is a claim about when.
Undated events get a labelled tray below the lanes, separated by a blank row, and the result
carries a note saying so. Placing them at x=0 would assert they were the earliest events in the
case; they were not early, they were untimed.

Three places where the measured/tested answer differed from the plan's sketch:

- **Longest-path ranking, not topological order.** Shortest-path ranking puts a node in the same
  column as an event it demonstrably follows whenever a second, shorter route exists — which
  reads as "these are contemporaneous". `TestSequenceRanksByLongestPathNotShortest` pins it.
- **A cluster is named after its lowest member, not its union-find root.** Caught by the
  determinism test: the root depends on the order the unions happened in, so a root-derived id
  renamed a component whose membership had not changed — and the canvas remembers which clusters
  are collapsed *by id*.
- **Rule clusters overlap, and say so.** An event really can be the 4624 in two chains. An
  exclusive assignment would have to pick a winner with no non-arbitrary way to pick one, so
  `Cluster.Overlapping` reports it rather than the canvas assuming exclusivity.

**Deviation D-7 — `Compute` returns a `Result`, not a `map[uint64]Point`.** The sketched signature
cannot express what the resource and temporal profiles have to say: which column is which, and
which nodes carry no time. A bare position map would have forced the canvas to re-derive both, or
to not say them at all. `Result` carries `Positions`, `Groups` (labelled, with an `Undated` flag)
and `Note` — the profile's own caveat, passed through verbatim to the panel.

### 8.2 Relationship inspector ❌

A panel for a selected edge: endpoints (event summaries, click to open), `relation_type`,
`label`, `confidence_score`, `created_by` (badged — inferred vs asserted must never look alike
🔒), `created_at`, and the new provenance: rule name + link, algorithm, `basis` rendered as
"matched because: logon_id = 0x3e7", `match_id` with "3 edges in this occurrence — highlight
all", `step_index` as "step 2 of 4".

For `rel_v == 0` it says **"provenance not recorded (built before v0.2.0) — rebuild this rule to
populate it"**, not a blank.

Binding: `GraphAPI.InspectRelation(id)` — one call, joining the relation, its endpoints' sort
views, and the rule name from the registry.

### 8.3 Snapshot & restore ✅

Per §5.3. UI: a snapshot list per graph with timestamp, app version, node/edge counts and a
thumbnail; "Restore" opens a preview showing exactly what will be re-applied, what will be
offered for re-creation, and what cannot be resolved. 🔒 Nothing restores silently.

**Built.** `backend/snapshot` — `snapshot.go` (storage, 12 tests) and `restore.go` (the planner,
10 tests). `SnapshotAPI` as an eighth bound struct (11 tests). Frontend: `lib/graph/snapshots.js`
(16 tests) + `SnapshotPanel.svelte` (2 smoke tests) in the graph aside.

🔒 **Endpoints are recorded by `hash_normalized`, and the restore follows the hash.** The end-to-end
test deletes both events, re-inserts the same content under new ids, and puts an unrelated event
on the old id — then asserts the saved positions land on the right records and *not* on the
impostor. That is the failure this design exists for, and the one that would have been invisible:
the canvas would look completely normal and every claim on it would be wrong.

🔒 **Preview writes nothing, and is tested for it.** `TestPreviewRestoreWritesNothing` moves a node
and deletes an edge first, so a preview that wrote would visibly undo them. Without that test,
"look before you leap" is a claim rather than a property.

🔒 **A re-created edge is the analyst's assertion, never the rule's.** It is written
`created_by: user` with `basis: ["restored from snapshot <id>"]` and *no* rule provenance — an edge
wearing the snapshot's `rule_id` would be indistinguishable from one the current rules produced,
and would say so in an export. Nothing is pre-selected in the offer list: re-creating a link is an
act the analyst performs, not a default they have to notice and undo.

Four decisions the work forced:

- **Past the cap, `Save` refuses rather than evicting the oldest.** A snapshot is something an
  analyst deliberately took; silently deleting one to make room destroys work without asking,
  which is the opposite of what a snapshot feature is for. The message says what to do instead.
- **An edge id is not trusted alone.** "Still there" means same id *and* same endpoints — an id
  that survived a re-ingest onto different endpoints is a different edge, and treating it as
  unchanged would leave the graph claiming a link the snapshot never held.
- **Ties resolve to the lowest id.** Deduplication should prevent one hash on two nodes, but a
  partial ingest can produce it, and resolving by map order would make the same snapshot restore
  differently between runs.
- **Snapshot ids are sanitized where they become a path.** They are generated by `NewID`, but they
  cross the API boundary, so `safeID` strips traversal rather than trusting the usual caller.

Edges to events that are not on the canvas are excluded from the snapshot: the graph can hold
them, but a snapshot that recorded them would let a restore re-create a link to something it never
displayed. Every node carries a descriptor for the same reason `findings.Descriptor` exists — an
orphaned bare hash tells the reader nothing about what was lost.

Two additions outside the package: `graphene.Store.EventHashes()` (one minimal-decode scan, rather
than a lookup per saved node) and `snapshotsDir` beside the other sidecars.

⬜ **Not built:** the thumbnail. It needs the canvas rendered off-screen, which is a different
piece of work from the restore semantics this item was really about.

### 8.4 Annotation layers ✅

Per §5.3. UI: a layer panel (add / rename / colour / show / hide / reorder), note pins anchored
to nodes, region rectangles anchored to world coordinates, arrows between two anchors. Layers
render beneath nodes and above the grid. Export includes visible layers.

**Built.** `backend/annotate` (21 tests) + `AnnotateAPI` as a **ninth** bound struct (9 tests).
Frontend: `lib/graph/annotations.js` (22 tests), `stores/annotations.js`,
`GraphAnnotations.svelte` (the overlay) and `LayerPanel.svelte` (6 smoke tests between them).

🔒 **Anchors are event content hashes, resolved to live node ids on every read.** The
resolution is deliberately NOT cached: the whole reason annotations are hash-anchored is that node
ids move, and a stored mapping would reintroduce exactly the staleness the hash exists to avoid.
`TestAnnotationsFollowTheEventAfterAReingest` deletes the events, re-inserts the same content
under new ids, parks an unrelated event on the old id, and asserts the note follows its evidence.

🔒 **An annotation is drawn only where it can honestly be placed.** An anchor whose event has left
the case — or is simply not on this canvas — has no position, and a pin at the origin or clamped
to the nearest card would be a mark pointing at the wrong evidence. It is not drawn, and the count
is reported. The two reasons are kept apart in the report (`orphaned` vs `offcanvas`) because they
have different fixes: one is gone, the other is merely elsewhere.

🔒 **A hidden layer is not counted as a problem.** Hiding is a choice the analyst made; reporting
it alongside genuine failures would train them to ignore the report.

Three decisions worth recording:

- **Deleting a layer takes its annotations with it, and says how many first.** Orphaning them onto
  another layer would leave marks the analyst thought they had removed; leaving them on a layer
  that no longer exists would leave them invisible and unreachable. All-or-nothing, with the count
  surfaced before the confirm.
- **The panel states, in words, what an annotation is NOT.** A layer marks up *this graph*; a
  finding is about the *event* and follows it everywhere. The two look identical from outside and
  are easy to reach for interchangeably, so the distinction is written down rather than left to be
  discovered by putting a note in the wrong place.
- **A second anchor on a non-arrow is dropped, not stored.** It would be data nothing reads, and a
  later build giving it a meaning would silently start honouring values nobody chose.

The overlay is pass-through: annotations are created and deleted from the panel, so making the
marks clickable on the canvas would be an affordance leading nowhere that also swallowed canvas
drags.

**Also added, outside the item:** `GraphAPI.WithCascades(...)` and `GraphCascade`. Deleting a graph
now drops its snapshots and annotations too. They are registered in `app.go` rather than imported
into the binding, so a new per-graph sidecar is added in one place instead of edited into
`DeleteGraph` — and `TestDeletingAGraphTakesItsSidecarsWithIt` pins it. Without this a deleted
graph left files nothing could list and nothing could restore.

⬜ **Not built:** region and arrow creation from the canvas (both render; only notes can be
created, from the panel), and "export includes visible layers" — export is Phase 7's shape.

### 8.5 Replay mode ✅

Frontend-only. `GraphView` already loads every node and edge for the active graph, so a backend
call would duplicate that.

🔒 **Replay orders by event timestamps, never by `created_at`** — `created_at` is when the build
ran, and animating a graph in build order would be meaningless. An edge appears when its later
endpoint's timestamp is reached. Undated nodes are shown from the start in a separate tray with
a label saying why, per "never lie about missing data".

Controls: play/pause, speed, scrub, and a shared playhead with `TimelineView` (which already has
one ✅).

**Built.** `lib/graph/replay.js` (25 tests) — the schedule, pure and clock-free. `stores/replay.js`
(10 tests) — the shared instant. `ReplayBar.svelte` (2 smoke tests) in the graph aside, plus the
canvas filter and the timeline's playhead rewired through the store.

🔒 **Ordered by event timestamps, never `created_at`** — an edge appears when its LATER endpoint's
timestamp is reached, the same rule the heatmap (§8.6) follows. The tests assert the edge times
directly rather than asserting "not created_at", so the ordering itself is pinned.

🔒 **Undated evidence is present from the start, not animated in at t=0.** An event with no
timestamp is not an event from the epoch; placing it first would assert it was the earliest thing
that happened. An edge touching one is marked **approximate** — it appears once both its ends are
on screen, which is the earliest point at which drawing it is coherent — and the count of both is
stated beside the transport every time, because a node that never animates in would otherwise
read as a bug in the playback.

🔒 **The shared value is an absolute instant, not a fraction.** The timeline measures the filtered
event set; the canvas measures the active graph. A shared fraction would have the two views
pointing at different moments while appearing to agree. Each converts at its own edge, and when
the shared instant falls outside the timeline's extent it says so rather than dropping the marker
silently.

Two structural decisions:

- **The playhead has ONE owner.** `TimelineView`'s local `playhead` state became a `$derived` read
  of the store, and all eight write sites became `setPlayhead(frac)`. Two-way sync between two
  local states is the update-loop shape this codebase has already been bitten by; one owner and
  derived readers has no loop to have.
- **Replay is a filter, exactly like folding.** `null` means replay is off and everything draws;
  a Set means only these are revealed. An empty Set and "no replay" must not render alike — hence
  the null rather than an empty set as the off state. `replay.reset()` is bound to
  `graph.clearCanvas()` alongside `clusters.reset()`, for the same reason.

Playback is scaled to the case's SPAN (`GRAPH.REPLAY_DURATION_MS`) rather than run at wall-clock
speed: a three-week case would otherwise take three weeks and a four-second one would be over
before it was seen. The cost is that perceived pace carries no information here, and the comment
says so — the timeline, where it does, stays the place to read pace from.

### 8.6 Relationship heatmap ✅

`GraphAPI.RelationHeatmap(graphID, buckets, groupBy)` → a matrix of relation counts per
(time bucket × group), where `groupBy` ∈ `{rule, relation_type, computer, created_by, step}`.
Implemented in `graphene_heatmap.go` **reusing the timeline's bucketing helpers** 🔒 so the two
views can never disagree about what a bucket is. Rendered as an overlay strip on the timeline
and as a standalone matrix.

**Built.** `backend/graphene/graphene_heatmap.go` (17 tests) + `GraphAPI.RelationHeatmap` /
`HeatmapGroups` (3 tests). Frontend: `lib/graph/heatmap.js` (23 tests) and one
`HeatmapMatrix.svelte` that renders in both shapes (7 smoke tests), wired into `TimelineView` as
a strip above the plot and a matrix below it.

🔒 **The bucketing was EXTRACTED, not copied.** `newBucketing`/`indexOf` came out of
`TimelineGrouped` into a shared `bucketing` type, and `TestHeatmapAndTimelineAgreeOnBucketBoundaries`
asks both for the same window and compares every boundary. Two implementations that agreed today
would drift, and the failure would be a strip whose columns did not line up with the histogram
beneath them — activity shown at a time it did not happen.

🔒 **A relation is placed at its LATER ENDPOINT's timestamp, never at `created_at`.** `created_at`
is when the rule ran, so a heatmap keyed on it shows one spike at build time and says nothing
about the case. The test fixture stamps `created_at` in 2030 so a regression cannot be subtle.
Same rule replay (§8.5) will follow, for the same reason.

🔒 **Everything is accounted for.** `Total = Placed + Undated + Outside`, asserted. A relation
touching an undated event has no moment at which it holds — the second event's time is not
"unknown but later", it is absent — so it is counted, not dropped. A relation outside an
explicitly requested window is counted and dropped rather than *clamped* into the nearest column,
which would pile everything past a zoomed window into the right-most cell.

Three decisions the work forced:

- **`HeatmapQuery` gained `From`/`To`.** Without a pinned window the heatmap spans the relations'
  own extent and the timeline spans the events' — different axes, so the strip could not honestly
  sit over the histogram. The window is pinned to the timeline's full extent and *zoom is applied
  to both arrays identically* (`sliceToView`), rather than re-fetching per window, which would
  re-bucket the heatmap and reintroduce the drift.
- **The group key is read from the anchor endpoint**, the same event the time came from. Reading
  the row off one endpoint and the column off the other would make one cell describe two events.
- **`step` grouping excludes edges that have no step** — lineage has no sequence, a hand-drawn
  edge has no rule. Defaulting them to the zero step would pile them into "step 1", a lane that
  would then read as the busiest stage of the case.

Two rendering rules, in `heatmap.js` because they are the ones a heatmap gets wrong: the ramp is
linear and anchored at the matrix maximum (a per-lane ramp makes a quiet rule and a screaming one
look identical), and **zero is not a shade** — an empty cell draws nothing, because "nothing
happened here" and "almost nothing happened here" must not be a few percent of lightness apart.

### 8.7 Cluster-aware grouping ✅

`graphlayout.Clusters(rels, nodes, mode)`, `mode` ∈ `{component, rule, slot}`:

- `component` — connected components (union-find, `O(E α(N))`);
- `rule` — group by `rule_id`;
- `slot` — group by a correlation slot value.

`GraphAPI.Clusters(graphID, mode)` returns `[{id, label, node_ids, size}]`. Canvas draws convex
hulls, supports collapse-to-single-node with an expand affordance, and the count is always
visible so a collapsed cluster never hides how much it contains.

**Built:** the engine (`clusters.go`, union-find `O(E α(N))`) and `GraphAPI.Clusters` /
`ClusterModes`, with 12 + 3 tests. `Size` is carried separately from `NodeIDs` precisely so a
collapsed cluster can state what it hides; a test asserts the two can never disagree. Nodes no
rule touched are collected under a named cluster rather than dropped.

**Canvas half built.** `lib/graph/clusters.js` (30 tests) — Andrew's monotone-chain convex hull,
outward padding, and the collapse mapping. `stores/clusters.js` (12 tests). Components:
`GraphHulls.svelte` (outline layer), `ClusterCard.svelte` (the folded card), `ClusterPanel.svelte`
(the sidebar). 6 component smoke tests.

🔒 **A hull encloses the CARDS, not their origins.** Drawn through the top-left corners it would
cut through half the cards it claims to contain, so a node inside a group would render outside its
outline — a wrong claim about the evidence, not a cosmetic glitch.

🔒 **A folded group's edges are re-pointed, never dropped.** An edge that vanished when a group
closed would make the collapsed card look *less* connected than it is — and collapsing is what an
analyst does to a big graph precisely to see the links between groups. Edges internal to one
folded group are hidden (they are inside the card) and counted. The relation id survives the
re-point, so selecting a re-pointed edge still inspects the real relation.

🔒 **The count is always on screen.** The card shows the cluster's own size — not how many members
are currently on this canvas, which is reported separately — and the panel row shows it too. A
folded group must never be able to hide how much it contains.

**Implemented by remapping, not by teaching the layers about clusters.** Folding drops members
from the node list, adds one proxy card, and re-points crossing edges; `GraphEdges` and `GraphNode`
learn nothing. Three canvas paths had to follow the shown set rather than every node — hit-testing
(a click would otherwise select and drag something invisible under the card), select-all, and
fit-to-content.

Two things the tests forced:

- **`remapEdges` checks the proxy, not just `from === to`.** A bare equality test also swallowed
  an ordinary self-loop and reported it as something the collapse had hidden.
- **`clusters.reset()` is bound to `graph.clearCanvas()`**, the funnel a graph switch already goes
  through. Grouping is scoped to the canvas it was computed for and cluster ids come from
  membership, so left in place it would draw graph A's outlines over graph B — or leave a folded
  card standing for events that are no longer there.

**Folds are dropped on every re-group**, because a cluster id is derived from its membership:
carrying them across a mode change would leave clusters folded that no longer exist, with no way
to unfold them. Likewise `hide()` unfolds everything — outlines off with folds left on would leave
cards standing for events and nothing on screen explaining what they stand for.

---

## 9. Shared Feature Implementation Plan

| Feature | State | Work |
|---|---|---|
| Import rule JSON | ✅ | v2 fixtures only |
| Export rule JSON | ⚠️ | bundle export, byte-exact (§7.5) |
| Live validation against schema | ✅ | extend both validators **fixture-first** 🔒 |
| Undo / redo | ✅ | new guided controls must route through `setFields` so one form action is one undo entry |
| Keyboard shortcuts | ✅ | add to `NAV_KEYS`/`shortcuts.js` only — the help dialog derives itself 🔒 |
| Dark / light theme | ✅ | new components use existing tokens; heatmap and cluster hulls need theme-aware palettes with a contrast check |

🔒 **The rule for validation changes, restated because it is the one that bites:** adding a
validation case to `backend/rules/testdata/validation-cases.json` is the **first** step of any
validation change. A rule enforced in Go but not JS (or vice versa) then shows up as a failing
test rather than as an editor that accepts what the loader will refuse.

---

## 10. Testing & CI Plan

### 10.1 Unit

| Area | Tests |
|---|---|
| `evtx/correlate.go` | slot extraction, alias handling, `-`/`S-1-0-0`/`0x0` → absent, hex lowercasing, basename reduction, truncation on a rune boundary |
| normalizer parity | file path and XML path project identical slots for the same event 🔒 |
| `autograph.Prepare` | grouping, bucket chronology inherited without re-sorting, lifetime intervals, requirement union |
| `field` | bucketing, absent-key exclusion, cross-bucket non-matching, determinism under shuffled input |
| `temporal` | window boundaries (`==` within is a match), `window_total`, the latest-anchor property, the case a greedy implementation would miss ⚠️ **this is the regression test that matters** |
| `lineage` | PID reuse across intervals, exit-bounded intervals, unresolved parents counted not guessed, transitive depth |
| provenance | `MatchID` determinism across runs and across input order |
| validation | algorithm dispatch, v2 gating, per-algorithm required/forbidden fields — via the shared fixture |
| `graphlayout` | each profile deterministic; cycle breaking; undated lane in `temporal` |
| `caseintegrity` | each detector against a constructed store |
| snapshot / annotate | round trip, hash-keyed restore, unresolvable reporting |

### 10.2 Schema regression ❌

1. `backend/rules/testdata/schema-golden.json` — the serialized `rules.Describe()`. Any change
   fails the test, forcing a deliberate update **and** a `RULES.md` edit in the same commit.
2. The existing reflection test extended: every `Field.AppliesTo` names a real algorithm; every
   algorithm in `consts.Algorithms` has at least one field or is explicitly parameterless.
3. `autograph` registry ↔ `consts.Algorithms` equality, both directions.
4. Node/edge property-key golden: the indexed key sets, so a silent index change is caught.
5. `corr_keys` positional golden: the slot vocabulary and its order. 🔒 Reordering must fail.

### 10.3 Integration ❌

`backend/integration`, build tag `integration`, running the real pipeline over the committed
`backend/evtx/testdata/Security.evtx`:

- ingest → query → run all built-in rules → assert graph counts → export → re-import → assert
  byte-identical;
- rebuild twice → assert identical relation sets including `match_id` (idempotency + determinism
  in one assertion);
- backfill on a store ingested without correlation keys → assert field rules that matched
  nothing now match;
- crash simulation: kill between `InsertRelations` and indexing → assert `RepairRelationIndex`
  restores it and the rebuild does not duplicate. ✅ pattern already exists, extended to
  provenance.

### 10.4 CI changes

```yaml
test:            # ✅ existing — vet, backend, race, frontend
  + go test -tags=integration ./backend/integration/ -count=1
  + go test ./backend/... -run=XXX -bench=. -benchtime=1x   # benchmarks must not rot
rules-lint:      # ❌ new
  + go build ./cmd/rohyctl
  + ./rohyctl format --check backend/rules/builtin/         # self-hosting formatter gate
  + ./rohyctl validate backend/rules/builtin/
schema-drift:    # ❌ new
  + go test ./backend/rules/ -run TestSchemaGolden
build:           # ✅ existing 4-target matrix
  + build cmd/rohyctl for each target
extension:       # ❌ new
  + npm ci && npm test in tools/vscode-rohy
```

🔒 Unchanged guarantees: `frontend/dist` deleted before every build and never cached; version
metadata injected at link time including the `-dirty` suffix; the release job refuses to publish
unless all six artefacts arrived.

---

## 11. Performance Plan

### 11.1 The cost model this plan is written against ✅

From `PERFORMANCE.md`, unchanged and binding:

| Layer | What dominates | What does not |
|---|---|---|
| Event queries | decoding matched records | locating candidate ids |
| Ingest / graph build | number of durable commits | property encoding |
| Store open | loading the property index | loading the graph |
| Ordering / paging | decoding a minimal per-node view | re-querying (order is cached) |

Every v0.2.0 decision above follows from this: the projection avoids decoding, the shared
dataset avoids re-sorting, batching is untouched, and **no new index keys are added on the node**
because index size is an open-time cost paid by every user on every launch.

### 11.2 Benchmarks ❌ (added to the existing 28)

| Benchmark | Asserts |
|---|---|
| `BenchmarkResidentBytesPerEvent` | 🟡 correlation keys add ≤ **200 B/event p95** — **fails the build if exceeded** |
| `BenchmarkStoreOpen/{10k,100k}` | 🟡 open time does not regress vs the v0.1.0 baseline (0.8 s at 100k) |
| `BenchmarkPrepare/{10k,100k}` × requirement sets | preparation is linear-ish and pays only for what is asked |
| `BenchmarkGenerate/{sequence,field,temporal,lineage}` | per-algorithm cost at 10k/100k |
| `BenchmarkBuild/{1,5,20}rules` | 🟡 20 rules cost ≈ 1 sort, not 20 — the GAP-4 fix, measured |
| `BenchmarkBackfill/100k` | throughput and bounded peak memory |
| `BenchmarkComputeLayout/{profile}` | layout is not the bottleneck at 5k nodes |
| `BenchmarkRelationHeatmap` | reuses the timeline path's cost profile |

🔒 Benchmark the **disk** backend. In-memory runs understate write-path changes badly, because
commit count only costs on disk — recorded caveat, easy to forget.

### 11.3 Rule caching layer ❌

One cache, two consumers (build + testbench):

```go
type datasetCache struct {
    mu           sync.Mutex
    key          string   // filter.orderKey() + requirements fingerprint
    storeVersion uint64
    ds           *autograph.Dataset
}
```

- **Capacity 1.** The dataset is the whole matching event set; holding several would defeat the
  memory discipline the cold store exists to protect.
- Invalidated by the store's existing version counter — the same mechanism the order cache uses
  🔒, so a write can never serve a stale dataset.
- The requirements fingerprint is part of the key: a dataset prepared without lifetimes cannot
  serve a lineage rule.
- Biggest win is the testbench: tuning a rule re-runs `Generate` over a prepared dataset instead
  of re-reading and re-sorting the case on every keystroke-triggered run.

### 11.4 Memory profiling & stress ❌

- `tools/profile/` — a harness capturing `pprof` heap and CPU for ingest, build and open at
  10k/100k/1M events, writing profiles to a gitignored directory.
- Extend `evtx/scale_test.go` with a **build-side** stress: 100k events × 20 mixed-algorithm
  rules, asserting peak memory stays bounded (the ingest side already asserts this ✅).
- A `graphene_memory_test.go` assertion on `corr_keys` cost, as above 🟡.

---

## 12. Case Integrity Plan

New package `backend/caseintegrity`. **Reads and reports; repairs only when explicitly asked.**
🔒 Nothing auto-deletes — the same principle that keeps orphaned findings on disk.

```go
type Report struct {
    RanAt      time.Time
    Deep       bool                 // whether VerifyIndexes was run
    Findings   []Finding            // code, severity, subject, detail, suggested action
    Counts     Counts
    DurationMs int64
}
```

### 12.1 Detectors

| # | Detector | Source | Cost |
|---|---|---|---|
| 1 | Index verification | `store.VerifyIndexes()` ✅ | proportional to the store — **deep mode only** 🔒 |
| 2 | Unindexed relations | `RepairRelationIndex` in count-only mode ✅ | edge walk |
| 3 | Findings audit | `findings.AuditFindings` ✅ — `Total`/`Live`/`Orphans`/`Stale` | hash set intersect |
| 4 | Dangling relations | endpoints that no longer resolve | edge walk |
| 5 | Orphan graphs | `graph_id` values in the store with no registry entry | edge index |
| 6 | Empty graphs | registry entries with zero relations | edge index |
| 7 | Orphan layouts | `layout/canvas-*.json` for graphs that no longer exist | dir scan |
| 8 | Payload tail | walk the length-prefixed log; report orphan bytes after a crash | log walk |
| 9 | Correlation-key staleness | events below `CorrelationKeyVersion` | node scan |
| 10 | **Missing channel** | §12.2 | index count |
| 11 | **Rule inertness** | §12.3 | index count |

**Built.** `backend/caseintegrity` (24 tests) — all eleven detectors — plus `graphene.Inventory`
and three read-only helpers, and `MaintenanceAPI.CheckIntegrity/RepairRelationIndex/RebuildIndexes`
(13 tests). No new bound struct: this is exactly what MaintenanceAPI is for.

🔒 **It reads and reports.** `TestCheckIntegrityWritesNothing` runs the deep check and asserts both
the node count and the store version are unchanged — a checker that tidied up as it went would
destroy the evidence that something went wrong, which is the only thing a report is for. Repair and
rebuild are separate, explicit calls, and the report names which one each finding needs.

🔒 **The deep index verification is opt-in**, and a quick report says so rather than staying
silent — silence would read as "the index is fine". Same judgement PERFORMANCE.md §12a already
records.

🔒 **A detector that could not run is reported, not skipped.** `Report.Errors` carries them, and a
cancelled run says it is partial. A clean-looking report that had not actually looked is the worst
thing this feature could produce.

**One scan, not four.** `graphene.Inventory` answers channels, per-slot correlation coverage,
undated counts, stale projection and the payload high-water mark in a single pass. `CorrKeys` is
decoded here and deliberately NOT added to `eventSortView` — the sort view is decoded by every
ordering and timeline query, and putting a ten-element slice on those paths is exactly the cost
PERFORMANCE.md §14 exists to keep off them.

Three decisions the work forced:

- **`CountUnindexedRelations` had to be added.** The existing `RepairRelationIndex` repairs as it
  counts, so using it to find out whether a repair was needed would mean the report could not be
  run without changing the thing it reports on.
- **Event-id counts are cached per id, not per rule.** Thirty-five built-ins share a handful of
  ids; a test pins that three rules over two ids cost two index lookups, not six.
- **A count that FAILED does not report a rule inert.** A wrong finding produced by an internal
  error is worse than a missing one — it sends the analyst looking for evidence that is actually
  there.

**Lineage rules are checked against their creation ids, not a sequence.** They have none, so
reading `Sequence` alone would report every lineage rule as trivially satisfiable — a rule that
cannot fire, reported as fine.

A note on the **dangling-relation** detector: the storage layer already refuses an edge to a
non-existent event, and deleting an event cascades. So it guards a state only a crash or a future
bypass can reach. The API test records that the front door is shut; the detector's own logic is
covered in `caseintegrity`, where the state can be built from literals.

### 12.2 Missing-channel detector ✅

1. Case channel inventory: distinct values of the indexed `channel` key — cheap, no hydration.
2. For each enabled rule, read its `channels` field (§3.4, an optional field, so no version
   bump).
3. A declared channel absent from the case → **"this rule cannot fire: it needs the
   `Microsoft-Windows-PowerShell/Operational` log, which was never ingested."**
4. All 30 built-ins get `channels` populated so this works out of the box on a fresh case.
5. ⚠️ A rule with no `channels` is not reported — silence means "not declared", never "fine".
   The report says how many rules were unchecked for exactly this reason.

### 12.3 Built-in rule inertness ✅

For each enabled rule, for each distinct event id in its `sequence`, count matching events via
the `event_id` index (`QueryNodeIDs`, no hydration — the same fast path `CountEvents` uses ✅).

Three distinct verdicts, and the distinction is the whole point:

| Verdict | Meaning | Message |
|---|---|---|
| **Inert** ❌ | at least one step has **zero** matching events | "cannot match: no `4688` events in this case" |
| **Live, unmatched** ⚠️ | every step has events, the rule produced nothing | "the events exist but the pattern does not occur" |
| **Live, matched** ✅ | produced relations | — |

Plus, for field/temporal rules: **"live, but blocked"** — every step has events, but the events
carry no value in a required correlation slot (typically an un-backfilled store), with the
backfill action offered inline. Without this, a field rule on an old case silently reports zero
matches and looks like a clean result.

### 12.4 UI ✅

A `MaintenanceView` route: run integrity (quick / deep), the report grouped by severity, each
finding with its suggested action, plus the backfill runner with progress on
`maintenance:progress`, and the read-only query console (§13 / §4.7). Actions are always
explicit buttons — 🔒 nothing runs on startup.

**Built.** `lib/maintenance/report.js` (23 tests), `stores/maintenance.js` (12 tests),
`routes/MaintenanceView.svelte` (3 smoke tests), registered as route 7 with `Alt+7` — the nav menu
and the shortcut list are both derived from `NAV_KEYS`, so adding it in one place added it to both.

🔒 **An all-clear is a claim, and it is always qualified.** `verdict()` returns `clean` only when
nothing was found AND every detector ran; a report whose inventory failed reads as **incomplete**,
not as clean, because it found nothing by not looking. A quick check that found nothing says so
and points at the deep one rather than implying the index was verified.

🔒 **Every action is a button.** Nothing runs on mount except the correlation-key status, which is
one cheap query and is the single thing worth knowing before anything is pressed — it decides
whether half the rule library can say anything at all.

**Only the three fixes the app can perform are buttons.** "Ingest the missing log" needs files
rohy does not have, so it renders as advice; a control that opened a file picker would be a guess,
and one that did nothing would be worse.

Three smaller decisions:

- **A fix re-runs the check afterwards**, at the same depth. A report left stale after its own
  remedy would keep asking for something already done.
- **One `running` flag, not one per action.** The backend serialises these passes over one store,
  so two buttons that both looked enabled would be a promise it refuses to keep.
- **`rules_unchecked` is shown even at zero.** It is how many rules could not be checked for a
  missing log, and its absence would read as reassurance when it means "not declared".

**A real bug the smoke test caught, unrelated to this feature.** `stores/theme.js` guarded storage
with `typeof localStorage !== 'undefined'` — but Node defines a `localStorage` global whose methods
throw unless the runtime has a backing file, so the object test passed and the call still failed.
Rendering any route that imports the theme store crashed. It now feature-detects the *method* and
wraps both read and write, which also covers a browser in private mode. This is the sixth defect
this class of test has caught, and the first outside a component's own code.

⬜ **Not built:** the read-only query console (§4.7), which is a Phase 7 item and belongs with the
rest of the tooling work.

---

## 13. Final Execution Timeline

Sequenced by dependency, not by preference. Each phase ends green: full suite, race, CI matrix.

### Phase 1 — Foundations (weeks 1–2) 🔴 the whole release depends on this

1. `consts`: slots, algorithm descriptors, `RuleFormatVersion = 2`, new codes.
2. `evtx/correlate.go` + wire into all three normalizers + parity test.
3. `graphene`: `CorrKeys`/`CorrKeyVersion` on `Event`; provenance fields on `Relation`;
   `rule_id` edge index; memory benchmark with the **hard budget assertion** 🟡.
4. `BackfillCorrelationKeys` — resumable, idempotent, opt-in, progress-reported.
5. `rules`: v2 fields, algorithm-aware validation, `AppliesTo` descriptor, schema golden.
6. `autograph`: `Dataset` + `Prepare` + 🔴 interface change; `sequence` refactored with its
   existing tests untouched; registry ↔ consts parity test.
7. `RULES.md` rewritten for v2 — §3 table, §4 per-algorithm rules, §5 version-bump record,
   §7 no longer "reserved", **§8 split into "what a `sequence` match establishes" vs "what a
   `field` match establishes"** (this is the honest headline of v0.2.0).

**Gate:** no behaviour change visible to a v0.1.0 rule. Existing 30 built-ins load, run and
produce identical graphs. 🔒

### Phase 2 — Algorithms (weeks 3–4)

8. `field` + fixtures + benchmark.
9. `temporal` (the DP, plus the regression test for the match a greedy implementation misses).
10. `lineage` (lifetime intervals, PID reuse, unresolved parents).
11. Provenance stamping in `graphbuild`; `MatchID` determinism test.
12. ~6 new built-in rules at v2 demonstrating each algorithm; `channels` on all 30 existing.

**Gate:** rebuild-twice produces identical relations including `match_id`.

### Phase 3 — Pipeline & caching (week 5) ✅

13. ✅ Requirements union; single `Prepare` per build; `BenchmarkBuild` proving the win —
    **2.2× faster, 3.5× less allocation at 20 rules**.
14. ✅ **Dataset cache**, capacity 1, keyed on `EventFilter.CacheKey()` + requirements
    fingerprint, invalidated by the store's own write counter. Two details worth keeping: the
    version is re-read **after** the query so a write landing mid-read costs a rebuild rather
    than serving a stale answer (the order cache's reasoning), and the requirements fingerprint
    is part of the key because a dataset prepared for computer-scoped rules holds no global
    partition and would silently correlate nothing.
15. ✅ **`DryRunRule`** — the rule testbench. Takes rule TEXT rather than an id, so an author can
    try something not yet on disk; returns the same located problems the editor renders rather
    than a rejected promise, because unparseable text is the normal state mid-typing; and
    reports `SkippedNoKeys` / `StaleCorrelationKeys` / `UnresolvedParents` as first-class
    results, because "this pattern is rare" and "none of your events could be considered" are
    both zero and must not look alike.

    🔒 The load-bearing test is `TestDryRunWritesNothing`: relations, events and graphs are all
    asserted unchanged afterwards. The safety claim is not "it usually does not persist" — it is
    that the path contains no write, and that assertion is what keeps it true when somebody
    later reaches for `EnsureForRule` to make the result linkable.

### Phase 4 — Editor (weeks 6–7)

16. ✅ **Algorithm-aware guided editor.** Three things, and the second was a real bug:
    - `applies_to` visibility via `lib/rules/fields.js` (21 tests, reading the **schema golden**
      so it runs against the real descriptor). A field the algorithm does not read is *hidden*,
      not disabled — a disabled control still asks the author to reason about a setting that
      cannot matter. 🔒 **Unless the file already sets it**, in which case it stays visible and
      marked inert: the value is preserved on save, and a control that vanished with its value
      would leave no way to remove it.
    - 🔴 **`string[]` fields had no control.** The form rendered any field with an `enum` as a
      single-value `<select>`, so picking a correlation field would have written a *string* into
      `match_fields` — which the loader then refuses as a type error. `ListField.svelte` now
      renders closed lists (`match_fields`) as toggles from the served vocabulary and open ones
      (`channels`, `lineage_create_ids`) as editable rows.
    - The algorithm's `summary` shown beside the selector — what the choice *means* is the one
      thing an author cannot infer from a name in a dropdown. ~~plus a one-click
      "set format_version to 2"~~ — deleted with the format-version reversal (§2); the format
      stayed at 1. (The Algorithms explainer's example rules still say `"format_version": 2` —
      `lib/learn/algorithms.js` — and pasted into the editor they are refused; backlog.)
18. ✅ **The build path now says what it could not see** — the honesty gap that had been open
    since Phase 1, where the engine reported `StaleCorrelationKeys` and nothing displayed it.

    - **`MaintenanceAPI`** (a 7th bound struct, `backend/api/api_maintenance.go`): correlation-key
      status and the backfill, with progress events and a concurrency guard. Its own struct
      rather than methods on `SystemAPI` because the two are different concerns — `SystemAPI`
      reports how the *application* is doing and never touches case data, while everything here
      reads or rewrites the case. 🔒 Nothing runs on startup; these are actions with a button,
      the same judgement `VerifyIndexes` already carries. This is where Phase 6's integrity work
      will grow.
    - **Per-rule attribution.** `RuleOutcome` gained `SkippedNoKeys` and `UnresolvedParents`.
      Run-level totals cannot distinguish one field rule excluding most of the case from every
      rule being blind, and those are completely different situations.
    - **`CorrelationNotice`** in the rules view: a persistent banner, not a snackbar. A caveat
      that changes how every subsequent run should be read is the wrong shape for a toast. It is
      **silent when there is nothing to say** — a permanent banner trains people to scroll past
      it, which is the one thing a caveat cannot afford — and the run summary now carries the
      undated / no-keys / unresolved-parent / stale counts alongside the match total.
    - A run that produced **nothing** refreshes the coverage first, so the notice is on screen
      beside the empty result rather than after it. That is the case most likely to be misread.

    Copy note: the UI says "events cannot be correlated on fields yet", not "stale correlation
    key projection". What an analyst needs is what it means for their results, not what the
    internal representation is called.

17. ✅ **Testbench panel**, wired to `DryRunRule`. The under-report counts are laid out *above*
    the matches, because they change how the match total should be read and a reader who scrolls
    past them has already drawn a conclusion. A stale result is dropped on every edit: "12
    matches" computed against text that has since changed looks current and is exactly the kind
    of number somebody acts on. 🔒 There is deliberately no "add to graph" affordance.
21. ✅ **Field-level diff.** It sits BESIDE the line diff rather than replacing it, because the
    two answer genuinely different questions and an author needs both: reformatting a file is a
    large line diff and no change to the rule at all, while inserting one sequence step is a
    small line diff that shifts every label after it.

    - `lib/rules/fielddiff.js`, 17 tests. Reads the served schema for the field list, so a field
      added to the format appears here the day it appears in the form.
    - **Sequences align as chains, not by position.** `["4625","4624"]` → `["4625","4688","4624"]`
      is *one step added*; a positional comparison calls it "position 1 changed, position 2
      added" and leaves the author to work out which. Each step carries the label on the
      connection LEAVING it, matching how the format defines `labels[i]`, so a label moves with
      its step instead of every label appearing to change.
    - **Empty means absent, matching the loader.** `labels: []` → nothing is not a change; the
      loader trims strings and treats a missing tail of labels as untagged.
    - Unknown fields are compared too — the format preserves them on save, so a diff that
      dropped them would under-report a real change.
    - An unparseable buffer reports *"cannot compare"* rather than rendering an empty table,
      which would read as "no changes".
19. ✅ **Shared selection + the relationship inspector** (Phase 5 item 22 pulled forward).
    Delivered together because they are one data path, and because provenance nothing displays
    is provenance nobody can check.

    - **Edges became selectable at all.** They were `aria-hidden` decoration with no pointer
      handling. Each now has a wide **invisible companion path** as its hit area: a 2px stroke is
      near-unclickable with a mouse and entirely so on a trackpad, and thickening the visible
      line would make the canvas heavier to read. Precision and appearance are separate concerns
      and now separate elements.
    - **`GraphAPI.InspectRelation`** resolves an edge in ONE call — endpoints, graph name,
      provenance, and the rest of the occurrence. Siblings go through the `rule_id` index and are
      then narrowed by match id in memory: exactly why `rule_id` is indexed and `match_id`
      deliberately is not (a near-unique key is what cost 8.0s vs 0.8s at store open).
    - **Selecting one edge lights up the whole match** and dims the rest. "What else is part of
      this chain?" is the question an analyst has when looking at one link, and before relations
      carried a match id there was no way to answer it. `highlightedEdges()` is pure and tested.
    - **The inspector draws the distinction the whole app is built around.** A rule-created edge
      shows its rule, algorithm, step and **basis** — the values that made the match. A
      hand-drawn edge is a *different kind of claim*, not a rule edge with fields missing. An
      edge predating provenance says so rather than rendering blanks.
    - **Canvas → editor**: the rule opens *in place* over the graph, because the question is
      about the edge in front of you and navigating away to answer it is the wrong trade. A
      built-in opens as a duplicate. A rule that no longer resolves is reported — an id is a slug
      of the name, so renaming strands the graph it built, which is what someone clicking here is
      trying to understand.

20. ✅ **The keyboard route to relation selection** — closing the gap the clickable edges opened
    in the same sitting, rather than letting it become permanent.

    The two click-handler warnings on the hit path stay *suppressed* rather than satisfied, and
    that remains right: hundreds of tab stops would be worse for a keyboard user than the
    problem, and the canvas is already one focusable `role="application"` region owning its
    selection. What was wrong was not the suppression — it was that **the mouse had a route and
    the keyboard had none.**

    `R` now steps through the selected event's relationships, `Shift+R` steps back, following the
    canvas's existing bare-letter convention. Three decisions in `lib/graph/relations.js`, which
    is pure and has 13 tests because an off-by-one in a cycle is invisible in review and obvious
    in use:

    - **Stable order** (by edge id). The same selection must produce the same cycle every time,
      or pressing twice does not move predictably — and a keyboard user has no other way to tell
      which edge they are on.
    - **Wraps at both ends.** A key that walks off the end leaves you pressing something that
      does nothing, with no way to know whether you reached the end or the binding broke.
    - **Never a silent no-op.** Nothing selected, or a node with no relations, says so. A key
      that quietly does nothing is indistinguishable from one that is broken.

    `Escape` unwinds the inspector first — it is the newest thing on screen, so one press should
    close it rather than also dropping the node selection underneath. The binding is registered
    in `shortcuts.js`, which the handler and the help dialog both read, so it cannot work while
    the documentation claims otherwise.
22. ✅ **Bundle export.** `RulesAPI.ExportRules(ids)`, empty selection taking the whole library —
    handing a case's rules to a colleague should not require selecting thirty checkboxes.

    🔒 **Byte-exact, and tested as such.** Each rule carries its file's bytes as stored, never a
    re-serialization: round-tripping through `Spec` would be less code and would quietly destroy
    the two things the format promises to keep — field order, and any field this build does not
    interpret. `TestExportRoundTripsThroughImport` writes the export into a fresh library and
    asserts the bytes come back identical. The frontend keeps `source` as an unparsed **string**
    in the bundle document for the same reason; parsing it to make the JSON tidier would undo
    the whole point. `missing` is reported rather than omitted, so a bundle is never quietly
    short — that is a discovery best not left to the recipient.

23. ⬜ Raw editor polish (bracket matching, problems gutter, format-on-save) — the last Phase 4
    item, and the least consequential.

24. ✅ **Documentation catch-up.** Not a numbered plan item, taken before Phase 5 because the
    three long-lived documents had drifted far enough that they described an architecture that
    no longer exists — and `LLD.md` in particular had not been touched since before any v0.2.0
    work began.

    - **`RULES.md`** — rewritten for four algorithms. §5 records **why v0.2.0 did *not* bump
      the format version** (deviation D-6), §8 is split per algorithm because "what a match
      establishes" is the one thing that genuinely differs between them, and a new §9 documents
      the correlation-field vocabulary.
    - **`LLD.md`** — counts refreshed (13 200 LOC, 482 Go tests, 364 frontend tests, 35
      built-ins), the ER diagram given `ck`/`ckv` and the relation provenance columns, plus four
      new sections: §5.6 the correlation projection, §11.5 the prepared dataset, §11.6 the four
      algorithms, §11.7 provenance. Five new entries in the sharp-edges list, all of them bugs
      that actually happened this cycle.
    - **`PERFORMANCE.md`** — two new sections rather than edited old ones, because both are new
      cost models: **§16** the correlation projection (the 819 B first measurement, the
      by-position and per-slot-bound fixes, 324 B worst / 71 B typical against a 400 B asserted
      ceiling) and **§17** prepare-once (50k events: 282 ms vs 505 ms and 16.2 MB vs 56.1 MB at
      twenty rules). Four rows added to the decisions-pinned-by-tests table.
    - **`README.md`** — v0.2.0, thirty-five built-ins, the algorithm table, the five
      stronger-than-ordering rules called out separately, and the roadmap split into what each
      release delivered.

    🔒 **Numbers were re-measured, not copied forward.** The benchmark figures already in
    `LLD.md` came from a single earlier run; re-running at `-benchtime=20x -count=3` gave
    materially different times (222 → 282 ms) with byte-identical allocation figures. Both
    documents now carry the medians, the spread, and a note that the ratios are the claim — not
    the digits. The typical projection cost was likewise 71 B, not the 66 B first written down.

    One inconsistency left deliberately: `build.sh` / `build.ps1` / `release.yml` still default
    to `0.1.0`. Bumping a release default is a release decision, not a documentation one.

### Phase 5 — Graph (weeks 8–9) ✅

_Numbering note: Phase 5's items 21–24 reuse numbers Phase 4 already used. Left as written,
because other sections cite them; read "Phase 5 #21" where the phase matters._

All seven items done. Two pieces were deliberately left, both recorded in their sections rather
than dropped: the snapshot **thumbnail** (§8.3), which needs the canvas rendered off-screen, and
**region/arrow creation from the canvas** (§8.4), which render but can only be authored for notes.

21. ✅ `backend/graphlayout` + four profiles + tests — see §8.1 (D-7 recorded there).
22. ✅ Relationship inspector — landed in Phase 4.
23. ✅ Clusters + hulls + collapse — see §8.7.
24. ✅ Heatmap (reusing timeline bucketing) — see §8.6.
25. ✅ `backend/snapshot` + panel + restore preview — see §8.3.
26. ✅ `backend/annotate` + layer panel — see §8.4.
27. ✅ Replay (event-time ordered 🔒) — see §8.5.

### Phase 6 — Integrity & DX (week 10) ✅

All four items done. One piece deliberately deferred and recorded in §12.4: the read-only query
console, which is a Phase 7 item and belongs with the rest of the tooling work.

28. ✅ `backend/caseintegrity` + 11 detectors — see §12.1.
29. ✅ Missing-channel + inertness (including "live but blocked") — see §12.2/§12.3.
30. ✅ `MaintenanceView` + backfill runner + progress channel — see §12.4.
31. ✅ `cmd/rohyctl`: `validate`, `format`, `explain` (25 tests) + the `rules-lint` CI job.

    🔒 **`validate` calls `rules.ValidateSource` — the same function the editor calls and the
    loader is built on.** A second implementation in the CLI would drift, and the drift would
    surface as the worst possible failure: a rule that passes in the terminal and is silently
    missing from the library on the next scan.

    🔒 **The import list is `rules`, `consts`, `version` and nothing else.** No graphene, no
    sidecars, no state — which is what lets it run in CI, in a pre-commit hook, and on a machine
    that has never seen the application.

    **Three exit codes, not two.** 0 fine, 1 a rule problem, 2 could-not-run. A CI job responds to
    a typo in a path differently from a broken rule, and collapsing them would report the first as
    the second.

    Four decisions:

    - **Warnings print but do not fail.** They are legal-but-worth-saying; failing on them would
      make the gate refuse rules the application itself accepts.
    - **`--write` and `--check` together are refused**, not resolved. They contradict — one
      rewrites, the other promises to write nothing — and guessing risks rewriting files in a job
      that was only meant to inspect them. A test asserts `--check` leaves the file byte-identical.
    - **Unparseable text is skipped, never rewritten.** Formatting must not be the thing that
      destroys a file somebody was mid-edit on.
    - **`explain` assembles what a match establishes from the rule's own fields**, not from a
      per-algorithm sentence. A sequence rule with `match_fields` proves strictly more than one
      without; a fixed sentence would overstate the weaker or understate the stronger. It says
      "NOTHING MORE" for the ordering-only case, and calls out global scope and undeclared
      channels explicitly.

    **CI gains `rules-lint`**, separate from `test` because it answers a different question: not
    "does the code work" but "is the DATA the code ships in the shape the code expects". It is
    self-hosting — the formatter's gate runs against the rules the app embeds — and two tests pin
    that the shipped library both validates and is already canonically formatted, so a formatter
    change that would reformat every built-in fails at the source rather than in someone's diff.

### Phase 7 — Tooling, console, hardening (weeks 11–12)

32. ⚠️ `tools/genschema` ✅ + `tools/vscode-rohy` ⬜ (CLI-backed diagnostics when `rohyctl` is on
    PATH, JSON Schema fallback otherwise — ⚠️ avoids a third validator implementation).

    **`tools/genschema` built** (11 tests) + the `schema-drift` CI job.

    🔒 **Generated, never hand-written.** A hand-maintained schema would be a third statement of
    the rule format — after the Go validator and the guided editor's descriptor — and three
    statements of one contract drift. `TestCommittedSchemaIsNotStale` and `go run ./tools/genschema
    -check` in CI are what stop it; `-check` writes nothing, so the gate cannot pass by making
    itself true.

    🔒 **A field is `required` in the schema only when EVERY algorithm needs it.** `sequence` is
    required — but not for lineage, which matches no sequence at all. Listing it unconditionally
    would underline every valid lineage rule, and a red squiggle on a correct file is the failure
    that makes somebody turn the extension off. The schema under-reports on purpose; `rohyctl
    validate` catches what it cannot express. `TestNoShippedRuleWouldBeFlaggedByTheSchema` runs
    that claim against all 35 built-ins.

    🔒 **`additionalProperties: true`.** The format preserves fields this build does not interpret
    (RULES.md §3), so a schema that refused them would flag a forward-compatible file as broken —
    and the loader would then accept the very thing the editor underlined.

    Two smaller decisions: the `$id` is a **URN, not a URL**, because there is nothing to fetch and
    a URL that 404s is worse than an identifier that never promised to resolve; and the "Read by:
    …" sentence is emitted only for **algorithm-specific** fields, since a field every algorithm
    reads has no "other algorithms" and saying so anyway trains the reader to skip the sentence
    where it carries the warning.

    ⬜ **`tools/vscode-rohy` not built.** It is a separate npm project — extension manifest, a
    language client, `rohyctl` discovery with the schema as fallback, and its own test suite — and
    it is the one item in the plan whose value is entirely outside this repository's own build.
33. `QueryAPI` read-only console (structured builder, **no expression language** 🔒).
34. Integration suite; stress + profiling harness; benchmark smoke in CI.
35. ⚠️ CI jobs: `rules-lint` ✅, `schema-drift` ✅, `extension` ⬜, CLI in the build matrix ⬜.

    `go vet` and `go test` now cover `./cmd/...` and `./tools/...` as well as `./backend/...`, so
    the CLI and the generator are held to the same bar as everything else.
36. Docs: `LLD.md` (new §s for the projection, the dataset, provenance, layout, integrity;
    update §19's ledger and §22's recipes; update §23's sharp edges), `README.md`,
    `PERFORMANCE.md` (new measurements), release notes.

### Phase 8 — Forensic integrity (weeks 13–15) 🔴 added after the audit; see §14

The engine rohy is built on has integrity machinery rohy does not call: signed commits,
Merkle snapshot roots, third-party-verifiable inclusion proofs, attributed redaction,
chain-of-custody reports, anchoring. `graphene_store.go:73` calls `graphene.Open(dir)` — the
historical, unsigned, unverified default — and no rohy file imports `graphene/disk`,
`graphene/signing`, or `graphene/merkle`.

For a tool whose entire thesis is that **inferred ≠ asserted**, the gap is the largest one
left: rohy can say a relation was inferred by a rule, and cannot say the event under it is
the event that was ingested.

37. ✅ **Roots and verification** (§14.2) — keyless. Finished 2026-11-04 (§15.5): every seal
    recorded through one path, `VerifyChain` + `custody.Chain` and five chain findings, no-op
    automatic seals skipped, the sealing indicator, and Go tests for the four forensic methods.
    - ✅ `graphene_forensics.go`: the `Forensics()` accessor, `ForensicStatus`, `SnapshotRoots`
      (hex, zero-hash-as-empty), `VerifyImage`, `WithVerifyOnOpen`. 9 tests, including the
      load-bearing one — a single flipped bit deep in the record area IS detected.
    - ✅ 🟡 **The gate is resolved, and harder than expected** (PERFORMANCE.md §18):
      `VerifyOnOpen` is `VerifyImage` run at open and nothing else — +617 ms and +449 MB on a
      100k case, matching the standalone cost to three significant figures. On demand is the
      route; the option is off by default **permanently**.
    - ✅ Detectors 12/13/14/15/18 in `caseintegrity` (16 and 17 do not exist yet — #43) + `MaintenanceAPI.ForensicStatus` /
      `VerifyImage` / `CompactCase` + the Maintenance page cards. 11 + 10 tests.
    - ✅ 🟡 **Compaction on the ingest boundary, and the gate answered the placement.** Measured
      (PERFORMANCE.md §18): adding **500 events** to a 100k case costs the same ~1.3 s / 566 MB to
      compact as compacting all 100k from nothing — allocations within 6%. Compaction is
      proportional to the CASE, not the ingest. So it is once per **request**, never per file: a
      twelve-file ingest sealing per file would spend ~16 s and throw eleven results away.

      🔒 Implemented as a **deferred** call, not a trailing one. The cancellation path returns from
      inside the file loop, so as a trailing statement it was skipped there and a cancelled ingest
      left everything it had written unaccounted for — caught by `TestCancelledIngestIsStillSealed`.
      New `case:sealing` / `case:sealed` channels, because a silent second after "ingest complete"
      is indistinguishable from a hang.
    - ✅ **`backend/custody`** — the fingerprint history (8 tests), detector 15 wired to it, the
      `ExportCaseFingerprint` artefact, and the save/copy actions on the Maintenance page.

      🔒 **The field is `exported`, never `retained`,** and this is the load-bearing naming decision
      of the whole item. rohy can see that a copy left the app. It cannot see whether the copy
      survives, whether it went beyond the reach of anyone able to rewrite the case, or whether it
      was transcribed correctly — and the copy is worth having *precisely* because those are outside
      what this process observes. So the satisfied state stays **info**, says what rohy cannot tell,
      and never becomes a tick. An unknown fingerprint reports not-exported, so a history lost to a
      corrupt sidecar produces the caveat rather than suppressing it.

      `establishes` / `does_not_establish` travel **inside** the exported document, because the
      document is what gets forwarded and may be read by somebody who never saw the screen.
38. ✅ **Inclusion proofs** (§14.3) — keyless. `ProveNode` / `ExportNodeProof`, export from the
    event detail, `rohyctl verify-proof`. Landed as **event proofs** (§15.4): the file carries
    the stored record and raw record beside graphene's proof, because a v3 leaf commits only to
    a hash of the record.
39. ✅ **Custody report** (§14.4) — `CustodyFor`, reported as gaps rather than a verdict. Landed
    2026-11-04 (§15.6): composed with the record, raw and history layers the engine cannot see, and
    a kept fingerprint compared against the image whatever the event's state.
40. ✅ **Attributed redaction** (§14.5) — landed 2026-11-05 (§15.7), payload tombstone first
    (§14.5.1): the cold store would otherwise have defeated graphene's redaction.
41. **Signing and attribution** (§14.6) — key custody OUTSIDE the case folder,
    `RequireSignedCommits`, `Audit`, `Retention`, analyst actor identity.
42. **Anchoring** (§14.7) — checkpoint digest out, receipt in. 🔒 rohy ships no anchor.
43. **Forensic detectors** in `caseintegrity` (§14.8) + the Maintenance page surface.

**Gate:** a case ingested by this build can have any single event proven to a third party who
holds only the exported proof and a root the analyst kept elsewhere — with no access to
`rohy-data/`. And 🔒 every posture rohy is *not* in is reported as such, never as clean.

### Critical path

```
consts+slots ─► extraction ─► Event schema ─► backfill ──┐
                                     └► Relation provenance ─► shared selection (Ph4)
                                                          └► inspector (Ph5)
Dataset/Prepare ─► field ─┬─► pipeline ─► cache ─► testbench (Ph4)
                          ├─► temporal
                          └─► lineage ─► lineage layout profile (Ph5)
rules v2 ─► schema descriptor ─► algorithm selector (Ph4) ─► genschema ─► vscode (Ph7)
                              └► channels field ─► missing-channel detector (Ph6)
```

### Risk register

| # | Risk | Severity | Mitigation |
|---|---|:---:|---|
| R1 | Correlation keys blow the resident-memory budget | 🔴 | Hard-failing benchmark assertion in Phase 1; response is to shrink the vocabulary, never to raise the budget |
| R2 | Backfill is slow or unfinishable on a large case | ⚠️ | Resumable + opt-in + progress; algorithms report `StaleCorrelationKeys` so a partial state is never mistaken for a clean result |
| R3 | v2 rules silently degrade on a v0.1.0 build | 🔴 | Already prevented by `format_version` refusal (`RULES.md` §5) — verified by test, and existing built-ins stay at v1 |
| R4 | Temporal greedy-vs-DP semantics confuse authors | ⚠️ | Documented in `RULES.md`; the missed-match case is a named regression test |
| R5 | Lineage guesses parents across PID reuse | 🔴 | Interval table + unresolved-parents counter; **no edge without a covering interval** 🔒 |
| R6 | Validation drift Go / JS / VSCode | ⚠️ | Shared fixture is the first step of any change; extension shells out to `rohyctl` rather than reimplementing |
| R7 | Edge index growth slows store open | ⚠️ | Only `rule_id` added (low cardinality); `BenchmarkStoreOpen` guards it 🟡 |
| R8 | Scope creep in the query console | ⚠️ | Structured builder only; an expression language is explicitly deferred past v0.2.0 |
| R9 | Snapshot restore corrupts a live graph | 🔴 | Hash-keyed endpoints, explicit preview, re-created edges marked `created_by: user` with a basis, unresolvable items reported |
| R10 | A second edge type sneaks in | 🔴 | Design forbids it (§5.4); `EdgesOf(..., nil)` reasoning re-stated in the LLD sharp-edges list |
| R11 | **A signing key stored in the case folder manufactures a false guarantee** | 🔴 | rohy REFUSES to write a key inside `rohy-data/` (§14.6). Whoever can rewrite the store can re-sign it, so a key beside the evidence proves nothing — and a green tick that means nothing is worse than no tick |
| R12 | **Redaction leaves the raw record in the payload cold store** | ✅ | Resolved by #40 (§15.7): the tombstone landed before any redaction UI, and the end-to-end test finds the raw record in no file under the case folder afterwards |
| R13 | `VerifyOnOpen` makes cold start unacceptable on a large case | 🟡 | Measured at 100k events before it is defaulted on; it lands behind the existing `Warm()` progress stage. Response is a per-case setting, never a silent skip |
| R14 | The only retained root lives inside the case | 🔴 | Every internal check compares the store against itself. Root export is a first-class action and the UI states plainly what an internal-only root does and does not establish (§14.2) |
| R15 | Nothing is provable because the case was never compacted | ⚠️ | Compaction moves to the ingest boundary (§14.2); un-attested events are reported as "live but unaccounted for", which is what `CustodyFor` already distinguishes |
| R16 | The forensic surface reports "clean" on a backend that has none | 🔴 | `Forensics()` is false on the in-memory backend, which is what the test suite uses. 🔒 Unavailable is reported as unavailable — the same discipline as a `caseintegrity` detector that could not run |

### Definition of done

- ✅ All four algorithms implemented, registered, validated, benchmarked, documented.
- ✅ Every relation a rule writes carries rule, algorithm, occurrence, step and basis.
- ✅ `RULES.md` §8 distinguishes what each algorithm establishes — the analytical claim is
  honest per algorithm.
- ✅ Resident cost per event within budget, proven by a failing-if-exceeded benchmark.
- ✅ A rebuild is still byte-identical and idempotent, including `match_id`.
- ✅ An analyst can tell "no matching events", "events but no pattern", and "blocked on
  correlation keys" apart without reading source.
- ⚠️ CI gates schema drift and rule formatting — **not the integration path**: there is no
  `backend/integration` suite yet (#34), and CI stays minimal for now (2026-11-03 audit).
- ✅ Nothing auto-deletes, nothing auto-runs at startup, nothing silently truncates.
- ✅ **A third party holding only an exported proof and an externally retained root can check
  that a named event was in the case — without access to `rohy-data/` and without rohy** (§14).
  `rohyctl verify-proof` needs neither; and the check covers the event's content, not only its
  presence (§15.4). Shipping `rohyctl` to recipients is the deferred release item (#35c).
- ⬜ **Every integrity posture rohy is not in is stated.** Unsigned commits, an unverified
  image, a case never compacted, a root never exported, a ledger that cannot be verified — all
  reported, none of them silent (§14.8).
- ✅ **A redaction removes the content everywhere rohy put it**, including the payload cold
  store, or it is not offered at all (§14.5.1).

---

## 14. Forensic Integrity Plan (Phase 8)

### 14.1 The load-bearing distinction: what needs a key and what does not

Read `graphene/SECURITY.md` before this section; it is the authority on what each mechanism
proves. The one-sentence version, which governs everything here:

> Graphene can make modification of its stored evidence detectable by a party holding an
> independently retained reference value. It cannot prevent modification by code running in
> its own process, and it cannot attest to the truth of what it was told.

The machinery splits cleanly in two, and rohy should adopt it in that order rather than in the
order the upstream adoption table happens to list:

| | Needs no key | Needs a key |
|---|---|---|
| **Mechanism** | Merkle snapshot roots, `VerifyOnOpen`, `ProveNode`, `ExportNodeProof`, `VerifyExportedProof`, `VerifyNodeInclusion`, tombstone proofs | Ed25519 commit signatures, snapshot attestations, verified redaction chains, `CustodyFor` with a verifier |
| **Establishes** | That this evidence has not changed since a root you kept elsewhere | Who asserted it |
| **rohy's position** | **Ship it, on by default.** `Compact()` computes the roots unconditionally, so the store already pays for them — not using them is pure waste | **Opt-in, with key custody outside the case.** §14.6 |

The keyless half is where almost all the value is, and it is free. `VerifyNodeInclusion` and
`VerifyExportedProof` are **package-level functions that take no store** — a recipient checking
evidence has a proof, a root, and nothing else. Verifying costs ~5 µs against ~8 ms to build:
asymmetric in the direction that matters.

🔒 **The root is an argument, never read from the proof.** Checking a proof against a root
carried inside it is circular, because whoever produced both chose both. Any rohy API that
verifies takes the root from the caller.

### 14.2 Roots and verification (item 37) — keyless

**Two routes to verification, and rohy's primary one is not the obvious one.**

The default `Open` runs structural validation that cannot be switched off (header bounds, record
counts, entity ids, a snapshot root that does not follow from its components) but does **not**
re-derive the record hashes — which is precisely what a deliberate edit to an otherwise-coherent
file looks like. Upstream is unambiguous that evidence stores should close that gap.

The obvious route is `VerifyOnOpen`. The better one for rohy is **on demand**:

```go
disk.VerifyCSRDigest(path)   // package-level, takes a dir or file, opens no store
disk.VerifyCSRRoots(path)    // same
```

Both are keyless, storeless, and callable whenever. So image verification becomes a **button in
the deep integrity check**, beside `VerifyIndexes`, which is exactly where work proportional to
the whole store already lives — and it costs **nothing at startup**. Upstream's own reasoning for
offering it this way is, word for word, rohy's PERFORMANCE.md §12a:

> *"a check that makes every open slower gets disabled, and a disabled check protects nothing. It
> is offered here … so that verifying is a decision rather than a tax."*

#### 🟡 → ✅ MEASURED, and it settles the question harder than expected

`PERFORMANCE.md` §18 has the tables. Ryzen 9 5980HS, medians of three, compacted store:

| Events | Open, default | Open, verified | Ratio | `VerifyImage` alone |
| -----: | ------------: | -------------: | ----: | ------------------: |
| 10k    |         59 ms |         117 ms | 2.0×  |               65 ms |
| 100k   |        500 ms |    **1117 ms** | 2.2×  |          **626 ms** |

**`VerifyOnOpen` is `VerifyImage` executed during open, and nothing else.** At 100k the open
overhead is +617 ms against a standalone cost of 626 ms, and the allocation delta is +449 MB
against 449 MB — matching to three significant figures *and* to the allocation count
(+5 100 090 vs 5 100 111). The option buys no evidence the button does not. What it buys is
automaticity, and it charges +617 ms and +449 MB of churn on every launch to get it.

🔒 **Verification is on demand. `VerifyOnOpen` is off by default permanently, not
provisionally** — this is not a threshold a faster machine clears later, because the two routes
do identical work, so the tax is pure. `WithVerifyOnOpen` stays available for a strict per-case
posture and `ForensicStatus` reports which posture is in force.

🔒 Off is a **reported state**, never a silent one (detector 12, §14.8). A verification that is
off without saying so is worse than one that was never offered.

⚠️ Verification is **allocation-heavy**, not merely slow: ~4.5 kB of churn per event. It must not
run concurrently with a graph build, which is what the maintenance binding's single-pass guard
already prevents.

**FOUR outcomes, not two** — and the fourth was found by a test failing, which is the useful way
to find it. `DigestAbsent` is not a failure: a pre-v8 image carries no digest, and that is the
honest answer for a file that was never covered. But a case that has **never been compacted** has
no image at all, and reporting that as "absent" would tell an analyst with a long-running case
that it was never compacted, which is false.

| State | Means | Remedy |
|---|---|---|
| `match` | the bytes are unchanged since they were written | — |
| `mismatch` | they are not — damage or edit, and the digest cannot tell which | investigate |
| `absent` | an image exists that digests never covered | compact (rewrites it) |
| `no_image` | nothing has been compacted; nothing can be proven yet | compact |

🔒 Collapsing any of these into a pass is the same error as treating an un-backfilled correlation
projection as an absent field. `ErrNoSnapshotRoots` is translated into a reported state rather
than passed up as an error, because a caller forced to string-match an error to learn that a case
is merely new is a caller that will eventually report a healthy case as broken.

**Compaction is what makes anything provable.** Snapshot roots and the attestation over them
are written by `Compact()`. rohy calls it in exactly one place — `app.go:177`, on clean
shutdown — so a case that was force-quit, crashed, or is simply still open has **no roots for
anything ingested this session**. Every event in it is live but unaccounted for.

Move compaction to the **ingest boundary**: after an ingest run completes, which is the natural
evidence boundary and already an explicit user action with a progress bar. Keep the shutdown
call. 🟡 Measure it — compaction is proportional to the store, and doing it after every small
live-capture batch would be the same mistake as verifying on every open. If the number is bad,
compact on ingest *completion* only, never per chunk.

**The retained root.** `SnapshotRoots().Snapshot` binds the component roots and the predecessor
into the one value worth keeping. rohy surfaces it and makes exporting it a first-class action:

```jsonc
// rohy-data/roots/roots.json — the history, for chaining (one document, not one file per
// version: §15.5) — AND an export the analyst takes elsewhere
{
  "root_version": 1,
  "snapshot": "9f2c…",          // the value to retain externally
  "prev": "41ab…",              // chains to the image this replaced
  "node_root": "…", "edge_root": "…", "index_root": "…", "tombstone_root": "…",
  "body_version": 2,
  "store_version": 412,
  "taken_at": "2026-08-09T14:11:33Z",
  "app_version": "0.2.0",
  "events": 104233
}
```

🔒 **The UI must state what an internally-held root does not establish.** Every check the
engine can run compares the store against itself; only a root kept elsewhere distinguishes
"internally consistent" from "not tampered with". rohy keeps the history inside the case *for
chaining* — `VerifyChain(earlier, later)` needs it — and says, in the copy beside the export
button, that a root which never left the folder proves nothing against someone who can rewrite
the folder. This is the same class of statement as `RULES.md` §8: what a match establishes, and
what it does not.

**`graphene.Store` gains a forensics accessor.** 🔒 `backend/graphene` is the only package that
opens the DB, so the `Forensics()` call and everything reached through it live in a new
`graphene_forensics.go`, exposing rohy-shaped types. Policy and report assembly go in
`backend/custody` (§14.9) — the same split as `graphene.Inventory` ↔ `caseintegrity`.

🔒 **`Forensics()` returns false on the in-memory backend**, which is what nearly every rohy
test uses. Availability is a reported state, not an assumption: `Unavailable` is distinct from
`Clean`, for the reason `caseintegrity` already carries — a detector that could not run must
never look like one that found nothing.

### 14.3 Inclusion proofs (item 38) — keyless

`ProveNode(id)` → `ExportNodeProof(id)` → a `.gprf` the analyst hands over. What travels is the
leaf, the sibling hashes and the component roots — **never the image and never any other
event**. That is what makes handing a proof over safe where handing over the case would not be.

> **Correction (2026-11-04, §15.4).** "The leaf" is not the event. graphene v0.4.0 writes v3
> leaves, which commit to `SHA256(0x07 ‖ properties)` rather than to the properties, so a bare
> `.gprf` proves only that node N with *some* content was sealed. What rohy hands over is an
> **event proof**: the `.gprf` plus the event's exact stored record plus its raw record, whose
> hash the stored record carries. The verifier checks the whole chain, root to raw record.

- Export lives on the **event detail** view, beside the raw record: the question "can you prove
  this specific event was in the case" is asked about one event in front of you.
- ⚠️ An event written since the last compaction **has no proof yet**, and the UI says that
  rather than failing opaquely — with the compaction action offered, since that is the fix.
- **`rohyctl verify-proof --root <hex> <file>`.** `VerifyExportedProof` touches no store, needs
  no key, and opens nothing — the recipient of evidence needs a verifier that does not require
  the case or the application.

  ⚠️ **This widens the CLI's import list**, and that deserves saying out loud rather than
  slipping in. Phase 6 pinned 🔒 *"the import list is `rules`, `consts`, `version` and nothing
  else — no graphene, no sidecars, no state"*. A storeless, keyless proof verifier satisfies
  the **purpose** of that invariant (runs in CI, in a pre-commit hook, on a machine that has
  never seen the application) while breaching its letter, because it must import
  `graphene/disk` for `UnmarshalProof` / `VerifyExportedProof`. Restate the invariant as *"the
  CLI never opens a case"* and pin it with a test, or keep the letter and ship the verifier as
  a separate `cmd/rohyproof`. **Decide explicitly; do not let the import drift in.**

  ✅ **Decided 2026-11-04 (owner): restate and pin.** The letter was already broken:
  `consts` imports `graphene/store`, so rohyctl reached graphene before this item.
  `TestRohyctlNeverOpensACase` now enforces the purpose: no rohy package that opens a case,
  not graphene's root package, and no `Open*` call on a graphene package.

### 14.4 Custody report (item 39)

`CustodyFor(id, verifier)` — verifier may be nil, in which case signature-dependent layers are
reported as unestablished rather than failing.

🔒 **It returns gaps, not a verdict**, and rohy must render it that way. "Not verified" is
useless to someone holding evidence; "the segment chain is intact but nothing attested the
current snapshot" says what to do next. The report already distinguishes the three states that
matter and rohy must not flatten them:

| State | Means |
|---|---|
| `Live && !InSnapshot` | written since the last compaction — unaccounted for, **not** absent |
| `!Live && InSnapshot` | deleted since the last seal — the seal still holds it, the next one drops it (added §15.6) |
| `!Live && Redacted != nil` | a documented removal |
| `!Live && Redacted == nil` | the case holds no such event **and keeps no record of removing one** (corrected §15.6: without a removal ledger it cannot tell removed-and-sealed-over from never-existed) |

🔒 `CustodyFor` **can never be `Complete()`** — it always reports that every check compared the
store against itself. Supplying a retained root (`CustodyForAnchored`) or an anchor
(`CustodyForAnchor`) is what closes that, and the UI says which of the three was used. A
"custody accounted for" badge on the unanchored form would be the single most misleading thing
this feature could produce.

### 14.5 Attributed redaction (item 40) ✅

> **Corrected by the build (§15.7).** Property scope does not remove "the personal data": graphene
> strips the **whole record**, so a redacted event keeps only its id, label and edges. rohy does
> not offer `RedactNode`. The tombstone below carries the owning event's id, not a reason digest;
> the reason lives in the ledger. And the payload log is not the only other copy: siblings, orphan
> payload records, rule-made relations, the sidecars and the WAL all held the content.

`RedactNodeProperties` is the one that matters. An erasure request is almost always about
personal data, not about the existence of the artefact that carried it — so removing the whole
node destroys evidence never in scope, including every relation that made it meaningful. Four
scopes exist because they are four decisions; rohy offers the property scope first, and
`RedactNode` only with the cascade impact shown (`RedactionImpactFor`).

Every form requires a `Reason`. 🔒 rohy makes it a required field with no default: an
unexplained redaction is indistinguishable from evidence destruction, which is the whole
distinction the ledger exists to draw.

#### 14.5.1 ✅ The payload cold store defeats it — and must be fixed first

**This is the finding that reorders the phase.** rohy stores `RawXML` and `ParsedFields`
outside graphene, in `payload/payloads.log`, precisely because they are ~70% of an event's
resident cost. `RedactNodeProperties` removes the graphene property blob. It does **not** touch
the cold store. And per GAP-10, `payloads.log` never shrinks.

So a redaction performed today would leave the complete raw Windows event record — the thing
that actually carries the personal data — sitting on disk, while the UI reported the event
redacted. That is not a partial feature; it is a false statement about evidence.

**The fix, and it lands before any redaction UI exists.** The log is length-prefixed and events
reference records by `Ref` (offset + length), so records cannot move. Overwrite the record **in
place with a same-length tombstone**: the framing and every other `Ref` stay valid, the content
is gone, and the record itself says it was deliberately removed rather than reading as
corruption.

```
payload record: [len][payload]  →  [len][tombstone marker + reason digest + zero fill]
```

🔒 Same length, always. A shorter write reframes the log and silently repoints every subsequent
`Ref` at the wrong bytes — the exact class of failure `hash_normalized`-keyed snapshots exist
to prevent elsewhere.

🔒 **A redaction is one operation across both stores, and it reports partial failure.** If the
payload tombstone succeeds and the graphene redaction fails, or vice versa, the analyst is told
which half landed. Silently completing one is how "redacted" comes to mean two different
things.

⚠️ Needs `Options.Redaction` set at open, or `RedactNode` refuses rather than degrading to
`DeleteNode`. And note what stays true regardless: 🔒 `DeleteNode` is **unattributed** — it
records nothing about who or why and leaves no trace after a compaction. Everywhere rohy
currently deletes an event, that is what it is doing.

**Proving a removal** needs a `Compact()` first: until then the ledger knows and the image does
not, and a recipient given only the image cannot tell a redacted entity from one that never
existed. `CustodyReport.RemovalProvable` reports exactly that window, and rohy surfaces it
rather than leaving the analyst to infer it.

### 14.6 Signing and attribution (item 41)

This is the half that needs a key, and the key is the whole problem.

🔴 **A signing key stored inside `rohy-data/` is worthless for tamper-evidence.** Anyone who can
rewrite the store can re-sign it with the key sitting beside it. It would establish only "some
copy of rohy wrote this", while presenting as a cryptographic guarantee — a false statement
believed exactly when it matters.

🔒 **rohy refuses to write a signing key inside the case folder.** Not a warning; a refusal.
This is the one place where a default that "just works" would manufacture a guarantee that is
not there, and the project's own discipline (the `-`/`S-1-0-0`-as-absent reasoning, the
qualified all-clear, `rel_v == 0` saying so) points one way here.

Three custody options, to be decided with the number of hands rohy expects:

| Option | Establishes | Cost |
|---|---|---|
| Analyst supplies a key path outside the case + passphrase | Attribution to whoever holds that key | The analyst manages a key; a lost key ends attribution for future commits, not past ones |
| OS keystore (Windows DPAPI / Keychain) | Attribution bound to a user account on a machine | Platform-specific, and the build matrix is four targets |
| **No key — run unsigned and say so** | Nothing about authorship | Zero. **This is the default, and it is honest** |

🔒 **`RequireSignedCommits` is not optional if signing is on at all.** Without it, stripping a
signature turns a signed commit into an ordinary unsigned one and verification is simply
skipped — file-level access downgrades every commit and nothing complains. It is separate from
`Verifier` upstream only so a store predating signing still replays until a compaction retires
those commits, which is exactly rohy's migration path: existing cases keep opening.

**`Audit` and `Retention` are both off by default upstream, and both defaults lose things rohy
wants.** The zero-value retention discards every pre-compaction actor, timestamp, signature and
key rotation. Set them together with signing, and note the measured cost: audit + retention is
~+15 ms per compaction of durable writes — the real bill, not the hashing.

⚠️ Signing itself is **+8 allocations and ~0.5 KB per commit, flat** — per commit, not per
record. rohy already batches through `Begin()`/`Commit()`, so this disappears. The upstream
performance advice and the evidentiary advice agree here.

🔒 **Do not call `SetSyncOnCommit(false)`.** It trades the one durability boundary the engine
defines for throughput. Recorded here because it is exactly the kind of thing a future
ingest-speed optimisation reaches for.

### 14.7 Anchoring (item 42)

A `Checkpoint` binds every history's head — snapshot, attestation, WAL segments, audit log,
redaction ledger — into one digest. **The engine ships no transport by design**: what makes an
anchor an anchor is being beyond the reach of whoever can rewrite the store, and nothing
in-process can establish that.

rohy's shape follows from that: **export the digest, take a receipt back.** rohy computes the
checkpoint, hands the analyst the digest to publish however their process requires (a
timestamping authority, opposing counsel, an organisational log, a printed sheet in a safe),
and records the receipt they paste back. The `Anchor` interface is implemented over that
sidecar.

🔒 **rohy ships no anchor, and never `InsecureLocalAnchor`.** Upstream is explicit that it
exists only so the verification path can be tested and demonstrated, and that a file on the
same machine is not an anchor. It may appear in rohy's tests and nowhere else.

⚠️ The guarantee is bounded by publication frequency — everything since the last checkpoint is
freely rewritable. `AnchorAudit` reports that window; rohy shows it rather than a tick.

### 14.8 Forensic detectors in `caseintegrity` (item 43)

The existing eleven detectors answer "is this case broken". These answer "what can this case
prove", which is a different question with the same home — and the Maintenance page is where
they surface.

| # | Detector | Severity | Message shape |
|---|---|---|---|
| 12 | Image not verified | ⚠️ | verification is off, or was on and failed (the second is 🔴) |
| 13 | Never compacted | ⚠️ | "nothing ingested this session can be proven yet" — offers compaction |
| 14 | Unsigned commits present | ⚠️ | how many, and that they can never be attributed retrospectively |
| 15 | No root retained externally | ⚠️ | "every check so far compared the store against itself" — offers the export |
| 16 | Redaction ledger unverifiable | ⚠️ | records exist, no key to check the chain against |
| 17 | Checkpoint stale / absent | ⚠️ | the rewritable window since the last anchor |
| 18 | Forensics unavailable | — | in-memory backend. 🔒 reported, and explicitly **not** clean |

🔒 Every one of these is a **posture**, not a fault, and the copy must not read as an
accusation. "Unsigned" is the honest default state of an existing case, not a problem the
analyst created. §12.4's rule holds: an all-clear is a claim, and it gets qualified.

### 14.9 New package and dependency direction

```
backend/graphene/graphene_forensics.go   Forensics() accessor, roots, proofs, redaction,
                                          custody, checkpoints — rohy-shaped types      ⚠️ roots, VerifyImage, Unsealed, VerifyChain (#37), ProveEvent (#38), EventCustody (#39, graphene_custody.go) and redaction (#40) built; checkpoints ❌
backend/custody                          policy + report assembly over narrow interfaces ✅ fingerprint history + Chain (#37, §15.5); custody report + DescendsFrom (#39, §15.6)
backend/api/api_seal.go                  every seal through one path, recorded            ✅ (#37, §15.5)
backend/payload/payload_redact.go        same-length in-place tombstone (§14.5.1)         ✅ (#40, §15.7)
backend/graphene/graphene_redact.go      redaction primitives over the ledger             ✅ (#40, §15.7)
backend/redaction                        journal (= suppression list), Prepare, Run        ✅ (#40, §15.7)
backend/durable                          synced write + durable rename                    ✅ (#40, §15.7)
backend/api/api_redact.go                Preview/Redact/Pending/Finish on MaintenanceAPI  ✅ (#40, §15.7)
backend/api/api_caselock.go              the case lock: writes shared, redaction alone    ✅ (#40, §15.7)
backend/api/api_custody.go               binding; hangs off MaintenanceAPI               — not created: the #37 methods and EventCustody (#39) live in api_integrity.go
backend/eventproof                       proof document: assemble + verify, storeless    ✅ (#38, §15.4)
cmd/rohyctl verify-proof                 storeless, keyless — §14.3's question decided    ✅ (#38, §15.4)
```

```
backend/api ──► custody ──► graphene (forensics accessor), payload
                       └──► consts
```

🔒 `custody` never opens the DB and never holds a key; it composes what `graphene` exposes and
decides what to report. Same construction as `caseintegrity`, and testable against literals for
the same reason.

### 14.10 What this phase does not claim

Stated here so it is not only in `graphene/SECURITY.md`, and so the UI copy has one source:

- **It prevents nothing.** rohy is a process holding its own key; code running there can call
  any API. What the machinery does is make the result detectable to someone outside.
- **It does not make contents true.** A signature proves who asserted something, never whether
  it is so. This is the same distinction `RULES.md` §8 draws for correlation, one layer down.
- **Nothing is anchored until an anchor exists**, and until then every guarantee is the store
  vouching for itself.
- **No encryption**, at rest or otherwise. Property blobs are stored as supplied. Volume
  encryption is the answer and is out of rohy's scope.
- **No access control.** Roles are recorded and never checked; `CheckCapability` is advisory and
  the engine never calls it. rohy must not present a role as a permission.
- **CRC32 is not tamper detection.** It catches a torn write after a crash and provides zero
  resistance to a deliberate edit. Nothing in rohy's UI may describe a checksum as integrity.
- **The audit log records operator actions, not reads.** Queries are not recorded, deliberately;
  a synchronous append on the query path is not something to add.

---

## 15. v0.3.0 — multi-source groundwork and the v0.2.0 fixes (2026-11-03)

Not part of the v0.2.0 plan above; recorded here so the decisions have one place until v0.3.0
gets its own plan. Owner decisions are marked **(owner)**.

### 15.1 What landed

- **Network vocabulary.** `dst_ip`, `dst_port`, `protocol` appended (13 slots,
  `CorrelationKeyVersion` 3, worst case 381 B under the unchanged 400 B ceiling). Address class
  via `net/netip`: RFC 5952 spelling, v4-mapped unmapped, zone dropped, brackets stripped only as
  a pair, unspecified address absent, non-addresses dropped not truncated. Port class: 1–65535.
  Protocol class: one byte, `0` absent, unlisted numbers re-formatted decimal, keyword synonyms
  (`icmpv6` → `ipv6-icmp`).
- **`src_ip` supported everywhere (owner).** One slot, two names. The alias is served in the
  schema (`Field.Aliases`), lands in the generated JSON Schema enum, is accepted by the JS
  validator and lit by the guided editor's chips (`lib/rules/choices.js`), and duplicates are
  judged by slot in both validators. Basis strings use the stored name.
- **WFP oriented by initiator (owner).** Inbound (`%%14592` / "Inbound") WFP records swap
  Source*/Dest* before projection, so `ip_address` always means the initiator. Sysmon 3 is read as
  written — **not yet verified** against an inbound sample.
- **Loopback kept as a key (owner).** Evidence of tunnelled RDP; the global-scope cost is
  documented in RULES.md §9 instead.
- **Identity recipes.** `Event.StampIdentity` dispatches through `consts.identityRecipes`, one
  explicit entry per source type (all Windows today);
  `TestEverySourceTypeHasADeliberateIdentityRecipe` reads every `SourceType*` constant from
  source and fails on one without an entry. Applies to dated events only (documented).
- **Panic containment.** Per chunk (`parseAndNormalize`), per record on the `.db` and live
  goroutines (`guardRecord`), and at the top of `runIngestion` (`reportIngestPanic`, which
  survives a faulting emitter). `evtx.Ingest` cancels a derived context on every return, so a
  run that ends early can no longer leave workers blocked holding the evidence file open —
  `TestIngestFaultIsReportedSealedAndReleasesTheFile` fails without it.
- **Silent chunk loss now reported.** velocidex/evtx stops at a damaged record and returns what
  it had with a nil error; `parseChunkAt` compares the result with the header's declared record
  range and the shortfall is reported and counted as skipped.

### 15.2 v0.2.0 defects fixed here (owner: fold into v0.3.0, no v0.2.1)

| # | Defect | Fix |
|---|---|---|
| D1 | Temporal chain rebuilt from per-step parent pointers: `4625 ×3 → 4624` drew `F2 → F2 → F3 → S`; `window_total` measured from the wrong anchor (§6.3 pseudocode had the same flaw) | chain kept per completion; 3 regression tests + the built-in asserts its chain |
| D2 | `sequence` honoured `match_scope` and its `AppliesTo` included `sequence`, contradicting the reversal | `Spec.ScopeOrDefault` returns the default for algorithms that do not read it; `AppliesTo` derived from the descriptors and cross-checked by test; inert-field warning in Go and JS |
| D3 | Unreadable roots reported as "never compacted" | `ForensicStatus.RootsError`, `roots_unreadable` finding (error, no compact action), its own Maintenance copy |
| D4 | `Dataset.Groups` wrote an on-demand partition into the cached, shared dataset | computed without storing; race test |

D2 narrows any user rule written as `sequence` + `match_scope: "global"`; RELEASE_NOTES says so.

### 15.3 Still open for v0.3.0

- No non-Windows reader exists; RULES.md §9's Suricata example is marked forthcoming.
- Sysmon 3 inbound orientation unverified.
- `TestBoundedMemoryStreaming` is noisy on the development machine (small-run peak 14–60 MiB
  across runs, the same on a clean `HEAD`): it fails intermittently without any regression.
- Backlog from the audit: testbench result race (`ruleEditor.js`), `openRuleAtStep` ignores the
  step, `release.yml` skips vet and cmd/tools tests and does not re-delete dist between arches,
  off-postures not shown on the Maintenance page, "Sealed" copy overclaims after later writes,
  `consts.IndexedNodeKeys` dead with no index-key golden, `complete.test.js` uses a hand-written
  schema, explainer examples still say `format_version: 2`, missing benchmarks (Backfill,
  ComputeLayout, RelationHeatmap).

### 15.4 #38 event proofs (2026-11-04)

**What a bare `.gprf` proves, measured against what was assumed.** §14.3 said the leaf travels,
and read that as the event travelling. It does not: graphene v0.4.0's v3 node leaf is
`0x05 ‖ id ‖ labels ‖ SHA256(0x07 ‖ properties)`, so a verified `.gprf` says "node N with some
content was in snapshot R". rohy already had the rest of the chain — a node's properties are
`json.Marshal(Event)`, which carries `hash_raw = SHA256(RawXML)` — so the export carries it:

```
root you kept ─ graphene proof ─ leaf ─ SHA256(0x07 ‖ stored record) ─ hash_raw ─ raw record
```

**Decisions (owner):**

| Question | Decision |
|---|---|
| Where the verifier lives | `rohyctl verify-proof`; the CLI invariant restated as "never opens a case" and pinned by `TestRohyctlNeverOpensACase` (deps + an AST check for graphene `Open*` calls) |
| What the export writes | one JSON document, `event-<id>-proof.json`: the `.gprf`, the exact stored record and the raw record (base64, byte-exact), with establishes / does-not-establish lists |
| graphene's leaf layout | mirrored in `backend/eventproof/leaf.go`, pinned by `TestLeafLayoutMatchesGraphene` against a real sealed store; an exported upstream helper is a follow-up |

**As built:**

- `backend/eventproof` — `Assemble` (refuses to hand out a document that would not verify, and
  verifies its own bytes before returning them) and `Verify(root, data)`. Storeless; imports only
  graphene's codec/verifier, `consts`, `utils`, `version`.
- 🔒 The root is always the caller's. `built_against` names the seal for pairing and is never
  used to verify.
- 🔒 The parser is stricter than `encoding/json`: exact key names, each once, nothing after the
  document. A repeated or case-variant key would let a person reading the file and the verifier
  see different raw records.
- 🔒 The verifier prints its own establishes / does-not lists; the file's copies are not covered
  by the proof.
- `graphene.Store.ProveEvent` returns states, not errors: `unavailable`, `not_sealed`,
  `written_since_seal`, `changed_since_seal` (a dedup count or backfill moved the stored record),
  `image_outdated`, `raw_mismatch` (damage — reported, not exported).
- `MaintenanceAPI.ExportEventProof` adds `fingerprint_exported`: whether a copy of the seal the
  proof was built against ever left the app. A later seal moves the root, so an earlier copy does
  not check a later proof.
- Tests: every event of the Security fixture proves through the real ingest → seal path and
  verifies against the exported fingerprint; tamper, wrong-root, forged-case, strict-parse and
  leaf-shape cases; `FuzzVerify` asserts that anything verifying carries exactly the sealed
  record and raw record.

**Upstream finding.** graphene v0.4.0 binds snapshot body versions 2 and 3 identically and checks
only that a v3 leaf claims "≥ 3", so a proof's version byte can be rewritten to any value 3–255
and still verify. No content claim changes, but graphene's own FORENSICS.md says one altered byte
"either fails to parse or fails to verify". rohy pins the version to exactly 3; upstream should
reject unknown body versions. Found by `TestAnyByteOfTheProofMatters`.

**Found while measuring, fixed with a test first:**

- **Raw record without `hash_raw`.** An event that has a raw record but an empty `hash_raw` made
  `ProveEvent` fail outright; `seedDiskStore`'s events are like this. The seal never bound such a
  raw record, so it is now left out of the file. The proof covers the stored fields and says so.
- 🔒 **Terminal injection in `verify-proof`.** The subject lines print values from the logs, and
  log content is attacker-influenced. C0/C1 controls and Unicode format characters (escape
  sequences, `\r`, bidi overrides) are now printed as escapes, so a record cannot repaint the
  recipient's screen or forge a `VERIFIED` line.

**From an independent review (all 🟡, all fixed, tests first where behavioural):**

- **Too large.** A raw record over ~12 MiB made `Assemble` fail its own size-capped self-check,
  and that surfaced as a generic error. It is now the `too_large` state, so the verifier's "no
  event proof is larger" holds because nothing larger is ever built.
- 🔒 **The disclosure was overstated.** "Nothing about any other event" is false for a Merkle
  proof. It reveals the record count, the event's position, and sibling hashes, and someone
  holding another event's exact record can use those hashes to confirm that event is in the
  case. The drawer copy, the release notes and `EventProofNotOthers` now say what is revealed.
- The drawer showed a stale maintenance error when sealing was refused because another task
  was running. It now checks busy first.
- `maintenance.compact()` returned failure when the seal succeeded but the report refresh after
  it failed. A caller would seal again and move the fingerprint twice. A successful seal now
  returns true, and a refresh failure is reported separately.

**Measured** (PERFORMANCE.md §18): `ProveEvent` takes 11.7 ms at 10k events and 94 ms at 100k.
It is linear, and the cost is graphene's leaf recomputation. It runs on a click, so nothing is
optimised.

**Still open:** shipping `rohyctl` to recipients is the deferred release item (#35c); an upstream
`disk` helper that binds a leaf to a property blob would retire the mirrored layout.

### 15.5 #37 finished — every seal recorded, the chain checked (2026-11-04)

**What was missing** (the 2026-11-03 audit): `custody.Observe` ran only on export, so the
fingerprint history held the fingerprints somebody happened to export rather than the case's
sequence of seals; nothing checked the chain; the UI ignored `case:sealing` / `case:sealed`; the
four MaintenanceAPI forensic methods had no Go tests.

**Found on the way, and it changed the work:**

- 🔒 **Every compaction makes a new fingerprint, even over identical content.** graphene's
  `Compact` always chains the new image to the old one (`PrevRoot`), and that link is bound into
  the root. rohy compacted on every close, and twice after an ingest (the ingest seal, then the
  close), so an exported fingerprint went stale at the next close and every new proof needed a
  fresh export.
- 🔒 **The history was destroyed when it could not be read.** A corrupt or newer-format
  `roots.json` opened as empty and the next seal wrote over it, losing the record of which
  fingerprints were exported — the one thing in it nothing else can reconstruct. A test pinned
  that behaviour as correct.
- **A compacted WAL is not zero bytes.** Truncation rewrites graphene's 50-byte container header,
  so "the log is empty" has to be asked as "the log holds no record" (`disk.InspectWAL`), not by
  size. Found by the first `Unsealed` test failing.

**Decisions (owner):**

| Question | Decision |
|---|---|
| Automatic seals with nothing new | **Skipped.** After an ingest and on close, rohy compacts only when `Unsealed()` says something was written (a WAL record, a delta or delete, or no image). **Seal case** always compacts: it is the remedy for an image in an older format. |
| History storage | **One `roots/roots.json`**, not the per-version files §14.2 sketched. An unusable document is moved aside (`roots.json.unreadable-<UTC>`), kept, and reported; if it cannot be moved, the history refuses to write rather than replace it. |

**As built:**

- `graphene.Store.Unsealed()` — errs towards sealing: any WAL record, delta record, delete or
  torn tail counts; any error other than unavailable comes back as "seal".
- `api/api_seal.go` — one `sealAndRecord(store, hist, force)` for all three seal points (ingest,
  **Seal case**, close), so every seal is recorded through one record builder. `case:sealed` now
  carries `{state, snapshot}`: `sealed`, `unchanged`, `unavailable`, `failed`. A fingerprint that
  could not be *recorded* after a good compaction is worded as a sealed case with a gap, never as
  a failed seal.
- `AttachSealHistory` is a package function, not a method: Wails binds every exported method of a
  bound struct, and wiring is not something the UI should be able to call. (`WithIntegrity` and
  `WithCascades` are bound today — a pre-existing leak, noted, not changed here.)
- `graphene.VerifyChain` (hex over `disk.VerifyChain`) and `custody.Chain(current)`, which places
  the current image: `intact`, `rolled_back`, `unrecorded_seal`, `diverged`, `empty`,
  `no_image`, plus gap and fork counts. Read-only: asking never records.
- caseintegrity: `roots_rolled_back` (error, review: the image is an earlier record, or the
  history holds a fork), `roots_diverged` (warn), `roots_chain_gap` (warn; names
  the pre-v0.3.0 export-only histories as the common cause), `roots_unrecorded_seal` (info),
  `roots_history_unreadable` (warn, with the kept path). An intact chain adds no finding.
- 🔒 What the chain is worth is stated, not implied: it is the case checked against its own
  record. It catches an image that is not the latest one rohy sealed — a restored backup, an image
  copied from another case — which every other check passes because nothing about it is broken.
  It does not catch a forger, who can rewrite the history as easily as the image.
- UI: the ingestion bar stays up through the seal ("Sealing case…", indeterminate); the
  Maintenance store re-reads the posture on `case:sealed` and drops an image verdict about the
  image the seal replaced.
- Tests: `Unsealed` across never-compacted / compacted / reopened / the startup migration /
  update / delete / insert; `VerifyChain` and every chain state against real compactions; the
  history kept-not-overwritten cases; every seal point recorded and chained; a **restored backup**
  staged end to end — it verifies as a perfect image and is reported as rolled back, and closing
  the case does not bury the finding; the four forensic methods, including the busy refusal.

**From an independent review (fixed, tests first):**

- 🔴 **A rollback faded once it was sealed again.** Restore a backup, ingest anything, seal: the
  history ended at the new image, so "is the image an earlier record" no longer fired and only a
  gap warning was left. The history now proves it: a record that names an *earlier* record, not
  the previous one, as its predecessor is a **fork**, reported as `roots_rolled_back` (error)
  however many seals follow.
- 🔴 **A foreign image was adopted by closing the case.** The unchanged seal on close recorded
  whatever image it found, so a replaced database became the newest seal and `roots_diverged`
  disappeared with no seal having run. An unchanged seal now records only where the history
  agrees (empty, at its head, or one unrecorded seal past it). A seal that actually *ran* is
  always recorded; that is what makes the fork provable.
- 🟡 **Seals could interleave.** The maintenance lock does not exclude an ingest, so a **Seal
  case** click during the post-ingest seal could leave one image unrecorded. One process-wide
  `sealMu` now covers every seal from check to record.
- 🟡 **A read error moved a valid history aside.** A transient failure (another process holding
  the file) is not corruption. Only a document that was read and is unusable is moved; one that
  could not be read is held in place and nothing is written this session.
- 🟡 **The reset notice lasted one session**, and was dropped on a case never sealed or with an
  unreadable fingerprint. The reset is now recorded *inside* the new history (`kept`), and the
  history findings are reported before the posture's early returns.
- 🟡 `Unsealed` returned false on a store that failed to open; it now errs towards sealing as
  documented. The rename-failure branch is now exercised for real (an open handle on Windows, a
  read-only directory on Unix).

**Measured** (PERFORMANCE.md §18): `Unsealed()` costs ~0.4 ms at 10k events and ~1.1 ms at 100k,
1.6 KB and 15 allocations flat — the price a close of an unchanged case now pays in place of a
~1.3 s / 566 MB compaction.

**Status correction.** This plan's own estimate said P8 would reach 3/7; it is **2½/7**, because
#43 is still ½ (detectors 16 and 17 need the redaction ledger and anchoring).

**Still open:** nothing in #37. The chain is internal; carrying a chain *out* of the case, so a
recipient holding an older exported fingerprint can follow it to a newer one, is not built and
belongs with #42 anchoring.

### 15.6 #39 custody report (2026-11-04)

**What `CustodyFor` alone misses, measured against what §14.4 assumed.** graphene's report is
synthesis over the store's own histories, and for rohy most of those are empty: no audit log,
retention, roles or signer (`forensicOptions`), so attestation, segments, audit and roles are "not
recorded" for every event. What makes an *event* accountable sits outside the engine, so the report
composes it:

- the stored record against the sealed one (`ProveEvent`'s `changed_since_seal`, which
  `CustodyFor` never compares);
- the raw record in the payload cold store (`hash_raw`, `raw_mismatch`, and the new
  `raw_unreadable`);
- the fingerprint history from #37 (`custody.Chain`);
- the fingerprint the analyst kept.

**Found on the way, and it changed the work** (an independent review of the design, every claim
checked against the code):

- 🔒 **graphene's anchored form skips the comparison when the entity is absent**
  (`CustodyForAnchored`, `custody.go:458`). Keep S3, which holds event E; roll the image back to
  S2. E is simply missing, and the one value able to show the rollback is never compared. rohy
  compares the kept fingerprint against the case's **current image** itself, whatever the event's
  state, and does not call the anchored form. Worth raising upstream.
- 🔒 **"Is it in the history" is the wrong test for an older fingerprint.** Keep S2, roll back to S1:
  S2 is in the history and the image does not descend from it. `custody.DescendsFrom` walks back
  from the current image through recorded, verifying links instead.
- **A fourth presence state.** `DeleteEvent` is reachable from the graph UI, and a deleted event is
  `!Live && InSnapshot`: the last seal holds it, the next one drops it. §14.4's table is corrected.
- **"Never heard of it" overclaimed.** With no removal ledger and no audit log, the case cannot tell
  an event removed and sealed over from one that never existed; the copy now says exactly that.
- **`ProveEvent` checked no label** (`eventFromNode` decodes any JSON), and an unreadable payload
  failed the whole call. Both fixed, tests first; the second is the `raw_unreadable` state, which
  the drawer's proof section shows as damage with no seal button. `payload.ErrClosed` keeps a case
  shutting down apart from damage.

**Decisions (owner):**

| Question | Decision |
|---|---|
| A kept fingerprint that is not the current seal | **Split by history.** If the image descends from it through recorded seals: *not established*, an earlier seal, and the link is the case's own word. Otherwise *broken*: another case, a rollback, a replaced image, or a reset history. Never reads as checked. |
| Export the report? | **Drawer only.** The event proof stays the one thing handed over; a saved report is the case speaking for itself. |

**As built:**

- `graphene.Store.EventCustody(id)` — `CustodyFor(id, nil)` in rohy's shape, every field the report
  decides on structured (counts, attestation, redaction scope), plus `ProveEvent`'s state and
  `RawBound`. A delete landing between the two reads reports an absence. A pin test fails if the
  engine's gap layers for rohy's posture change.
- `custody.EventReport` — pure, tested against literals. Presence is decided absence-first, so an
  id the case never held is never described by the state of the seal. Event layers: seal, record,
  raw, removal (when one is recorded), history (caseintegrity's weights: a rollback or fork is
  broken and never offered a seal), kept fingerprint. The engine's store-wide layers are grouped as
  case records, read from its structured fields; its text appears only as a cause. Unknown layers
  and states fail closed.
- 🔒 Invariants, each a test: a case-only report never establishes the kept-fingerprint layer; a
  rolled-back or forked history is broken in every combination; every report lists what it does
  not establish (who, truth, parsed fields, the source file, the history's own word); no frontend
  copy says "accounted for".
- `MaintenanceAPI.EventCustody(id, kept)` — the kept value normalized (trimmed, lowercase, 64 hex,
  not all-zero; `FuzzNormalizeFingerprint`), assembled under `sealMu` so no seal lands inside one.
- Drawer: **Account for this event**, and a **Fingerprint you kept** field that is never prefilled.
  The report is dropped on another event, on any seal (`case:sealed`, the maintenance store's
  `seals` counter), and a late response is discarded by generation.

**From an independent review of the implementation (all fixed, tests first):**

- 🔴 **A damaged image failed the report for every live event.** The record in a damaged image
  may not even decode, and `ProveEvent`'s self-check refuses what does, so the broken seal line —
  built for exactly this — could not appear. A sweep that flips each byte of a sealed image found
  more failing offsets than the review did. When the engine reports a fatal snapshot gap the proof
  is no longer asked for, and the new `image_damaged` state covers the self-check for the proof
  export too (damage, no seal button).
- 🔴 **The drawer dropped the report on every store update.** Both stores replace their whole
  state on each progress tick, so no report could be had during an ingest or a backfill. It now
  drops on `caseMark`: a seal, an ingest starting or ending, a maintenance pass. A **Seal case**
  that compacted but failed to record now counts as a seal too.
- 🟡 **A held history softened a mismatched kept fingerprint** to not established — against the
  decision above, and a downgrade path for anyone able to make `roots.json` unreadable. It is
  broken; only descent is unknown. *(This reverses a default in the approved #39 plan.)*
- 🟡 Kept-fingerprint copy claimed a rollback where the history only had gaps; a seal was still
  suggested beside a broken history (remedies are now a separate field, cleared when the history
  is broken); an in-memory case with a pasted value said the value was checked (form `none`); a
  redacted event still in the seal was told no seal held it; a hash with no raw record read as no
  hash (`RawHashed`); an I/O error that may pass was called damage (only `OutOfRangeError` and
  decode errors are); the race test accepted states it could not reach, and the
  delete-between-reads branch now has a deterministic test.

**Measured** (PERFORMANCE.md §18): `EventCustody` is ~20 ms at 10k events and ~190 ms at 100k —
two proofs, so twice `ProveEvent`, linear. It runs on a click.

**Still open:** the third form, a published anchor (`CustodyForAnchor`), arrives with #42; the
store-wide layers stay "not recorded" until #41 turns on audit, retention, roles and signing.

### 15.7 #40 attributed redaction (2026-11-05)

**What §14.5 assumed, measured against the engine.** Property scope was meant to remove the
personal data and keep the rest. graphene's `RedactNodeProperties` strips the **whole record**
(`disk/redact.go:740`): an event keeps its id, label and edges, and nothing else. Writing the safe
fields back afterwards would be an unrecorded edit, and it would break the ledger's
`SurvivingHash`. So in rohy, redacting an event means all of its content goes; the node, the
fact of the removal, and the relations an analyst drew stay. `RedactNode` is not offered.

**Where the content was** — the list the build had to empty, each now a test:

| Copy | How it goes |
|---|---|
| Node record and its index entries | `RedactNodeProperties`, then `PurgeNodeIndex` on a retry: strip and purge are two WAL records, and a crash between them leaves the index holding the values |
| Payload log | Same-length tombstone in place (§14.5.1), the owning event's id in the marker |
| Orphan payload copies (a failed batch, a resumed ingest, `DeleteEvent`) | Found by walking the log: the same bytes, or a raw record hashing to `hash_raw` |
| Sibling events (the same record ingested twice, or from another path) | Redacted too, one ledger record each |
| Rule-made relations, whose basis quotes the event | `RedactEdge`, one ledger record each |
| Findings and snapshot descriptors; snapshot basis copies; stale `.tmp` files | Scrubbed, durably |
| Sealed image and WAL | The redaction's own seal |
| Analyst notes, tags, annotations, analyst-drawn relations | Listed in the preview, never touched |
| Exported proofs; backups, shadow copies, freed blocks, the page file | Out of reach, and the dialog says so every time |

**Found on the way, and it changed the work** (an independent review of the design, 13 defects,
all folded in before code):

- 🔒 **A re-ingest brought it straight back.** Dedup looks events up through the
  `hash_normalized` index, which a stripped event has left; and `hash_normalized` carries the
  source, so the same record from a new path is a new identity. The journal is the suppression
  list, checked on the stamped identity and on `hash_raw`, whether or not the run deduplicates.
- 🔒 **Read-then-write paths would un-redact it.** `IncrementDedupCounts`, the backfill and a
  build read a record and write it later; a strip landing between the two is undone, and no check
  on the record can see it. Guards skip a stripped event, `toNode` refuses one, and the case lock
  keeps every writer out while a redaction runs.
- 🔒 **Crash order.** The payload reference lives only in the record, and the strip destroys it.
  The journal entry is written durably — synced file, write-through rename — before any
  destructive step.
- **Images below body version 3** hash leaves differently, so such a case is sealed first.
- **A torn tail in the ledger** misframes the next record, and every compaction after it fails.
  rohy checks the framing against the file size and refuses to append.
- **Proof, custody and integrity read a stripped event as damage or work.** New states:
  `ProveEvent` → `redacted` / `raw_redacted` / `record_empty`; custody presence
  `redacted_content`, with the seal as the remedy until the removal is bound in; the inventory and
  backfill status count redacted events apart, and caseintegrity no longer calls a tombstone
  "tail", a finding on a redacted hash "orphaned", or a stripped event "unprojected".
- 🔴 **A pre-existing bug: the drawer never received the raw record or the parsed fields.**
  `GetEvent` returned `*graphene.Event`, whose cold fields are `json:"-"` because that JSON is
  also the stored record. It has been true since v0.1.0. `api.EventDetail` carries them.
- **A crafted payload could steer the tombstone.** Evidence content can contain a byte pattern
  that looks like a record header; checking only the four bytes before an offset would accept it.
  The tombstone confirms the offset by walking the framing from the start of the log.

**Decisions (owner, 2026-11-04):**

| Question | Decision |
|---|---|
| Rule-made relations touching the event | **Removed**, each by its own recorded `RedactEdge`. Analyst-drawn relations stay. |
| Re-ingest | **Suppression list** in the case: ingest skips matches and counts them (`records_suppressed`). |
| Sidecars | **Scrub the copies rohy made.** The analyst's notes and annotations are listed, never deleted. |

**As built:**

- `payload.Store.Redact` / `Walk` (`FuzzRedact`); `graphene` primitives in `graphene_redact.go`;
  the ledger on for every disk case (an existing case opens to the same roots).
- `backend/redaction`: the journal (fail closed — a journal that cannot be read refuses ingest and
  redaction and is never replaced; `FuzzDecode`), `Prepare` and `Run`. Five steps — relations,
  records, payloads, sidecars, seal — each recorded as it ends, each safe to run again. Finishing
  is running it again. A fault injected at each step stops there, says so, and is finished.
- `api.theCase`: every binding that writes the case or a copy of its content shares it; a
  redaction holds it alone. Both sides only try, and a refusal names what is running.
- `MaintenanceAPI`: `PreviewRedaction`, `RedactEvent`, `PendingRedactions`, `FinishRedaction`;
  `case:redacted` carries the outcome.
- UI: **Redact this event…** in the drawer — what goes, what stays, what rohy cannot reach, a
  required reason with the permanence warning, the outcome step by step. Canvas cards read
  *Redacted*. Unfinished redactions are listed on the Maintenance page with **Finish the
  redactions**, which the integrity check also offers.
- 🔒 The definition of done is checked on disk: the end-to-end test searches every file under the
  case folder for the raw record and the copied values — found before, absent after — and finds
  the analyst's note exactly once. The custody report then reads `redacted_content`, its seal layer
  established.

**Upstream notes** (graphene, worth raising):

- A property redaction that keeps a residual blob — the fields a recipient needs to place the
  entity (a timestamp, a type) — would let rohy keep a redacted event on the timeline without an
  unrecorded write.
- The ledger's append does not repair or refuse a torn tail; rohy checks the framing itself.

**Measured** (PERFORMANCE.md §18): the preview is ~50 ms at 10k events and ~600 ms at
100k, a redaction ~205 ms and ~2.1 s, seal included — linear, and at 100k mostly the seal. Both run
on a click with a working state.

**Still open:** who redacted is not recorded (`ActorID` is 0) until #41 adds an analyst identity;
the suppression hashes match only a byte-identical raw record, so a copy whose raw form differs —
if, say, the live channel renders a record differently from an exported file — is not recognised.
