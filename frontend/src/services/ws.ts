// 백엔드 WebSocket 연결을 관리하는 싱글턴 서비스 (재연결 지수백오프, 전송 큐, 하트비트)
import {ClientMsg, ServerMsg} from '../types';

import {backendWS} from './backend';
const HEARTBEAT_MS = 25_000;
const MAX_BACKOFF_MS = 30_000;
const REJECT_RETRY_MS = 60_000;
const MAX_QUEUE = 100; // 재연결 대기 큐 상한 — 장애가 길어져도 스탬피드를 막는다

type Handler = (msg: ServerMsg) => void;
// 끊긴 경우 reconnectAt = 다음 재시도 예정 시각(epoch ms)
type StatusHandler = (connected: boolean, reconnectAt?: number) => void;
type RejectHandler = (retryAt: number) => void;

// WsService는 단일 WS 연결을 유지하며 재연결과 전송 큐를 처리한다.
export class WsService {
  private ws: WebSocket | null = null;
  private handlers = new Set<Handler>();
  private statusHandlers = new Set<StatusHandler>();
  private rejectHandlers = new Set<RejectHandler>();
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
      //console.log('📤 대기 중인 메시지 전송:', pending.length);
      pending.forEach(m => this.send(m));
    };

    ws.onmessage = ev => {
      try {
        const msg = JSON.parse(ev.data as string) as ServerMsg;

        // 동시 접속 제한 거부 처리
        if (msg.type === 'client_limit_exceeded') {
          console.log('⚠️ 동시 접속 제한 도달 — 60초 후 재시도');
          this.backoff = REJECT_RETRY_MS;
          const retryAt = Date.now() + REJECT_RETRY_MS;
          this.rejectHandlers.forEach(h => h(retryAt));
          return;
        }

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

  // onRejected는 동시 접속 제한 거부 핸들러를 등록한다.
  onRejected(handler: RejectHandler): () => void {
    this.rejectHandlers.add(handler);
    return () => this.rejectHandlers.delete(handler);
  }

  // send는 메시지를 전송한다. 연결이 끊긴 경우 큐에 적재한다.
  // start_stream/stop_stream는 카메라별 마지막 의도만 유지한다(last-wins) —
  // 리컨실리어가 끊긴 동안 1초마다 쌓는 start_stream이 재연결 시 스탬피드가 되는 것을 막는다.
  send(msg: ClientMsg) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
      return;
    }
    if (msg.type === 'ptz' || msg.type === 'ping') {
      return; // 일회성 명령은 큐잉하지 않는다 (재연결 시 무의미)
    }
    const key = msg.cameraId ? `${msg.type}:${msg.cameraId}` : null;
    if (key) {
      // 같은 (type, cameraId)의 이전 메시지는 마지막 의도로 대체된다
      this.queue = this.queue.filter(m => (m.cameraId ? `${m.type}:${m.cameraId}` : null) !== key);
    }
    this.queue.push(msg);
    if (this.queue.length > MAX_QUEUE) {
      this.queue.splice(0, this.queue.length - MAX_QUEUE);
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
    const delay = this.backoff;
    this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS);
    const reconnectAt = Date.now() + delay;
    this.statusHandlers.forEach(h => h(false, reconnectAt));
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
    this.rejectHandlers.clear();
  }
}

export const wsService = new WsService();
