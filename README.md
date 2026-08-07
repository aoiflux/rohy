<div align="center">

<img src="frontend/src/assets/logo.svg" width="96" alt="rohy logo" />

# rohy

**Forensic event mapping for Windows event logs.**

Ingest EVTX logs, map how events relate, and correlate them with rules you
control — entirely on your own machine.

`v0.2.0` · Windows / Linux / macOS · Go + Wails + Svelte

</div>

---

## What it does

rohy is a desktop tool for working through Windows event logs as a _graph_
rather than a flat list. You ingest logs, it normalizes and de-duplicates them,
and then you map relationships between events — by hand on a canvas, or
automatically with correlation rules.

- **Ingest** `.evtx` files, folders of them, a SQLite `.db` carrying event data,
  or the live Windows Event Log — continuously, with pause/resume.
- **Investigate** with a filtered, paginated event list that stays fast on large
  cases.
- **Correlate** using rule files: an ordered chain of event IDs with optional
  labels (`4625 → 4625 —then succeeds→ 4624`). Four algorithms — plain ordering,
  shared field values, bounded time windows, and process ancestry. Thirty-five
  conservative rules ship built in.
- **Map** events on a graph canvas, by hand or generated from a rule — one rule,
  one graph.
- **Place** everything in time on a dedicated timeline with zoom, pan, scrub,
  range selection and lanes — sharing one selection with the graph canvas.
- **Annotate** with your own findings — a flag, tags and a note per event, kept
  in a readable sidecar beside the evidence rather than inside it.
- **See provenance** everywhere: a relation the tool inferred and one you
  asserted never look the same — and every generated edge can name the rule,
  algorithm, match and step that produced it.
- **Learn how it works** from an explainer page that animates each correlation
  algorithm step by step over a worked example.

Everything stays local. rohy makes no network calls; your case data never leaves
the machine.

## Features

| Area            | What you get                                                                                                                                                                             |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Ingestion**   | `.evtx` files/folders, SQLite `.db` (two documented schemas), live capture with durable per-channel bookmarks, pause/resume, hash-based de-duplication with per-source occurrence counts |
| **Events**      | Accurate counts, progressive loading, collapsible search with persisted filters, relation-aware quick filters, CSV/JSON export                                                           |
| **Rules**       | Portable one-file-per-rule JSON, 35 built-ins, import/delete, enable/disable, inspector showing the rule exactly as authored, byte-exact bundle export, field-level diff against the saved file, and a testbench that dry-runs a rule without writing anything |
| **Graphs**      | Multiple named graphs, manual and rule-generated edges, connect mode, snap-to-target, box select, fit-to-content, selectable edges with a relationship inspector that traces an edge back to its rule and match |
| **Timeline**    | Backend density histogram (cheap at any size), zoom/pan/scrub, range selection that filters, lanes by provider, channel, user, computer or graph, shared selection with the canvas       |
| **Findings**    | Per-event flag, tags and note in a plain-JSON sidecar; tag suggestions, finding filters on the event list, and an audit that reports orphaned findings rather than deleting them         |
| **Correlation** | Four algorithms — sequence, field, temporal and lineage — scoped per computer, non-overlapping, capped; one prepared dataset per build rather than one per rule; idempotent rebuilds (re-running replaces, never duplicates) |
| **Maintenance** | Opt-in, resumable correlation-key backfill for cases ingested by an older build, with a status readout that explains why a field or lineage rule found nothing |

## Install

```powershell
Get-FileHash .\rohy.exe -Algorithm SHA256   # Windows — compare against SHA256SUMS.txt
```

> **Binaries are not code-signed yet.** Windows SmartScreen and macOS Gatekeeper
> will warn you. That is expected at v0.2.0 — signing and notarization are
> planned, and this note will go away when they land rather than being quietly
> dropped.

### Build from source

