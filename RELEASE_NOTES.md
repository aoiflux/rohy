<div align="center">

<img src="frontend/src/assets/logo.svg" width="72" alt="rohy logo" />

# Release notes

</div>

---

## v0.3.0 — "Perseus" (unreleased)

_Not yet tagged. The current release is v0.2.0 "Andromeda", below._

_The release that can show its work to someone who was not there._

v0.2.0 learned to say what a match proves. It could not yet prove anything about the case itself
to anyone outside it. A reviewer, a court or a colleague had to take the analyst's word that the
events on screen were the events that were ingested, that nothing had been removed, and that the
person who sealed the case was who they said.

v0.3.0 gives them something to check instead. A case keeps a fingerprint for every seal and checks
the chain between them. One event can be exported with a proof that anyone holding the fingerprint
verifies without the case or rohy. Each event can account for itself layer by layer. A redaction
is recorded, attributed and kept from coming back. Every change names the analyst who made it,
seals can be signed, and a checkpoint's digest can be published somewhere the case cannot reach.
The integrity check now reports what the case **cannot** prove, and never reports what it could
not read as fine.

Beside that: a read-only query console, error codes that reach the screen, network correlation
fields, ingest in file order with an integration suite over the real pipeline, and a VS Code
extension for rule files.

> **Named for Perseus**, who stands beside Andromeda in the sky, as Andromeda stands beside
> Cassiopeia. v0.1.0 ordered events and v0.2.0 related them. v0.3.0 makes a claim about the case
> that someone else can hold it to.

### ⚠️ Action required: run the correlation backfill

The correlation projection recipe is now version **3**. Every event in a case ingested by an
earlier build reports stale correlation keys until the backfill on the Maintenance page has run,
and field, temporal and lineage rules under-report against it until then. Nothing is lost —
the backfill re-reads the stored fields — but the result of a rule run before it is not the
whole answer.

The backfill seals the case when it has rewritten any event, so **the case fingerprint changes**.
Keep the new one: a fingerprint exported before the backfill no longer matches the case.

### Added

- **Event proofs.** *Save proof…* in the event drawer writes `event-<id>-proof.json`: a file
  that lets somebody without the case, or rohy, check that this one event was in the sealed case
  whose fingerprint they hold. They run `rohyctl verify-proof --root <fingerprint> <file>`. The
  check covers the event's content as well as its presence: graphene's inclusion proof, the
  event's stored record exactly as the case holds it, and the raw record, whose hash the stored
  record carries. The file holds this event's full record, including the path it was read from.
  It holds no other event's content.
  - **What the file does reveal about the rest of the case:** how many records it has, this
    event's position among them, and hashes from which nothing can be read back. Someone who
    already holds another event's exact record could use those hashes to confirm that event is
    in the case too.
  - **A proof is only as good as the fingerprint it is checked against.** It must be the
    fingerprint of the seal the proof was built from, kept outside the case. The drawer says
    when no copy of that fingerprint has been saved.
  - **When an event cannot be proven yet, the drawer says why instead of failing.** The event
    arrived after the last seal, or changed after it (a re-ingest moves its occurrence count, a
    backfill rewrites its correlation fields). In each case it offers to seal the case.
  - **A raw record that no longer matches its hash is reported as damage**, not exported.
- **`rohyctl verify-proof`.** rohyctl's rule is now worded as "it never opens a case", and a
  test enforces it. It already reached graphene's store types through `consts`, so "no graphene"
  was no longer accurate.
- **The case fingerprint stays the same until the case changes.** Every seal makes a new
  fingerprint, even over identical content, and rohy used to seal on every close. So a
  fingerprint you exported went stale the next time you closed the app. The seals after an
  ingest and on close are now skipped when nothing was written since the last one. *Seal the
  case* on the Maintenance page still always seals.
- **Every seal is recorded**, not only the ones whose fingerprint was exported. The integrity
  check uses that history to report a case whose database is **older than the last one rohy
  sealed**, such as a restored backup. That image passes every other check, because nothing about
  it is broken. The report stays after the restored database is written to and sealed again. It
  also reports an image that does not descend from the last recorded seal, and breaks in the
  history. A history kept by an earlier build recorded only exported fingerprints,
  so it will show breaks. These are the case checked against its own record: they do not replace
  a fingerprint kept outside the case.
- **"Sealing case…"** now shows in the ingestion bar after an ingest, instead of a silent pause
  after "complete".
