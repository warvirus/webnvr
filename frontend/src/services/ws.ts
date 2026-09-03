// 백엔드 WebSocket 연결을 관리하는 싱글턴 서비스 (재연결 지수백오프, 전송 큐, 하트비트)
import {ClientMsg, ServerMsg} from '../types';

import {backendWS} from './backend';
const HEARTBEAT_MS = 25_000;
const MAX_BACKOFF_MS = 30_000;

type Handler = (msg: ServerMsg) => void;
type StatusHandler = (connected: boolean) => void;

// WsService는 단일 WS 연결을 유지하며 재연결과 전송 큐를 처리한다.
export class WsService {
  private ws: WebSocket | null = null;
  private handlers = new Set<Handler>();
  private statusHandlers = new Set<StatusHandler>();
  private queue: ClientMsg[] = [];
  private backoff = 1000;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private disposed = false;
  public connected = false;

  connect() {
    if (this.disposed || this.ws) return;
    const url = backendWS();
    console.log('🔗 WebSocket 연결 시도:', url);
    const ws = new WebSocket(url);
    this.ws = ws;

    ws.onopen = () => {
      console.log('✅ WebSocket 연결 성공!', {url, readyState: ws.readyState});
      this.backoff = 1000;
      this.connected = true;
      this.statusHandlers.forEach(h => h(true));
      this.startHeartbeat();
      // 대기 중이던 요청을 먼저 보낸다
      const pending = this.queue.splice(0);
      console.log('📤 대기 중인 메시지 전송:', pending.length);
      pending.forEach(m => this.send(m));
    };

    ws.onmessage = ev => {
      try {
        const msg = JSON.parse(ev.data as string) as ServerMsg;
        this.handlers.forEach(h => h(msg));
      } catch {
        // 잘못된 JSON 무시
      }
    };

    ws.onclose = () => {
      console.log('❌ WebSocket 연결 종료');
      this.cleanup();
      if (!this.disposed) this.scheduleReconnect();
    };
    ws.onerror = (event) => {
      console.log('⚠️ WebSocket 에러:', event);
      // onclose에서 재연결 처리
    };
  }

  // on은 서버 메시지 핸들러를 등록하고 해제 함수를 반환한다.
  on(handler: Handler): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  // onStatus는 연결 상태 변경 핸들러를 등록한다.
  onStatus(handler: StatusHandler): () => void {
    this.statusHandlers.add(handler);
    return () => this.statusHandlers.delete(handler);
  }

  // send는 메시지를 전송한다. 연결이 끊긴 경우 큐에 적재한다.
  send(msg: ClientMsg) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    } else if (msg.type === 'ptz' || msg.type === 'ping') {
      // 일회성 명령은 큐잉하지 않는다 (재연결 시 무의미)
    } else {
      this.queue.push(msg);
    }
  }

  private startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => this.send({type: 'ping'}), HEARTBEAT_MS);
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
    this.heartbeatTimer = null;
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return;
    this.statusHandlers.forEach(h => h(false));
    const delay = this.backoff;
    this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS);
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private cleanup() {
    this.stopHeartbeat();
    this.connected = false;
    this.ws = null;
  }

  dispose() {
    this.disposed = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.cleanup();
    this.ws?.close();
    this.handlers.clear();
    this.statusHandlers.clear();
  }
}

export const wsService = new WsService();
