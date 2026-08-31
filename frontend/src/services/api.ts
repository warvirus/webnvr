// 백엔드 HTTP API(/api/*) 클라이언트 — 프론트엔드의 유일한 카메라 관리 통신 경로 (doc v1.1 §5.1)
// Wails 바인딩을 대체하며, 네이티브 앱과 DevServer 브라우저에서 동일하게 동작한다.
import * as t from '../types/api';

import {backendBase} from './backend';

const BASE = backendBase();

// request는 JSON 응답을 파싱하고 오류( error 필드 )를 예외로 변환한다.
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
  if (res.status === 204) return undefined as T;
  return await res.json() as T;
}

export const api = {
  health: () => request<t.HealthResponse>('GET', '/api/health'),

  listCameras: () => request<t.CameraDTO[]>('GET', '/api/cameras'),

  getCamera: (id: string) => request<t.CameraDTO>('GET', `/api/cameras/${id}`),

  createCamera: (req: t.CreateCameraRequest) =>
    request<t.CameraDTO>('POST', '/api/cameras', req),

  updateCamera: (id: string, req: t.UpdateCameraRequest) =>
    request<t.CameraDTO>('PUT', `/api/cameras/${id}`, req),

  deleteCamera: (id: string) => request<void>('DELETE', `/api/cameras/${id}`),

  reorderCameras: (ids: string[]) =>
    request<void>('POST', '/api/cameras/reorder', {ids}),

  discover: () => request<t.DiscoveredCamera[]>('POST', '/api/cameras/discover', {}),

  testONVIF: (req: t.TestONVIFRequest) =>
    request<t.TestONVIFResponse>('POST', '/api/cameras/test-onvif', req),

  testDirectStream: (req: t.TestDirectStreamRequest) =>
    request<t.TestDirectStreamResponse>('POST', '/api/cameras/test-direct', req),

  // 미등록 카메라의 프로필 조회 (자격증명 전달)
  onvifProfiles: (req: t.GetProfilesRequest) =>
    request<t.ProfileDTO[]>('POST', '/api/onvif/profiles', req),

  // 등록된 카메라의 조회 (저장 자격증명 사용 — 자격증명 미노출)
  cameraProfiles: (id: string) => request<t.ProfileDTO[]>('GET', `/api/cameras/${id}/profiles`),

  cameraPresets: (id: string) => request<t.PresetDTO[]>('GET', `/api/cameras/${id}/presets`),

  cameraStreamURI: (id: string) => request<{uri: string}>('GET', `/api/cameras/${id}/stream-uri`),

  // ── 설정/보안/백업 (doc 5.6) ──

  appConfig: () => request<t.AppConfig>('GET', '/api/config'),

  updateAppConfig: (cfg: t.AppConfig) =>
    request<t.AppConfig>('PUT', '/api/config', cfg),

  security: () => request<t.SecurityInfo>('GET', '/api/security'),

  backup: () => request<t.BackupFile>('GET', '/api/backup'),

  restoreBackup: (backup: t.BackupFile) =>
    request<t.RestoreResult>('POST', '/api/backup/restore', backup),
};
