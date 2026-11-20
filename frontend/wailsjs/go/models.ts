export namespace core {
	
	export class Skill {
	    name: string;
	    path: string;
	    desc: string;
	    content?: string;
	
	    static createFrom(source: any = {}) {
	        return new Skill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.desc = source["desc"];
	        this.content = source["content"];
	    }
	}
	export class AgentSkills {
	    name: string;
	    path: string;
	    skills: Skill[];
	
	    static createFrom(source: any = {}) {
	        return new AgentSkills(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.skills = this.convertValues(source["skills"], Skill);
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
	export class SessionClient {
	    id: string;
	    name: string;
	    root: string;
	    rootExists: boolean;
	    count: number;
	    totalSize: number;
	    lastMod: string;
	    managed: boolean;
	    note: string;
	    resumeSupported: boolean;
	    resumeNote: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.root = source["root"];
	        this.rootExists = source["rootExists"];
	        this.count = source["count"];
	        this.totalSize = source["totalSize"];
	        this.lastMod = source["lastMod"];
	        this.managed = source["managed"];
	        this.note = source["note"];
	        this.resumeSupported = source["resumeSupported"];
	        this.resumeNote = source["resumeNote"];
	    }
	}
	
	export class TargetInfo {
	    id: string;
	    name: string;
	    apiFormat: string;
	    configFormat: string;
	    skillsSupported: boolean;
	    mcpSupported: boolean;
	    skillsDir?: string;
	    mcpFile?: string;
	    configFile?: string;
	    presetFile?: string;
	    placeholder?: boolean;
	    bin?: string;
	    pkg?: string;
	    installed: boolean;
	    version?: string;
	    canInstall: boolean;
	    canUninstall: boolean;
	    uninstallPaths?: string;
	    installCmd?: string;
	    updateCmd?: string;
	
	    static createFrom(source: any = {}) {
	        return new TargetInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.apiFormat = source["apiFormat"];
	        this.configFormat = source["configFormat"];
	        this.skillsSupported = source["skillsSupported"];
	        this.mcpSupported = source["mcpSupported"];
	        this.skillsDir = source["skillsDir"];
	        this.mcpFile = source["mcpFile"];
	        this.configFile = source["configFile"];
	        this.presetFile = source["presetFile"];
	        this.placeholder = source["placeholder"];
	        this.bin = source["bin"];
	        this.pkg = source["pkg"];
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.canInstall = source["canInstall"];
	        this.canUninstall = source["canUninstall"];
	        this.uninstallPaths = source["uninstallPaths"];
	        this.installCmd = source["installCmd"];
	        this.updateCmd = source["updateCmd"];
	    }
	}
	export class TargetOp {
	    targetId: string;
	    action: string;
	
	    static createFrom(source: any = {}) {
	        return new TargetOp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetId = source["targetId"];
	        this.action = source["action"];
	    }
	}
	export class TargetResult {
	    targetId: string;
	    name: string;
	    injectedSkills: number;
	    injectedMcp: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new TargetResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetId = source["targetId"];
	        this.name = source["name"];
	        this.injectedSkills = source["injectedSkills"];
	        this.injectedMcp = source["injectedMcp"];
	        this.error = source["error"];
	    }
	}

}

export namespace main {
	
	export class SystemTerminal {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemTerminal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}

}

export namespace types {
	
	export class Usage {
	    prompt_tokens?: number;
	    completion_tokens?: number;
	
	    static createFrom(source: any = {}) {
	        return new Usage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prompt_tokens = source["prompt_tokens"];
	        this.completion_tokens = source["completion_tokens"];
	    }
	}
	export class LogEntry {
	    time: string;
	    path: string;
	    model: string;
	    provider: string;
	    reasoning?: string;
	    reasoningOut?: string;
	    status: number;
	    ms: number;
	    req?: string;
	    resp?: string;
	    usage?: Usage;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.path = source["path"];
	        this.model = source["model"];
	        this.provider = source["provider"];
	        this.reasoning = source["reasoning"];
	        this.reasoningOut = source["reasoningOut"];
	        this.status = source["status"];
	        this.ms = source["ms"];
	        this.req = source["req"];
	        this.resp = source["resp"];
	        this.usage = this.convertValues(source["usage"], Usage);
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

