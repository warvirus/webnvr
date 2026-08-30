// 카메라별 RTP 수신 → Jitter 버퍼 → RFC 6184/7798 디페이저 → Annex B 조립 → WebCodecs 디코딩 워커
export {};

// tsconfig가 DOM lib를 포함하므로 워커 컨텍스트를 로컬 타입으로 선언한다.
interface WorkerCtx {
  postMessage(msg: unknown, transfer?: Transferable[]): void;
  onmessage: ((ev: MessageEvent) => void) | null;
}
const ctx = self as unknown as WorkerCtx;

interface PacketIn {
  seq: number;
  ts: number;
  marker: boolean;
  payload: Uint8Array;
}

interface CodecConfig {
  codec: 'h264' | 'h265';
  sps: Uint8Array;
  pps: Uint8Array;
  vps: Uint8Array;
  clockRate: number;
}

const TICK_MS = 30;          // 큐 처리 주기 (지터 스무딩)
const MAX_QUEUE = 150;       // 큐 상한 초과 시 가장 오래된 패킷 드롭
const STATS_MS = 1000;

const START_CODE = new Uint8Array([0, 0, 0, 1]);

// base64 → Uint8Array
function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// 16비트 시퀀스 번호 비교 (랩어라웃 처리)
function seqNewer(a: number, b: number): boolean {
  return ((a - b) & 0xffff) > 0 && ((a - b) & 0xffff) < 0x8000;
}

function concatBytes(parts: Uint8Array[]): Uint8Array {
  let len = 0;
  for (const p of parts) len += p.length;
  const out = new Uint8Array(len);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}

// SPS에서 WebCodecs H.264 코덱 문자열을 만든다 (avc1.PPCCLL)
function h264CodecString(sps: Uint8Array): string {
  const hex = (n: number) => n.toString(16).padStart(2, '0');
  return `avc1.${hex(sps[1])}${hex(sps[2])}${hex(sps[3])}`;
}

// H.264 NAL 타입
function h264Type(n: Uint8Array): number {
  return n[0] & 0x1f;
}

// H.265 NAL 타입
function h265Type(n: Uint8Array): number {
  return (n[0] >> 1) & 0x3f;
}

function isKeyframeNalu(codec: 'h264' | 'h265', n: Uint8Array): boolean {
  if (n.length === 0) return false;
  if (codec === 'h264') return h264Type(n) === 5;
  const t = h265Type(n);
  return t >= 16 && t <= 23; // BLA/IDR/CRA
}

function isParamSet(codec: 'h264' | 'h265', n: Uint8Array): boolean {
  if (n.length === 0) return false;
  if (codec === 'h264') {
    const t = h264Type(n);
    return t === 7 || t === 8;
  }
  const t = h265Type(n);
  return t === 32 || t === 33 || t === 34; // VPS/SPS/PPS
}

// ───────────────────────────────────────────────────────────

// Session은 카메라 하나의 디코딩 파이프라인이다.
class Session {
  cameraId: string;
  config: CodecConfig | null = null;
  decoder: VideoDecoder | null = null;
  configured = false;
  sawKeyframe = false;

  queue: PacketIn[] = [];
  lastSeq = -1;

  // 현재 프레임 누적
  frameNalus: Uint8Array[] = [];
  frameTs = -1;
  frameIsKey = false;
  // 스트림에서 수집한 파라미터 셋 (config가 비어 있을 때 사용)
  streamSps: Uint8Array | null = null;
  streamPps: Uint8Array | null = null;
  streamVps: Uint8Array | null = null;

  // 통계
  bytes = 0;
  packets = 0;
  frames = 0;
  drops = 0;
  lastStats = 0;
  lastFrames = 0;
  lastBytes = 0;

  constructor(cameraId: string) {
    this.cameraId = cameraId;
  }

  hasDecoder(): boolean {
    return this.decoder !== null && this.decoder.state === 'configured';
  }

