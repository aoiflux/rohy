<div align="center">

<img src="frontend/src/assets/logo.svg" width="72" alt="rohy logo" />

# Release notes

</div>

---

## v0.2.0 — "Andromeda"

_The release that learned to say what a match actually proves._

rohy v0.1.0 could tell you that a set of Windows event IDs occurred **in a given order on one
computer**. That is a real observation, and for a great many investigations it is also not enough.
It cannot tell you whether the failed logons and the successful one concern the same account,
whether a process was started by the session you are looking at, or whether two events are related
at all beyond having happened on the same host in the same week.

v0.2.0 closes that gap — and, just as importantly, is precise about where the gap remains. Every
correlation rule now declares **how** it matches, and the application states **what each kind of
match establishes** rather than presenting all of them as one undifferentiated "correlation".

> **Named for Andromeda** — daughter of Cassiopeia, which named v0.1.0. The kinship is the point:
> this release is about establishing genuine relationships between events rather than mere
> adjacency. Releases are named after constellations, continuing the line.

---

### The headline: four algorithms, four different claims

| Algorithm | Correlates on | A match establishes |
| --- | --- | --- |
| `sequence` | order alone | these IDs occurred in this order on one computer — **and nothing more** |
| `field` | order **+ shared field values** | …and the steps concern the same logon session, account, or process |
| `temporal` | order **+ bounded gaps** | …and the chain reads as one episode rather than a coincidence |
| `lineage` | process ancestry | one process created another, resolved through the PID's **lifetime interval** |

`sequence` is what v0.1.0 did, unchanged and still the default. The other three are new.

**Why lineage is not a PID join.** Windows reuses process IDs constantly. Matching a parent PID
directly produces confident, wrong ancestry over a case spanning days. rohy resolves each parent
through the interval during which that PID was actually alive, and reports a process whose parent
predates the log as **unresolved** rather than attaching it to whichever process last held the
number.

**Why temporal is not a greedy scan.** A greedy matcher anchored on the earliest candidate misses
matches a later anchor would have found. The temporal matcher is a single-pass sweep anchored on
the *latest* viable step, which finds them.

---

### Correlation fields

Field, temporal and lineage matching need to compare values across events — and the bulky parsed
fields live in a cold store precisely so they are not resident for every event in a case. So a
**fixed, bounded projection** of ten named fields now travels in each event record:

`logon_id` · `subject_logon_id` · `process_id` · `new_process_id` · `parent_process_id` ·
`process_name` · `target_user` · `subject_user` · `ip_address` · `service_name`

Two properties this depends on:

- **Absent is not empty.** A field that was not recorded projects to *nothing*, so it can never
  match another absent field. `-`, `S-1-0-0`, `null`, `N/A` and a zero identifier all mean absent.
  Without this, a field matcher becomes a false-positive engine that correlates every event
  missing the same field.
- **Bounded by construction.** Each slot has its own length limit, sized to the widest legitimate
  value of its kind. Measured cost: **71 bytes per event** on a typical process-creation record,
  324 bytes at the format's worst case, against an asserted ceiling of 400.

> ### ⚠️ Upgrading an existing case
>
> Events ingested by v0.1.0 carry no projection, so **field, temporal and lineage rules will match
> nothing against them**. This is the one thing to do after upgrading:
>
> **Maintenance → Fill in correlation fields.**
>
> The backfill is opt-in, resumable, and safe to cancel — what it completes is durable and a later
> run continues from there. rohy tells you when a case needs it rather than reporting a convincing
> zero. Sequence rules are unaffected and need no backfill.

---

### Every edge now says where it came from

v0.1.0 could distinguish an edge a rule inferred from one a human drew — a colour on the canvas.
Every generated relation now also carries **which rule**, **which algorithm**, **which occurrence**,
**which step**, and the **basis**: the values that made the match, in words.

The new **relationship inspector** turns that into something checkable. Select an edge and it shows
both endpoints, the rule that drew it, "matched because: `logon_id = 0x3e7`", "step 2 of 4", and
offers to light up the other edges of the same occurrence. From there you can open the rule at the
step that produced the edge.

Edges built before this release say so explicitly — *"built before rohy recorded why; re-run its
rule to fill that in"* — rather than showing blank fields that read as a rule having recorded
nothing.

---

### Authoring rules

- **Rule testbench.** Dry-run a rule against the current case from inside the editor: how many
  matches, sampled examples, and — the part that matters — **what it could not consider**. Events
  skipped for missing correlation keys, stale projections and unresolved parents are first-class
  results, because "this pattern is rare" and "none of your events could be examined" are both zero
  and must not look alike. It writes nothing.
- **Algorithm selector** in the guided form, generated from the backend's own descriptor, so the
  editor can never offer a matcher the engine does not implement.
- **Field-level diff** against the saved file, with sequence steps aligned rather than compared
  line by line.
- **Byte-exact bundle export.** Hand a colleague your rules as a single document. Each rule keeps
  its file's exact bytes — field order and any field this build does not interpret both survive —
  and anything that could not be read is reported rather than quietly omitted.
- **Algorithms explainer page.** Each matcher animated step by step over a worked example, showing
  what it keeps and what it discards.

### The built-in library: 30 → 35

Five new rules demonstrate the new matchers, and every existing rule now declares which Windows log
channels it needs.