Requires **Go 1.26**, **Node 24**, and the
[Wails CLI](https://wails.io/docs/gettingstarted/installation) **v2.13.0** — the
same versions CI builds releases with.

```bash
git clone <repo> && cd rohy
./build.sh 0.2.0             # Linux / macOS — the version shown at the top of this file
.\build.ps1 -Version 0.2.0   # Windows
```

The version argument is **required** and has no default anywhere — not in the build
scripts, not in `backend/version`, and not in the release workflow. This file is the
only place the release version is written down, so bumping it means editing one line
here and passing the same number to the build. A binary built without a stamp reports
itself as `unreleased`, never as a number it has no basis for.

The build scripts run the test suites, delete `frontend/dist` and rebuild it
from scratch, then stamp version/commit/date into the binary. The clean-frontend
step is deliberate: the Go binary embeds whatever is in `dist`, so reusing a
stale build ships an old UI behind a new backend.

### Application icon

The icon is **derived**, never hand-drawn. `frontend/src/assets/logo.svg` is the
one place the mark is defined; after changing it, regenerate the raster assets:

```bash
python tools/gen_icons.py      # needs Pillow
```

That rewrites `build/appicon.png` (used for macOS and Linux) and
`build/windows/icon.ico` (all six Windows sizes), and writes
`build/icon-preview.png` so small sizes can be checked by eye. Both generated
assets are committed — Wails substitutes its own default artwork when they are
missing, so an uncommitted icon means a fresh clone silently ships the
placeholder.

Working on the persistence layer? Read [PERFORMANCE.md](PERFORMANCE.md) first —
it records what rohy's costs actually are, the rules that follow from that, and
one documented optimisation that made cold start 10× slower before it was
measured.

## Usage

1. **Ingest** — on the Dashboard, pick `.evtx` files or a folder (or start a
   live capture) and press _Start ingestion_. Progress shows app-wide while it
   runs.
2. **Explore** — the Events page lists everything ingested. `Ctrl+F` opens
   search; the chips filter by relation or timeline participation.
3. **Correlate** — the Rules page lists built-in and imported rules. _Run
   enabled rules_ builds one graph per rule; the result links straight to the
   canvas.
4. **Map** — on the Graph page, drag from a node's link handle (or press `C` for
   connect mode and drag from anywhere on a card) to relate two events.
5. **Place in time** — the Timeline page shows the filtered set chronologically.
   Drag to pan (or select, depending on mode), scrub the playhead, and group
   into lanes by provider, channel, user, computer or graph. Selecting an event
   here focuses it on the canvas. Undated events can't be placed on a timeline,
   so the page says how many there are and links to them.
6. **Annotate** — flag an event, tag it, or write a note. Findings are saved to
   a readable JSON sidecar and can be filtered on from the Events page.

Press <kbd>?</kbd> anywhere for the full keyboard reference.

Case data is written to `rohy-data/` **beside wherever you launch the app**, so
a working folder is self-contained and portable.

## Platform support

