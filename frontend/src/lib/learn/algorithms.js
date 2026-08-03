// The explainer's content model: one worked scenario per correlation algorithm, expressed as
// a sequence of drawable steps.
//
// WHY THIS IS DATA RATHER THAN DRAWING CODE
//
// A diagram that explains an algorithm is a claim about how that algorithm behaves, and a
// hand-drawn one is a claim nothing checks. The failure is quiet and bad: the engine changes,
// the picture does not, and the page teaches something that is no longer true — to exactly the
// people who came here because they did not already know.
//
// So each scenario is built from the SAME event sets and the SAME expected outcomes as the Go
// tests in backend/autograph. algorithms.test.js asserts those outcomes here, so a change to
// what an algorithm does breaks this file's tests rather than silently rewriting what the page
// claims. Where a scenario mirrors a specific Go test, its `mirrors` field names it.
//
// A step is a complete snapshot — every event's state and every edge — rather than a delta.
// That makes stepping backwards, jumping, and rendering a static final frame under reduced
// motion all the same operation: draw step N.

/** @typedef {'idle'|'scanning'|'matched'|'excluded'|'rejected'|'dimmed'} EventState */
/** @typedef {'forming'|'kept'|'rejected'} EdgeState */

/**
 * @typedef {object} ScenarioEvent
 * @property {string} id
 * @property {string} eventId  the Windows event ID, e.g. "4625"
 * @property {number} t        seconds from the scenario's start; drives x position
 * @property {number} lane     which row the chip sits in at rest
 * @property {string} [tag]    a short annotation shown under the chip (a logon id, a PID)
 * @property {string} [note]   longer text for the accessible description
 */

/**
 * @typedef {object} Step
 * @property {string} title
 * @property {string} body
 * @property {Record<string, EventState>} states  event id → state (absent means 'idle')
 * @property {Array<{from:string,to:string,state:EdgeState,label?:string}>} edges
 * @property {number} [scanAt]   seconds; draws the sweep line
 * @property {number} [lanes]    override the lane count for this step (bucketing)
 * @property {Record<string, number>} [moveTo]  event id → lane, for steps that regroup
 * @property {string} [callout]  a short highlighted conclusion
 */

/**
 * @typedef {object} Scenario
 * @property {string} id
 * @property {string} algorithm
 * @property {string} title
 * @property {string} tagline
 * @property {string} establishes  what a match proves — the RULES.md section 8 claim
 * @property {string} rule         the rule JSON shown beside the diagram
 * @property {string[]} laneLabels
 * @property {ScenarioEvent[]} events
 * @property {Step[]} steps
 * @property {string} mirrors      the Go test this scenario's outcome is taken from
 */

const HOST = 'HOST-A';

// --- sequence -----------------------------------------------------------------------------

/**
 * The worked example from the low-level design: ["4625","4625","4624"] over seven events on
 * one host, showing that steps need not be adjacent and that matching does not overlap.
 * @returns {Scenario}
 */
