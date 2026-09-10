// 스트림 상태(스트리밍 중 카메라, 통계)와 WS↔디코더 연결을 관리하는 스토어
// v1.1: 디코더 파이프라인은 메인 스레드에서 실행한다 (Safari WebKit은 Worker 내
// VideoDecoder 출력이 동작하지 않는 사례 대응 — F8). VideoDecoder는 비동기 HW 가속.
import {create} from 'zustand';
import {wsService} from '../services/ws';
import {useCameraStore} from './cameraStore';
import {consumeSelfEdit} from './selfEdits';
import {useUIStore} from './uiStore';
import {DecoderHub} from '../services/decoder';
import {PacketIn} from '../types/stream';
import {StatSample, StreamState, StreamStats} from '../types/stream';
import {recordings, RecordingInfo} from '../services/recordings';

// 다른 클라이언트가 카메라를 수정/재정렬/복원했을 때 툴바에 띄우는 "적용 대기" 상태.
// 추가/삭제는 즉시 반영하므로 여기에 담지 않는다.
export interface PendingCameraUpdate {
  count: number;      // 수신한 변경 알림 수 (배지 표시용)
  ids: string[];      // 재연결 대상 카메라 ID (updated)
  reloadAll: boolean; // restored — 스트리밍 중인 전 카메라 재연결
}

// 통계 히스토리 샘플 (스파크라인용) — types/stream에서 재노출
export type {StatSample} from '../types/stream';

const STATS_HISTORY_MAX = 60; // 최근 60초

interface StreamStoreState {
  // cameraId → 상태
  states: Record<string, StreamState>;
  stats: Record<string, StreamStats>;
  // cameraId → 통계 히스토리 (스파크라인용)
  history: Record<string, StatSample[]>;
  // cameraId → 해상도 {width, height}
  resolution: Record<string, {width: number; height: number}>;
  // cameraId → 사용자가 의도한 스트림 상태 (true=재생 중이어야 함 — 자동 재연결 기준)
  desired: Record<string, boolean>;
  // cameraId → 자동 재연결 시도 횟수 (타일 표시용)
  retries: Record<string, number>;
  connected: boolean;
  rejected: boolean;
  retryAt: number | null;
  // 연결이 끊긴 동안 다음 WS 재시도 예정 시각(epoch ms). 연결되면 null.
  reconnectAt: number | null;
  // 다른 클라이언트의 카메라 수정/재정렬/복원 — 사용자가 툴바 버튼으로 반영한다.
  pendingCameraUpdate: PendingCameraUpdate;
  // 녹화 중 카메라(모드 포함) — 1분 폴링 + recording_state WS 수신 시 즉시 갱신
  recording: Record<string, RecordingInfo>;
  // 녹화 데이터 총 사용량(bytes) — refreshRecording에서 함께 갱신 (topbar 표기용)
  recUsedBytes: number;

  startStream: (cameraId: string) => void;
  stopStream: (cameraId: string) => void;
  startAllStreams: (cameraIds: string[]) => void;
  stopAllStreams: () => void;
  ptzControl: (cameraId: string, command: {action: 'move' | 'stop' | 'preset'; pan?: number; tilt?: number; zoom?: number; presetToken?: string}) => void;
  applyCameraUpdate: () => Promise<void>;
  init: () => () => void;
}

// 디코더 허브 싱글턴 (모듈 로드 시 1회 생성)
let hub: DecoderHub | null = null;

// ── 프레임 구독 레지스트리 ────────────────────────────────────
// 화면에 표시 중인 타일(CameraTile)이 카메라별로 등록한다. 구독자가 없는 카메라의
// VideoFrame은 디코더 허브가 즉시 close한다(페이지네이션 숨김 채널의 GPU 메모리 누수 방지).
const frameListeners = new Map<string, number>();

// frameListenerAdded/Removed는 타일이 구독을 시작/끝낼 때 호출한다.
export function frameListenerAdded(cameraId: string): void {
  frameListeners.set(cameraId, (frameListeners.get(cameraId) ?? 0) + 1);
}
export function frameListenerRemoved(cameraId: string): void {
  const n = (frameListeners.get(cameraId) ?? 1) - 1;
  if (n <= 0) frameListeners.delete(cameraId);
  else frameListeners.set(cameraId, n);
}

