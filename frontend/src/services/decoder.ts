// H.264/H.265 RTP 스트림 디코딩 파이프라인 (컨텍스트 독립 — Worker와 메인 스레드 공용)
//
// Safari(WebKit)는 Worker 내 VideoDecoder 인스턴스가 출력을 생성하지 않는 사례가 있어
// v1.1에서는 메인 스레드 실행을 기본으로 한다. VideoDecoder는 비동기 HW 가속이므로
// 메인 스레드 실행 시에도 블로킹이 없다.
//
// 포맷 후보: AVCC(avcC description + 길이 접두어) 우선 → 무출력 시 Annex B로 자동 전환.
// Safari는 description 없는 H.264 입력을 받지 않는다(F7 참조).

export interface PacketIn {
  seq: number;
  ts: number;
  marker: boolean;
  payload: Uint8Array;
}

export interface CodecConfigIn {
  codec: 'h264' | 'h265';
  sps: string;
  pps: string;
  vps: string;
  clockRate: number;
}

export interface SessionEvents {
  onFrame: (cameraId: string, frame: VideoFrame) => void;
  onDecoded: (cameraId: string) => void; // 첫 프레임 디코딩 성공
  onNotice: (cameraId: string, message: string) => void; // 자가 치성 진행 알림 (오류 아님)
  onError: (cameraId: string, message: string) => void;
  onStats: (cameraId: string, stats: SessionStats) => void;
}

export interface SessionStats {
  fps: number;
  kbps: number;
  packets: number;
  drops: number;
}

// 디코더 청크 포맷 후보 (실측 기준 — F8/F12):
// ANNEXB : description 없음 + 시작코드 청크 — 사용자 WebKit(메인 스레드)에서 동작 확인
// AVCC   : description=avcC + 길이 접두어 청크 — Annex B 실패 환경용 폴백
// 성공한 포맷은 모듈 레벨에서 기억해 재접속 시 처음부터 사용한다.
const FORMAT_ANNEXB = 0;
const FORMAT_AVCC = 1;
const FORMAT_COUNT = 2;
let lastGoodFormat: number | null = null;

const TICK_MS = 30;          // 큐 처리 주기 (지터 스무딩)
const MAX_QUEUE = 1200;      // 큐 상한 (버스트 흡수)
const STATS_MS = 1000;
const STALL_MS = 4_000;      // 키프레임 후 무출력 시 포맷 전환 대기
const STALL_GIVEUP_MS = 12_000; // 모든 포맷 시도 후 포기

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

// buildAvcC는 SPS/PPS로 AVCDecoderConfigurationRecord를 만든다 (Safari 필수).
function buildAvcC(sps: Uint8Array, pps: Uint8Array): Uint8Array {
  const out: number[] = [];
  out.push(1);                       // configurationVersion
  out.push(sps[1]);                  // AVCProfileIndication
  out.push(sps[2]);                  // profile_compatibility
  out.push(sps[3]);                  // AVCLevelIndication
  out.push(0xff);                    // lengthSizeMinusOne = 3 (4바이트 길이)
  out.push(0xe1);                    // numOfSPS = 1
  out.push((sps.length >> 8) & 0xff, sps.length & 0xff);
  for (const b of sps) out.push(b);
  out.push(1);                       // numOfPPS = 1
  out.push((pps.length >> 8) & 0xff, pps.length & 0xff);
  for (const b of pps) out.push(b);
  return new Uint8Array(out);
}

// toAvcc는 NALU 목록을 4바이트 길이 접두어 청크로 변환한다.
function toAvcc(nalus: Uint8Array[]): Uint8Array {
  let total = 0;
  for (const n of nalus) total += 4 + n.length;
  const out = new Uint8Array(total);
  let off = 0;
  for (const n of nalus) {
    const len = n.length;
    out[off] = (len >>> 24) & 0xff;
    out[off + 1] = (len >>> 16) & 0xff;
    out[off + 2] = (len >>> 8) & 0xff;
    out[off + 3] = len & 0xff;
    out.set(n, off + 4);
    off += 4 + len;
  }
  return out;
}