- **A custody report for each event.** *Account for this event* in the drawer says, layer by
  layer, what the case can account for about the event and what it cannot: whether it is in the
  current seal, whether its stored and raw records are the sealed ones, where the case's image sits
  in its history of seals, and what the case does not record at all (who sealed it, what was done
  to it). It is a list of gaps, not a verdict.
  - **Paste a fingerprint you kept** to check the case against it. It is compared against the
    case's image whatever the event's state, so a restored backup that no longer holds the event
    is still caught. An earlier fingerprint of the same case is reported as an earlier seal, not as
    a check: keep the current one. The field is never filled in from the case.
  - It does not establish who did anything, that the event is true, the parsed fields, or that the
    record is what the source file held: rohy records no hash of the source file at ingest.
- **Redaction.** *Redact this event…* in the drawer removes an event's content from the case for
  good, records that it was removed, when and why, and keeps it from being ingested again. Use it
  for a lawful erasure request: the case can then tell it apart from evidence that was destroyed.
  - **What goes:** the stored record, the raw record and parsed fields, every other event holding
    the same record (ingested twice, or from another path), every unreferenced copy in the payload
    log, the relations a rule made to it, and the descriptions rohy copied into findings and
    snapshots. The case is then sealed, so its image no longer holds the content either.
  - **What stays:** the event itself, marked *Redacted* with the date and reason; relations an
    analyst drew; your own flags, tags and notes, which the dialog lists so you can edit a note that
    repeats what is being removed; and two hashes of the record, which are how a re-ingest is
    refused. They are not reversible, but they are not secret either.
  - **What rohy cannot reach:** event proofs and exports already saved outside the case, and copies
    the operating system kept (backups, shadow copies, freed disk blocks, the page file).
  - **The reason is required** and is kept in plain text, permanently, with who redacted.
  - A redaction that stops part way says which step failed. It is listed on the Maintenance page,
    with **Finish the redactions**, and the integrity check reports it until it is finished.
  - Refused on a case with no ledger (in memory), while anything else is writing the case (an
    ingest, live capture, a rule run, a maintenance pass), and if its journal cannot be read.
- **Who did it, and signed seals.** rohy now records who works on a case and can sign its seals.
  - **Your name** is asked once, on first start, and kept on this computer in your user profile
    folder (`rohy/analyst.json`), outside every case. Every change you make to a case records it,
    and the case keeps a list of everyone who has worked on it.
  - **Sign this case…** on the Maintenance page creates a signing key, or uses one you have, and
    signs the case's seals from its next start. The key is a file **you keep outside the case
    folder**, locked with a passphrase; rohy refuses one inside it. The page shows the key's
    fingerprint: keep a copy somewhere else. Someone checking a seal compares that fingerprint
    with the one the seal names — the case's own list of keys proves nothing by itself.
  - **A signed case asks for the passphrase when it opens.** Without it, the case opens
    read-only, and you can unlock the key later on the Maintenance page. *Continue this case
    unsigned* is there for a lost key; it needs a reason, which is recorded.
  - **A signed case whose log changed while rohy was closed** — after a crash, or an edit — says
    so before it opens, and nothing is signed over it until you accept it with a reason.
  - **The custody report has a Signing layer**: whether the current seal is signed by a key the
    case lists, and whether the case's record of its signed seals is unbroken.
  - **An audit log** (`graphene.audit` in the case folder) records every seal, redaction, ingest,
    proof export and signing change. It holds ids, counts and the reasons you gave — never event
    content or file paths.
  - Several analysts can work on one case, each with their own key: *Add my key* on opening it.
- **The integrity check says what the case can prove about who did what.**
  - **Who signs the case:** whether its seals are signed, and if not, how many of the recorded seals
    are unsigned. A signed case gets the same verdict on its current seal as the custody report.
    A case that stopped signing says when, by whom and why.
  - **This session:** read-only, recovery, an audit log that is off, or actions it could not
    record. In a read-only session the check offers no action that would be refused, and the
    Maintenance page disables those buttons.
  - **Removal records:** whether the redaction ledger holds together, and whether each record is
    signed by a key the case lists. A record written without a signature while the case was
    signing is an error.
  - **Checkpoints:** none has been published, so anyone able to rewrite the case's files can rewrite
    its whole history consistently. A fingerprint you keep elsewhere covers the seal, not the rest.
  - Anything the check could not read is listed as not checked, never as fine.
