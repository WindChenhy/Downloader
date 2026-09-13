export namespace engine {
	
	export class Settings {
	    saveDir: string;
	    connections: number;
	    concurrentTasks: number;
	    speedLimit: number;
	    userAgent: string;
	    extraHeaders: string;
	    proxyMode: string;
	    proxyUrl: string;
	    githubMirror: boolean;
	    mirrorTemplate: string;
	    clipboardWatch: boolean;
	    apiEnabled: boolean;
	    apiPort: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saveDir = source["saveDir"];
	        this.connections = source["connections"];
	        this.concurrentTasks = source["concurrentTasks"];
	        this.speedLimit = source["speedLimit"];
	        this.userAgent = source["userAgent"];
	        this.extraHeaders = source["extraHeaders"];
	        this.proxyMode = source["proxyMode"];
	        this.proxyUrl = source["proxyUrl"];
	        this.githubMirror = source["githubMirror"];
	        this.mirrorTemplate = source["mirrorTemplate"];
	        this.clipboardWatch = source["clipboardWatch"];
	        this.apiEnabled = source["apiEnabled"];
	        this.apiPort = source["apiPort"];
	    }
	}
	export class Task {
	    id: string;
	    url: string;
	    fileName: string;
	    saveDir: string;
	    totalSize: number;
	    downloaded: number;
	    speed: number;
	    status: string;
	    connections: number;
	    error?: string;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.fileName = source["fileName"];
	        this.saveDir = source["saveDir"];
	        this.totalSize = source["totalSize"];
	        this.downloaded = source["downloaded"];
	        this.speed = source["speed"];
	        this.status = source["status"];
	        this.connections = source["connections"];
	        this.error = source["error"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
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