function sequenceScenario() {
  const events = [
    { id: 'a', eventId: '4625', t: 0, lane: 0 },
    { id: 'b', eventId: '4672', t: 1, lane: 0, note: 'not part of the sequence' },
    { id: 'c', eventId: '4625', t: 2, lane: 0 },
    { id: 'd', eventId: '4624', t: 3, lane: 0 },
    { id: 'e', eventId: '4625', t: 4, lane: 0 },
    { id: 'f', eventId: '4625', t: 5, lane: 0 },
    { id: 'g', eventId: '4624', t: 6, lane: 0 },
  ];

  const steps = [
    {
      title: 'Seven events, one host, in time order',
      body:
        'Correlation always runs inside a scope — by default the computer the events came from, ' +
        'so a chain is never assembled across unrelated machines.',
      states: {},
      edges: [],
    },
    {
      title: 'Step 1 of the sequence: find a 4625',
      body: 'The scan runs forward through time and takes the first event matching the current step.',
      states: { a: 'matched' },
      edges: [],
      scanAt: 0,
    },
    {
      title: 'Steps need not be adjacent',
      body:
        'The 4672 between them is simply skipped. The rule says which events must occur in which ' +
        'order — not that nothing may happen in between.',
      states: { a: 'matched', b: 'dimmed' },
      edges: [],
      scanAt: 1,
    },
    {
      title: 'Step 2: the second 4625',
      body: 'An edge is emitted between each consecutive pair of matched events, not just end to end.',
      states: { a: 'matched', b: 'dimmed', c: 'matched' },
      edges: [{ from: 'a', to: 'c', state: 'kept' }],
      scanAt: 2,
    },
    {
      title: 'Step 3: the 4624 completes the match',
      body: 'Three matched events produce two edges. This occurrence is now finished.',
      states: { a: 'matched', b: 'dimmed', c: 'matched', d: 'matched' },
      edges: [
        { from: 'a', to: 'c', state: 'kept' },
        { from: 'c', to: 'd', state: 'kept', label: 'then succeeds' },
      ],
      scanAt: 3,
    },
    {
      title: 'Matching does not overlap',
      body:
        'The next occurrence starts strictly after the previous one ended — the scan resumes past ' +
        'the 4624, never reusing an event it has already matched.',
      states: { a: 'dimmed', b: 'dimmed', c: 'dimmed', d: 'dimmed', e: 'scanning' },
      edges: [
        { from: 'a', to: 'c', state: 'kept' },
        { from: 'c', to: 'd', state: 'kept', label: 'then succeeds' },
      ],
      scanAt: 4,
    },
    {
      title: 'A second occurrence, and the run ends',
      body: 'Two matches, four edges. Each scanned segment is disjoint, so the whole pass is linear.',
      states: { a: 'matched', b: 'dimmed', c: 'matched', d: 'matched', e: 'matched', f: 'matched', g: 'matched' },
      edges: [
        { from: 'a', to: 'c', state: 'kept' },
        { from: 'c', to: 'd', state: 'kept', label: 'then succeeds' },
        { from: 'e', to: 'f', state: 'kept' },
        { from: 'f', to: 'g', state: 'kept', label: 'then succeeds' },
      ],
      callout:
        'A match establishes a temporally ordered pairing on one host — and nothing more. ' +
        'It does not establish that these events concern the same user, account or session.',
    },
  ];

  return {
    id: 'sequence',
    algorithm: 'sequence',
    title: 'Sequence',
    tagline: 'These event IDs, in this order, on one host.',
    establishes: 'A temporally ordered pairing within the scope.',
    rule: `{
  "name": "Failed Logons Then Success",
  "sequence": ["4625", "4625", "4624"],
  "labels": ["", "then succeeds"]
}`,
    laneLabels: [HOST],
    events,
    steps,
    mirrors: 'autograph.TestSequenceGreedyWithNoise / TestSequenceNonOverlapping',
  };
}

// --- field --------------------------------------------------------------------------------

/**
 * Two interleaved logon sessions plus an event carrying no logon id at all. Mirrors
 * TestFieldCorrelationRequiresSharedValue and TestFieldCorrelationExcludesEventsWithNoValue.
 * @returns {Scenario}
 */
function fieldScenario() {
  const events = [
    { id: 'a', eventId: '4625', t: 0, lane: 0, tag: '0xaaa' },
    { id: 'b', eventId: '4625', t: 1, lane: 0, tag: '0xbbb' },
    { id: 'c', eventId: '4624', t: 2, lane: 0, tag: '0xbbb' },
    { id: 'd', eventId: '4624', t: 3, lane: 0, tag: '0xaaa' },
    { id: 'e', eventId: '4625', t: 4, lane: 0, tag: '—', note: 'no logon id recorded' },
  ];

  const steps = [
    {
      title: 'Two logon sessions, interleaved',
      body:
        'Each event carries the logon session it belongs to. Read as a flat sequence they are ' +
        'indistinguishable; read as sessions they are two separate stories.',
      states: {},
      edges: [],
    },
    {
      title: 'What sequence matching alone would do',
      body:
        'It takes the first 4625 and the first 4624 that follows it — which here belong to two ' +
        'different sessions. The pairing is real in time and wrong in substance.',
      states: { a: 'matched', c: 'matched' },
      edges: [{ from: 'a', to: 'c', state: 'rejected', label: 'crosses two sessions' }],
      callout: 'This is the limitation field correlation exists to remove.',
    },
    {
      title: 'Partition by the matched field first',
      body:
        'Events are grouped by their value for every field in match_fields. Only events sharing ' +
        'all of them can take part in the same match.',
      states: {},
      edges: [],
      lanes: 3,
      moveTo: { a: 0, d: 0, b: 1, c: 1, e: 2 },
    },
    {
      title: 'An event with no value is excluded, not grouped',
      body:
        'Windows writes “-” and the null SID constantly. Treating those as a shared value would ' +
        'bucket every event that happens not to carry the field together and correlate them all ' +
        'with each other — a rule that looks precise and is a false-positive engine.',
      states: { e: 'excluded' },
      edges: [],
      lanes: 3,
      moveTo: { a: 0, d: 0, b: 1, c: 1, e: 2 },
      callout: 'The run reports how many events it left out, so a small result cannot be mistaken for a rare pattern.',
    },
    {
      title: 'Now match inside each session',
      body: 'The same ordered scan as before, run once per group. Each group is already in time order.',
      states: { a: 'matched', d: 'matched', b: 'matched', c: 'matched', e: 'excluded' },
      edges: [
        { from: 'a', to: 'd', state: 'kept', label: 'logon_id=0xaaa' },
        { from: 'b', to: 'c', state: 'kept', label: 'logon_id=0xbbb' },
      ],
      lanes: 3,
      moveTo: { a: 0, d: 0, b: 1, c: 1, e: 2 },
      callout:
        'A match now establishes that these events belong to one logon session — a claim about the ' +
        'evidence, not just about the clock.',
    },
  ];

  return {
    id: 'field',
    algorithm: 'field',
    title: 'Field',
    tagline: 'The same order — and the same session, account or process.',
    establishes: 'Ordering within the scope, plus a shared value for every matched field.',
    rule: `{
  "format_version": 2,
  "name": "Failed Logons Then Success, Same Session",
  "algorithm": "field",
  "sequence": ["4625", "4624"],
  "match_fields": ["logon_id"]
}`,
    laneLabels: ['session 0xaaa', 'session 0xbbb', 'no logon id'],
    events,
    steps,
    mirrors: 'autograph.TestFieldCorrelationRequiresSharedValue / …ExcludesEventsWithNoValue',
  };
}

