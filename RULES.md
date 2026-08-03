# rohy Rule Format

A rule tells rohy which events to correlate into a graph. This document is the authority on
the format: an author who has never read the source should be able to write a valid rule from
here alone.

---

## 1. The four principles

1. **One file is one rule.** A rule is a single JSON file. There is no multi-rule file.
2. **One rule is one graph.** Running a rule builds (or rebuilds) exactly one named graph.
   Re-running replaces that graph's contents rather than appending, so a rule's graph never
   accumulates duplicates.
3. **A rule may be arbitrarily long** — up to a generous cap (see §4). A two-event rule and
   a fifty-event rule are the same kind of thing.
4. **The algorithm decides what a match means.** Four are available, and choosing between them
   is the most consequential decision in a rule — not because of what they find, but because
   of what a match *establishes*. See §8.

---

## 2. A minimal rule

```json
{
  "name": "Logon Then Security Log Cleared",
  "sequence": ["4624", "1102"]
}
```

That is a complete, valid rule: a name and at least two event IDs. Everything else has a
sensible default.

And the same idea made precise, so that a match means something stronger:

```json
{
  "name": "Failed Logons Then Success, Same Session",
  "description": "Repeated 4625 then a 4624 sharing one logon session.",
  "algorithm": "field",
  "channels": ["Security"],
  "sequence": ["4625", "4625", "4624"],
  "match_fields": ["logon_id"]
}
```

## 3. Every field

### Always available

| Field | Required | Default | Meaning |
|---|:---:|---|---|
| `name` | **yes** | — | Human-readable name. Also the rule's identity: its id is a slug of the name, so two rules with the same name collide. |
| `format_version` | no | current (`1`) | The schema version this file targets. See §5. |
| `description` | no | `""` | Free text shown in the rules list and inspector. |
| `relation_type` | no | `correlation` | Edge type for the relations produced: `correlation`, `temporal`, or `default`. An empty or unrecognized value becomes `correlation`. |
| `algorithm` | no | `sequence` | How this rule's events are correlated. See §7 and §8. |
| `channels` | no | none | The Windows log channels this rule needs in order to fire. No algorithm reads it — see below. |

### Read by the sequence-matching algorithms (`sequence`, `field`, `temporal`)

| Field | Required | Default | Meaning |
|---|:---:|---|---|
| `sequence` | **yes** | — | Ordered list of event IDs (as strings) to match in chronological order. |
| `labels` | no | none | Optional per-connection labels; `labels[i]` labels the edge from `sequence[i]` to `sequence[i+1]`. See §6. |

### Read by `field`, `temporal` and `lineage`

| Field | Required | Default | Meaning |
|---|:---:|---|---|
| `match_scope` | no | `computer` | How events are partitioned before matching: `computer` or `global`. Not available to `sequence` — see §5. |

### Read by `field` and `temporal`

| Field | Required | Default | Meaning |
|---|:---:|---|---|
| `match_fields` | **for `field`** | none | Correlation fields every event in a match must share. See §9. |

### Read by `temporal`

| Field | Required | Default | Meaning |
|---|:---:|---|---|
| `window_within` | **yes** | — | Maximum time between consecutive matched steps, as a duration (`"90s"`, `"5m"`, `"2h"`). |
| `window_total` | no | unbounded | Maximum time from the first matched step to the last. Must be at least `window_within`. |

### Read by `lineage`

| Field | Required | Default | Meaning |
|---|:---:|---|---|
| `lineage_create_ids` | no | `["4688"]` | Event IDs that record a process being created. Sysmon's is `1`. |
| `lineage_depth` | no | `0` | Ancestor levels above the direct parent to link. `0` emits direct edges only. |

**About `channels`.** No algorithm reads it. It exists so rohy can tell you *"this rule cannot
fire, the log it depends on was never ingested"* instead of reporting zero matches and leaving
you to work out why. Declare every channel the rule's event IDs come from. A rule that omits it
is simply not checked — and rohy says so rather than staying silent, because silence there
would read as "fine".

**Unknown fields are ignored, not rejected** — a rule written for a newer rohy still loads on
an older one, as long as its `format_version` is accepted (§5). The same applies to a field
belonging to a *different* algorithm: setting `window_within` on a `sequence` rule is legal,
preserved on save, and reported as having no effect. The editor is never stricter than the
loader.