// ── 자동 재연결(Desired-State Reconciler) ─────────────────────
// desired[cameraId]=true인 스트림이 'streaming'이 아니면 재시도한다.
// 중요: 'starting'(연결/GOP 대기 진행 중)은 실패가 아니다 — 시도 시간 초과 시에만
// 재시도한다. 그렇지 않으면 진행 중인 세션을 계속 리셋해 영상이 영원히 못 나온다.
const RETRY_BASE_MS = 1_000;
const RETRY_MAX_MS = 60_000; // 영구 오류(404 등) 시에도 서버 부하를 주지 않는다
const ATTEMPT_TIMEOUT_MS = 20_000; // start_stream 후 성공/실패 판정 대기 상한 (dial 10s + GOP 여유)
let nextRetryAt: Record<string, number> = {};      // 실패 백오프 예정 시각 (비반응형)
let lastAttemptAt: Record<string, number> = {};    // 마지막 start_stream 전송 시각 (비반응형)
let reconcileTimer: ReturnType<typeof setInterval> | null = null;

// ── 설정 변경 적용(reload_stream) ────────────────────────────
// "적용" 버튼이 여러 카메라를 동시에 재연결하면 ONVIF 재조회가 몰려(thundering herd)
// 재다이얼이 실패하고 "스트림 오류"가 쏟아진다. 카메라별로 시차를 두고,
// teardown 후 카메라가 옛 RTSP 세션을 놓을 시간을 준 뒤 재구독한다.
const RELOAD_STAGGER_MS = 400;      // 카메라 간 reload_stream 간격
const RELOAD_REDIAL_DELAY_MS = 2_500; // teardown 후 재구독까지 대기 (카메라 세션 해제 여유)
// 의도적 재연결 중인 카메라 — 뒤따르는 stream_stopped를 "예상된 정지"로 처리한다.
const reloadingIds = new Set<string>();

function retryDelayMs(attempt: number): number {
  return Math.min(RETRY_BASE_MS * 2 ** Math.min(attempt - 1, 4), RETRY_MAX_MS);
}

function ensureReconciler(): void {
  if (reconcileTimer) return;
  reconcileTimer = setInterval(() => tickReconciler(), 1_000);
}

// ── cameras_changed 처리 ─────────────────────────────────────
// added/deleted는 목록만 새로고침하면 MonitoringPage의 id-diff effect가 반영한다.
// 버스트(재정렬 드래그, 다중 추가)를 합치기 위해 250ms 디바운스한다.
let refetchTimer: ReturnType<typeof setTimeout> | null = null;
function scheduleCameraRefetch(): void {
  if (refetchTimer) return;
  refetchTimer = setTimeout(() => {
    refetchTimer = null;
    useCameraStore.getState().fetchCameras().catch(() => {
      // 조용히 실패 — 다음 브로드캐스트/재연결에서 복구
    });
  }, 250);
}