- **Checkpoints outside the case.** *Publish a checkpoint…* on the Maintenance page seals the case
  if it has unsealed changes, records a checkpoint, and saves `rohy-checkpoint-<n>.json`. The
  checkpoint fixes the seal, the audit log and the removal records in one digest.
  - **Publishing is yours.** Put the digest somewhere this case's rewriter cannot reach: a
    timestamping authority, opposing counsel, an organisational log, a printed sheet in a safe.
    rohy publishes nothing.
  - **Record a receipt** with what that place gave back. It is the case's own record, a pointer to
    follow, never proof.
  - **The integrity check states the window.** It reports what was recorded since the last checkpoint
    with a receipt, and a checkpoint record that was removed or edited is an error.
  - **Check a digest you hold** compares the case with a digest taken from where it was published.
    It is the one check rohy runs that does not rest on the case. The event drawer's "fingerprint
    you kept" accepts a checkpoint digest too.
- **An integration suite.** `go test -tags=integration ./backend/integration/` runs the real
  pipeline on the disk backend over the committed log: ingest, every built-in rule against a golden
  file, rule export and import into a second case, rebuilds, re-ingests, the correlation backfill,
  recovery from an interrupted build, and a build-side memory test. `go run ./tools/profile`
  writes CPU and heap profiles of ingest, build and open at chosen sizes.
- **A query console.** The new Query page (Alt+8) asks the case about its events or relations:
  - **Conditions** on event ID, provider, channel, computer, user, source, time, text, occurrences,
    relations, rule, algorithm, who made a relation, graph, match or event.
  - **Answers** list the matches or count them by a field.
  - **Every answer says what it left out:**
    - it shows at most 1000 rows or groups, and gives the full total beside them
    - an events answer says whether undated events were counted
    - an answer says when the case changed while it ran
  - **Read-only, and structured:** there is no query language, and nothing on the page writes.
- **The raw rule editor pairs brackets.** The bracket beside the caret and its partner are shaded,
  and one with no partner is shaded red. Typing `{`, `[` or `"` closes it, typing the closer steps
  over it, Backspace in an empty pair removes both halves, and Enter keeps the indent. None of it
  applies inside a string.
- **Format on save.** A checkbox in the rule editor lays the rule out in the house style as it is
  saved. It is off unless you turn it on, a rule that fails its check is never rewritten, and field
  order and fields this version does not read are kept.
- **A VS Code extension for rule files** (`tools/vscode-rohy`). With `rohyctl` installed, it
  underlines problems as you type, using the check the application loads rules with, and **Format
  Document** lays a rule out in the house style. Without it, files are checked against the rule
  schema, and the status bar says that check is partial. It has no dependencies, and it runs only a
  `rohyctl` named in your user settings or found on `PATH`, never one an opened folder points to.
- **`rohyctl validate --json`** prints one report for a program to read: every file, with each error
  and warning's code, field, line and column.

### Fixed

- **Tab now accepts a suggestion in the raw rule editor.** It used to insert two spaces instead.
  Moving the caret out of the word being completed now closes the list, which used to stay open and
  could write the suggestion where the caret had been.
- **`rohyctl validate` no longer fails on a rules folder.** It read `rules-state.json`, the file
  that holds the rules' on/off switches, as a broken rule, so every run over `rohy-data/rules`
  failed. The application always skipped it; `validate` and `format` now skip it in a folder too,
  and still check it when it is named on its own.

- **Errors now say whose fault they are.** A failed call used to reach the screen as its message
  alone, so a refused write, a busy case and a disk failure read alike. Each now carries its code,
  and the Query page uses it to tell a refused query from a case that could not answer.

- **The same file could give a different graph on each ingest.** Chunks were written in the order
  the parse workers finished, so the same file got different event ids each time, and the rule
  engine orders events at the same instant by id. Two such events could be joined on one ingest
  and not on the next, and a duplicate's kept copy changed with them. A file is now written in its
  own order: the same file gives the same ids, the same graph and the same match ids. Several
  files are still written in the order you ingest them.
- **A crash during a build could hide relations from their rule.** A relation's index entries are
  written one at a time; the repair checked only the one that lists it under its graph. A relation
  that its graph listed and its rule did not was never repaired, and the integrity check did not
  count it. Both now check every entry.