  // tryConfigure는 파라미터 셋이 준비되면 디코더를 만든다.
  async tryConfigure(): Promise<void> {
    if (this.configured || !this.config) return;
    const codec = this.config.codec;
    const sps = this.config.sps.length ? this.config.sps : this.streamSps;
    const pps = this.config.pps.length ? this.config.pps : this.streamPps;
    if (!sps || !pps || sps.length === 0 || pps.length === 0) return;

    let codecStr = '';
    if (codec === 'h264') {
      if (sps.length < 4) return;
      codecStr = h264CodecString(sps);
    } else {
      // H.265는 일반적인 Main 프로필 문자열을 시도한다
      codecStr = 'hev1.1.6.L93.B0';
    }

    try {
      const support = await VideoDecoder.isConfigSupported({
        codec: codecStr,
        optimizeForLatency: true,
        hardwareAcceleration: 'prefer-hardware',
      });
      if (!support.supported) {
        // 폴백 문자열 시도
        codecStr = codec === 'h264' ? 'avc1.42E01E' : 'hev1.1.6.L120.90';
      }
      const dec = new VideoDecoder({
        output: (frame: VideoFrame) => {
          const cameraId = this.cameraId;
          ctx.postMessage({type: 'frame', cameraId, frame}, [frame]);
        },
        error: (e: DOMException) => {
          this.configured = false;
          this.decoder = null;
          ctx.postMessage({type: 'error', cameraId: this.cameraId, message: `디코더 오류: ${e.message}`});
        },
      });
      dec.configure({
        codec: codecStr,
        optimizeForLatency: true,
        hardwareAcceleration: 'prefer-hardware',
      });
      this.decoder = dec;
      this.configured = true;
    } catch (e) {
      this.configured = false;
      ctx.postMessage({
        type: 'error',
        cameraId: this.cameraId,
        message: `디코더 설정 실패 (${codecStr}): ${String(e)}`,
      });
    }
  }

  // push는 패킷을 큐에 시퀀스 순으로 삽입한다.
  push(p: PacketIn) {
    this.packets++;
    this.bytes += p.payload.length;
    if (this.lastSeq >= 0) {
      if (p.seq === this.lastSeq) return; // 중복
      if (!seqNewer(p.seq, this.lastSeq)) {
        this.drops++; // 늦게 도착한 패킷
        return;
      }
    }
    this.queue.push(p);
    this.lastSeq = p.seq;
    if (this.queue.length > MAX_QUEUE) {
      const overflow = this.queue.length - MAX_QUEUE;
      this.queue.splice(0, overflow);
      this.drops += overflow;
    }
  }

  // flush는 큐의 패킷을 순서대로 처리한다. (주기 호출)
  flush() {
    const due = this.queue.splice(0);
    for (const p of due) this.processPacket(p);
  }

  // processPacket은 RTP 페이로드를 디페이즈해 NALU로 누적한다.
  private processPacket(p: PacketIn) {
    const codec = this.config?.codec ?? 'h264';
    const nalus = this.depacketize(codec, p.payload);
    if (nalus === null) return; // FU 조각 진행 중

    for (const n of nalus) {
      if (isParamSet(codec, n)) {
        this.collectParamSet(codec, n);
        continue; // 파라미터 셋은 프레임에 포함하지 않고 별도 저장
      }
      if (p.ts !== this.frameTs && this.frameNalus.length > 0) {
        this.completeFrame();
      }
      this.frameTs = p.ts;
      if (isKeyframeNalu(codec, n)) this.frameIsKey = true;
      this.frameNalus.push(n);
    }
    if (p.marker) this.completeFrame();
  }

