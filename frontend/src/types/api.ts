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