- Closing a case twice no longer fails with `wal closed`.

- **The event drawer showed no raw record and no parsed fields** (since v0.1.0). The binding
  returned the stored record, which deliberately leaves both out, so the drawer never received
  them. It now carries them beside it.

- **Temporal chains with a repeated event ID were assembled wrongly.** When one event could
  satisfy several steps (`4625, 4625, 4625, 4624`), the chain was rebuilt from overwritten
  pointers: the built-in *Failed Logon Burst Then Success Within Five Minutes* drew
  `F2 → F2 → F3 → S` — an edge from an event to itself, with the first failure missing — and
  `window_total` was measured from the wrong first step. Rebuilding the graphs that rule (or any
  temporal rule with a repeated ID) produced is recommended.
- **`match_scope` was read by `sequence` rules.** The reversal recorded in RULES.md §5 removed
  that combination, but the matcher still honoured it, so one rule file could match across hosts
  on v0.2.0 and per host on v0.1.0. A `sequence` rule is now always matched per computer, and a
  rule that sets `match_scope: "global"` gets a warning that the field has no effect. **This
  narrows any such rule you wrote** — switch it to `field` or `temporal` with a shared field if
  cross-host matching was the intent.
- **An unreadable case fingerprint reported as "never sealed".** It is now its own error, with
  the read failure shown, and no seal button — sealing would write over whatever caused it.
- A latent data race in the shared prepared dataset when a rule asked for a scope the build had
  not prepared.

- **Deleting an event broke every canvas that still held it.** Another graph's canvas, and this
  one's until *Save layout*, failed to load, and *Arrange*, *Group* and *Take snapshot* failed with
  it. Pressing *Save layout* on the empty canvas then erased its positions and viewport. Deleting an
  event now removes it from every saved canvas, and a canvas that still names a gone event loads
  without it. If a canvas file cannot be updated, the error names it; the event is deleted anyway.
- **A canvas that failed to load could be saved over the real one.** If a graph's canvas could not
  be read (a damaged canvas file, a store error), it opened empty, and *Save layout* or *Keep* in
  *Arrange* wrote the empty canvas over the saved positions. Both now stay unavailable until the
  canvas has loaded in full. *Load saved mapping* tries again and keeps anything added meanwhile.
- **Arrange, Group, Take snapshot and the canvas cards no longer read raw records.** None of them
  shows one, and reading them was most of the wait: *Arrange* on 5 000 events is about four to five
  times faster (see Under the hood). One unreadable raw record no longer empties a canvas; the event
  drawer and *Save proof…* still report it.
- **The correlation backfill now seals the case when it rewrote any event**, also when it was
  stopped or failed part way. It used to leave every rewritten event in memory and in the write log
  until the next seal: about 58 MiB at 100 000 events. The seal is recorded in the fingerprint
  history like every other seal. The notice before the backfill says the fingerprint will change,
  and the page says when it has. A seal that does not
  finish is shown beside the backfill's result; what the pass rewrote is kept either way.
- **A snapshot restore could draw the same link twice.** A link counted as still there only while
  it kept the id it had in the snapshot. A link the restore had re-created, one drawn again by hand,
  or one a rule found again after the rules were re-run was offered again, and restoring a second
  time drew it twice. A link is now matched by the two events it joins, its direction and its type,
  and one restore writes a link once even when the snapshot recorded it twice.
- **The layout note counted cycles it had not found.** When no event was free to place next,
  *Sequence* and *Lineage* forced one and reported each as a broken cycle, or as a group with no root.
  One cycle could read as two, and *Lineage* said every event in a group had a parent inside it when
  one did not. The note now counts forced placements.
- **Replay said approximate links are drawn as a weaker claim.** They are drawn like any other link,
  and the caption now says so.
- **Stop was offered during a case check**, where it did nothing, and could stop a backfill started
  from the Rules page instead. It is now offered only while a correlation backfill runs.
- **The relationship heatmap kept the timeline's old range.** After the timeline reloaded over a
  changed case (*Group by*, *Retry*), the strip's columns no longer sat over the histogram's. It now
  follows the timeline's range, and a *Retry* that changed nothing does not fetch it again.
- **Notes, tags, annotation text, layer names and snapshot labels now count characters**, as their
  messages say, not bytes. A note in Japanese or Russian used to get a third or half of the room one
  in English gets, and was refused with no warning. A long tag or layer name was cut by bytes, which
  could split a character; it is now cut between characters, and a cut tag that ends in a space is
  trimmed so it can still be filtered.