| Rule | Algorithm | Also requires |
| --- | --- | --- |
| Logon Then Process Creation, Same Session | `field` | the same `logon_id` |
| Password Reset Then Logon, Same Account | `field` | the same `target_user` |
| Failed Logon Burst Then Success Within Five Minutes | `temporal` | each gap ≤ 5m |
| Service Installed Then Security Log Cleared Within An Hour | `temporal` | the gap ≤ 1h |
| Process Ancestry | `lineage` | process-creation auditing |

---

### Graph

- **Four auto-layout profiles** — sequence (columns by position in the chain), lineage (a tidy
  process tree), resource (one column per session, account or host), and time (a scatter over real
  time). Computed deterministically in the backend, so the same graph produces the same picture
  every time.
  **Nothing is saved until you keep it**: a profile is previewed, and *Put back* restores exactly
  where things were. Node positions are the only thing on the canvas placed by hand.
- **Clusters** — outline connected components, a rule's own edges, or everything sharing a
  correlation field. Fold a group into one card that always shows how many events it holds; links
  crossing the boundary are re-pointed at the card rather than disappearing.
- **Relationship heatmap** over the timeline — when the things rohy inferred actually happened,
  grouped by rule, kind of link, computer, step, or inferred-vs-asserted. Placed at each relation's
  **later endpoint**, never at build time.
- **Replay** — watch the graph assemble in the order the evidence says it happened, with a playhead
  shared with the timeline. Ordered by event timestamps, never by when the rule ran.
- **Snapshots** — record what a graph looked like and put it back later. Endpoints are stored by
  content hash, so a restore survives a re-ingest that reassigned every node id. **Nothing restores
  silently**: you see exactly what will be re-applied, what is offered for re-creation, and what can
  no longer be resolved. A link you choose to re-create is recorded as *your* assertion, never as
  something a rule found.
- **Annotation layers** — notes pinned to events, regions, and arrows, organised into layers you can
  show, hide, reorder and colour. Notes follow their event by content hash. A mark that cannot be
  honestly placed is not drawn, and is counted.

---

### Case maintenance and integrity

A new **Maintenance** view (`Alt+7`) runs a case check with eleven detectors and reports what it
finds, grouped by severity, each with the one button that fixes it.

The check that makes the feature worth having tells three things apart that all look like zero:

| Verdict | Meaning |
| --- | --- |
| **Inert** | a step has no matching events — the rule cannot fire at all |
| **Blocked** | every step has events, but nothing carries a field the rule matches on |
| **Unmatched** | everything is present; the pattern simply does not occur |

Only the third is a clean result. It also reports rules that need a log channel the case never
ingested, by name — and says how many rules could not be checked because they declare no channels,
so silence is never mistaken for an all-clear.

**It reads and reports. It repairs nothing on its own and deletes nothing, ever.** Repairs are
separate, explicit actions. The deep index verification is opt-in, and a quick check says so rather
than letting silence read as "the index is fine".

---

### Tooling

**`rohyctl`** — a standalone CLI for rule files that never opens a case, so it runs in CI, in a
pre-commit hook, and on a machine that has never seen the application.

```
rohyctl validate <path>...        would these load?
rohyctl format --check <path>...  canonical form, as a CI gate
rohyctl explain <name|path>...    what does this algorithm — or this rule — establish?
```

`validate` calls the same function the loader is built on, so it can never disagree with the
application about whether a file will load.

**`tools/genschema`** emits a JSON Schema for the rule format, generated from the same descriptor
the guided editor is built from. CI gates that it is current.

**CI** gains `rules-lint` (the shipped rule library must validate and be canonically formatted) and
`schema-drift` (generated artefacts must be regenerated).

---

### Under the hood

- **One prepared dataset per build, not per rule.** Every algorithm needs the same chronological
  ordering and scope partitioning; v0.1.0 re-derived it for each rule. At twenty rules over 50 000
  events this is **282 ms instead of 505 ms, and 16.2 MB allocated instead of 56.1 MB**.
- **A dry run and a build share that work**, cached against the store's own write counter so a
  stale dataset can never produce a plausible graph from events that no longer match.
- **GrapheneDB 0.3.0 → 0.4.0.**
- **Four new backend packages** (`graphlayout`, `snapshot`, `annotate`, `caseintegrity`), two new
  commands, and 22 Go packages in total.
- **663 Go tests and 581 frontend tests**, including a compatibility gate that asserts v0.1.0 rules
  produce byte-identical edges under the new engine.

---

### Compatibility

- **`format_version` is still `1`.** The new algorithms did not need a bump: an older build refuses
  a rule whose algorithm it does not recognise, which is exactly the protection a version bump would
  have provided. Every v0.1.0 rule loads and runs unchanged, and produces identical graphs.
- **Rebuilds still replace, never duplicate** — including the occurrence identifier, which is derived
  from the match itself rather than generated, so two builds can be compared.
- **Your case data is untouched.** The correlation projection is added to events by the backfill;
  nothing is rewritten or removed. Findings, layouts, graphs and rules are all carried forward.

### Not in this release

Stated rather than left to be discovered:

- The VS Code extension (the JSON Schema it will use ships here; the extension does not).
- The read-only query console.
- Snapshot thumbnails, and drawing regions or arrows directly on the canvas — both render, but only
  notes can be authored, from the layer panel.
- Code signing and notarization. Windows SmartScreen and macOS Gatekeeper will still warn.
- Manual verification on Linux and macOS — both build in CI, neither has been exercised by hand.

---

<div align="center">

Releases are named after constellations. v0.1.0 was **Cassiopeia**; v0.2.0 is **Andromeda**, her
daughter.

The version this build reports comes from the linker, and is written down in exactly one place —
[README.md](README.md).

</div>