// ───────────────────────────────────────────────────────────

// Session은 카메라 하나의 디코딩 파이프라인이다.
export class Session {
  cameraId: string;
  config: CodecConfigIn | null = null;
  decoder: VideoDecoder | null = null;
  formatIdx = lastGoodFormat ?? FORMAT_ANNEXB; // 마지막 성공 포맷 우선 (기본: Annex B)
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

  // 디코더가 준비되기 전 도착한 키프레임 보관 (1개면 충분)
  heldKey: {nalus: Uint8Array[]; ts: number} | null = null;

  // 진단
  packets = 0;
  frames = 0;
  drops = 0;
  diagSent = false;
  firstPacketMs = 0;
  firstKeyMs = 0;
  lastError = '';

  // 통계
  bytes = 0;
  lastStats = 0;
  lastFrames = 0;
  lastBytes = 0;

  private ev: SessionEvents;

  constructor(cameraId: string, ev: SessionEvents) {
    this.cameraId = cameraId;
    this.ev = ev;
  }

  // paramSets는 유효한 파라미터 셋을 반환한다.
  private paramSets(): {sps: Uint8Array | null; pps: Uint8Array | null; vps: Uint8Array | null} {
    const codec = this.config?.codec ?? 'h264';
    return {
      sps: this.config && this.config.sps.length ? b64ToBytes(this.config.sps) : this.streamSps,
      pps: this.config && this.config.pps.length ? b64ToBytes(this.config.pps) : this.streamPps,
      vps: codec === 'h265' ? (this.config && this.config.vps.length ? b64ToBytes(this.config.vps) : this.streamVps) : null,
    };
  }

  // prepareDecoder는 외부(config 수신 시점)에서 디코더 준비를 트리거한다.
  prepareDecoder(): boolean {
    return this.ensureDecoder();
  }

  // ensureDecoder는 현재 포맷 후보로 디코더를 동기적으로 준비한다.
  private ensureDecoder(): boolean {
    if (this.decoder && this.decoder.state === 'configured') return true;
    if (this.decoder) {
      try {
        this.decoder.close();
      } catch { /* 무시 */ }
      this.decoder = null;
    }
    if (!this.config) return false;

    const {sps, pps} = this.paramSets();
    if (!sps || !pps || sps.length === 0 || pps.length === 0) return false;
    if (this.config.codec === 'h264' && sps.length < 4) return false;

    const codecStr = this.config.codec === 'h264'
      ? h264CodecString(sps)
      : 'hev1.1.6.L93.B0';

    // 포맷 후보별 설정: AVCC(description 포함) → AnnexB(description 없음)
    const buildConfig = (format: number): VideoDecoderConfig => {
      const cfg: VideoDecoderConfig = {
        codec: codecStr,
        optimizeForLatency: true,
      };
      if (format === FORMAT_AVCC && this.config!.codec === 'h264') {
        cfg.description = buildAvcC(sps!, pps!);
      }
      return cfg;
    };

    const attempt = (format: number, hw: boolean): VideoDecoder | null => {
      try {
        const dec = new VideoDecoder({
          output: (frame: VideoFrame) => {
            this.ev.onFrame(this.cameraId, frame);
          },
          error: (e: DOMException) => {
            this.lastError = `디코더 오류: ${e.message}`;
            // 비동기 오류는 포맷 후보 전환 트리거
            this.advanceFormat();
          },
        });
        const cfg = buildConfig(format);
        if (hw) cfg.hardwareAcceleration = 'prefer-hardware';
        dec.configure(cfg);
        return dec;
      } catch {
        return null; // 동기 설정 실패
      }
    };

    let dec = attempt(this.formatIdx, true);
    if (dec === null) dec = attempt(this.formatIdx, false); // 하드웨어 선호 실패 시 소프트웨어
    if (dec !== null) {
      this.decoder = dec;
      return true;
    }
    return false;
  }

