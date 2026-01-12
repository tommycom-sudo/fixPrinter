export namespace main {
	
	export class PrintJob {
	    id: number;
	    computerName: string;
	    printerName: string;
	    documentName: string;
	    submittedTime: string;
	    jobStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new PrintJob(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.computerName = source["computerName"];
	        this.printerName = source["printerName"];
	        this.documentName = source["documentName"];
	        this.submittedTime = source["submittedTime"];
	        this.jobStatus = source["jobStatus"];
	    }
	}
	export class PrinterStatus {
	    name: string;
	    printerStatus: number;
	    startTime: number;
	    untilTime: number;
	    isPaused: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PrinterStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.printerStatus = source["printerStatus"];
	        this.startTime = source["startTime"];
	        this.untilTime = source["untilTime"];
	        this.isPaused = source["isPaused"];
	    }
	}

}

export namespace monitor {
	
	export class TaskConfig {
	    name: string;
	    cron: string;
	    curl: string;
	    timeoutMs: number;
	    enabled: boolean;
	    lastExecuted?: string;
	    lastStatus?: string;
	    lastError?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.cron = source["cron"];
	        this.curl = source["curl"];
	        this.timeoutMs = source["timeoutMs"];
	        this.enabled = source["enabled"];
	        this.lastExecuted = source["lastExecuted"];
	        this.lastStatus = source["lastStatus"];
	        this.lastError = source["lastError"];
	    }
	}
	export class Config {
	    pushPlusToken: string;
	    tasks: TaskConfig[];
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pushPlusToken = source["pushPlusToken"];
	        this.tasks = this.convertValues(source["tasks"], TaskConfig);
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
	export class ParsedRequest {
	    url: string;
	    method: string;
	    headers: Record<string, string>;
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new ParsedRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.method = source["method"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	    }
	}

}

export namespace printer {
	
	export class Reportlet {
	    reportlet: string;
	    idMedpers: string;
	    orgNa: string;
	    idVismed: string;
	    documentNumber: string;
	
	    static createFrom(source: any = {}) {
	        return new Reportlet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reportlet = source["reportlet"];
	        this.idMedpers = source["idMedpers"];
	        this.orgNa = source["orgNa"];
	        this.idVismed = source["idVismed"];
	        this.documentNumber = source["documentNumber"];
	    }
	}
	export class PrintData {
	    reportlets: Reportlet[];
	
	    static createFrom(source: any = {}) {
	        return new PrintData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reportlets = this.convertValues(source["reportlets"], Reportlet);
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
	export class PrintParams {
	    printUrl: string;
	    printType: number;
	    pageType: number;
	    isPopUp: boolean;
	    printerName: string;
	    data: PrintData;
	    entryUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new PrintParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.printUrl = source["printUrl"];
	        this.printType = source["printType"];
	        this.pageType = source["pageType"];
	        this.isPopUp = source["isPopUp"];
	        this.printerName = source["printerName"];
	        this.data = this.convertValues(source["data"], PrintData);
	        this.entryUrl = source["entryUrl"];
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
	export class PrintResult {
	    requestId: string;
	    success: boolean;
	    error?: string;
	    durationMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new PrintResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.success = source["success"];
	        this.error = source["error"];
	        this.durationMs = source["durationMs"];
	    }
	}

}

