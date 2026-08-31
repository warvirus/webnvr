// 스트림 파이프라인 관련 공용 타입 (디코더/스토어/타일 공유)

// 디코더에 공급되는 RTP 패킷
export interface PacketIn {
  seq: number;
  ts: number;
  marker: boolean;
  payload: Uint8Array;
}

// 타일에 표시할 스트림 통계 (디코더에서 1초 주기 산출)
export interface StreamStats {
  fps: number;
  kbps: number;
  packets: number;
  drops: number;
}

// 통계 히스토리 샘플 (스파크라인용)
export interface StatSample {
  fps: number;
  kbps: number;
}

// 카메라별 스트림 상태
export type StreamState = 'idle' | 'starting' | 'streaming' | 'error';
