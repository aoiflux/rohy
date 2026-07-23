// API wrapper layer (P5.3). The ONLY module that talks to the Wails backend.
// Components and stores call these functions; they never touch window.go or the
// runtime directly. Wails injects bound methods at window.go.<pkg>.<Struct>.<Method>
// (here pkg = "api", the Go package name) and each returns a Promise.
//
// When running outside the Wails runtime (e.g. `vite dev` in a plain browser),
// window.go is undefined; calls reject with a clear error and event subscription is a
// no-op, so the UI degrades instead of throwing on load.

import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime.js';

const PKG = 'api';
const EVENTS = 'EventsAPI';
const GRAPH = 'GraphAPI';
const RULES = 'RulesAPI';
const BUILD = 'BuildAPI';
const FINDINGS = 'FindingsAPI';
const SYSTEM = 'SystemAPI';

function bound(struct, method) {
  const go = typeof window !== 'undefined' ? window.go : undefined;
  const fn = go && go[PKG] && go[PKG][struct] && go[PKG][struct][method];
  return typeof fn === 'function' ? fn : null;
}

function call(struct, method, ...args) {
  const fn = bound(struct, method);
  if (!fn) {
    return Promise.reject(new Error(`backend unavailable: ${struct}.${method} (not running inside Wails)`));
  }
  return fn(...args);
}

// isBackendAvailable lets the shell show a friendly notice in browser-only dev.
export function isBackendAvailable() {
  return bound(EVENTS, 'IsIngesting') !== null;
}

// --- Events API ---

/** @param {{source:string, paths?:string[], channels?:string[], idempotent?:boolean, continuous?:boolean}} req */
export function startIngest(req) {
  return call(EVENTS, 'StartIngest', req);
}
/** Live-capture session state + durable per-channel positions (P7).
 * @returns {Promise<{active:boolean, continuous:boolean, channels:string[], positions:Record<string,number>}>} */
export function captureStatus() {
  return call(EVENTS, 'CaptureStatus');
}
/** Clears capture bookmarks so the next run re-reads from the beginning. Empty = all.
 * @param {string} channel */
export function resetCapturePositions(channel) {
  return call(EVENTS, 'ResetCapturePositions', channel || '');
}
export function cancelIngestion() {
  return call(EVENTS, 'CancelIngestion');
}
export function isIngesting() {
  return call(EVENTS, 'IsIngesting');
}
/** Backend-authoritative ingestion lifecycle (P8). @returns {Promise<string>} */
export function ingestState() {
  return call(EVENTS, 'IngestState');
}
/** Halts the pipeline at its next batch boundary, flushing first. */
export function pauseIngestion() {
  return call(EVENTS, 'PauseIngestion');
}
/** Continues a paused pipeline — no gap, no duplicate. */
export function resumeIngestion() {
  return call(EVENTS, 'ResumeIngestion');
}
export function checkPermissions() {
  return call(EVENTS, 'CheckPermissions');
}
export function evaluateAccess(channels) {
  return call(EVENTS, 'EvaluateAccess', channels);
}
export function relaunchAsAdmin() {
  return call(EVENTS, 'RelaunchAsAdmin');
}
/** @param {object} query */
export function queryEvents(query) {
  return call(EVENTS, 'QueryEvents', query);
}
/** Total events matching a filter, ignoring paging (P1 accurate counts). @param {object} query */
export function countEvents(query) {
  return call(EVENTS, 'CountEvents', query);
}
export function getEvent(id) {
  return call(EVENTS, 'GetEvent', id);
}
/**
 * Timeline shape for the filtered set (P24): extent, dated/undated counts, and a density
 * histogram. Returns COUNTS, not events — the page draws density and fetches individual
 * events only for the range in view.
 * @param {object} query @param {number} buckets @param {string} groupBy lane grouping ('' = none)
 * @returns {Promise<{from:string,to:string,dated:number,undated:number,group_by:string,
 *   buckets:{start:string,end:string,count:number}[], lanes:{key:string,total:number,counts:number[]}[]}>}
 */
