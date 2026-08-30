// 스트림 상태(스트리밍 중 카메라, 통계)와 WS↔디코더 워커 연결을 관리하는 스토어
import {create} from 'zustand';
import {wsService} from '../services/ws';
import {StreamState, StreamStats} from '../types';
import {getWorkerInstance} from '../workers/workerInstance';

interface StreamStoreState {
  // cameraId → 상태
  states: Record<string, StreamState>;
  stats: Record<string, StreamStats>;
  connected: boolean;
  lastError: string | null;

  startStream: (cameraId: string) => void;
  stopStream: (cameraId: string) => void;
  startAllStreams: (cameraIds: string[]) => void;
  stopAllStreams: () => void;
  ptzControl: (cameraId: string, command: {action: 'move' | 'stop' | 'preset'; pan?: number; tilt?: number; zoom?: number; presetToken?: string}) => void;
  init: () => () => void;
}

export const useStreamStore = create<StreamStoreState>((set, get) => ({
  states: {},
  stats: {},
  connected: false,
  lastError: null,

  init: () => {
    // WS 서버 메시지 → 워커/상태 라우팅
    const offMsg = wsService.on(msg => {
      const cameraId = msg.cameraId ?? '';
      switch (msg.type) {
        case 'stream_started': {
          getWorkerInstance().postMessage({
            type: 'config',
            cameraId,
            codec: msg.codec ?? 'h264',
            sps: msg.sps ?? '',
            pps: msg.pps ?? '',
            vps: msg.vps ?? '',
            clockRate: msg.clockRate ?? 90000,
          });
          set(s => ({states: {...s.states, [cameraId]: 'streaming'}}));
          break;
        }
        case 'rtp_packet': {
          getWorkerInstance().postMessage({
            type: 'packet',
            cameraId,
            payload: msg.payload ?? '',
            timestamp: msg.timestamp ?? 0,
            marker: msg.marker ?? false,
            sequence: msg.sequence ?? 0,
          });
          break;
        }
        case 'stream_stopped': {
          getWorkerInstance().postMessage({type: 'detach', cameraId});
          set(s => {
            const states = {...s.states};
            const stats = {...s.stats};
            delete states[cameraId];
            delete stats[cameraId];
            return {states, stats};
          });
          break;
        }
        case 'stream_error': {
          getWorkerInstance().postMessage({type: 'detach', cameraId});
          set(s => ({states: {...s.states, [cameraId]: 'error'}, lastError: msg.error ?? '알 수 없는 오류'}));
          break;
        }
        case 'pong':
          break;
      }
    });

    // 워커 출력 → 상태
    const onWorkerMsg = (ev: MessageEvent) => {
      const msg = ev.data;
      switch (msg.type) {
        case 'stats':
          set(s => ({stats: {...s.stats, [msg.cameraId]: msg.stats}}));
          break;
        case 'error':
          set(s => ({
            states: {...s.states, [msg.cameraId]: 'error'},
            lastError: msg.message as string,
          }));
          break;
      }
    };
    getWorkerInstance().addEventListener('message', onWorkerMsg);

    // 연결 상태 추적 + 재연결 시 세션 리셋
    const offStatus = wsService.onStatus(connected => {
      set({connected});
      if (!connected) {
        // 연결이 끊기면 모든 스트림이 유실된 것으로 간주한다
        const w = getWorkerInstance();
        Object.keys(get().states).forEach(cameraId => w.postMessage({type: 'detach', cameraId}));
        set({states: {}, stats: {}});
      }
    });

    wsService.connect();

    return () => {
      offMsg();
      offStatus();
      getWorkerInstance().removeEventListener('message', onWorkerMsg);
    };
  },

  startStream: (cameraId) => {
    getWorkerInstance().postMessage({type: 'attach', cameraId});
    set(s => ({states: {...s.states, [cameraId]: 'starting'}}));
    wsService.send({type: 'start_stream', cameraId});
  },

  stopStream: (cameraId) => {
    wsService.send({type: 'stop_stream', cameraId});
    getWorkerInstance().postMessage({type: 'detach', cameraId});
    set(s => {
      const states = {...s.states};
      const stats = {...s.stats};
      delete states[cameraId];
      delete stats[cameraId];
      return {states, stats};
    });
  },

  startAllStreams: (cameraIds) => {
    cameraIds.forEach(id => get().startStream(id));
  },

  stopAllStreams: () => {
    Object.keys(get().states).forEach(id => get().stopStream(id));
    wsService.send({type: 'stop_all_streams'});
  },

  ptzControl: (cameraId, command) => {
    wsService.send({type: 'ptz', cameraId, command});
  },
}));