| Platform              | Status                              | Runtime requirement                                                                                       |
| --------------------- | ----------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Windows 10/11 (amd64) | Primary — developed and tested here | [WebView2 runtime](https://developer.microsoft.com/microsoft-edge/webview2/) (preinstalled on Windows 11) |
| Windows (arm64)       | Builds in CI                        | WebView2 runtime                                                                                          |
| Linux (amd64, arm64)  | Builds in CI                        | `libwebkit2gtk-4.0` + `libgtk-3`                                                                          |
| macOS (amd64, arm64)  | Builds in CI                        | WKWebView (system)                                                                                        |

**Live event-log capture is Windows-only** — it uses the native `wevtapi`. On
other platforms the app runs and ingests `.evtx`/`.db` files normally; only live
capture is unavailable.

Honest note on testing: development and manual verification happen on
**Windows**. The other targets are built by CI but are not yet manually
exercised — cross-platform builds are wired, not battle-tested.

## SQLite `.db` ingestion

rohy reads two **documented** SQLite shapes. It does not sniff arbitrary schemas
— guessing column meanings could silently mis-map evidence, so a database
matching neither is rejected with a message naming both shapes it checked.

**1. Events** — one row per event occurrence:

```sql
events(event_id, timestamp, provider, channel, computer,  -- required
       user, raw_xml)                                     -- optional
```

**2. Provider/message catalogue** — descriptions of what event IDs mean:

```sql
messages(id, event_id, provider_id, message)
providers(id, name)
```

Catalogue rows carry no timestamp, so they cannot be placed on a timeline. They
are still ingested and fully searchable, labelled as their own source type, and
shown with `—` where a time would be — never a fabricated date.

## Rule format

One file, one rule, one graph:

```json
{
  "format_version": 1,
  "name": "Failed Logons Then Successful Logon",
  "description": "Three failed logons followed by a success on the same computer.",
  "relation_type": "correlation",
  "algorithm": "sequence",
  "sequence": ["4625", "4625", "4625", "4624"],
  "labels": ["", "", "then succeeds"]
}
```

`labels[i]` names the edge between `sequence[i]` and `sequence[i+1]`; leave it
empty for an unlabelled link. Drop files into the rules folder (shown on the
Rules page) or import them from the UI.

`algorithm` chooses **how** the sequence is correlated, and each algorithm reads
its own extra fields — everything else is ignored:

| Algorithm  | Correlates on                                                                       | Its fields                            |
| ---------- | ------------------------------------------------------------------------------------- | --------------------------------------- |
| `sequence` | Order alone. The default, and the weakest claim: these IDs occurred in this order.   | —                                       |
| `field`    | Order **plus shared field values** — same logon session, same account, same process. | `match_fields`, `match_scope`           |
| `temporal` | Order **plus bounded gaps**, so a chain reads as one episode rather than a coincidence. | `window_within`, `window_total`       |
| `lineage`  | Process ancestry, resolved through each PID's lifetime rather than by matching the number. | `lineage_create_ids`, `lineage_depth` |

The correlation fields a `field` rule can match on (`logon_id`, `target_user`,
`process_id`, …) are projected into each event at ingest. A case ingested by an
older build has no projection, so those rules match nothing until the backfill
on the Maintenance page runs — which the app tells you rather than reporting a
silent zero.

You do not have to write the JSON by hand. _New rule_ on the Rules page opens an
editor with two modes over the same file: a **guided** form generated from the
rule format — every field carrying its own description, allowed values, and
example, with each connection label edited between the two steps it joins — and
a **raw** JSON view with highlighting, completion, and formatting. Switching
between them is a change of view, not a conversion. A built-in cannot be edited
in place, so _Duplicate_ opens an editable copy; a file that failed to load can
be repaired with _Fix_ instead of leaving the application. The editor shows what
changed against the saved file field by field, and its **testbench** dry-runs the
rule against the current case — how many matches, sampled examples, and what it
could not consider — without writing an edge.

See [RULES.md](RULES.md) for the complete format — every field, the validation
rules, the format-version and extensibility contract, and what a rule match does
and does not establish.

### Built-in library

Thirty-five rules ship inside the application, enabled by default and
individually toggleable. They cannot be edited in place — import your own copy
under a different name to vary one.

| Theme                      | Rule                                                  | Chain                          |
| -------------------------- | ----------------------------------------------------- | ------------------------------ |
| **Credential attack**      | Failed Logons Then Successful Logon                   | `4625 4625 4625 → 4624`        |
|                            | Failed Logons Then Account Lockout                    | `4625 4625 → 4740`             |
|                            | Kerberos Pre-Authentication Failures Then Logon       | `4771 4771 4771 → 4624`        |
|                            | Account Lockout Then Account Unlocked                 | `4740 → 4767`                  |
|                            | Failed Logons Then Logon Then Account Created         | `4625 4625 4625 → 4624 → 4720` |
| **Accounts and privilege** | Account Created Then Added To Group                   | `4720 → 4732`                  |
|                            | Account Created Then Added To Domain Group †          | `4720 → 4728`                  |
|                            | Account Enabled Then Added To Group                   | `4722 → 4732`                  |
|                            | Password Reset Then Added To Group                    | `4724 → 4732`                  |
|                            | Password Reset Then Logon                             | `4724 → 4624`                  |
|                            | Account Created Then Deleted                          | `4720 → 4726`                  |
|                            | Group Member Added Then Removed                       | `4732 → 4733`                  |
| **Persistence**            | Privileged Logon Then Service Installed               | `4672 → 7045`                  |
|                            | Privileged Logon Then Scheduled Task Created          | `4672 → 4698`                  |
|                            | Privileged Logon Then WMI Persistence Registered ‡    | `4672 → 5861`                  |
|                            | PowerShell Script Block Then Scheduled Task Created ‡ | `4104 → 4698`                  |
|                            | Service Installed Then Start Type Changed             | `7045 → 7040`                  |
|                            | Scheduled Task Created Then Deleted                   | `4698 → 4699`                  |
| **Lateral movement**       | Explicit Credential Logon Then Share Accessed         | `4648 → 5140`                  |
|                            | Share Accessed Then Scheduled Task Created            | `5140 → 4698`                  |
|                            | RDP Authentication Then Logon ‡                       | `1149 → 4624`                  |
|                            | Firewall Exception Added Then Logon                   | `4946 → 4624`                  |
| **Defence tampering**      | Defender Protection Disabled Then Service Installed ‡ | `5001 → 7045`                  |
|                            | Malware Detected Then Security Log Cleared ‡          | `1116 → 1102`                  |
|                            | Service Terminated Then New Service Installed         | `7034 → 7045`                  |
| **Anti-forensics**         | Logon Then Security Log Cleared                       | `4624 → 1102`                  |
|                            | Log Cleared Then System Time Changed                  | `1102 → 4616`                  |
|                            | Audit Policy Changed Then Security Log Cleared        | `4719 → 1102`                  |
|                            | Service Installed Then Security Log Cleared           | `7045 → 1102`                  |
|                            | Security Log Cleared Then Another Log Cleared         | `1102 → 104`                   |

† matches domain controller logs. ‡ needs a channel beyond Security and System —
WMI-Activity, PowerShell, Defender, or TerminalServices — to be ingested, and
matches nothing at all if it was not.

Five more make a **stronger** claim than ordering, by correlating on a shared
field, a bounded window, or process ancestry:

| Rule                                                | Algorithm  | Chain                   | Also requires                    |
| --------------------------------------------------- | ---------- | ----------------------- | -------------------------------- |
| Logon Then Process Creation, Same Session           | `field`    | `4624 → 4688`           | the same `logon_id`              |
| Password Reset Then Logon, Same Account             | `field`    | `4724 → 4624`           | the same `target_user`           |
| Failed Logon Burst Then Success Within Five Minutes | `temporal` | `4625 4625 4625 → 4624` | each gap ≤ 5m                    |
| Service Installed Then Security Log Cleared Within An Hour | `temporal` | `7045 → 1102`    | the gap ≤ 1h                     |
| Process Ancestry                                    | `lineage`  | `4688` parent links     | process creation auditing        |

For a `sequence` rule, a match means those event IDs appeared **in that order on
one computer** — not that they involve the same account, and not that the steps
were adjacent. The `field` and `lineage` rules above do establish a shared
session, account or parent process; the `temporal` ones establish only that the
ordering was tight. Each rule's description says what it does and does not
establish, names the channel it depends on, and hedges where its anchor is a
high-volume event (4672, 4648, 4771, 4104, 5140, 4946, 7034, 7040, 1149, 5001,
4688). Read the description before acting on a graph.

The **Algorithms** page walks through each of the four on a worked example,
animating what the matcher keeps and what it discards at every step.

## Roadmap

**Delivered in v0.1.0** — ingestion (files, folders, SQLite, live capture with
pause/resume), event querying with accurate counts and progressive loading, the
rule engine and built-in library, the dual-mode rule editor, auto-graphing,
multiple graphs, the graph canvas, relation provenance, the timeline page (zoom,
pan, scrub, lanes, shared selection with the canvas), analyst findings with
orphan auditing, keyboard shortcuts, and the release pipeline.

**Delivered in v0.2.0** — the correlation-key projection and its backfill, three
further correlation algorithms (field, temporal, lineage), the prepared dataset
that costs one pass per build rather than one per rule, edge-level provenance and
the relationship inspector, the rule testbench and field-level diff, byte-exact
rule bundle export, and the algorithms explainer page.

**Next**

- **Graph layout profiles**, clustering, and a density heatmap.
- **Case integrity** — missing-channel detection and inert-rule reporting.
- **Code signing and notarization** for released binaries.
- **Manual verification on Linux and macOS** — both build in CI today, but
  neither has been exercised by hand.

**Deferred, deliberately** — streaming progress for very large rule runs,
windowed evaluation for very large event sets, a full keyboard-only connect path
on the canvas, and EVTX export.

## Versioning

SemVer. `0.x` means the rule format and stored schema may still change between
minor versions; a `format_version` field in rule files guards forward
compatibility. `1.0` will be cut when the on-disk format and the rule
specification are stable.

The running build reports its own version, commit and build date under **About**
(click the logo in the title bar). A build made outside the release scripts
labels itself a _dev build_ rather than claiming a release version.