export function timeline(query, buckets, groupBy) {
  // Every parameter must be forwarded: Wails matches the bound method by arity, so a
  // dropped argument fails the call outright rather than defaulting.
  return call(EVENTS, 'Timeline', query, buckets, groupBy || '');
}
export function stats() {
  return call(EVENTS, 'Stats');
}
export function pickEVTXFiles() {
  return call(EVENTS, 'PickEVTXFiles');
}
export function pickEVTXFolder() {
  return call(EVENTS, 'PickEVTXFolder');
}
export function totalSize(paths) {
  return call(EVENTS, 'TotalSize', paths);
}

// --- Graph API ---

export function getEvents(ids) {
  return call(GRAPH, 'GetEvents', ids);
}
export function getRelations() {
  return call(GRAPH, 'GetRelations');
}
/** Relations scoped to one graph (P15). @param {number} graphId */
export function getGraphRelations(graphId) {
  return call(GRAPH, 'GetGraphRelations', graphId);
}
export function relationsOf(eventId) {
  return call(GRAPH, 'RelationsOf', eventId);
}
/** @param {number[]} ids @returns {Promise<Record<number,{count:number,types:string[],related_ids:number[]}>>} */
export function relationsAdjacency(ids) {
  return call(GRAPH, 'RelationsAdjacency', ids);
}
/** @param {{from:number,to:number,relation_type?:string,relation_label?:string,confidence_score?:number,created_by?:string}} req */
export function createRelation(req) {
  return call(GRAPH, 'CreateRelation', req);
}
/** @param {{id:number,relation_type?:string,relation_label?:string,confidence_score?:number}} req */
export function updateRelation(req) {
  return call(GRAPH, 'UpdateRelation', req);
}
export function deleteRelation(id) {
  return call(GRAPH, 'DeleteRelation', id);
}
export function deleteEvent(id) {
  return call(GRAPH, 'DeleteEvent', id);
}

/** @param {number} graphId @param {{nodes:Record<number,{x:number,y:number}>, viewport:{x:number,y:number,zoom:number}}} l */
export function saveLayout(graphId, l) {
  return call(GRAPH, 'SaveLayout', graphId, l);
}
export function loadLayout(graphId) {
  return call(GRAPH, 'LoadLayout', graphId);
}

// --- Graph management (multiple graphs, P15) ---

export function listGraphs() {
  return call(GRAPH, 'ListGraphs');
}
export function activeGraph() {
  return call(GRAPH, 'ActiveGraph');
}
export function setActiveGraph(id) {
  return call(GRAPH, 'SetActiveGraph', id);
}
/** @param {{name:string, description?:string}} req */
export function createGraph(req) {
  return call(GRAPH, 'CreateGraph', req);
}
/** @param {{id:number, name:string, description?:string}} req */
export function renameGraph(req) {
  return call(GRAPH, 'RenameGraph', req);
}
export function deleteGraph(id) {
  return call(GRAPH, 'DeleteGraph', id);
}

// --- Rules API (correlation rules, P2/P4/P5) ---

/** @returns {Promise<{rules:object[], errors:{path:string,message:string}[]}>} */
export function listRules() {
  return call(RULES, 'ListRules');
}
/** @param {string} id @param {boolean} enabled */
export function setRuleEnabled(id, enabled) {
  return call(RULES, 'SetRuleEnabled', id, enabled);
}
export function reloadRules() {
  return call(RULES, 'ReloadRules');
}
/** Opens a native file dialog. @returns {Promise<{imported:string[], errors:{path:string,message:string}[]}>} */
export function importRuleFiles() {
  return call(RULES, 'ImportRuleFiles');
}
/** Opens a native folder dialog (recursive import). */
export function importRuleFolder() {
  return call(RULES, 'ImportRuleFolder');
}
/** @param {string} id */
export function deleteRule(id) {
  return call(RULES, 'DeleteRule', id);
}
export function rulesDir() {
  return call(RULES, 'RulesDir');
}
/** A rule's file exactly as authored, for the inspector (P19).
 * @param {string} id
 * @returns {Promise<{id:string, origin:string, file:string, path?:string, source:string}>} */
