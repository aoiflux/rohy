# rohy Rule Format

A rule tells rohy which sequence of Windows event IDs to correlate into a graph. This
document is the authority on the format: an author who has never read the source should be
able to write a valid rule from here alone.

---

## 1. The four principles

1. **One file is one rule.** A rule is a single JSON file. There is no multi-rule file.
2. **One rule is one graph.** Running a rule builds (or rebuilds) exactly one named graph.
   Re-running replaces that graph's contents rather than appending, so a rule's graph never
   accumulates duplicates.
3. **A rule may be arbitrarily long** — up to a generous cap (see §4). A two-event rule and
   a fifty-event rule are the same kind of thing.
4. **Event-ID sequences are the matcher.** v1 correlates on the ordered list of event IDs.
   Other matchers (fields, time windows) are reserved but not yet implemented — see §7.

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

## 3. Every field

| Field | Required | Default | Meaning |
|---|:---:|---|---|
| `name` | **yes** | — | Human-readable name. Also the rule's identity: its id is a slug of the name, so two rules with the same name collide. |
| `sequence` | **yes** | — | Ordered list of event IDs (as strings) to match in chronological order. |
| `format_version` | no | current (`1`) | The schema version this file targets. See §5. |
| `description` | no | `""` | Free text shown in the rules list and inspector. |
| `relation_type` | no | `correlation` | Edge type for the relations produced: `correlation`, `temporal`, or `default`. An empty or unknown value becomes `correlation`. |
| `algorithm` | no | `sequence` | How the sequence is matched. `sequence` is the only value v1 accepts. See §7. |
| `labels` | no | none | Optional per-connection labels; `labels[i]` labels the edge from `sequence[i]` to `sequence[i+1]`. See §6. |

Unknown fields are ignored, not rejected — a rule written for a newer rohy still loads on an
older one, as long as its `format_version` is accepted (§5).

## 4. Validation rules

A file must satisfy all of these, or it is rejected with a message naming the problem:

- `name` is present and not blank.
- `sequence` has **at least 2** and **at most 1000** event IDs.
- No event ID in `sequence` is blank.
- `labels` has **at most `len(sequence) - 1`** entries (one per connection).
- `algorithm`, if given, is `sequence`.
- `format_version` is not greater than the version this rohy understands (§5).

Strings are trimmed on load, so leading and trailing whitespace never changes a match.

When a folder of rules is imported, each file is validated **independently**: one bad file
is reported by name and skipped, and the good files in the same folder still load. Import
also refuses a file above a size cap, so a stray large file in an imported folder cannot
stall the load.

## 5. Format version and forward compatibility

`format_version` is how a rule file and the software agree on what the file means.

- The current version is **`1`**. A file that omits it is treated as current.
- A file declaring a version **higher** than this rohy understands is **refused with an
  explanation** — never partially matched. A newer rule may rely on a matcher this build
  does not have, and silently ignoring that would produce a graph that is wrong rather than
  absent.
- A file declaring the current version or lower always loads.

**Version-bump policy** (what forces a new `format_version`):

- Adding a new **optional** field with a safe default is **not** a breaking change and does
  not bump the version. Older builds ignore the field (§3).
- Adding a field that changes what an existing rule **matches**, or a new required field, or
  a new `algorithm` value, **is** breaking and bumps the version — so an older build refuses
  the file rather than matching it wrongly.

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

## 7. Extensibility

The format has two designed extension axes. Both are **reserved** — described here so rule
authors and future contributors know the shape, not because they work yet.

**New matchers (the `algorithm` field).** `sequence` is the only algorithm v1 implements.
The engine resolves `algorithm` through a registry, so a new matcher — field correlation, a
temporal window — is added by registering an implementation and accepting its name in
validation. Until then, any value other than `sequence` is rejected at load, so a rule
cannot half-run on a matcher that does not exist.

**New field matchers within a rule.** The event-ID sequence is the only matcher today. Future
matchers (provider, channel, user, a time window between steps) would arrive as new optional
fields. Per §5, adding one that changes matching bumps `format_version`, so an older build
refuses a rule that depends on it rather than matching only the event IDs and producing a
subtly wrong graph.

Nothing reserved is half-implemented: a field this document does not list has no effect
today beyond being ignored.

## 8. What a rule match does and does not establish

This matters for how much weight to put on a rule-generated graph.

A `sequence` match finds its event IDs **in chronological order, on the same computer**,
greedily and without overlap. That is all it establishes: a **temporally ordered pairing on
one host**. It is a lead worth inspecting.

It does **not** establish that the events involve the same user, account, or logon session —
v1 correlates on event IDs alone and scopes only to the computer. So a rule pairing "account
created" with "added to group" matches those two events on one host in that order; it does
not, by itself, prove they concern the same account. Built-in rule descriptions are written
to respect this distinction, and the rules that anchor on high-volume events (for example
4672, which fires on essentially every administrative or SYSTEM logon) say so. Confirm the
entity linkage before drawing a conclusion from any correlation graph.

## 9. Where rules live

- **Built-in rules** ship inside the application. They can be enabled or disabled, and a
  disabled state persists, but they cannot be edited in place — vary one by importing your
  own copy under a different name.
- **User rules** are imported from a file or folder and stored in the case's rules
  directory. They follow this exact format and are portable: a rule authored on one platform
  loads unchanged on any other.

See [README.md](README.md) for the built-in library and the import workflow.
