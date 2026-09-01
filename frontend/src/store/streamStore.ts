// 스트림 상태(스트리밍 중 카메라, 통계)와 WS↔디코더 연결을 관리하는 스토어
// v1.1: 디코더 파이프라인은 메인 스레드에서 실행한다 (Safari WebKit은 Worker 내
// VideoDecoder 출력이 동작하지 않는 사례 대응 — F8). VideoDecoder는 비동기 HW 가속.
import {create} from 'zustand';
import {wsService} from '../services/ws';
import {DecoderHub} from '../services/decoder';
import {PacketIn} from '../types/stream';
import {StatSample, StreamState, StreamStats} from '../types/stream';

// 통계 히스토리 샘플 (스파크라인용) — types/stream에서 재노출
export type {StatSample} from '../types/stream';

const STATS_HISTORY_MAX = 60; // 최근 60초

interface StreamStoreState {
  // cameraId → 상태
  states: Record<string, StreamState>;
  stats: Record<string, StreamStats>;
  // cameraId → 통계 히스토리 (스파크라인용)
  history: Record<string, StatSample[]>;
  // cameraId → 사용자가 의도한 스트림 상태 (true=재생 중이어야 함 — 자동 재연결 기준)
  desired: Record<string, boolean>;
  // cameraId → 자동 재연결 시도 횟수 (타일 표시용)
  retries: Record<string, number>;
  connected: boolean;
  lastError: string | null;

  startStream: (cameraId: string) => void;
  stopStream: (cameraId: string) => void;
  startAllStreams: (cameraIds: string[]) => void;
  stopAllStreams: () => void;
  ptzControl: (cameraId: string, command: {action: 'move' | 'stop' | 'preset'; pan?: number; tilt?: number; zoom?: number; presetToken?: string}) => void;
  init: () => () => void;
}

// 디코더 허브 싱글턴 (모듈 로드 시 1회 생성)
let hub: DecoderHub | null = null;

// ── 자동 재연결(Desired-State Reconciler) ─────────────────────
// desired[cameraId]=true인 스트림이 'streaming'이 아니면 재시도한다.
// 중요: 'starting'(연결/GOP 대기 진행 중)은 실패가 아니다 — 시도 시간 초과 시에만
// 재시도한다. 그렇지 않으면 진행 중인 세션을 계속 리셋해 영상이 영원히 못 나온다.
const RETRY_BASE_MS = 1_000;
const RETRY_MAX_MS = 15_000;
const ATTEMPT_TIMEOUT_MS = 20_000; // start_stream 후 성공/실패 판정 대기 상한 (dial 10s + GOP 여유)
let nextRetryAt: Record<string, number> = {};      // 실패 백오프 예정 시각 (비반응형)
let lastAttemptAt: Record<string, number> = {};    // 마지막 start_stream 전송 시각 (비반응형)
let reconcileTimer: ReturnType<typeof setInterval> | null = null;

function retryDelayMs(attempt: number): number {
  return Math.min(RETRY_BASE_MS * 2 ** Math.min(attempt - 1, 4), RETRY_MAX_MS);
}

function ensureReconciler(): void {
  if (reconcileTimer) return;
  reconcileTimer = setInterval(() => tickReconciler(), 1_000);
}

function tickReconciler(): void {
  const s = useStreamStore.getState();
  const now = Date.now();
  const patchRetries: Record<string, number> = {};
  let changed = false;

  for (const [cameraId, want] of Object.entries(s.desired)) {
    if (!want) continue;
    const st = s.states[cameraId];
    if (st === 'streaming') {
      // 정상 출력 — 시도 카운터 리셋
      if (s.retries[cameraId]) {
        patchRetries[cameraId] = 0;
        nextRetryAt[cameraId] = 0;
        changed = true;
      }
      continue;
    }

    const last = lastAttemptAt[cameraId] ?? 0;
    const attempt = s.retries[cameraId] ?? 0;
    const elapsed = now - last;

    // 진행 중인 시도는 상한(20초) 내에서는 건드리지 않는다
    if (st === 'starting' && elapsed < ATTEMPT_TIMEOUT_MS) continue;
    // 실패 상태는 백오프 대기 후 재시도
    if ((st === 'error' || st === undefined) && elapsed < retryDelayMs(Math.max(attempt, 1))) continue;

    // (재)시도
    lastAttemptAt[cameraId] = now;
    patchRetries[cameraId] = attempt + 1;
    changed = true;
    getHub().reset(cameraId); // 이전 세션 정리 후 신규 시작
    wsService.send({type: 'start_stream', cameraId});
  }
  if (changed) useStreamStore.setState({retries: {...s.retries, ...patchRetries}});
}

function getHub(): DecoderHub {
  if (!hub) {
    hub = new DecoderHub({
      onFrame: (cameraId, frame) => {
        // 프레임은 VideoFrameRenderer(CustomEvent)로 타일에 전달한다
        // (스토어에 VideoFrame을 보관하지 않아 GC 부담 최소화)
        window.dispatchEvent(new CustomEvent('webnvr-frame', {detail: {cameraId, frame}}));
      },
      onDecoded: (cameraId) => {
        // 디코딩 성공 → 재시도 카운터 리셋 + 배너 해제
        nextRetryAt[cameraId] = 0;
        useStreamStore.setState(s => ({
          states: {...s.states, [cameraId]: 'streaming'},
          retries: {...s.retries, [cameraId]: 0},
          lastError: null,
        }));
      },      onNotice: (cameraId, message) => {
        // 자가 치유 진행(포맷 전환 등) — 타일 상태는 유지하고 배너에만 표시
        useStreamStore.setState({lastError: message});
      },
      onError: (cameraId, message) => {
        useStreamStore.setState(s => ({
          states: {...s.states, [cameraId]: 'error'},
          lastError: message,
        }));
      },
      onStats: (cameraId, stats) => {
        useStreamStore.setState(s => {
          const hist = [...(s.history[cameraId] ?? []), {fps: stats.fps, kbps: stats.kbps}];
          if (hist.length > STATS_HISTORY_MAX) hist.splice(0, hist.length - STATS_HISTORY_MAX);
          return {
            stats: {...s.stats, [cameraId]: stats},
            history: {...s.history, [cameraId]: hist},
          };
        });
      },
    });
  }
  return hub;
}

