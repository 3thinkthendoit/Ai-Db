export namespace agent {
	
	export class StepEvent {
	    kind: string;
	    name?: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new StepEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.content = source["content"];
	    }
	}
	export class FunctionCall {
	    name: string;
	    arguments: string;
	
	    static createFrom(source: any = {}) {
	        return new FunctionCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.arguments = source["arguments"];
	    }
	}
	export class ToolCall {
	    id: string;
	    type: string;
	    function: FunctionCall;
	
	    static createFrom(source: any = {}) {
	        return new ToolCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.function = this.convertValues(source["function"], FunctionCall);
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
	export class Message {
	    role: string;
	    content?: string;
	    reasoning_content?: string;
	    tool_calls?: ToolCall[];
	    tool_call_id?: string;
	    name?: string;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.reasoning_content = source["reasoning_content"];
	        this.tool_calls = this.convertValues(source["tool_calls"], ToolCall);
	        this.tool_call_id = source["tool_call_id"];
	        this.name = source["name"];
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
	export class ChatResponse {
	    messages: Message[];
	    steps: StepEvent[];
	    final: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = this.convertValues(source["messages"], Message);
	        this.steps = this.convertValues(source["steps"], StepEvent);
	        this.final = source["final"];
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

export namespace config {
	
	export class Connection {
	    id: string;
	    name: string;
	    type: string;
	    host: string;
	    port: number;
	    user: string;
	    password: string;
	    passwordSet?: boolean;
	    database: string;
	    authSource?: string;
	    sslMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.password = source["password"];
	        this.passwordSet = source["passwordSet"];
	        this.database = source["database"];
	        this.authSource = source["authSource"];
	        this.sslMode = source["sslMode"];
	    }
	}
	export class LLMConfig {
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    allowWrites: boolean;
	    maxRows: number;
	    maxAgentSteps: number;
	
	    static createFrom(source: any = {}) {
	        return new LLMConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.allowWrites = source["allowWrites"];
	        this.maxRows = source["maxRows"];
	        this.maxAgentSteps = source["maxAgentSteps"];
	    }
	}
	export class LLMProfileView {
	    id: string;
	    name: string;
	    baseUrl: string;
	    model: string;
	    hasApiKey: boolean;
	    active: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LLMProfileView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.hasApiKey = source["hasApiKey"];
	        this.active = source["active"];
	    }
	}

}

export namespace db {
	
	export class CharsetOption {
	    name: string;
	    defaultCollation: string;
	    collations: string[];
	
	    static createFrom(source: any = {}) {
	        return new CharsetOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.defaultCollation = source["defaultCollation"];
	        this.collations = source["collations"];
	    }
	}
	export class CreateDatabaseOptions {
	    charsets: CharsetOption[];
	    encodings: string[];
	
	    static createFrom(source: any = {}) {
	        return new CreateDatabaseOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.charsets = this.convertValues(source["charsets"], CharsetOption);
	        this.encodings = source["encodings"];
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
	export class CreateDatabaseResult {
	    ok: boolean;
	    completed: string[];
	    failedStep?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateDatabaseResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.completed = source["completed"];
	        this.failedStep = source["failedStep"];
	        this.error = source["error"];
	    }
	}
	export class CreateDatabaseSpec {
	    name: string;
	    charset?: string;
	    collate?: string;
	    encoding?: string;
	    account?: string;
	    password?: string;
	    host?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateDatabaseSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.charset = source["charset"];
	        this.collate = source["collate"];
	        this.encoding = source["encoding"];
	        this.account = source["account"];
	        this.password = source["password"];
	        this.host = source["host"];
	    }
	}
	export class ObjectNode {
	    name: string;
	    kind: string;
	    lazy?: boolean;
	    badge?: string;
	    children?: ObjectNode[];
	
	    static createFrom(source: any = {}) {
	        return new ObjectNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.lazy = source["lazy"];
	        this.badge = source["badge"];
	        this.children = this.convertValues(source["children"], ObjectNode);
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
	export class QueryResult {
	    columns: string[];
	    rows: string[][];
	    rowCount: number;
	    durationMs: number;
	    message: string;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	        this.rowCount = source["rowCount"];
	        this.durationMs = source["durationMs"];
	        this.message = source["message"];
	        this.truncated = source["truncated"];
	    }
	}

}

export namespace main {
	
	export class AgentChatRequest {
	    connectionId: string;
	    history: agent.Message[];
	    message: string;
	    confirmWrites: boolean;
	    preferredSchema: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentChatRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connectionId = source["connectionId"];
	        this.history = this.convertValues(source["history"], agent.Message);
	        this.message = source["message"];
	        this.confirmWrites = source["confirmWrites"];
	        this.preferredSchema = source["preferredSchema"];
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
	export class LLMTestResult {
	    ok: boolean;
	    message: string;
	    model: string;
	    durationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new LLMTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.model = source["model"];
	        this.durationMs = source["durationMs"];
	    }
	}
	export class SaveLLMProfileRequest {
	    id: string;
	    name: string;
	    baseUrl: string;
	    model: string;
	    apiKey: string;
	    clearKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveLLMProfileRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.apiKey = source["apiKey"];
	        this.clearKey = source["clearKey"];
	    }
	}

}