## 4. Validation rules

A file must satisfy all of these, or it is rejected with a message naming the problem:

- `name` is present and not blank.
- `algorithm`, if given, is one this build implements (§7).
- `format_version` is not greater than the version this rohy understands (§5).
- `match_scope`, if given, is `computer` or `global`.
- `channels`, if given, contains no blank entries.

For `sequence`, `field` and `temporal`:

- `sequence` has **at least 2** and **at most 1000** event IDs, none of them blank.
- `labels` has **at most `len(sequence) - 1`** entries (one per connection).

For `field`:

- `match_fields` has at least one entry, each naming a correlation field (§9), with no
  duplicates.

For `temporal`:

- `window_within` is present, parses as a duration, is greater than zero, and is at most 30
  days. A window measured in weeks is almost always a units slip.
- `window_total`, if given, satisfies the same bounds and is at least `window_within` —
  otherwise no match could ever complete.

For `lineage`:

- `lineage_create_ids`, if given, contains no blank entries.
- `lineage_depth` is between 0 and 16.

Strings are trimmed on load, so leading and trailing whitespace never changes a match.

An **unknown algorithm stops validation** and is reported on its own. Almost every other check
depends on which algorithm a rule uses — whether a sequence is required at all, whether
`match_fields` must be present — so checking against a guessed algorithm would put confident,
wrong complaints in front of you beside the real one.

When a folder of rules is imported, each file is validated **independently**: one bad file is
reported by name and skipped, and the good files in the same folder still load. Import also
refuses a file above a size cap, so a stray large file cannot stall the load.

## 5. Format version and forward compatibility

`format_version` is how a rule file and the software agree on what the file means.

- The current version is **`1`**, and it is the only one. A file that omits it is treated as
  current.
- A file declaring a version **higher** than this rohy understands is **refused with an
  explanation** — never partially matched. A newer rule may rely on a matcher this build does
  not have, and silently ignoring that would produce a graph that is wrong rather than absent.

**Why v0.2.0 did not bump it, despite adding three algorithms.** A new `algorithm` value looks
like a breaking change, and by the policy below it is one: an older build reading a `field` rule
and matching on the event-ID sequence alone would produce a wrong graph. But it cannot get that
far — **the algorithm name is itself the guard.** A build that does not implement `field`
refuses the rule by name and says which matcher is missing, which is more useful than a version
number. A bump would have bought nothing and cost every author a second concept to reason about.

This is only true because every v0.2.0 field is read by a *new* algorithm. The one exception was
`match_scope`, which would have applied to `sequence` as well — an older build would have ignored
it and quietly scoped by computer. Rather than carry a version to protect one combination, that
combination was removed: `match_scope` belongs to `field`, `temporal` and `lineage` only. Global
scope is meaningless without `match_fields` anyway, and a `sequence` rule has none.

**Version-bump policy** (what forces a new `format_version`):

- Adding a new **optional** field with a safe default is **not** breaking and does not bump the
  version. Older builds ignore the field (§3). `channels` was added this way.
- Adding a **new `algorithm` value** does not bump it either, for the reason above: the name is
  refused by any build that lacks the implementation.
- Adding a field that changes what an **existing** algorithm matches, or a new required field,
  **is** breaking and bumps the version — because an older build would ignore it and match
  differently rather than refusing. This is the case the mechanism exists for, and the only one.

## 6. Labels

Labels annotate the connections between consecutive steps. `labels[i]` is the label on the
edge from `sequence[i]` to `sequence[i+1]`, so a sequence of _n_ steps has _n − 1_
connections and at most _n − 1_ labels.

Labels are optional and may be partial. An empty string, or a missing trailing entry, leaves
that connection untagged:

```json
{
  "name": "Failed Logons Then Successful Logon",
  "sequence": ["4625", "4625", "4625", "4624"],
  "labels": ["", "", "then succeeds"]
}
```

Here only the last connection (`4625 → 4624`) is labelled; the two between the failures are
untagged.

## 7. The algorithms