export const useStreamStore = create<StreamStoreState>((set, get) => ({
  states: {},
  stats: {},
  history: {},
  desired: {},
  retries: {},
  connected: false,
  lastError: null,

  init: () => {
    // WS 서버 메시지 → 디코더/상태 라우팅
    const offMsg = wsService.on(msg => {
      const cameraId = msg.cameraId ?? '';
      switch (msg.type) {
        case 'stream_started': {
          getHub().config(cameraId, {
            codec: (msg.codec ?? 'h264') as 'h264' | 'h265',
            sps: msg.sps ?? '',
            pps: msg.pps ?? '',
            vps: msg.vps ?? '',
            clockRate: msg.clockRate ?? 90000,
          });
          // 화면 표시는 첫 프레임 디코딩('decoded') 시점으로 전환 — GOP 대기 중 "연결 중" 유지
          set(s => ({states: {...s.states, [cameraId]: 'starting'}}));
          break;
        }
        case 'rtp_packet': {
          const p: PacketIn = {
            seq: (msg.sequence ?? 0) & 0xffff,
            ts: (msg.timestamp ?? 0) >>> 0,
            marker: !!msg.marker,
            payload: base64ToBytes(msg.payload ?? ''),
          };
          getHub().packet(cameraId, p);
          break;
        }
        case 'rtp_batch': {
          // 고비트레이트 스트림: 여러 패킷을 한 메시지로 전달 (백엔드 확장)
          if (msg.packets && msg.packets.length > 0) {
            const hub = getHub();
            for (const p of msg.packets) {
              hub.packet(cameraId, {
                seq: (p.sq ?? 0) & 0xffff,
                ts: (p.ts ?? 0) >>> 0,
                marker: !!p.m,
                payload: base64ToBytes(p.p),
              });
            }
          }
          break;
        }
        case 'stream_stopped': {
          getHub().detach(cameraId);
          set(s => {
            const states = {...s.states};
            const stats = {...s.stats};
            const history = {...s.history};
            delete states[cameraId];
            delete stats[cameraId];
            delete history[cameraId];
            return {states, stats, history};
          });
          break;
        }
        case 'stream_error': {
          getHub().detach(cameraId);
          set(s => ({states: {...s.states, [cameraId]: 'error'}, lastError: msg.error ?? '알 수 없는 오류'}));
          break;
        }
        case 'pong':
          break;
      }
    });

    // 연결 상태 추적 + 재연결 시 세션 리셋
    const offStatus = wsService.onStatus(connected => {
      set({connected});
      if (!connected) {
        // 연결이 끊기면 모든 스트림이 유실된 것으로 간주한다.
        // desired는 유지 — WS 재연결 후 리컨실리어가 자동으로 다시 시작한다.
        const h = getHub();
        Object.keys(get().states).forEach(cameraId => h.detach(cameraId));
        set({states: {}, stats: {}, history: {}});
      } else {
        // 재연결 직후 리컨실리어가 즉시 desired 스트림을 복구하도록 백오프 해제
        nextRetryAt = {};
        lastAttemptAt = {};
      }
    });

    wsService.connect();
    ensureReconciler(); // desired 스트림 자동 재연결 감시 시작

    return () => {
      offMsg();
      offStatus();
    };
  },

  startStream: (cameraId) => {
    ensureReconciler();
    // 사용자 의도 등록 + 이전 세션/백오프 완전 초기화 후 신규 시작
    nextRetryAt[cameraId] = 0;
    lastAttemptAt[cameraId] = Date.now();
    set(s => ({
      desired: {...s.desired, [cameraId]: true},
      retries: {...s.retries, [cameraId]: 0},
      states: {...s.states, [cameraId]: 'starting'},
    }));
    getHub().reset(cameraId);
    wsService.send({type: 'start_stream', cameraId});
  },

  stopStream: (cameraId) => {
    // 사용자 의도 해제 — 자동 재연결 대상에서 제외
    nextRetryAt[cameraId] = 0;
    delete lastAttemptAt[cameraId];
    set(s => ({
      desired: {...s.desired, [cameraId]: false},
      retries: {...s.retries, [cameraId]: 0},
    }));
    wsService.send({type: 'stop_stream', cameraId});
    getHub().detach(cameraId);
    set(s => {
      const states = {...s.states};
      const stats = {...s.stats};
      const history = {...s.history};
      delete states[cameraId];
      delete stats[cameraId];
      delete history[cameraId];
      return {states, stats, history};
    });
  },

  startAllStreams: (cameraIds) => {
    cameraIds.forEach(id => get().startStream(id));
  },

  stopAllStreams: () => {
    Object.keys(get().states).forEach(id => get().stopStream(id));
    Object.keys(get().desired).forEach(id => {
      if (get().desired[id]) get().stopStream(id);
    });
    wsService.send({type: 'stop_all_streams'});
  },

  ptzControl: (cameraId, command) => {
    wsService.send({type: 'ptz', cameraId, command});
  },
}));

// base64 → Uint8Array (ws → 디코더 경로)
function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// 카메라를 layout_order 순으로 정렬해 반환하는 셀렉터는 cameraStore에 있다.
