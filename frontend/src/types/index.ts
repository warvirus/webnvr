// WS JSON 프로토콜 및 스트림 관련 프론트엔드 타입 (doc §5.3 대응)

// 클라이언트 → 서버 메시지
export interface ClientMsg {
  type:
    | 'start_stream'
    | 'stop_stream'
    | 'start_all_streams'
    | 'stop_all_streams'
    | 'ptz'
    | 'request_keyframe'
    | 'subscribe'
    | 'unsubscribe'
    | 'reload_stream'
    | 'ping';
  cameraId?: string;
  command?: PTZCommand;
}

// PTZ 명령 (action: move|stop|preset)
export interface PTZCommand {
  action: 'move' | 'stop' | 'preset';
  pan?: number;  // -1.0~1.0
  tilt?: number; // -1.0~1.0
  zoom?: number; // 0~1.0
  presetToken?: string;
}

// 서버 → 클라이언트 메시지
export interface ServerMsg {
  type:
    | 'stream_started'
    | 'rtp_packet'
    | 'rtp_batch'
    | 'stream_stopped'
    | 'stream_error'
    | 'cameras_changed'
    | 'config_changed'
    | 'stats'
    | 'pong'
    | 'client_limit_exceeded';
  cameraId?: string;
  reason?: string;
  error?: string;
  // stream_started
  codec?: string; // 'h264' | 'h265'
  ssrc?: number;
  clockRate?: number;
  payloadType?: number;
  sps?: string;  // base64
  pps?: string;  // base64
  vps?: string;  // base64 (H.265)
  width?: number;
  height?: number;
  // rtp_packet
  payload?: string; // base64
  timestamp?: number;
  marker?: boolean;
  sequence?: number;
  // rtp_batch (고비트레이트 스트림 배치 전송 — 백엔드 확장)
  packets?: RTPPacketItem[];
}

// rtp_batch의 개별 패킷 (축약 키)
export interface RTPPacketItem {
  p: string;      // base64 payload
  ts: number;     // timestamp
  m?: boolean;    // marker
  sq: number;     // sequence
}

// 스트림 통계/상태는 types/stream.ts가 단일 출처다 (재노출)
export type {StreamStats, StreamState, StatSample, PacketIn} from './stream';

// 모니터링 그리드 모드 (타일 수 기준)
export type GridMode = 'auto' | 1 | 4 | 9 | 16 | 25;