export function ruleSource(id) {
  return call(RULES, 'RuleSource', id);
}

// --- Rule-driven graph building (P6) ---

/**
 * Applies rules to the event set: one rule = one graph. An empty rule_ids runs every
 * enabled rule; filter scopes the dataset (its paging fields are ignored). Re-running
 * rebuilds each rule's graph rather than appending to it.
 * @param {{rule_ids?:string[], filter?:object}} req
 * @returns {Promise<{outcomes:object[], events:number}>}
 */
export function runRules(req) {
  return call(BUILD, 'RunRules', req);
}
/** Stops an in-flight rule run; graphs already rebuilt are kept. */
export function cancelRuleRun() {
  return call(BUILD, 'CancelRuleRun');
}
/** Whether a rule run is in flight, so a view opened mid-run shows the right state. */
export function isRunningRules() {
  return call(BUILD, 'IsRunningRules');
}

// --- Analyst findings (P25) ---
//
// Findings are keyed by an event's hash_normalized — its content identity — not by its node
// id. Node ids are assignment-order, so a re-ingest would move an analyst's note onto a
// different event. Every event the frontend renders already carries the hash.

/** @param {string} key an event's hash_normalized
 *  @returns {Promise<{key:string,flagged:boolean,tags:string[],note:string,descriptor:string,
 *    created_at:string,updated_at:string}|null>} */
export function getFinding(key) {
  return call(FINDINGS, 'GetFinding', key);
}
/** Findings for a whole page of events in one call, keyed by event hash.
 * @param {string[]} keys @returns {Promise<Record<string,object>>} */
export function getFindings(keys) {
  return call(FINDINGS, 'GetFindings', keys);
}
/**
 * Writes an annotation. Resolves to null when the request cleared the last of its content —
 * the finding is removed rather than kept as an empty shell that would still read as
 * annotated.
 * @param {{key:string, flagged?:boolean, tags?:string[], note?:string, descriptor?:string}} req
 */
export function setFinding(req) {
  return call(FINDINGS, 'SetFinding', req);
}
/** @param {string} key */
export function removeFinding(key) {
  return call(FINDINGS, 'RemoveFinding', key);
}
/** Every finding, most recently updated first. @returns {Promise<object[]>} */
export function listFindings() {
  return call(FINDINGS, 'ListFindings');
}
/** Tags in use with their counts, most used first.
 * @returns {Promise<{tag:string,count:number}[]>} */
export function listTags() {
  return call(FINDINGS, 'ListTags');
}
/** @returns {Promise<{total:number,flagged:number,noted:number,tagged:number}>} */
export function findingStats() {
  return call(FINDINGS, 'FindingStats');
}
/**
 * Reconciles the findings sidecar against the events actually in the case. Findings outlive
 * the events they describe — clearing the store and ingesting a different dataset leaves the
 * old ones keyed to hashes nothing can produce — so the case has to be able to say how many
 * of its findings still refer to something.
 * @returns {Promise<{total:number, live:number, orphans:object[], stale:boolean, hash_version:number}>}
 */
export function auditFindings() {
  return call(FINDINGS, 'AuditFindings');
}

// --- System / initialization (P21) ---

/**
 * Current initialization state. Polled once on mount so a view that starts AFTER
 * initialization finished still learns it is ready instead of waiting for an event that
 * has already fired.
 * @returns {Promise<{phase:string, stage:string, error:string}>}
 */
export function initStatus() {
  return call(SYSTEM, 'InitStatus');
}

/** Build identity for the About surface (P13).
 * @returns {Promise<{name:string, version:string, commit:string, date:string, development:boolean}>} */
export function version() {
  return call(SYSTEM, 'Version');
}

// --- Runtime events ---

/**
 * Subscribe to a backend event channel. Returns an unsubscribe function. No-op (but
 * still returns a disposer) when the runtime is unavailable.
 * @param {string} channel
 * @param {(data:any)=>void} handler
 */
export function on(channel, handler) {
  if (typeof EventsOn !== 'function') return () => {};
  EventsOn(channel, handler);
  return () => EventsOff(channel);
}