| Algorithm | Matches | Also reads |
|---|---|---|
| `sequence` | An ordered event-ID sequence, chronologically, on one computer. | — |
| `field` | The same, **plus** a shared value for every named correlation field. | `match_fields`, `match_scope` |
| `temporal` | The same, **plus** a bounded time gap between consecutive steps. Composes with `match_fields`. | `window_within`, `window_total`, `match_scope` |
| `lineage` | Process ancestry reconstructed from process-creation records. No sequence. | `lineage_create_ids`, `lineage_depth`, `match_scope` |

Any value other than these is rejected at load, so a rule can never half-run on a matcher that
does not exist.

**`sequence` and `temporal` anchor differently, and it matters.** `sequence` takes the
*earliest* subsequence: it scans forward and accepts each step the moment it sees it. That is
correct when order is the only constraint. With a window it is not — the earliest subsequence
can fail the window where a later one would have satisfied it — so `temporal` sweeps once and
keeps the *most recent* event that completed each step. Given an unbounded window the two would
not always produce the same edges. They are different algorithms, not variants.

**`lineage` resolves parents through time, not by PID.** Windows reuses process IDs
constantly, so "the creation event whose new PID equals this event's parent PID" matches many
unrelated processes over a case spanning days. Each creation opens an interval for its PID,
closed by the next creation of that PID or by its exit (`4689`); a child's parent is the
process whose interval **contained** the child's creation time. Where no such interval exists —
overwhelmingly because the parent was created before your log begins — **nothing is emitted and
the count is reported**. Guessing the nearest candidate would be the very error the interval
table exists to prevent.

## 8. What a match does and does not establish

This is the most important section in this document. It decides how much weight to put on a
rule-generated graph, and the answer is **different for each algorithm**.

### `sequence`

A match finds its event IDs **in chronological order, within the scope**, greedily and without
overlap. That is all it establishes: a **temporally ordered pairing**. It is a lead worth
inspecting.

It does **not** establish that the events involve the same user, account, logon session, or
process. A rule pairing "account created" with "added to group" matches those two events on one
host in that order; it does not, by itself, prove they concern the same account.

### `field`

A match establishes everything `sequence` does **and** that every matched event carried the
same value in each field of `match_fields`. Correlating on `logon_id` does establish that the
events belong to one logon session; correlating on `target_user` does establish that they
concern one account.

This is the algorithm to reach for when the pairing matters. Two cautions:

- An event carrying **no value** for a required field is **excluded from matching** and
  counted, never grouped with the others. Windows writes `-` and the null SID constantly, and
  bucketing those together would correlate every event that happens not to carry the field with
  every other one. The run reports how many events were left out — read that number, because a
  small result can mean "this pattern is rare" or "most of my events could not be considered",
  and they look identical otherwise.
- Correlating on the right field beats correlating on three. `logon_id` ties a session
  together; adding `target_user` on top usually narrows nothing and risks excluding events that
  legitimately leave it blank.

### `temporal`

A match establishes everything `sequence` does **and** that consecutive steps happened within
`window_within` of each other. Combined with `match_fields` it establishes both.

Proximity in time is evidence of relatedness, not proof of it. A busy host generates plenty of
unrelated events seconds apart.

### `lineage`

An edge establishes that the child process was created by a process that was **alive and
holding that PID at that moment** — resolved through the interval table, not by PID equality.
That is a strong claim, and it is why direct lineage edges carry full confidence.

Edges above the direct parent (`lineage_depth > 0`) are **derived** by walking direct links
rather than read from any single record, and they carry a lower confidence to say so.

### About `confidence_score`

In rohy it measures **how exact the match was**. It is not, and must never be read as, an
estimate of how likely the activity is to be malicious. Every matcher this build offers is
exact; the only value below `1.0` is on a transitive lineage edge.

### And in every case

Built-in rule descriptions are written to respect these distinctions, and the rules that anchor
on high-volume events (for example 4672, which fires on essentially every administrative or
SYSTEM logon) say so. Confirm the entity linkage before drawing a conclusion from any
correlation graph.

## 9. Correlation fields

`match_fields` draws from a fixed vocabulary, projected from each event's `EventData` when it
is ingested:

| Field | What it is |
|---|---|
| `logon_id` | The logon session this event belongs to. The single most useful correlation field. |
| `subject_logon_id` | The session of the account that **caused** the event, when the event distinguishes actor from target. |
| `process_id` | The verbatim `ProcessId` field. Its meaning is event-dependent — on a 4688 it is the creator, not the new process. |
| `new_process_id` | The process created by this event (4688). |
| `parent_process_id` | An explicitly named parent/creator (Sysmon, and providers that spell it out). |
| `process_name` | The image of the process this event is about, basename only. |
| `target_user` | The account acted upon. |
| `subject_user` | The account acting. |
| `ip_address` | The remote address involved. |
| `service_name` | The service involved (7045, 4697…). |

Values are normalized so two spellings of one thing compare equal: identifiers become
lowercase `0x`-prefixed hex whether the provider wrote decimal or hex, image paths reduce to
their lowercased basename, and `-`, the null SID, `null` and `n/a` count as **absent** rather
than as values.

**Events ingested before v0.2.0 have no projection**, so field, temporal and lineage rules
under-report against them. rohy reports the count rather than returning the short answer as
though it were the whole one, and offers a one-off backfill that fills them in.

## 10. Where rules live

- **Built-in rules** ship inside the application. They can be enabled or disabled, and a
  disabled state persists, but they cannot be edited in place — vary one with **Duplicate**,
  which opens an editable copy under a new name (§11).
- **User rules** are written by the in-app editor or imported from a file or folder, and are
  stored in the case's rules directory. They follow this exact format and are portable: a
  rule authored on one platform loads unchanged on any other.

See [README.md](README.md) for the built-in library and the import workflow.

## 11. Authoring rules in the application

Everything above can be written by hand in any text editor. The **Rules** page also has an
editor, with two modes over the same file.

**Guided** is a form generated from the rule format itself: every field shows what it is,
and a `?` beside it opens how to choose a value and an example. The form shows the fields the
selected algorithm actually reads, so a lineage rule does not offer a window and a sequence
rule does not offer `match_fields` — except when the file already sets one, which stays visible
carrying the advisory that says it has no effect. The sequence is edited as a chain, with each
connection's label sitting *between* the two steps it joins — which is what makes the
`labels[i]` rule of §6 impossible to get wrong.

**Raw** is the file as text, with highlighting, `Ctrl+Space` completion drawn from this
format, and `Ctrl+Shift+F` to format it in the same style the built-in library uses.

Switching between them is a change of view, not a conversion: both edit one document and
share one undo history. Guided → raw always works. Raw → guided needs the JSON to parse
first, and says so rather than opening a form seeded from a file it could not read.

Three things are worth knowing before you save:

- **A rule's id is a slug of its name (§3), so renaming a rule replaces it.** The editor
  says so before saving, and names the old and new ids. The graph the old id built is not
  deleted, but it is no longer linked to this rule.
- **A field this build does not interpret is preserved.** Per §3 it is ignored, not
  rejected; the guided form lists such fields read-only rather than hiding them, and saving
  writes them back untouched. Editing a rule written for a newer rohy does not downgrade it.
- **Nothing invalid is written.** A save is validated by the same code that loads a rule
  file, so a refused save changes nothing on disk. A file that already failed to load can be
  opened with **Fix** from the load-errors panel and repaired in place.

### Adding a validation rule

Validation is implemented twice on purpose: in Go, which decides whether a rule loads, and
in JavaScript, so the editor can underline a mistake on the keystroke that makes it. The two
are kept in step by `backend/rules/testdata/validation-cases.json`, which is read by both
test suites.

**Adding a case to that fixture is the first step of any change to validation.** A rule
enforced on one side and not the other is then a failing test, rather than an editor that
quietly accepts something the loader will refuse.

### Adding a field to the format

The descriptor in `backend/rules/schema.go` is the format described as data, and three things
generate themselves from it: the guided form's controls, the raw editor's completion list, and
the client-side validator. A reflection test fails the build if a `Spec` field has no
descriptor, and a golden test fails if the descriptor changes at all — which is the reminder
that this document has to change with it.

### Adding an algorithm

`consts.Algorithms` is the vocabulary the validator accepts; `autograph.registry` holds the
implementations. A test asserts the two agree **in both directions**, so an algorithm can never
be accepted at load without an implementation behind it — which would build an empty graph and
look exactly like a rule that found nothing.