// --- temporal -----------------------------------------------------------------------------

/**
 * The case a forward-greedy matcher gets wrong. Mirrors
 * TestTemporalFindsTheMatchGreedyWouldMiss exactly, including the timings.
 * @returns {Scenario}
 */
function temporalScenario() {
  const events = [
    { id: 'a', eventId: '4625', t: 0, lane: 0, tag: '10:00:00' },
    { id: 'b', eventId: '4625', t: 600, lane: 0, tag: '10:10:00' },
    { id: 'c', eventId: '4624', t: 630, lane: 0, tag: '10:10:30' },
  ];

  const steps = [
    {
      title: 'Two failures and a success, within a one-minute window',
      body:
        'The rule asks for a 4625 followed by a 4624 no more than a minute apart. Note the real ' +
        'spacing: ten minutes, then thirty seconds.',
      states: {},
      edges: [],
    },
    {
      title: 'A forward-greedy scan anchors on the first 4625',
      body: 'It takes the earliest event matching the current step — the same rule the sequence algorithm uses.',
      states: { a: 'scanning' },
      edges: [],
      scanAt: 0,
    },
    {
      title: '…and the window rejects it',
      body:
        'The 4624 arrives 10 minutes 30 seconds later, far outside the one-minute window. Greedy ' +
        'matching has already walked past the alternative.',
      states: { a: 'rejected', c: 'rejected' },
      edges: [{ from: 'a', to: 'c', state: 'rejected', label: 'Δt 10m30s > 1m' }],
      callout: 'Two obvious repairs are both wrong: giving up here drops real matches, and restarting from every failed anchor is quadratic.',
    },
    {
      title: 'But there was a match',
      body:
        'The second 4625 is only 30 seconds before the 4624. A correct matcher has to find it ' +
        'without re-scanning from every candidate.',
      states: { b: 'scanning', c: 'scanning' },
      edges: [],
    },
    {
      title: 'One sweep, keeping the most recent completion of each step',
      body:
        'As the sweep passes each event it records it as the latest event satisfying its step. A ' +
        'later completion is always at least as good as an earlier one — it has a larger timestamp, ' +
        'so it satisfies the window for strictly more future events.',
      states: { a: 'dimmed', b: 'matched' },
      edges: [],
      scanAt: 600,
    },
    {
      title: 'The 4624 completes against the nearer 4625',
      body: 'One pass, no backtracking, no restarts — and the match greedy walked past is found.',
      states: { a: 'dimmed', b: 'matched', c: 'matched' },
      edges: [{ from: 'b', to: 'c', state: 'kept', label: 'Δt 30s ≤ 1m' }],
      scanAt: 630,
      callout:
        'Because of this, temporal matching is latest-anchored where sequence matching is ' +
        'earliest-anchored. They are different algorithms, not variants.',
    },
  ];

  return {
    id: 'temporal',
    algorithm: 'temporal',
    title: 'Temporal',
    tagline: 'The same order, inside a bounded time window.',
    establishes: 'Ordering within the scope, plus a bounded gap between consecutive steps.',
    rule: `{
  "format_version": 2,
  "name": "Burst Then Success",
  "algorithm": "temporal",
  "sequence": ["4625", "4624"],
  "window_within": "1m"
}`,
    laneLabels: [HOST],
    events,
    steps,
    mirrors: 'autograph.TestTemporalFindsTheMatchGreedyWouldMiss',
  };
}