  // advanceFormat은 다음 청크 포맷 후보로 전환한다. (Safari/Chromium 호환성 자동 대응)
  private advanceFormat(): boolean {
    if (this.formatIdx + 1 >= FORMAT_COUNT) return false;
    this.formatIdx++;
    try {
      this.decoder?.close();
    } catch { /* 무시 */ }
    this.decoder = null;
    this.sawKeyframe = false; // 새 포맷에서 키프레임부터 다시 시작
    this.firstKeyMs = 0;
    this.heldKey = null;
    return true;
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
    this.watchdog();
  }

  // watchdog는 정체 상태를 감시하고 포맷 전환/오류 보고를 수행한다.
  private watchdog() {
    if (this.frames > 0) return; // 정상 출력 중
    if (this.firstPacketMs === 0) this.firstPacketMs = performance.now();
    if (this.sawKeyframe && this.firstKeyMs === 0) this.firstKeyMs = performance.now();
    if (this.packets < 10) return;
    const now = performance.now();

    // 키프레임 이후 무출력 → 다른 청크 포맷으로 전환 (Safari avcC 필수 이슈 대응)
    if (this.sawKeyframe && now - this.firstKeyMs > STALL_MS) {
      if (this.advanceFormat()) {
        this.ev.onNotice(this.cameraId, `디코딩 출력이 없어 청크 포맷을 전환했습니다 (${this.formatIdx === FORMAT_ANNEXB ? 'Annex B' : 'AVCC'})`);
        return;
      }
      if (!this.diagSent && now - this.firstKeyMs > STALL_GIVEUP_MS) {
        this.diagSent = true;
        this.ev.onError(this.cameraId, `키프레임 이후 ${STALL_GIVEUP_MS / 1000}초간 프레임이 출력되지 않았습니다 (이 환경의 WebCodecs가 ${this.config?.codec} 디코딩을 지원하지 않는 것으로 보임)`);
      }
      return;
    }

    // 키프레임 자체가 안 오는 경우 (모든 포맷과 무관 — 카메라 GOP 확인 필요)
    if (!this.sawKeyframe && now - this.firstPacketMs > 20_000) {
      if (!this.diagSent) {
        this.diagSent = true;
        this.ev.onError(this.cameraId, '20초간 키프레임을 수신하지 못했습니다 — 카메라의 GOP(키 프레임 간격) 설정을 확인하세요');
      }
    }
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

  // completeFrame은 누적 NALU를 현재 포맷 청크로 조립해 디코더에 넣는다 (동기, 레이스 없음)
  private completeFrame() {
    if (this.frameNalus.length === 0) return;
    const isKey = this.frameIsKey;
    this.frameIsKey = false;
    const nalus = this.frameNalus;
    const ts = this.frameTs;
    this.frameNalus = [];

    // 키프레임 이전의 델타 프레임은 폐기한다 (중간 참여 대응)
    if (!this.sawKeyframe) {
      if (!isKey) {
        this.drops++;
        return;
      }
      this.sawKeyframe = true;
    }

    // 디코더가 아직 준비되지 않았으면 키프레임을 보관하고 대기한다
    if (!this.ensureDecoder()) {
      if (isKey) {
        this.heldKey = {nalus, ts};
      } else {
        this.drops++;
      }
      return;
    }

    // 보관했던 키프레임을 먼저 디코딩한다
    if (this.heldKey) {
      const held = this.heldKey;
      this.heldKey = null;
      this.decodeFrame(held.nalus, held.ts, true);
    }
    this.decodeFrame(nalus, ts, isKey);
  }

  // decodeFrame은 NALU 목록을 현재 포맷의 청크로 만들어 디코더에 넣는다.
  private decodeFrame(nalus: Uint8Array[], ts: number, isKey: boolean) {
    if (!this.decoder || this.decoder.state !== 'configured') {
      this.drops++;
      return;
    }
    let data: Uint8Array;
    if (this.formatIdx === FORMAT_AVCC) {
      // AVCC: 길이 접두어 (파라미터 셋은 description에 포함)
      data = toAvcc(nalus);
    } else {
      // Annex B: 키프레임 앞에 파라미터 셋 삽입
      const parts: Uint8Array[] = [];
      if (isKey) {
        const {sps, pps, vps} = this.paramSets();
        if (vps) parts.push(START_CODE, vps);
        if (sps) parts.push(START_CODE, sps);
        if (pps) parts.push(START_CODE, pps);
      }
      for (const n of nalus) parts.push(START_CODE, n);
      data = concatBytes(parts);
    }

    const clockRate = this.config?.clockRate ?? 90000;
    const chunk = new EncodedVideoChunk({
      type: isKey ? 'key' : 'delta',
      timestamp: Math.round((ts * 1_000_000) / clockRate),
      data,
    });
    this.frames++;
    if (this.frames === 1) {
      // 첫 프레임 디코딩 성공 → 성공 포맷을 기억해(재접속 시 우선 사용)
      // UI의 오류/대기 상태를 해제한다
      lastGoodFormat = this.formatIdx;
      this.ev.onDecoded(this.cameraId);
    }
    try {
      this.decoder.decode(chunk);
    } catch (e) {
      // 디코더 상태 이상 → 포맷 전환 후 다음 키프레임에서 재시작
      this.lastError = `디코딩 실패: ${String(e)}`;
      this.advanceFormat();
      this.drops++;
    }
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
        // 주의: new Uint8Array(숫자)는 "길이 n의 0 배열"을 만든다 — [값] 배열이 필요
        const naluHeader = new Uint8Array([(payload[0] & 0xe0) | fuType]);
        if (s) {
          this.fuBuf = concatBytes([naluHeader, payload.subarray(2)]);
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
  stats(): SessionStats | null {
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
    this.sawKeyframe = false;
    this.queue = [];
    this.frameNalus = [];
    this.fuBuf = null;
    this.heldKey = null;
  }
}

// DecoderHub는 세션 집합과 주기 처리를 관리한다. (Worker/메인 스레드 공용)
export class DecoderHub {
  private sessions = new Map<string, Session>();
  private ev: SessionEvents;
  private ticker: ReturnType<typeof setInterval> | null = null;
  private statsTimer: ReturnType<typeof setInterval> | null = null;

  constructor(ev: SessionEvents) {
    this.ev = ev;
    this.ticker = setInterval(() => {
      for (const s of this.sessions.values()) s.flush();
    }, TICK_MS);
    this.statsTimer = setInterval(() => {
      for (const [cameraId, s] of this.sessions) {
        const st = s.stats();
        if (st) this.ev.onStats(cameraId, st);
      }
    }, STATS_MS);
  }

  attach(cameraId: string) {
    if (!this.sessions.has(cameraId)) {
      this.sessions.set(cameraId, new Session(cameraId, this.ev));
    }
  }

  config(cameraId: string, cfg: CodecConfigIn) {
    const s = this.sessions.get(cameraId);
    if (!s) return;
    s.config = cfg;
    s.prepareDecoder();
  }

  packet(cameraId: string, p: PacketIn) {
    this.sessions.get(cameraId)?.push(p);
  }

  detach(cameraId: string) {
    const s = this.sessions.get(cameraId);
    if (s) {
      s.dispose();
      this.sessions.delete(cameraId);
    }
  }

  reset(cameraId: string) {
    this.detach(cameraId);
    this.attach(cameraId);
  }

  dispose() {
    if (this.ticker) clearInterval(this.ticker);
    if (this.statsTimer) clearInterval(this.statsTimer);
    for (const s of this.sessions.values()) s.dispose();
    this.sessions.clear();
  }
}
