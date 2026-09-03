// 백엔드 HTTP API(/api/*)의 요청/응답 DTO — 서버 JSON 스키마와 1:1 대응 (doc v1.1 §5.1)

export type CameraType = 'onvif' | 'rtsp' | 'rtp' | 'rtmp';

export interface StreamConfig {
  transport: string; // 'tcp' | 'udp'
  protocol: string;  // 'rtsp' | 'rtp' | 'rtmp'
  buffer_size: number;
}

export interface CameraDTO {
  id: string;
  name: string;
  type: CameraType;
  xaddr: string;
  username: string;
  hasPassword: boolean;
  profileToken: string;
  streamUrl: string;
  streamConfig: StreamConfig;
  ptzSupported: boolean;
  groupId: string;
  layoutOrder: number;
  enabled: boolean;
  addedAt: string;
  updatedAt: string;
}

export interface CreateCameraRequest {
  name: string;
  type: CameraType;
  xaddr: string;
  username: string;
  password: string;
  profileToken: string;
  streamUrl: string;
  streamConfig?: StreamConfig;
  ptzSupported: boolean;
  groupId: string;
}

export interface UpdateCameraRequest {
  name?: string;
  xaddr?: string;
  username?: string;
  password?: string;
  profileToken?: string;
  streamUrl?: string;
  streamConfig?: StreamConfig;
  ptzSupported?: boolean;
  groupId?: string;
  enabled?: boolean;
}

export interface DiscoveredCamera {
  xaddr: string;
  scopes: string;
}

export interface TestONVIFRequest {
  xaddr: string;
  username: string;
  password: string;
}

export interface TestONVIFResponse {
  ok: boolean;
  error: string;
  manufacturer: string;
  model: string;
  firmware: string;
}

export interface GetProfilesRequest {
  xaddr: string;
  username: string;
  password: string;
}

export interface ProfileDTO {
  token: string;
  name: string;
  width: number;
  height: number;
}

export interface GetStreamURIRequest {
  xaddr: string;
  username: string;
  password: string;
  profileToken: string;
  protocol: string;
}

export interface TestDirectStreamRequest {
  url: string;
  timeoutMs: number;
}

export interface TestDirectStreamResponse {
  ok: boolean;
  error: string;
}

export interface PresetDTO {
  token: string;
  name: string;
}

export interface HealthResponse {
  ok: boolean;
  version: number;
}

// ── 설정/보안/백업 (doc 5.6) ──

export interface AppConfig {
  version: number;
  server: {ws_port: number; http_port: number; max_clients: number};
  stream: {
    default_transport: string;
    rtp_timeout_ms: number;
    jitter_buffer_ms: number;
    max_concurrent_streams: number;
  };
  discovery: {
    scan_timeout_ms: number;
    scan_interfaces: string[];
    auto_scan_interval_min: number;
  };
  decoder: {prefer_hardware: boolean; max_threads: number};
  logging: {level: string; file: string; max_size_mb: number; max_backups: number};
}

export interface SecurityInfo {
  masterKeySource: string;
}

export interface BackupFile {
  version: number;
  exportedAt: string;
  cameras: CameraDTO[];
  appConfig?: AppConfig;
}

export interface RestoreResult {
  ok: boolean;
  restored: number;
}