// --- lineage ------------------------------------------------------------------------------

/**
 * PID reuse: one PID held by two different processes at two different times. Mirrors
 * TestLineageDoesNotLinkAcrossPIDReuse.
 * @returns {Scenario}
 */
function lineageScenario() {
  const events = [
    { id: 'a', eventId: '4688', t: 0, lane: 0, tag: 'explorer.exe → 0x100' },
    { id: 'b', eventId: '4689', t: 100, lane: 0, tag: '0x100 exits' },
    { id: 'c', eventId: '4688', t: 200, lane: 0, tag: 'svchost.exe → 0x100' },
    { id: 'd', eventId: '4688', t: 300, lane: 0, tag: 'cmd.exe, parent 0x100' },
  ];

  const steps = [
    {
      title: 'One PID, two processes, two different times',
      body:
        'Windows reuses process IDs aggressively — a busy host cycles the whole PID space in hours. ' +
        'Here 0x100 is explorer.exe, exits, and is later handed to svchost.exe.',
      states: {},
      edges: [],
    },
    {
      title: 'A new process names 0x100 as its parent',
      body: 'cmd.exe is created at 10:05, recording parent PID 0x100. Which 0x100?',
      states: { d: 'scanning' },
      edges: [],
      scanAt: 300,
    },
    {
      title: 'Joining on the PID alone matches both',
      body:
        'And it produces a confidently wrong graph, because the wrong edge looks exactly like the ' +
        'right one. Nothing downstream can tell them apart.',
      states: { a: 'rejected', c: 'matched', d: 'matched' },
      edges: [
        { from: 'a', to: 'd', state: 'rejected', label: 'wrong: already exited' },
        { from: 'c', to: 'd', state: 'forming' },
      ],
    },
    {
      title: 'A PID is only an identifier for an interval',
      body:
        'Each creation opens a lifetime for its PID, closed by that PID’s exit or by the next ' +
        'creation of it. The parent is the process whose interval CONTAINED the child’s creation time — ' +
        'a lookup in time, not in number.',
      states: { a: 'dimmed', c: 'matched', d: 'matched' },
      edges: [{ from: 'c', to: 'd', state: 'kept', label: 'ppid=0x100, alive 10:03–' }],
      scanAt: 300,
      callout:
        'Where no interval contains the child — usually because the parent was created before your ' +
        'log begins — nothing is emitted and the count is reported. Nothing is guessed.',
    },
  ];

  return {
    id: 'lineage',
    algorithm: 'lineage',
    title: 'Lineage',
    tagline: 'Process ancestry, resolved through time rather than by PID.',
    establishes: 'That the child was created by a process alive and holding that PID at that moment.',
    rule: `{
  "format_version": 2,
  "name": "Process Ancestry",
  "algorithm": "lineage",
  "lineage_create_ids": ["4688"]
}`,
    laneLabels: [HOST],
    events,
    steps,
    mirrors: 'autograph.TestLineageDoesNotLinkAcrossPIDReuse',
  };
}

/** SCENARIOS is the page's content, in the order rules should be learned in. */
export const SCENARIOS = Object.freeze([
  sequenceScenario(),
  fieldScenario(),
  temporalScenario(),
  lineageScenario(),
]);

/**
 * kept returns the edges a scenario finishes with — the ones the algorithm would actually
 * persist. It is what the tests assert against the Go outcomes.
 * @param {Scenario} scenario
 * @returns {Array<[string,string]>}
 */
export function keptEdges(scenario) {
  const last = scenario.steps[scenario.steps.length - 1];
  return last.edges.filter((e) => e.state === 'kept').map((e) => [e.from, e.to]);
}

