export namespace api {
	
	export class CameraDTO {
	    id: string;
	    name: string;
	    type: string;
	    xaddr: string;
	    username: string;
	    hasPassword: boolean;
	    profileToken: string;
	    streamUrl: string;
	    streamConfig: camera.StreamConfig;
	    ptzSupported: boolean;
	    groupId: string;
	    layoutOrder: number;
	    enabled: boolean;
	    addedAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new CameraDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.xaddr = source["xaddr"];
	        this.username = source["username"];
	        this.hasPassword = source["hasPassword"];
	        this.profileToken = source["profileToken"];
	        this.streamUrl = source["streamUrl"];
	        this.streamConfig = this.convertValues(source["streamConfig"], camera.StreamConfig);
	        this.ptzSupported = source["ptzSupported"];
	        this.groupId = source["groupId"];
	        this.layoutOrder = source["layoutOrder"];
	        this.enabled = source["enabled"];
	        this.addedAt = source["addedAt"];
	        this.updatedAt = source["updatedAt"];
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
	export class CreateCameraRequest {
	    name: string;
	    type: string;
	    xaddr: string;
	    username: string;
	    password: string;
	    profileToken: string;
	    streamUrl: string;
	    streamConfig?: camera.StreamConfig;
	    ptzSupported: boolean;
	    groupId: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateCameraRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.xaddr = source["xaddr"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.profileToken = source["profileToken"];
	        this.streamUrl = source["streamUrl"];
	        this.streamConfig = this.convertValues(source["streamConfig"], camera.StreamConfig);
	        this.ptzSupported = source["ptzSupported"];
	        this.groupId = source["groupId"];
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
	export class DiscoveredCamera {
	    xaddr: string;
	    scopes: string;
	
	    static createFrom(source: any = {}) {
	        return new DiscoveredCamera(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.xaddr = source["xaddr"];
	        this.scopes = source["scopes"];
	    }
	}
	export class GetProfilesRequest {
	    xaddr: string;
	    username: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new GetProfilesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.xaddr = source["xaddr"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}
	export class GetStreamURIRequest {
	    xaddr: string;
	    username: string;
	    password: string;
	    profileToken: string;
	    protocol: string;
	
	    static createFrom(source: any = {}) {
	        return new GetStreamURIRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.xaddr = source["xaddr"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.profileToken = source["profileToken"];
	        this.protocol = source["protocol"];
	    }
	}
	export class ProfileDTO {
	    token: string;
	    name: string;
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new ProfileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.name = source["name"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class StreamStatus {
	    running: boolean;
	    codec?: string;
	    width?: number;
	    height?: number;
	
	    static createFrom(source: any = {}) {
	        return new StreamStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.codec = source["codec"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class TestDirectStreamRequest {
	    url: string;
	    timeoutMs: number;
	
	    static createFrom(source: any = {}) {
	        return new TestDirectStreamRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.timeoutMs = source["timeoutMs"];
	    }
	}
	export class TestDirectStreamResponse {
	    ok: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new TestDirectStreamResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	    }
	}
	export class TestONVIFRequest {
	    xaddr: string;
	    username: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new TestONVIFRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.xaddr = source["xaddr"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}
	export class TestONVIFResponse {
	    ok: boolean;
	    error: string;
	    manufacturer: string;
	    model: string;
	    firmware: string;
	
	    static createFrom(source: any = {}) {
	        return new TestONVIFResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.manufacturer = source["manufacturer"];
	        this.model = source["model"];
	        this.firmware = source["firmware"];
	    }
	}
	export class UpdateCameraRequest {
	    name?: string;
	    xaddr?: string;
	    username?: string;
	    password?: string;
	    profileToken?: string;
	    streamUrl?: string;
	    streamConfig?: camera.StreamConfig;
	    ptzSupported?: boolean;
	    groupId?: string;
	    enabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateCameraRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.xaddr = source["xaddr"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.profileToken = source["profileToken"];
	        this.streamUrl = source["streamUrl"];
	        this.streamConfig = this.convertValues(source["streamConfig"], camera.StreamConfig);
	        this.ptzSupported = source["ptzSupported"];
	        this.groupId = source["groupId"];
	        this.enabled = source["enabled"];
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

export namespace camera {
	
	export class StreamConfig {
	    transport: string;
	    protocol: string;
	    buffer_size: number;
	
	    static createFrom(source: any = {}) {
	        return new StreamConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.transport = source["transport"];
	        this.protocol = source["protocol"];
	        this.buffer_size = source["buffer_size"];
	    }
	}

}

export namespace stream {
	
	export class Hub {
	
	
	    static createFrom(source: any = {}) {
	        return new Hub(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace ws {
	
	export class PTZCommand {
	    action: string;
	    pan?: number;
	    tilt?: number;
	    zoom?: number;
	    presetToken?: string;
	
	    static createFrom(source: any = {}) {
	        return new PTZCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.pan = source["pan"];
	        this.tilt = source["tilt"];
	        this.zoom = source["zoom"];
	        this.presetToken = source["presetToken"];
	    }
	}

}