  // completeFrame은 누적 NALU를 Annex B로 조립해 디코더에 넣는다.
  private completeFrame() {
    if (this.frameNalus.length === 0) return;
    const codec = this.config?.codec ?? 'h264';
    const isKey = this.frameIsKey;
    this.frameIsKey = false;

    // 키프레임 이전의 델타 프레임은 폐기한다 (중간 참여 대응)
    if (!this.sawKeyframe) {
      if (!isKey) {
        this.frameNalus = [];
        this.drops++;
        return;
      }
      this.sawKeyframe = true;
    }

    void this.tryConfigure().then(() => {
      if (!this.hasDecoder()) {
        this.frameNalus = [];
        return;
      }
      const parts: Uint8Array[] = [];
      if (isKey) {
        // 키프레임 앞에 파라미터 셋을 삽입한다
        const sps = this.config?.sps.length ? this.config.sps : this.streamSps;
        const pps = this.config?.pps.length ? this.config.pps : this.streamPps;
        const vps = this.config?.vps.length ? this.config.vps : this.streamVps;
        if (codec === 'h265' && vps) parts.push(START_CODE, vps);
        if (sps) parts.push(START_CODE, sps);
        if (pps) parts.push(START_CODE, pps);
      }
      for (const n of this.frameNalus) parts.push(START_CODE, n);
      this.frameNalus = [];

      const data = concatBytes(parts);
      const clockRate = this.config?.clockRate ?? 90000;
      const chunk = new EncodedVideoChunk({
        type: isKey ? 'key' : 'delta',
        timestamp: Math.round((this.frameTs * 1_000_000) / clockRate),
        data,
      });
      this.frames++;
      try {
        this.decoder!.decode(chunk);
      } catch {
        // 디코더 상태 이상 → 다음 키프레임에서 재설정
        this.configured = false;
        this.sawKeyframe = false;
        try {
          this.decoder?.close();
        } catch { /* 무시 */ }
        this.decoder = null;
        this.drops++;
      }
    });
  }

  // collectParamSet은 스트림에서 SPS/PPS/VPS를 수집한다.
  private collectParamSet(codec: 'h264' | 'h265', n: Uint8Array) {
    if (codec === 'h264') {
      if (h264Type(n) === 7) this.streamSps = n;
      if (h264Type(n) === 8) this.streamPps = n;
    } else {
      const t = h265Type(n);
      if (t === 32) this.streamVps = n;
      if (t === 33) this.streamSps = n;
      if (t === 34) this.streamPps = n;
    }
  }

  // depacketize는 RTP 페이로드를 NALU 배열로 변환한다.
  // FU 조각이 진행 중이면 null을 반환한다.
  private fuBuf: Uint8Array | null = null;
  private fuCodec: 'h264' | 'h265' = 'h264';

  private depacketize(codec: 'h264' | 'h265', payload: Uint8Array): Uint8Array[] | null {
    if (payload.length === 0) return [];

    if (codec === 'h264') {
      const t = payload[0] & 0x1f;
      if (t >= 1 && t <= 23) return [payload];
      if (t === 24) {
        // STAP-A: [size:2][nalu] 반복
        const out: Uint8Array[] = [];
        let off = 1;
        while (off + 2 <= payload.length) {
          const size = (payload[off] << 8) | payload[off + 1];
          off += 2;
          if (off + size > payload.length) break;
          out.push(payload.subarray(off, off + size));
          off += size;
        }
        return out;
      }
      if (t === 28) {
        // FU-A: indicator[0] + FU header[1] (S|E|R|type)
        if (payload.length < 2) return null;
        const s = (payload[1] & 0x80) !== 0;
        const e = (payload[1] & 0x40) !== 0;
        const fuType = payload[1] & 0x1f;
        const nalu = new Uint8Array((payload[0] & 0xe0) | fuType);
        if (s) {
          this.fuCodec = codec;
          this.fuBuf = concatBytes([nalu, payload.subarray(2)]);
          return null;
        }
        if (this.fuBuf === null) return null;
        this.fuBuf = concatBytes([this.fuBuf, payload.subarray(2)]);
        if (e) {
          const out = this.fuBuf;
          this.fuBuf = null;
          return [out];
        }
        return null;
      }
      return []; // MTAP 등 미지원
    }

    // H.265 (RFC 7798)
    const t = (payload[0] >> 1) & 0x3f;
    if (t <= 40 && t !== 48 && t !== 49 && t !== 50) return [payload];
    if (t === 48) {
      // AP (Aggregation Packet): [size:2][nalu] 반복
      const out: Uint8Array[] = [];
      let off = 2;
      while (off + 2 <= payload.length) {
        const size = (payload[off] << 8) | payload[off + 1];
        off += 2;
        if (off + size > payload.length) break;
        out.push(payload.subarray(off, off + size));
        off += size;
      }
      return out;
    }
    if (t === 49) {
      // FU: 1바이트 패킷 헤더 복사 + 1바이트 FU 헤더
      if (payload.length < 3) return null;
      const s = (payload[2] & 0x80) !== 0;
      const e = (payload[2] & 0x40) !== 0;
      const fuType = payload[2] & 0x3f;
      if (s) {
        // NALU 헤더 재구성: [첫 바이트의 상위 비트 유지, (fuType<<1)|layerId 하위]
        const hdr = new Uint8Array([(payload[0] & 0x81) | (fuType << 1), payload[1]]);
        this.fuCodec = codec;
        this.fuBuf = concatBytes([hdr, payload.subarray(3)]);
        return null;
      }
      if (this.fuBuf === null) return null;
      this.fuBuf = concatBytes([this.fuBuf, payload.subarray(3)]);
      if (e) {
        const out = this.fuBuf;
        this.fuBuf = null;
        return [out];
      }
      return null;
    }
    return [];
  }