- **A failed finding or annotation save no longer leaves the screen ahead of the disk.** When
  writing a finding or an annotation failed, the screen kept the change that was never saved. It
  now shows what the file holds. Deleting one annotation can no longer delete another made in the
  same millisecond.
- **A layout refused for the node cap, an unknown profile, field or grouping is the request's
  fault.** It was coded `internal_error`; it is now `parse_error`. The message is unchanged.
- **A correction to the v0.2.0 notes:** the *Resource* layout makes one column per value of the
  correlation field chosen beside it (`logon_id` unless another is chosen): a session, an account, a
  process, an address or a service. No correlation field holds the host, so there is no column per
  host.

### Changed

- **Every disk case now keeps a redaction ledger** (`graphene.redactions` in the case folder). A
  case written by an earlier build opens unchanged, with the same fingerprint; the ledger is empty
  until something is redacted.
- **Writes to a case refuse while a redaction runs**, and a redaction refuses while anything else
  writes: an ingest or live capture, a rule run or rule test, a maintenance pass, a snapshot, a
  finding, annotation or graph edit. The refusal names what is running. A redaction takes a few
  seconds; nothing ever waits behind one.
- **An ingest refuses** if the redaction journal (`rohy-data/redactions/journal.json`) cannot be
  read, and names the file. It lists what must not be ingested again, so it is never replaced.
- **Every disk case now keeps an audit log** (`graphene.audit`). A case written by an earlier build
  opens unchanged, with the same fingerprint; the log starts with the next seal.
- **The case opens after a short session step** when there is something to ask: your name on first
  start, and how to open a signed case. A case you have opened before, never signed, opens as it
  always did.
- **A read-only session refuses every change**, names why, and is marked in the status bar. The
  image check still runs.
- **The fingerprint history records who sealed and the seal's signature** (format 2). A build that
  reads only format 1 keeps a format-2 history aside rather than drop those fields by rewriting it;
  no released build reads format 1.
- **Some writes now commit once instead of once per relation**: a snapshot restore, the migration of
  relations from before named graphs, and clearing a graph whose relations had drifted.

- **A raw record that cannot be read, or a damaged sealed image, is reported as damage.** *Save
  proof…* used to fail with a generic error in both cases. It now says which, the same way as a
  raw record that no longer matches its hash, and offers no seal: sealing repairs neither.
- **An unreadable fingerprint history is kept, not overwritten.** It used to be treated as empty
  and replaced at the next seal, losing the record of which fingerprints were exported. It is now
  moved aside as `roots.json.unreadable-<time>`, and the integrity check says where on every
  later open. A history that could not be read at all is left in place, and no seals are recorded
  until it can be.
- **Three network correlation fields:** `dst_ip`, `dst_port` and `protocol`, plus `src_ip` as a
  second name for `ip_address`. Addresses are normalized to one spelling (RFC 5952, v4-mapped
  unmapped, zone dropped), and the unspecified address is absent rather than a value shared by
  every local logon. RULES.md §9 has the details.
- **WFP events are oriented by initiator.** For an inbound 5156, `ip_address` is the remote peer
  and `dst_ip`/`dst_port` this machine's listener, matching what the fields mean everywhere else.
- **Parser faults are contained.** A parser that fails on hostile input now skips and reports the
  chunk or record it was reading instead of ending the application, and a damaged EVTX chunk
  reports how many of its declared records could not be read instead of losing them silently.
- **A deep check verifies the image under the maintenance lock**, as *Check the case has not
  changed* does. While a backfill or a fix runs, the report says the image was not verified. While a
  deep check reads the image (about 0.6 s at 100 000 events), a backfill, a fix, *Seal the case* or
  a checkpoint publish is refused as already running, and a redaction is refused because the case
  is busy. The Maintenance page shows itself busy for that time, including the re-check it runs
  after a fix, so its buttons are not offered then; only a backfill started from the Rules page in
  that window meets the refusal.

### Under the hood

- **Verifying the image stays a button.** Run at every open, it would add **617 ms and 449 MB** of
  allocation to each launch of a 100 000-event case. Off is a reported state, never a silent one.
- **A case seals once per ingest request**, never per file, because a seal costs the whole case
  (about 1.3 s at 100 000 events) however little was added. A case with nothing new is not sealed
  again: asking costs about a millisecond.
