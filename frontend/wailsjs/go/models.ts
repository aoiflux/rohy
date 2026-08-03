export namespace api {
	
	export class EventQuery {
	    event_id: string;
	    provider: string;
	    channel: string;
	    user: string;
	    time_from: string;
	    time_to: string;
	    search: string;
	    source_type: string;
	    source_identifier: string;
	    min_duplicate_count: number;
	    relation_state: string;
	    undated: string;
	    finding_state: string;
	    tag: string;
	    offset: number;
	    limit: number;
	    descending: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EventQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.event_id = source["event_id"];
	        this.provider = source["provider"];
	        this.channel = source["channel"];
	        this.user = source["user"];
	        this.time_from = source["time_from"];
	        this.time_to = source["time_to"];
	        this.search = source["search"];
	        this.source_type = source["source_type"];
	        this.source_identifier = source["source_identifier"];
	        this.min_duplicate_count = source["min_duplicate_count"];
	        this.relation_state = source["relation_state"];
	        this.undated = source["undated"];
	        this.finding_state = source["finding_state"];
	        this.tag = source["tag"];
	        this.offset = source["offset"];
	        this.limit = source["limit"];
	        this.descending = source["descending"];
	    }
	}
	export class BuildRequest {
	    rule_ids: string[];
	    filter: EventQuery;
	
	    static createFrom(source: any = {}) {
	        return new BuildRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule_ids = source["rule_ids"];
	        this.filter = this.convertValues(source["filter"], EventQuery);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CaptureStatus {
	    active: boolean;
	    continuous: boolean;
	    channels: string[];
	    positions: Record<string, number>;
	
	    static createFrom(source: any = {}) {
	        return new CaptureStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.continuous = source["continuous"];
	        this.channels = source["channels"];
	        this.positions = source["positions"];
	    }
	}
	export class ClusterRequest {
	    graph_id: number;
	    mode: string;
	    slot?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClusterRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.graph_id = source["graph_id"];
	        this.mode = source["mode"];
	        this.slot = source["slot"];
	    }
	}
	
	export class FindingRequest {
	    key: string;
	    flagged: boolean;
	    tags: string[];
	    note: string;
	    descriptor: string;
	
	    static createFrom(source: any = {}) {
	        return new FindingRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.flagged = source["flagged"];
	        this.tags = source["tags"];
	        this.note = source["note"];
	        this.descriptor = source["descriptor"];
	    }
	}
	export class FindingsAudit {
	    total: number;
	    live: number;
	    orphans: findings.Finding[];
	    stale: boolean;
	    hash_version: number;
	
	    static createFrom(source: any = {}) {
	        return new FindingsAudit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.live = source["live"];
	        this.orphans = this.convertValues(source["orphans"], findings.Finding);
	        this.stale = source["stale"];
	        this.hash_version = source["hash_version"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GraphRequest {
	    id: number;
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new GraphRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class HeatmapRequest {
	    graph_id: number;
	    buckets: number;
	    group_by: string;
	    all_graphs?: boolean;
	    // Go type: time
	    from?: any;
	    // Go type: time
	    to?: any;
	
	    static createFrom(source: any = {}) {
	        return new HeatmapRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.graph_id = source["graph_id"];
	        this.buckets = source["buckets"];
	        this.group_by = source["group_by"];
	        this.all_graphs = source["all_graphs"];
	        this.from = this.convertValues(source["from"], null);
	        this.to = this.convertValues(source["to"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class IngestRequest {
	    source: string;
	    paths: string[];
	    channels: string[];
	    idempotent: boolean;
	    continuous: boolean;
	
	    static createFrom(source: any = {}) {
	        return new IngestRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.paths = source["paths"];
	        this.channels = source["channels"];
	        this.idempotent = source["idempotent"];
	        this.continuous = source["continuous"];
	    }
	}
	export class InitState {
	    phase: string;
	    stage: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new InitState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.stage = source["stage"];
	        this.error = source["error"];
	    }
	}
	export class LayoutProfileInfo {
	    name: string;
	    label: string;
	    summary: string;
	    needs_slot: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LayoutProfileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.summary = source["summary"];
	        this.needs_slot = source["needs_slot"];
	    }
	}
	export class LayoutRequest {
	    graph_id: number;
	    profile: string;
	    slot?: string;
	
	    static createFrom(source: any = {}) {
	        return new LayoutRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.graph_id = source["graph_id"];
	        this.profile = source["profile"];
	        this.slot = source["slot"];
	    }
	}
	export class RelationDetail {
	    relation?: graphene.Relation;
	    from?: graphene.Event;
	    to?: graphene.Event;
	    graph_name: string;
	    sibling_ids: number[];
	    recorded: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RelationDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.relation = this.convertValues(source["relation"], graphene.Relation);
	        this.from = this.convertValues(source["from"], graphene.Event);
	        this.to = this.convertValues(source["to"], graphene.Event);
	        this.graph_name = source["graph_name"];
	        this.sibling_ids = source["sibling_ids"];
	        this.recorded = source["recorded"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RelationRequest {
	    from: number;
	    to: number;
	    graph_id: number;
	    relation_type: string;
	    relation_label: string;
	    confidence_score: number;
	    created_by: string;
	
	    static createFrom(source: any = {}) {
	        return new RelationRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = source["from"];
	        this.to = source["to"];
	        this.graph_id = source["graph_id"];
	        this.relation_type = source["relation_type"];
	        this.relation_label = source["relation_label"];
	        this.confidence_score = source["confidence_score"];
	        this.created_by = source["created_by"];
	    }
	}
	export class RelationUpdate {
	    id: number;
	    relation_type: string;
	    relation_label: string;
	    confidence_score: number;
	
	    static createFrom(source: any = {}) {
	        return new RelationUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.relation_type = source["relation_type"];
	        this.relation_label = source["relation_label"];
	        this.confidence_score = source["confidence_score"];
	    }
	}
	export class RestoreRequest {
	    graph_id: number;
	    id: string;
	    recreate?: number[];
	
	    static createFrom(source: any = {}) {
	        return new RestoreRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.graph_id = source["graph_id"];
	        this.id = source["id"];
	        this.recreate = source["recreate"];
	    }
	}
	export class RestoreResult {
	    plan: snapshot.Plan;
	    nodes_moved: number;
	    relations_created: number;
	
	    static createFrom(source: any = {}) {
	        return new RestoreResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plan = this.convertValues(source["plan"], snapshot.Plan);
	        this.nodes_moved = source["nodes_moved"];
	        this.relations_created = source["relations_created"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuleBundle {
	    rules: rules.RuleSource[];
	    missing?: string[];
	
	    static createFrom(source: any = {}) {
	        return new RuleBundle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rules = this.convertValues(source["rules"], rules.RuleSource);
	        this.missing = source["missing"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RulesResult {
	    rules: rules.Rule[];
	    errors: rules.LoadError[];
	
	    static createFrom(source: any = {}) {
	        return new RulesResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rules = this.convertValues(source["rules"], rules.Rule);
	        this.errors = this.convertValues(source["errors"], rules.LoadError);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SnapshotRequest {
	    graph_id: number;
	    label?: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.graph_id = source["graph_id"];
	        this.label = source["label"];
	    }
	}
	export class StatsResult {
	    events: number;
	    relations: number;
	
	    static createFrom(source: any = {}) {
	        return new StatsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.events = source["events"];
	        this.relations = source["relations"];
	    }
	}

}

export namespace evtx {
	
	export class AccessDecision {
	    needed: boolean;
	    blocked_channels: string[];
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new AccessDecision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.needed = source["needed"];
	        this.blocked_channels = source["blocked_channels"];
	        this.message = source["message"];
	    }
	}
	export class PermissionStatus {
	    platform: string;
	    elevated: boolean;
	    administrator: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PermissionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.elevated = source["elevated"];
	        this.administrator = source["administrator"];
	    }
	}

}

export namespace findings {
	
	export class Finding {
	    key: string;
	    flagged: boolean;
	    tags: string[];
	    note: string;
	    descriptor?: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Finding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.flagged = source["flagged"];
	        this.tags = source["tags"];
	        this.note = source["note"];
	        this.descriptor = source["descriptor"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Summary {
	    total: number;
	    flagged: number;
	    noted: number;
	    tagged: number;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.flagged = source["flagged"];
	        this.noted = source["noted"];
	        this.tagged = source["tagged"];
	    }
	}
	export class TagCount {
	    tag: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new TagCount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tag = source["tag"];
	        this.count = source["count"];
	    }
	}

}

export namespace graphbuild {
	
	export class DryRunEvent {
	    id: number;
	    event_id: string;
	    // Go type: time
	    timestamp: any;
	    computer: string;
	    provider: string;
	    channel: string;
	    user: string;
	
	    static createFrom(source: any = {}) {
	        return new DryRunEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.event_id = source["event_id"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.computer = source["computer"];
	        this.provider = source["provider"];
	        this.channel = source["channel"];
	        this.user = source["user"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DryRunMatch {
	    match_id: string;
	    basis: string[];
	    events: DryRunEvent[];
	
	    static createFrom(source: any = {}) {
	        return new DryRunMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.match_id = source["match_id"];
	        this.basis = source["basis"];
	        this.events = this.convertValues(source["events"], DryRunEvent);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DryRunResult {
	    valid: boolean;
	    problems: rules.ValidationReport;
	    matches: number;
	    relations: number;
	    truncated: boolean;
	    dropped: number;
	    events: number;
	    skipped_undated: number;
	    skipped_no_keys: number;
	    unresolved_parents: number;
	    stale_correlation_keys: number;
	    elapsed_ms: number;
	    samples: DryRunMatch[];
	
	    static createFrom(source: any = {}) {
	        return new DryRunResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.valid = source["valid"];
	        this.problems = this.convertValues(source["problems"], rules.ValidationReport);
	        this.matches = source["matches"];
	        this.relations = source["relations"];
	        this.truncated = source["truncated"];
	        this.dropped = source["dropped"];
	        this.events = source["events"];
	        this.skipped_undated = source["skipped_undated"];
	        this.skipped_no_keys = source["skipped_no_keys"];
	        this.unresolved_parents = source["unresolved_parents"];
	        this.stale_correlation_keys = source["stale_correlation_keys"];
	        this.elapsed_ms = source["elapsed_ms"];
	        this.samples = this.convertValues(source["samples"], DryRunMatch);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuleOutcome {
	    rule_id: string;
	    rule_name: string;
	    graph_id: number;
	    graph_name: string;
	    matches: number;
	    relations: number;
	    removed: number;
	    truncated: boolean;
	    dropped: number;
	    graph_discarded: boolean;
	    skipped_no_keys: number;
	    unresolved_parents: number;
	
	    static createFrom(source: any = {}) {
	        return new RuleOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule_id = source["rule_id"];
	        this.rule_name = source["rule_name"];
	        this.graph_id = source["graph_id"];
	        this.graph_name = source["graph_name"];
	        this.matches = source["matches"];
	        this.relations = source["relations"];
	        this.removed = source["removed"];
	        this.truncated = source["truncated"];
	        this.dropped = source["dropped"];
	        this.graph_discarded = source["graph_discarded"];
	        this.skipped_no_keys = source["skipped_no_keys"];
	        this.unresolved_parents = source["unresolved_parents"];
	    }
	}
	export class Result {
	    outcomes: RuleOutcome[];
	    events: number;
	    skipped_undated: number;
	    repaired_relations: number;
	    stale_correlation_keys: number;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcomes = this.convertValues(source["outcomes"], RuleOutcome);
	        this.events = source["events"];
	        this.skipped_undated = source["skipped_undated"];
	        this.repaired_relations = source["repaired_relations"];
	        this.stale_correlation_keys = source["stale_correlation_keys"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace graphene {
	
	export class BackfillResult {
	    examined: number;
	    projected: number;
	    already_current: number;
	    failed: number;
	    cancelled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackfillResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.examined = source["examined"];
	        this.projected = source["projected"];
	        this.already_current = source["already_current"];
	        this.failed = source["failed"];
	        this.cancelled = source["cancelled"];
	    }
	}
	export class CorrelationKeyStatus {
	    total: number;
	    current: number;
	    stale: number;
	
	    static createFrom(source: any = {}) {
	        return new CorrelationKeyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.current = source["current"];
	        this.stale = source["stale"];
	    }
	}
	export class Event {
	    id: number;
	    event_id: string;
	    // Go type: time
	    timestamp: any;
	    provider: string;
	    channel: string;
	    computer: string;
	    user: string;
	    hash_raw: string;
	    hash_normalized: string;
	    source_type: string;
	    source_identifier: string;
	    deduplication_count: number;
	    source_counts?: Record<string, number>;
	    payload?: payload.Ref;
	    ck?: string[];
	    ckv?: number;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.event_id = source["event_id"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.provider = source["provider"];
	        this.channel = source["channel"];
	        this.computer = source["computer"];
	        this.user = source["user"];
	        this.hash_raw = source["hash_raw"];
	        this.hash_normalized = source["hash_normalized"];
	        this.source_type = source["source_type"];
	        this.source_identifier = source["source_identifier"];
	        this.deduplication_count = source["deduplication_count"];
	        this.source_counts = source["source_counts"];
	        this.payload = this.convertValues(source["payload"], payload.Ref);
	        this.ck = source["ck"];
	        this.ckv = source["ckv"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TimelineLane {
	    key: string;
	    total: number;
	    counts: number[];
	
	    static createFrom(source: any = {}) {
	        return new TimelineLane(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.total = source["total"];
	        this.counts = source["counts"];
	    }
	}
	export class TimelineBucket {
	    // Go type: time
	    start: any;
	    // Go type: time
	    end: any;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new TimelineBucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = this.convertValues(source["start"], null);
	        this.end = this.convertValues(source["end"], null);
	        this.count = source["count"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HeatmapSummary {
	    // Go type: time
	    from: any;
	    // Go type: time
	    to: any;
	    total: number;
	    placed: number;
	    undated: number;
	    outside: number;
	    buckets: TimelineBucket[];
	    group_by: string;
	    lanes: TimelineLane[];
	    max: number;
	
	    static createFrom(source: any = {}) {
	        return new HeatmapSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = this.convertValues(source["from"], null);
	        this.to = this.convertValues(source["to"], null);
	        this.total = source["total"];
	        this.placed = source["placed"];
	        this.undated = source["undated"];
	        this.outside = source["outside"];
	        this.buckets = this.convertValues(source["buckets"], TimelineBucket);
	        this.group_by = source["group_by"];
	        this.lanes = this.convertValues(source["lanes"], TimelineLane);
	        this.max = source["max"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Relation {
	    id: number;
	    from: number;
	    to: number;
	    graph_id: number;
	    relation_type: string;
	    relation_label: string;
	    confidence_score: number;
	    created_by: string;
	    // Go type: time
	    created_at: any;
	    rule_id?: string;
	    algorithm?: string;
	    match_id?: string;
	    step_index?: number;
	    basis?: string[];
	    rel_v?: number;
	
	    static createFrom(source: any = {}) {
	        return new Relation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.graph_id = source["graph_id"];
	        this.relation_type = source["relation_type"];
	        this.relation_label = source["relation_label"];
	        this.confidence_score = source["confidence_score"];
	        this.created_by = source["created_by"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.rule_id = source["rule_id"];
	        this.algorithm = source["algorithm"];
	        this.match_id = source["match_id"];
	        this.step_index = source["step_index"];
	        this.basis = source["basis"];
	        this.rel_v = source["rel_v"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class TimelineSummary {
	    // Go type: time
	    from: any;
	    // Go type: time
	    to: any;
	    dated: number;
	    undated: number;
	    buckets: TimelineBucket[];
	    group_by: string;
	    lanes: TimelineLane[];
	
	    static createFrom(source: any = {}) {
	        return new TimelineSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = this.convertValues(source["from"], null);
	        this.to = this.convertValues(source["to"], null);
	        this.dated = source["dated"];
	        this.undated = source["undated"];
	        this.buckets = this.convertValues(source["buckets"], TimelineBucket);
	        this.group_by = source["group_by"];
	        this.lanes = this.convertValues(source["lanes"], TimelineLane);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace graphlayout {
	
	export class Cluster {
	    id: string;
	    label: string;
	    node_ids: number[];
	    size: number;
	    overlapping?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Cluster(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.node_ids = source["node_ids"];
	        this.size = source["size"];
	        this.overlapping = source["overlapping"];
	    }
	}
	export class Group {
	    label: string;
	    node_ids: number[];
	    undated?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.node_ids = source["node_ids"];
	        this.undated = source["undated"];
	    }
	}
	export class Point {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new Point(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class Result {
	    profile: string;
	    positions: Record<number, Point>;
	    groups: Group[];
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profile = source["profile"];
	        this.positions = this.convertValues(source["positions"], Point, true);
	        this.groups = this.convertValues(source["groups"], Group);
	        this.note = source["note"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace graphreg {
	
	export class Graph {
	    id: number;
	    rule_id?: string;
	    name: string;
	    description: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Graph(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.rule_id = source["rule_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace layout {
	
	export class Viewport {
	    x: number;
	    y: number;
	    zoom: number;
	
	    static createFrom(source: any = {}) {
	        return new Viewport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.zoom = source["zoom"];
	    }
	}
	export class Position {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new Position(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class Layout {
	    nodes: Record<number, Position>;
	    viewport: Viewport;
	
	    static createFrom(source: any = {}) {
	        return new Layout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodes = this.convertValues(source["nodes"], Position, true);
	        this.viewport = this.convertValues(source["viewport"], Viewport);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace payload {
	
	export class Ref {
	    o?: number;
	    l?: number;
	
	    static createFrom(source: any = {}) {
	        return new Ref(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.o = source["o"];
	        this.l = source["l"];
	    }
	}

}

export namespace rules {
	
	export class Algorithm {
	    name: string;
	    summary: string;
	    requires_sequence: boolean;
	    fields: string[];
	
	    static createFrom(source: any = {}) {
	        return new Algorithm(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.summary = source["summary"];
	        this.requires_sequence = source["requires_sequence"];
	        this.fields = source["fields"];
	    }
	}
	export class Field {
	    name: string;
	    kind: string;
	    required: boolean;
	    group: string;
	    read_only: boolean;
	    default?: any;
	    enum?: string[];
	    min_items?: number;
	    max_items?: number;
	    description: string;
	    guidance: string;
	    example: any;
	    applies_to?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Field(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.required = source["required"];
	        this.group = source["group"];
	        this.read_only = source["read_only"];
	        this.default = source["default"];
	        this.enum = source["enum"];
	        this.min_items = source["min_items"];
	        this.max_items = source["max_items"];
	        this.description = source["description"];
	        this.guidance = source["guidance"];
	        this.example = source["example"];
	        this.applies_to = source["applies_to"];
	    }
	}
	export class LoadError {
	    path: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LoadError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.message = source["message"];
	    }
	}
	export class ImportResult {
	    imported: string[];
	    errors: LoadError[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imported = source["imported"];
	        this.errors = this.convertValues(source["errors"], LoadError);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Rule {
	    format_version: number;
	    name: string;
	    description: string;
	    relation_type: string;
	    algorithm?: string;
	    sequence: string[];
	    labels?: string[];
	    match_fields?: string[];
	    match_scope?: string;
	    window_within?: string;
	    window_total?: string;
	    lineage_create_ids?: string[];
	    lineage_depth?: number;
	    channels?: string[];
	    id: string;
	    source: string;
	    enabled: boolean;
	    path?: string;
	    file?: string;
	
	    static createFrom(source: any = {}) {
	        return new Rule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format_version = source["format_version"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.relation_type = source["relation_type"];
	        this.algorithm = source["algorithm"];
	        this.sequence = source["sequence"];
	        this.labels = source["labels"];
	        this.match_fields = source["match_fields"];
	        this.match_scope = source["match_scope"];
	        this.window_within = source["window_within"];
	        this.window_total = source["window_total"];
	        this.lineage_create_ids = source["lineage_create_ids"];
	        this.lineage_depth = source["lineage_depth"];
	        this.channels = source["channels"];
	        this.id = source["id"];
	        this.source = source["source"];
	        this.enabled = source["enabled"];
	        this.path = source["path"];
	        this.file = source["file"];
	    }
	}
	export class RuleSource {
	    id: string;
	    origin: string;
	    file: string;
	    path?: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new RuleSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.origin = source["origin"];
	        this.file = source["file"];
	        this.path = source["path"];
	        this.source = source["source"];
	    }
	}
	export class SaveRequest {
	    id: string;
	    source: string;
	    replace_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source = source["source"];
	        this.replace_path = source["replace_path"];
	    }
	}
	export class SaveResult {
	    rule?: Rule;
	    created: boolean;
	    renamed: boolean;
	    previous_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule = this.convertValues(source["rule"], Rule);
	        this.created = source["created"];
	        this.renamed = source["renamed"];
	        this.previous_id = source["previous_id"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Schema {
	    format_version: number;
	    max_file_bytes: number;
	    group_order: string[];
	    fields: Field[];
	    algorithms: Algorithm[];
	    correlation_fields: string[];
	
	    static createFrom(source: any = {}) {
	        return new Schema(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format_version = source["format_version"];
	        this.max_file_bytes = source["max_file_bytes"];
	        this.group_order = source["group_order"];
	        this.fields = this.convertValues(source["fields"], Field);
	        this.algorithms = this.convertValues(source["algorithms"], Algorithm);
	        this.correlation_fields = source["correlation_fields"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Spec {
	    format_version: number;
	    name: string;
	    description: string;
	    relation_type: string;
	    algorithm?: string;
	    sequence: string[];
	    labels?: string[];
	    match_fields?: string[];
	    match_scope?: string;
	    window_within?: string;
	    window_total?: string;
	    lineage_create_ids?: string[];
	    lineage_depth?: number;
	    channels?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Spec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format_version = source["format_version"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.relation_type = source["relation_type"];
	        this.algorithm = source["algorithm"];
	        this.sequence = source["sequence"];
	        this.labels = source["labels"];
	        this.match_fields = source["match_fields"];
	        this.match_scope = source["match_scope"];
	        this.window_within = source["window_within"];
	        this.window_total = source["window_total"];
	        this.lineage_create_ids = source["lineage_create_ids"];
	        this.lineage_depth = source["lineage_depth"];
	        this.channels = source["channels"];
	    }
	}
	export class ValidationError {
	    code: string;
	    field?: string;
	    index: number;
	    line: number;
	    col: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ValidationError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.field = source["field"];
	        this.index = source["index"];
	        this.line = source["line"];
	        this.col = source["col"];
	        this.message = source["message"];
	    }
	}
	export class ValidationReport {
	    valid: boolean;
	    errors: ValidationError[];
	    warnings: ValidationError[];
	    normalized?: Spec;
	    unknown_fields?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ValidationReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.valid = source["valid"];
	        this.errors = this.convertValues(source["errors"], ValidationError);
	        this.warnings = this.convertValues(source["warnings"], ValidationError);
	        this.normalized = this.convertValues(source["normalized"], Spec);
	        this.unknown_fields = source["unknown_fields"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace snapshot {
	
	export class NodePlan {
	    snapshot_id: number;
	    live_id?: number;
	    hash: string;
	    descriptor?: string;
	    x: number;
	    y: number;
	    outcome: string;
	
	    static createFrom(source: any = {}) {
	        return new NodePlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.snapshot_id = source["snapshot_id"];
	        this.live_id = source["live_id"];
	        this.hash = source["hash"];
	        this.descriptor = source["descriptor"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.outcome = source["outcome"];
	    }
	}
	export class RelationPlan {
	    snapshot_id: number;
	    live_id?: number;
	    from_id?: number;
	    to_id?: number;
	    relation_type: string;
	    relation_label?: string;
	    outcome: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new RelationPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.snapshot_id = source["snapshot_id"];
	        this.live_id = source["live_id"];
	        this.from_id = source["from_id"];
	        this.to_id = source["to_id"];
	        this.relation_type = source["relation_type"];
	        this.relation_label = source["relation_label"];
	        this.outcome = source["outcome"];
	        this.reason = source["reason"];
	    }
	}
	export class Viewport {
	    x: number;
	    y: number;
	    zoom: number;
	
	    static createFrom(source: any = {}) {
	        return new Viewport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.zoom = source["zoom"];
	    }
	}
	export class Plan {
	    snapshot_id: string;
	    graph_id: number;
	    viewport: Viewport;
	    nodes: NodePlan[];
	    relations: RelationPlan[];
	    nodes_applied: number;
	    nodes_moved: number;
	    nodes_unresolved: number;
	    relations_applied: number;
	    relations_recreatable: number;
	    relations_unresolved: number;
	    reingested: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.snapshot_id = source["snapshot_id"];
	        this.graph_id = source["graph_id"];
	        this.viewport = this.convertValues(source["viewport"], Viewport);
	        this.nodes = this.convertValues(source["nodes"], NodePlan);
	        this.relations = this.convertValues(source["relations"], RelationPlan);
	        this.nodes_applied = source["nodes_applied"];
	        this.nodes_moved = source["nodes_moved"];
	        this.nodes_unresolved = source["nodes_unresolved"];
	        this.relations_applied = source["relations_applied"];
	        this.relations_recreatable = source["relations_recreatable"];
	        this.relations_unresolved = source["relations_unresolved"];
	        this.reingested = source["reingested"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Summary {
	    id: string;
	    label?: string;
	    graph_id: number;
	    graph_name?: string;
	    // Go type: time
	    taken_at: any;
	    nodes: number;
	    relations: number;
	    app_version?: string;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.graph_id = source["graph_id"];
	        this.graph_name = source["graph_name"];
	        this.taken_at = this.convertValues(source["taken_at"], null);
	        this.nodes = source["nodes"];
	        this.relations = source["relations"];
	        this.app_version = source["app_version"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace version {
	
	export class Info {
	    name: string;
	    version: string;
	    commit: string;
	    date: string;
	    development: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.commit = source["commit"];
	        this.date = source["date"];
	        this.development = source["development"];
	    }
	}

}

