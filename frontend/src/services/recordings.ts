// 재생/녹화 API 클라이언트 — 타임라인, 이벤트 트리거, 녹화 상태
import {backendBase} from './backend';

const BASE = backendBase();

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(BASE + path, {
      method,
      headers: body !== undefined ? {'Content-Type': 'application/json'} : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new Error(`백엔드 서버(${BASE})에 연결할 수 없습니다. 앱이 실행 중인지 확인하세요.`);
  }
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const errBody = await res.json() as {error?: string};
      if (errBody.error) msg = errBody.error;
    } catch { /* 본문 없음 */ }
    throw new Error(msg);
  }
  return await res.json() as T;
}

export interface RecRange {
  fromMs: number;
  toMs: number;
  bytes: number;
  count: number;
}

export interface RecEvent {
  id: number;
  ts: number;
  type: string;
  segmentId: number;
}

export interface RecSegment {
  id: number;
  startTs: number;
  durMs: number;
  bytes: number;
  kind: string;
  flags: number;
}

export interface Timeline {
  cameraId: string;
  fromMs: number;
  toMs: number;
  ranges: RecRange[];
  segments: RecSegment[];
  events: RecEvent[];
  usedBytes: number;
}

export interface StorageStatus {
  path: string;
  freePercent: number;
  freeKnown: boolean;
  minFreePercent: number;
}

export interface RecordingInfo {
  cameraId: string;
  mode: string; // continuous | event | both
}

export interface RecordingStatus {
  enabled: boolean;
  recording: RecordingInfo[];
  usedBytes: number;
  storages: StorageStatus[];
}

export interface CamOverview {
  cameraId: string;
  segments: number;
  bytes: number;
  totalDurMs: number; // 실제 녹화 시간(공백 제외)
  firstMs: number;
  lastMs: number;
  events: number;
}

export interface Overview {
  fromMs: number;
  toMs: number;
  cameras: CamOverview[];
}

export interface DayCount {
  day: string; // YYYY-MM-DD (서버 로컬)
  segments: number;
  bytes: number;
  cameras: number;
}

export const recordings = {
  dayCounts: (fromMs: number, toMs: number) =>
    request<{fromMs: number; toMs: number; days: DayCount[]}>('GET', `/api/recordings/days?from=${fromMs}&to=${toMs}`),

  overview: (fromMs: number, toMs: number) =>
    request<Overview>('GET', `/api/recordings/overview?from=${fromMs}&to=${toMs}`),

  timeline: (cameraId: string, fromMs: number, toMs: number) =>
    request<Timeline>('GET', `/api/recordings/${cameraId}?from=${fromMs}&to=${toMs}`),

  playlistUrl: (cameraId: string, fromMs: number, toMs: number) =>
    `${BASE}/api/recordings/${cameraId}/playlist.m3u8?from=${fromMs}&to=${toMs}`,

  triggerEvent: (cameraId: string, type = 'manual') =>
    request<{ok: boolean}>('POST', `/api/cameras/${cameraId}/record/event`, {type}),

  status: () => request<RecordingStatus>('GET', '/api/recordings/status'),
};