- **Attribution costs nothing measurable.** Every write is now an attributed transaction, and a
  512-event batch takes the same time inside the run-to-run spread, signed or not.
- **GrapheneDB 0.4.0 and Wails 2.13.0**, both unchanged from v0.2.0.
- **Ten new backend packages** (`analyst`, `anchor`, `custody`, `durable`, `eventproof`, `keyfile`,
  `query`, `redaction`, and `synth` and `integration` for tests), the `tools/profile` harness, and
  33 Go packages in total.
- **A canvas reads node records only.** *Arrange*, *Group*, snapshots and the canvas cards no longer
  read raw records. The layout binding at 5 000 events drops from 133–165 ms to about 25–40 ms, and
  from 46–50 MB to 8–12 MB allocated, on the same machine.
- **1,192 Go tests, 10 fuzz targets and 65 benchmarks; 852 frontend tests; 58 extension tests.**
  The new benchmarks measure auto-layout, the relationship heatmap and the backfill on the disk
  backend (PERFORMANCE.md §21).

### Compatibility

- **`format_version` is still `1`.** Every v0.2.0 rule loads. Some match differently: a `sequence`
  rule with `match_scope: "global"` now matches per computer, temporal chains with a repeated event
  ID are assembled correctly, and `ip_address` values are normalized and, on inbound WFP events,
  oriented by initiator (see Fixed and Changed).
- **A v0.2.0 case opens unchanged, with the same fingerprint.** The redaction ledger and the audit
  log are added empty. Its events report stale correlation keys until the backfill runs (see the
  action above).
- **The fingerprint history (`roots.json`, format 2) is new in this release.** v0.2.0 neither reads
  nor writes it, so a seal v0.2.0 makes is not recorded. If a case goes back to v0.2.0 and returns,
  the integrity check reports that its image does not follow the last recorded seal.
- **New portable files are versioned from 1**: event proofs (checked with this release's
  `rohyctl verify-proof`) and signing keys.

### Known issues

- **A damaged canvas file blocks deleting events.** While one canvas file cannot be read, every
  event deletion reports an error naming it. The event is deleted and every other canvas is updated,
  but the card stays until the file is fixed or removed.
- **A restore can still offer a link again** after a crash in the middle of applying it, and a
  restore applied while rules run or a link is drawn can still draw one link twice.
- **A backfill started from the Rules page is not shown on the Maintenance page**, and Stop is not
  offered for it there. During the seal at the end of a backfill, Stop is shown and does nothing.
- **Layouts force the lowest id, not a node on the cycle.** A graph with a cycle can need more forced
  placements than it has cycles, and an event can land in the same column as its cause. The note
  counts the forced placements correctly.
- **On macOS and Linux, the tag field may take more than 64 characters.** The backend then cuts the
  tag, possibly inside a combined character such as an emoji sequence. This has not been checked on
  those builds.
- Tags and layer names cut by bytes in an earlier release keep the replacement character (�) the
  cut left.

### Not in this release

Stated rather than left to be discovered:

- Code signing and notarization. Windows SmartScreen and macOS Gatekeeper will still warn.
- Manual verification on Linux and macOS — both build in CI, neither has been exercised by hand.
- `rohyctl` in the release downloads. A recipient checking a proof builds it with
  `go build ./cmd/rohyctl`.
- The VS Code extension on the Marketplace. It is packaged by hand from `tools/vscode-rohy`; its
  README says how.
- In the query console: an expression language, saved queries, export, and conditions on findings.
- Snapshot thumbnails, and drawing regions or arrows directly on the canvas — both render, but only
  notes can be authored, from the layer panel.
- The integration suite and the extension's tests in CI. They run in the local gate.

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
- Snapshot thumbnails, and drawing regions or arrows directly on the canvas — both render, but only
  notes can be authored, from the layer panel.
- Code signing and notarization. Windows SmartScreen and macOS Gatekeeper will still warn.
- Manual verification on Linux and macOS — both build in CI, neither has been exercised by hand.

---

<div align="center">

Releases are named after constellations. v0.1.0 was **Cassiopeia**; v0.2.0 was **Andromeda**, her
daughter; v0.3.0 is **Perseus**, who stands beside them both.

The version this build reports comes from the linker, and is written down in exactly one place —
[README.md](README.md).

</div>