// updated/reordered/restored는 자동 반영하지 않고 툴바 배지로 누적한다.
function notePendingUpdate(reason: string | undefined, cameraId: string | undefined): void {
  // 이 클라이언트가 만든 변경의 에코이면 자기 자신에게는 배지를 띄우지 않는다.
  if (consumeSelfEdit(reason, cameraId)) return;
  const cur = useStreamStore.getState().pendingCameraUpdate;
  const ids = cur.ids.slice();
  if (reason === 'updated' && cameraId && !ids.includes(cameraId)) ids.push(cameraId);
  useStreamStore.setState({
    pendingCameraUpdate: {
      count: cur.count + 1,
      ids,
      reloadAll: cur.reloadAll || reason === 'restored',
    },
  });
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
        // 구독 중인 타일이 없으면(페이지네이션으로 숨겨진 채널) 즉시 close한다 —
        // GPU 백 VideoFrame이 GC에만 의존하면 장시간 운용에서 메모리 압박이 온다.
        if ((frameListeners.get(cameraId) ?? 0) === 0) {
          frame.close();
          return;
        }
        window.dispatchEvent(new CustomEvent('webnvr-frame', {detail: {cameraId, frame}}));
      },
      onDecoded: (cameraId) => {
        // 디코딩 성공 → 재시도 카운터 리셋
        nextRetryAt[cameraId] = 0;
        useStreamStore.setState(s => ({
          states: {...s.states, [cameraId]: 'streaming'},
          retries: {...s.retries, [cameraId]: 0},
        }));
      },      onNotice: (cameraId, message) => {
        // 자가 치유 진행(포맷 전환 등) — 타일 상태는 유지. 레이아웃을 밀지 않도록
        // 하단 고정 토스트로만 알린다.
        useUIStore.getState().pushToast('info', message);
      },
      onError: (cameraId, message) => {
        // 디코더 오류 — 타일이 '오류' 상태로 표시되므로 레이아웃을 밀지 않게
        // 하단 고정 토스트로만 알린다(기존 그리드 위 배너 제거).
        useStreamStore.setState(s => ({states: {...s.states, [cameraId]: 'error'}}));
        useUIStore.getState().pushToast('error', message);
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
  resolution: {},
  desired: {},
  retries: {},
  connected: false,
  rejected: false,
  retryAt: null,
  reconnectAt: null,
  pendingCameraUpdate: {count: 0, ids: [], reloadAll: false},
  recording: {},
  recUsedBytes: 0,

  init: () => {
    console.log('🔌 streamStore.init() 시작 — WS 연결 초기화');

    // 녹화 상태 조회 + 갱신 스케줄 (1분 폴링 + recording_state 수신 시 즉시)
    let recTimer: ReturnType<typeof setInterval> | null = null;
    const refreshRecording = () => {
      recordings.status()
        .then(s => {
          const map: Record<string, RecordingInfo> = {};
          for (const r of s.recording) map[r.cameraId] = r;
          useStreamStore.setState({recording: map, recUsedBytes: s.usedBytes});
        })
        .catch(() => { /* 백엔드 다운 — 마지막 상태 유지 */ });
    };
    refreshRecording();
    recTimer = setInterval(refreshRecording, 60_000);

    // WS 서버 메시지 → 디코더/상태 라우팅
    const offMsg = wsService.on(msg => {
      const cameraId = msg.cameraId ?? '';
      switch (msg.type) {
        case 'recording_state':
          refreshRecording(); // 녹화 세션 변화 → 즉시 갱신 (1분 폴링 보완)
          break;
        case 'stream_started': {
          console.log('📤 스트림 시작:', {cameraId, codec: msg.codec, resolution: `${msg.width}x${msg.height}`});
          const hub = getHub();
          // 기존 세션이 있으면 정리하고 새로 생성 (경합 조건 해결)
          hub.detach(cameraId);
          hub.attach(cameraId);
          hub.config(cameraId, {
            codec: (msg.codec ?? 'h264') as 'h264' | 'h265',
            sps: msg.sps ?? '',
            pps: msg.pps ?? '',
            vps: msg.vps ?? '',
            clockRate: msg.clockRate ?? 90000,
          });
          // 화면 표시는 첫 프레임 디코딩('decoded') 시점으로 전환 — GOP 대기 중 "연결 중" 유지
          set(s => ({
            states: {...s.states, [cameraId]: 'starting'},
            resolution: {...s.resolution, [cameraId]: {width: msg.width ?? 0, height: msg.height ?? 0}},
          }));
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
          if (reloadingIds.has(cameraId)) {
            // 의도적 재연결 — 상태를 지우지 않고 'starting' 유지 (곧 start_stream 재전송).
            // 리컨실리어는 lastAttemptAt이 최신이라 20초 동안 개입하지 않는다.
            set(s => ({states: {...s.states, [cameraId]: 'starting'}}));
            break;
          }
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
          console.error('❌ stream_error 수신', {cameraId, error: msg.error});
          getHub().detach(cameraId);
          // 백엔드 RTSP 오류 — 타일 '오류' 상태 + 하단 토스트(레이아웃 무영향)
          set(s => ({states: {...s.states, [cameraId]: 'error'}}));
          useUIStore.getState().pushToast('error', msg.error ?? '알 수 없는 오류');
          break;
        }
        case 'cameras_changed': {
          if (msg.reason === 'added' || msg.reason === 'deleted') {
            scheduleCameraRefetch(); // 즉시 반영
          } else {
            notePendingUpdate(msg.reason, msg.cameraId); // updated/reordered/restored → 툴바 배지
          }
          break;
        }
        case 'config_changed':
          window.dispatchEvent(new CustomEvent('webnvr-config-changed'));
          break;
        case 'pong':
          break;
      }
    });

    // 연결 상태 추적 + 재연결 시 세션 리셋
    const offStatus = wsService.onStatus((connected, reconnectAt) => {
      console.log('🔌 WebSocket 상태 변경:', connected ? '✅ 연결됨' : '❌ 끊김');
      set(s => ({
        connected,
        ...(connected ? {rejected: false, retryAt: null, reconnectAt: null} : {reconnectAt: reconnectAt ?? s.reconnectAt}),
      }));
      if (!connected) {
        // 연결이 끊기면 모든 스트림이 유실된 것으로 간주한다.
        // desired는 유지 — WS 재연결 후 리컨실리어가 자동으로 다시 시작한다.
        console.log('🧹 모든 스트림 정리, 자동 재연결 대기');
        const h = getHub();
        Object.keys(get().states).forEach(cameraId => h.detach(cameraId));
        set({states: {}, stats: {}, history: {}});
      } else {
        // 재연결 직후 리컨실리어가 즉시 desired 스트림을 복구하도록 백오프 해제
        console.log('🔄 WebSocket 재연결 성공, 스트림 복구 시작');
        nextRetryAt = {};
        lastAttemptAt = {};
      }
    });

    // 동시 접속 제한 거부 상태 추적
    const offRejected = wsService.onRejected(retryAt => {
      console.log('⚠️ 동시 접속 제한 중 — 재시도 예정:', new Date(retryAt).toLocaleTimeString());
      set({rejected: true, retryAt});
    });

    wsService.connect();
    ensureReconciler(); // desired 스트림 자동 재연결 감시 시작

    return () => {
      offMsg();
      offStatus();
      offRejected();
      if (recTimer) clearInterval(recTimer);
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

  // 툴바 "설정 변경 적용" 버튼 — 목록을 새로고침하고, 변경된 카메라의 스트림을 재연결한다.
  // 카메라별로 시차를 두고 teardown → 카메라가 옛 세션을 놓을 시간을 준 뒤 재구독한다
  // (동시 재다이얼 stampede로 "스트림 오류"가 쏟아지던 문제 방지).
  applyCameraUpdate: async () => {
    const {reloadAll, ids} = get().pendingCameraUpdate;
    set({pendingCameraUpdate: {count: 0, ids: [], reloadAll: false}});
    await useCameraStore.getState().fetchCameras().catch(() => {});
    const stateNow = get().states;
    const targets = (reloadAll ? Object.keys(stateNow) : ids).filter(id => stateNow[id]);
    targets.forEach((id, i) => {
      reloadingIds.add(id);
      // 타일은 '재연결 시도 중'으로, 리컨실리어는 20초 starting 가드로 억제한다.
      set(s => ({
        states: {...s.states, [id]: 'starting'},
        retries: {...s.retries, [id]: Math.max(1, s.retries[id] ?? 0)},
      }));
      window.setTimeout(() => {
        lastAttemptAt[id] = Date.now();
        wsService.send({type: 'reload_stream', cameraId: id});
        // 카메라가 옛 RTSP 세션을 놓은 뒤 재구독
        window.setTimeout(() => {
          reloadingIds.delete(id);
          if (!get().desired[id]) return; // 그 사이 사용자가 정지했으면 중단
          lastAttemptAt[id] = Date.now();
          getHub().reset(id);
          wsService.send({type: 'start_stream', cameraId: id});
        }, RELOAD_REDIAL_DELAY_MS);
      }, i * RELOAD_STAGGER_MS);
    });
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
