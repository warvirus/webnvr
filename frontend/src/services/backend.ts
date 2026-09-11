// 백엔드 주소 결정 (same-origin first) — api.ts와 ws.ts가 공유한다.
//
// 원칙: UI가 서빙된 오리진 = 백엔드 오리진. 백엔드 포트(config ws_port)가 바뀌어도
// 프론트는 포트를 알 필요가 없다.
//
// ┌──────────────────────────┬────────────────────────────────────────────┐
// │ 컨텍스트                  │ 백엔드 주소                                 │
// ├──────────────────────────┼────────────────────────────────────────────┤
// │ 프로덕션 (백엔드 서빙 UI)  │ 상대 경로 — UI 오리진이 곧 백엔드 오리진      │
// │ dev (vite 5173)          │ 상대 경로 — vite 프록시가 /api,/ws를 전달    │
// │ Wails 네이티브 셸         │ 127.0.0.1:<주입 포트> — 셸이 포트를 주입     │
// └──────────────────────────┴────────────────────────────────────────────┘
//
// Wails 셸은 가상 호스트(wails.localhost)라 same-origin으로 백엔드에 도달할 수 없다.
// 셸의 assetserver 미들웨어가 index.html에 window.__WEBNVR_BACKEND_PORT__를
// 주입하며, 값은 config ws_port다(미주입 시 8080 폴백).

const WAILS_FALLBACK_PORT = 8080;

// isWailsShell은 Wails 네이티브 셸 컨텍스트인지 판정한다 (가상 호스트).
function isWailsShell(): boolean {
  if (typeof location === 'undefined') return false;
  const h = (location.hostname || '').toLowerCase();
  return !h || h === 'wails.localhost' || h.endsWith('.wails.localhost');
}

// wailsPort는 셸이 주입한 백엔드 포트를 반환한다.
function wailsPort(): number {
  const p = typeof window !== 'undefined' ? window.__WEBNVR_BACKEND_PORT__ : undefined;
  return p && p > 0 ? p : WAILS_FALLBACK_PORT;
}

// backendHost는 표시용 백엔드 "host[:port]" 문자열을 반환한다. (StatusBar)
export function backendHost(): string {
  if (typeof location === 'undefined') return `127.0.0.1:${WAILS_FALLBACK_PORT}`;
  if (isWailsShell()) return `127.0.0.1:${wailsPort()}`;
  return location.host || `127.0.0.1:${WAILS_FALLBACK_PORT}`;
}

// backendBase는 REST API의 기본 URL을 반환한다. same-origin이면 '' (상대 경로).
export function backendBase(): string {
  if (typeof location === 'undefined') return `http://127.0.0.1:${WAILS_FALLBACK_PORT}`;
  if (isWailsShell()) {
    const proto = location.protocol === 'https:' ? 'https' : 'http';
    return `${proto}://127.0.0.1:${wailsPort()}`;
  }
  return ''; // same-origin — 포트·호스트 무관, 백엔드 서빙 포트를 그대로 따른다
}

// backendWS는 WS 엔드포인트 URL을 반환한다.
export function backendWS(): string {
  if (typeof location === 'undefined') return `ws://127.0.0.1:${WAILS_FALLBACK_PORT}/ws`;
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  if (isWailsShell()) {
    return `${proto}://127.0.0.1:${wailsPort()}/ws`;
  }
  return `${proto}://${location.host}/ws`;
}

// window 전역 선언 — Wails 셸 미들웨어가 index.html에 주입한다.
declare global {
  interface Window {
    __WEBNVR_BACKEND_PORT__?: number;
  }
}
