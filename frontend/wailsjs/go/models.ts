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
	    }
	}
	export class Result {
	    outcomes: RuleOutcome[];
	    events: number;
	    skipped_undated: number;
	    repaired_relations: number;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcomes = this.convertValues(source["outcomes"], RuleOutcome);
	        this.events = source["events"];
	        this.skipped_undated = source["skipped_undated"];
	        this.repaired_relations = source["repaired_relations"];
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
	
	export class Event {
	    id: number;
	    event_id: string;
	    // Go type: time
	    timestamp: any;
	    provider: string;
	    channel: string;
	    computer: string;
	    user: string;
	    raw_xml: string;
	    parsed_fields: Record<string, string>;
	    hash_raw: string;
	    hash_normalized: string;
	    source_type: string;
	    source_identifier: string;
	    deduplication_count: number;
	
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
	        this.raw_xml = source["raw_xml"];
	        this.parsed_fields = source["parsed_fields"];
	        this.hash_raw = source["hash_raw"];
	        this.hash_normalized = source["hash_normalized"];
	        this.source_type = source["source_type"];
	        this.source_identifier = source["source_identifier"];
	        this.deduplication_count = source["deduplication_count"];
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

export namespace rules {
	
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