  // stats는 1초 주기로 통계를 산출한다.
  stats(): {fps: number; kbps: number; packets: number; drops: number} | null {
    const now = performance.now();
    if (this.lastStats === 0) {
      this.lastStats = now;
      return null;
    }
    const dt = (now - this.lastStats) / 1000;
    if (dt < STATS_MS / 1000) return null;
    const fps = (this.frames - this.lastFrames) / dt;
    const kbps = ((this.bytes - this.lastBytes) * 8) / 1000 / dt;
    this.lastStats = now;
    this.lastFrames = this.frames;
    this.lastBytes = this.bytes;
    return {fps: Math.round(fps * 10) / 10, kbps: Math.round(kbps), packets: this.packets, drops: this.drops};
  }

  dispose() {
    try {
      this.decoder?.close();
    } catch { /* 무시 */ }
    this.decoder = null;
    this.configured = false;
    this.sawKeyframe = false;
    this.queue = [];
    this.frameNalus = [];
    this.fuBuf = null;
  }
}

// ───────────────────────────────────────────────────────────

const sessions = new Map<string, Session>();

const ticker = setInterval(() => {
  for (const s of sessions.values()) s.flush();
}, TICK_MS);

const statsTimer = setInterval(() => {
  for (const [cameraId, s] of sessions) {
    const st = s.stats();
    if (st) ctx.postMessage({type: 'stats', cameraId, stats: st});
  }
}, STATS_MS);

ctx.onmessage = (ev: MessageEvent) => {
  const msg = ev.data;
  switch (msg.type) {
    case 'attach': {
      if (!sessions.has(msg.cameraId)) {
        sessions.set(msg.cameraId, new Session(msg.cameraId));
      }
      break;
    }
    case 'config': {
      const s = sessions.get(msg.cameraId);
      if (!s) break;
      s.config = {
        codec: msg.codec,
        sps: b64ToBytes(msg.sps ?? ''),
        pps: b64ToBytes(msg.pps ?? ''),
        vps: b64ToBytes(msg.vps ?? ''),
        clockRate: msg.clockRate ?? 90000,
      };
      void s.tryConfigure();
      break;
    }
    case 'packet': {
      const s = sessions.get(msg.cameraId);
      if (!s) break;
      s.push({
        seq: msg.sequence & 0xffff,
        ts: msg.timestamp >>> 0,
        marker: !!msg.marker,
        payload: b64ToBytes(msg.payload),
      });
      break;
    }
    case 'detach': {
      const s = sessions.get(msg.cameraId);
      if (s) {
        s.dispose();
        sessions.delete(msg.cameraId);
      }
      break;
    }
    case 'reset': {
      const s = sessions.get(msg.cameraId);
      if (s) {
        const camId = s.cameraId;
        s.dispose();
        sessions.set(camId, new Session(camId));
      }
      break;
    }
  }
};
