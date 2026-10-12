export namespace engine {
	
	export class AddTaskParams {
	    url: string;
	    saveDir: string;
	    connections: number;
	    customName: string;
	    checksumAlgo: string;
	    checksumExpected: string;
	    priority: number;
	    startAt: string;
	    speedLimit: number;
	    cookie: string;
	    referer: string;
	
	    static createFrom(source: any = {}) {
	        return new AddTaskParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.saveDir = source["saveDir"];
	        this.connections = source["connections"];
	        this.customName = source["customName"];
	        this.checksumAlgo = source["checksumAlgo"];
	        this.checksumExpected = source["checksumExpected"];
	        this.priority = source["priority"];
	        this.startAt = source["startAt"];
	        this.speedLimit = source["speedLimit"];
	        this.cookie = source["cookie"];
	        this.referer = source["referer"];
	    }
	}
	export class Task {
	    id: string;
	    url: string;
	    fileName: string;
	    customName?: string;
	    saveDir: string;
	    totalSize: number;
	    downloaded: number;
	    speed: number;
	    status: string;
	    connections: number;
	    error?: string;
	    blockReason?: string;
	    // Go type: time
	    createdAt: any;
	    activeMs: number;
	    // Go type: time
	    finishedAt: any;
	    avgSpeed: number;
	    priority: number;
	    // Go type: time
	    startAt: any;
	    speedLimit: number;
	    cookie?: string;
	    referer?: string;
	    checksumAlgo?: string;
	    checksumExpected?: string;
	    checksumActual?: string;
	    checksumStatus?: string;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.fileName = source["fileName"];
	        this.customName = source["customName"];
	        this.saveDir = source["saveDir"];
	        this.totalSize = source["totalSize"];
	        this.downloaded = source["downloaded"];
	        this.speed = source["speed"];
	        this.status = source["status"];
	        this.connections = source["connections"];
	        this.error = source["error"];
	        this.blockReason = source["blockReason"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.activeMs = source["activeMs"];
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	        this.avgSpeed = source["avgSpeed"];
	        this.priority = source["priority"];
	        this.startAt = this.convertValues(source["startAt"], null);
	        this.speedLimit = source["speedLimit"];
	        this.cookie = source["cookie"];
	        this.referer = source["referer"];
	        this.checksumAlgo = source["checksumAlgo"];
	        this.checksumExpected = source["checksumExpected"];
	        this.checksumActual = source["checksumActual"];
	        this.checksumStatus = source["checksumStatus"];
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
	export class BatchAddResult {
	    tasks: Task[];
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new BatchAddResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tasks = this.convertValues(source["tasks"], Task);
	        this.errors = source["errors"];
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
	export class DailyStat {
	    date: string;
	    bytes: number;
	    completed: number;
	    failed: number;
	    peakSpeed: number;
	    sumSpeed: number;
	    samples: number;
	
	    static createFrom(source: any = {}) {
	        return new DailyStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.bytes = source["bytes"];
	        this.completed = source["completed"];
	        this.failed = source["failed"];
	        this.peakSpeed = source["peakSpeed"];
	        this.sumSpeed = source["sumSpeed"];
	        this.samples = source["samples"];
	    }
	}
	export class DirCategory {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new DirCategory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class QueueStatus {
	    active: boolean;
	    suspended: boolean;
	    windowEnabled: boolean;
	    windowActive: boolean;
	    windowStart: string;
	    windowEnd: string;
	    opensAt: string;
	    blockedTasks: number;
	
	    static createFrom(source: any = {}) {
	        return new QueueStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.suspended = source["suspended"];
	        this.windowEnabled = source["windowEnabled"];
	        this.windowActive = source["windowActive"];
	        this.windowStart = source["windowStart"];
	        this.windowEnd = source["windowEnd"];
	        this.opensAt = source["opensAt"];
	        this.blockedTasks = source["blockedTasks"];
	    }
	}
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
	    apiToken?: string;
	    allowSaveDirs?: string[];
	    closeAction: string;
	    dirCategories: DirCategory[];
	    autoExtract: boolean;
	    notifyOnCreate: boolean;
	    notifyOnPause: boolean;
	    notifyOnComplete: boolean;
	    notifyOnFail: boolean;
	    afterComplete: string;
	    language: string;
	    queueSuspended: boolean;
	    windowEnabled: boolean;
	    windowStart: string;
	    windowEnd: string;
	    diskGuardOff: boolean;
	    diskMinFree: number;
	
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
	        this.apiToken = source["apiToken"];
	        this.allowSaveDirs = source["allowSaveDirs"];
	        this.closeAction = source["closeAction"];
	        this.dirCategories = this.convertValues(source["dirCategories"], DirCategory);
	        this.autoExtract = source["autoExtract"];
	        this.notifyOnCreate = source["notifyOnCreate"];
	        this.notifyOnPause = source["notifyOnPause"];
	        this.notifyOnComplete = source["notifyOnComplete"];
	        this.notifyOnFail = source["notifyOnFail"];
	        this.afterComplete = source["afterComplete"];
	        this.language = source["language"];
	        this.queueSuspended = source["queueSuspended"];
	        this.windowEnabled = source["windowEnabled"];
	        this.windowStart = source["windowStart"];
	        this.windowEnd = source["windowEnd"];
	        this.diskGuardOff = source["diskGuardOff"];
	        this.diskMinFree = source["diskMinFree"];
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
	export class SpeedPoint {
	    t: number;
	    v: number;
	
	    static createFrom(source: any = {}) {
	        return new SpeedPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.v = source["v"];
	    }
	}
	export class TaskStat {
	    taskId: string;
	    fileName: string;
	    url: string;
	    status: string;
	    bytes: number;
	    totalSize: number;
	    avgSpeed: number;
	    peakSpeed: number;
	    activeMs: number;
	    updatedAt: number;
	    samples?: SpeedPoint[];
	
	    static createFrom(source: any = {}) {
	        return new TaskStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	        this.fileName = source["fileName"];
	        this.url = source["url"];
	        this.status = source["status"];
	        this.bytes = source["bytes"];
	        this.totalSize = source["totalSize"];
	        this.avgSpeed = source["avgSpeed"];
	        this.peakSpeed = source["peakSpeed"];
	        this.activeMs = source["activeMs"];
	        this.updatedAt = source["updatedAt"];
	        this.samples = this.convertValues(source["samples"], SpeedPoint);
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
	export class Stats {
	    generatedAt: number;
	    todayBytes: number;
	    weekBytes: number;
	    totalBytes: number;
	    currentSpeed: number;
	    peakSpeed: number;
	    avgSpeed: number;
	    windowMs: number;
	    globalFrom: number;
	    globalTo: number;
	    todayCompleted: number;
	    todayFailed: number;
	    taskCount: number;
	    running: number;
	    queued: number;
	    completed: number;
	    failed: number;
	    paused: number;
	    daily: DailyStat[];
	    global: SpeedPoint[];
	    tasks: TaskStat[];
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.generatedAt = source["generatedAt"];
	        this.todayBytes = source["todayBytes"];
	        this.weekBytes = source["weekBytes"];
	        this.totalBytes = source["totalBytes"];
	        this.currentSpeed = source["currentSpeed"];
	        this.peakSpeed = source["peakSpeed"];
	        this.avgSpeed = source["avgSpeed"];
	        this.windowMs = source["windowMs"];
	        this.globalFrom = source["globalFrom"];
	        this.globalTo = source["globalTo"];
	        this.todayCompleted = source["todayCompleted"];
	        this.todayFailed = source["todayFailed"];
	        this.taskCount = source["taskCount"];
	        this.running = source["running"];
	        this.queued = source["queued"];
	        this.completed = source["completed"];
	        this.failed = source["failed"];
	        this.paused = source["paused"];
	        this.daily = this.convertValues(source["daily"], DailyStat);
	        this.global = this.convertValues(source["global"], SpeedPoint);
	        this.tasks = this.convertValues(source["tasks"], TaskStat);
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