/**
 * layout maps a scenario's events to drawable coordinates in a 0..1 box, so the component
 * only has to scale. Time drives x; the step's grouping drives y.
 *
 * Positions are computed by RANK rather than by raw time. Real logs are spiky — the temporal
 * scenario spans ten minutes with two events thirty seconds apart — and a linear time axis
 * would pile those two on top of each other and make the picture unreadable. The tags carry
 * the real times, and the sweep line still moves in time order, so nothing is misstated.
 *
 * @param {Scenario} scenario
 * @param {Step} step
 * @returns {Array<{id:string, x:number, y:number, lane:number}>}
 */
export function layout(scenario, step) {
  const ordered = [...scenario.events].sort((a, b) => a.t - b.t);
  const lanes = step.lanes ?? 1;
  const laneOf = (id) => step.moveTo?.[id] ?? scenario.events.find((e) => e.id === id)?.lane ?? 0;

  // Within a lane, events keep their time order and spread evenly across the width, so a lane
  // holding two events does not squeeze them into the left edge.
  const byLane = new Map();
  for (const e of ordered) {
    const lane = laneOf(e.id);
    if (!byLane.has(lane)) byLane.set(lane, []);
    byLane.get(lane).push(e);
  }

  const out = [];
  for (const [lane, members] of byLane) {
    members.forEach((e, i) => {
      const span = members.length === 1 ? 0.5 : i / (members.length - 1);
      out.push({
        id: e.id,
        x: 0.08 + span * 0.84,
        y: lanes === 1 ? 0.5 : (lane + 0.5) / lanes,
        lane,
      });
    });
  }
  return out;
}

/**
 * clampStep keeps a step index inside a scenario's range.
 *
 * It exists because the player's index and the scenario it indexes into are updated at
 * DIFFERENT TIMES. Switching algorithm changes the scenario prop, and the template re-renders
 * with it before the effect that resets the index has run — so for one frame a 4-step scenario
 * can be asked for step 5, left over from a 7-step walkthrough that was still playing.
 *
 * Unclamped, that read is `undefined` and the next property access throws, killing the
 * component mid-render: playback simply stops, and only when a reader switches tabs late
 * enough in a longer walkthrough, which is what made it look intermittent.
 *
 * Clamping rather than guarding at each use is deliberate — there are four deriveds reading
 * the step and each would need the same defence.
 *
 * @param {Scenario} scenario
 * @param {number} index
 * @returns {number}
 */
export function clampStep(scenario, index) {
  const total = scenario?.steps?.length ?? 0;
  if (total === 0) return 0;
  if (!Number.isFinite(index)) return 0;
  return Math.max(0, Math.min(Math.trunc(index), total - 1));
}

/**
 * CHIP_STATE_ORDER and EDGE_STATE_ORDER fix the order legend entries appear in, so the key
 * does not reshuffle itself as the walkthrough advances — a legend whose rows move is harder
 * to use than one with an irrelevant row in it.
 */
const CHIP_STATE_ORDER = ['scanning', 'matched', 'rejected', 'excluded', 'dimmed'];
const EDGE_STATE_ORDER = ['kept', 'forming', 'rejected'];

/**
 * legendFor returns the marks a scenario actually uses, in a stable order.
 *
 * It is derived rather than hardcoded because the vocabulary differs per algorithm: only the
 * field scenario excludes anything, only lineage and field draw a rejected edge. Listing every
 * possible mark on every diagram would explain states the reader is never going to see, which
 * is its own kind of noise.
 *
 * @param {Scenario} scenario
 * @returns {Array<{kind:'chip'|'edge', state:string}>}
 */
export function legendFor(scenario) {
  const chips = new Set();
  const edges = new Set();
  for (const step of scenario.steps) {
    for (const state of Object.values(step.states ?? {})) chips.add(state);
    for (const edge of step.edges) edges.add(edge.state);
  }
  return [
    ...CHIP_STATE_ORDER.filter((s) => chips.has(s)).map((state) => ({ kind: 'chip', state })),
    ...EDGE_STATE_ORDER.filter((s) => edges.has(s)).map((state) => ({ kind: 'edge', state })),
  ];
}

/**
 * scanX maps a step's sweep position onto the same 0..1 axis the chips use, so the line lands
 * on the event it is describing rather than at an unrelated fraction of the width.
 * @param {Scenario} scenario
 * @param {Step} step
 * @returns {number|null}
 */
export function scanX(scenario, step) {
  if (step.scanAt === undefined) return null;
  const positions = layout(scenario, step);
  const ordered = [...scenario.events].sort((a, b) => a.t - b.t);
  const at = ordered.find((e) => e.t === step.scanAt);
  if (!at) return null;
  return positions.find((p) => p.id === at.id)?.x ?? null;
}
