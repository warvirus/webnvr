// 백엔드 주소 결정 규칙 (doc v1.1) — api.ts와 ws.ts가 공유한다.
//
// 컨텍스트별 location.hostname과 올바른 백엔드 호스트:
// ┌────────────────────────────────┬──────────────────┬─────────────────┐
// │ 컨텍스트                        │ hostname          │ 백엔드 호스트     │
// ├────────────────────────────────┼──────────────────┼─────────────────┤
// │ Wails 네이티브 셸               │ wails.localhost   │ 127.0.0.1        │ ← 가상 호스트
// │ DevServer (로컬 브라우저)        │ localhost         │ localhost        │
// │ 백엔드 서빙 UI (외부 브라우저)     │ <LAN IP>          │ 동일 (같은 오리진) │
// │ DevServer (외부 브라우저)        │ <LAN IP>          │ 해당 IP          │
// └────────────────────────────────┴──────────────────┴─────────────────┘

const HTTP_BACKEND_PORT = 8080;
const HTTPS_BACKEND_PORT = 8443;

// backendHost는 현재 컨텍스트에서 접근 가능한 백엔드 호스트명을 반환한다.
export function backendHost(): string {
  if (typeof location === 'undefined') return '127.0.0.1';
  const h = (location.hostname || '').toLowerCase();
  // Wails 네이티브 셸의 가상 호스트는 실제 주소가 아니므로 로컬 백엔드로 폴백한다.
  if (!h || h === 'wails.localhost' || h.endsWith('.wails.localhost')) {
    return '127.0.0.1';
  }
  return h;
}

// backendPort는 현재 프로토콜에 맞는 백엔드 포트를 반환한다.
function backendPort(): number {
  return typeof location !== 'undefined' && location.protocol === 'https:' ? HTTPS_BACKEND_PORT : HTTP_BACKEND_PORT;
}

// backendBase는 REST API의 기본 URL을 반환한다.
// HTTP/HTTPS 접속에 맞춰 자동으로 프로토콜과 포트 선택
export function backendBase(): string {
  const isHTTPS = typeof location !== 'undefined' && location.protocol === 'https:';
  const protocol = isHTTPS ? 'https' : 'http';
  return `${protocol}://${backendHost()}:${backendPort()}`;
}

// backendWS는 WS 엔드포인트 URL을 반환한다.
export function backendWS(): string {
  const isHTTPS = typeof location !== 'undefined' && location.protocol === 'https:';
  const protocol = isHTTPS ? 'wss' : 'ws';
  return `${protocol}://${backendHost()}:${backendPort()}/ws`;
}
