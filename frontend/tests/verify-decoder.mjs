// decoder.ts 포맷 전환 로직 검증 하네스 — WebCodecs/성능 API 모의
// 시나리오: 정상 출력 / SPS 지연 hold / 포맷 불일치 → 전환 1회 / 세션 재시작 스팸 방지
import {Session} from './decoder.mjs';

// ── WebCodecs 모의 ──
let decoderBehavior = 'ok'; // ok | no-output | fail-sync

globalThis.VideoFrame = class {};

class FakeDecoder {
  constructor({output, error}) {
    this.output = output;
    this.error = error;
    this.state = '';
  }
  configure(cfg) {
    if (decoderBehavior === 'fail-sync') throw new Error('unsupported');
    this.state = 'configured';
    this.cfg = cfg;
    // no-output 시뮬레이션: 이 포맷으로는 절대 출력이 안 나옴
    if (decoderBehavior === 'no-output' && !cfg.description) return; // Annex B(=desc 없음) → 침묵
    // 정상: 출력은 비동기로 소량 지연 후 발생
    setTimeout(() => {}, 0);
  }
  decode(chunk) {
    if (decoderBehavior === 'no-output' && !this.cfg?.description) return; // 침묵
    // 2프레임 지연 뒤 출력 (WebCodecs 리오더링 모의)
    setTimeout(() => this.output({close() {}, displayWidth: 640, displayHeight: 360}), 5);
  }
  close() { this.state = 'closed'; }
}
globalThis.VideoDecoder = FakeDecoder;
// 제출된 청크 타임스탬프 기록 — 랩 시나리오의 단조 증가 검증용
const chunkLog = [];
globalThis.EncodedVideoChunk = class { constructor(o) { chunkLog.push(o.timestamp); Object.assign(this, o); } };

let nowMs = 0;
globalThis.performance = {now: () => nowMs};

function makeEvents() {
  const log = {notice: [], error: [], decoded: 0, frames: 0};
  const ev = {
    onFrame: () => log.frames++,
    onDecoded: () => log.decoded++,
    onNotice: (_id, m) => log.notice.push(m),
    onError: (_id, m) => log.error.push(m),
    onStats: () => {},
  };
  return {log, ev};
}

// SPS(0x67)+PPS(0x68)+IDR(0x65) / 슬라이스(0x61) 패킷 생성
const STAP_A_KEY = new Uint8Array([0x78, 0x00, 0x04, 0x67, 0x64, 0x00, 0x1f, 0x00, 0x02, 0x68, 0x42, 0x00]);
const BARE_IDR = new Uint8Array([0x65, 0xaa, 0xbb, 0xcc]); // SPS/PPS 없는 단독 IDR
function fuA(idrPart, start, end) {
  return new Uint8Array([0x7c, (start ? 0x80 : 0) | (end ? 0x40 : 0) | 5, ...idrPart]);
}
const IDR_P1 = new Uint8Array(40).fill(0xaa);
const IDR_P2 = new Uint8Array(40).fill(0xbb);
const DELTA = new Uint8Array([0x61, 0x05]);

function feedSession(s, nFrames, startMs) {
  // 패킷 주입: 30fps, 3패킷/프레임
  for (let i = 0; i < nFrames; i++) {
    const base = startMs + i * 33;
    s.push({seq: s.lastSeq + 1, ts: base * 90, marker: false, payload: STAP_A_KEY});
    s.push({seq: s.lastSeq + 1, ts: base * 90, marker: false, payload: fuA(IDR_P1, true, false)});
    s.push({seq: s.lastSeq + 1, ts: base * 90, marker: false, payload: fuA(IDR_P2, false, true)});
    s.push({seq: s.lastSeq + 1, ts: base * 90, marker: true, payload: DELTA});
  }
}

let pass = 0, fail = 0;
function check(name, cond, extra = '') {
  if (cond) { pass++; console.log(`  ✅ ${name}`); }
  else { fail++; console.log(`  ❌ ${name} ${extra}`); }
}

// ── 시나리오 1: 정상 — SPS 즉시, 출력 출현 → 알림/오류 0건 ──
{
  console.log('시나리오 1: 정상 스트림');
  decoderBehavior = 'ok';
  const {log, ev} = makeEvents();
  const s = new Session('cam-a', ev);
  s.config = {codec: 'h264', sps: '', pps: '', vps: '', clockRate: 90000}; // 빈 config — 스트림 SPS 사용
  feedSession(s, 10, 0);
  nowMs += 200; s.flush();
  nowMs += 200; s.flush();
  check('프레임 제출됨', s.frames > 0);
  check('알림 0건', log.notice.length === 0, JSON.stringify(log.notice));
  check('오류 0건', log.error.length === 0, JSON.stringify(log.error));
}

// ── 시나리오 2: SPS 지연 — 키프레임 hold 상태에서는 포맷 전환 금지 ──
{
  console.log('시나리오 2: SPS 지연 (hold 상태)');
  decoderBehavior = 'ok';
  const {log, ev} = makeEvents();
  const s = new Session('cam-b', ev);
  s.config = {codec: 'h264', sps: '', pps: '', vps: '', clockRate: 90000};
  // SPS/PPS 없이 델타 + 단독 IDR — IDR은 hold됨(디코더 준비 불가)
  let seq = 0;
  for (let i = 0; i < 60; i++) s.push({seq: seq++, ts: i * 3000, marker: true, payload: DELTA});
  s.push({seq: seq++, ts: 60 * 3000, marker: true, payload: BARE_IDR});
  nowMs += 10_000; s.flush();
  check('hold 상태에서 알림 0건', log.notice.length === 0, JSON.stringify(log.notice));
  check('hold 상태에서 포맷 전환 없음', s.formatIdx === 0, `formatIdx=${s.formatIdx}`);
  // 이후 SPS/PPS+IDR 도착 → 즉시 제출 → 출력 (알림 없이 회복)
  s.push({seq: seq++, ts: 70 * 3000, marker: false, payload: STAP_A_KEY});
  s.push({seq: seq++, ts: 70 * 3000, marker: false, payload: fuA(IDR_P1, true, false)});
  s.push({seq: seq++, ts: 70 * 3000, marker: true, payload: fuA(IDR_P2, false, true)});
  nowMs += 200; s.flush();
  check('SPS 도착 후 알림 여전히 0건', log.notice.length === 0, JSON.stringify(log.notice));
}

// ── 시나리오 3: 포맷 불일치 (Annex B 침묵) — 전환 1회 + 알림 1회 + heldKey 재사용 ──
{
  console.log('시나리오 3: 포맷 불일치 → 전환');
  decoderBehavior = 'no-output';
  const {log, ev} = makeEvents();
  const s = new Session('cam-c', ev);
  s.config = {codec: 'h264', sps: '', pps: '', vps: '', clockRate: 90000};
  feedSession(s, 5, 0);
  nowMs += 200; s.flush();          // 티커 초기화 (firstPacketMs/firstKeyMs 설정)
  nowMs += 4_500; s.flush();        // STALL_MS(4s) 경과 → 전환
  check('전환 발생 (AVCC)', s.formatIdx === 1, `formatIdx=${s.formatIdx}`);
  check('알림 1회', log.notice.length === 1, `count=${log.notice.length}`);
  nowMs += 5_000; s.flush();
  check('알림 여전히 1회 (스팸 없음)', log.notice.length === 1, `count=${log.notice.length}`);
  // 전환 후 패킷은 계속 흐른다 — 새 키프레임이 AVCC로 제출되어 출력 복구
  feedSession(s, 5, 20);
  nowMs += 200; s.flush();
  setTimeout(() => {
    check('전환 후 출력 복구', s.outputs > 0, `outputs=${s.outputs}`);
    check('오류 0건 (복구)', log.error.length === 0, JSON.stringify(log.error));
    runScenario4();
  }, 50);
}

// ── 시나리오 5: RTP 32비트 타임스탬프 랩 — 청크 ts가 역행하지 않는다 ──
{
  console.log('시나리오 5: RTP 타임스탬프 랩');
  decoderBehavior = 'ok';
  const {log, ev} = makeEvents();
  const s = new Session('cam-f', ev);
  s.config = {codec: 'h264', sps: '', pps: '', vps: '', clockRate: 90000};
  const W = 0x100000000;
  chunkLog.length = 0;
  let seq = 0;
  const frame = (ts) => {
    s.push({seq: seq++, ts, marker: false, payload: STAP_A_KEY});
    s.push({seq: seq++, ts, marker: false, payload: fuA(IDR_P1, true, false)});
    s.push({seq: seq++, ts, marker: true, payload: fuA(IDR_P2, false, true)});
  };
  frame(W - 3000); // 랩 직전 키프레임
  frame(0);        // 랩 발생 — raw 0 (실제 +3000)
  frame(3000);
  frame(6000);
  nowMs += 200; s.flush();
  check('랩 후 프레임 제출됨', s.frames >= 4, `frames=${s.frames}`);
  check('랩 후 오류 0건', log.error.length === 0, JSON.stringify(log.error));
  const mine = chunkLog.slice(-4);
  const mono = mine.every((t, i) => i === 0 || t > mine[i - 1]);
  check('청크 타임스탬프 단조 증가', mono, JSON.stringify(mine));
}

// ── 시나리오 4: 세션 재시작 반복 — 알림 스팸 없음 + 카메라별 포맷 기억 ──
async function runScenario4() {
  console.log('시나리오 4: 세션 재시작 반복 (같은 카메라)');
  decoderBehavior = 'no-output';
  const {log, ev} = makeEvents();
  const sleep = (ms) => new Promise(r => setTimeout(r, ms));
  // 3회 재시작 — 매번 새 세션이지만 같은 카메라. 출력 콜백(실시간 타이머)이
  // 카메라별 포맷 기록을 마치도록 재시작 사이에 대기한다.
  for (let r = 0; r < 3; r++) {
    const s = new Session('cam-d', ev);
    s.config = {codec: 'h264', sps: '', pps: '', vps: '', clockRate: 90000};
    feedSession(s, 3, r * 10_000);
    nowMs += 200; s.flush();
    nowMs += 4_500; s.flush();
    // 전환 후에도 패킷은 계속 흐른다 — 새 포맷에서 출력 → 카메라별 포맷 기록
    feedSession(s, 3, r * 10_000 + 5_000);
    nowMs += 200; s.flush();
    await sleep(30);
  }
  check('3회 재시작 동안 알림 1회', log.notice.length === 1, `count=${log.notice.length}`);
  check('오류 0건', log.error.length === 0, JSON.stringify(log.error));

  // 카메라별 격리: 다른 카메라는 영향 받지 않음
  decoderBehavior = 'ok';
  const s2 = new Session('cam-e', ev);
  check('다른 카메라는 기본 포맷(Annex B) 시작', s2.formatIdx === 0, `formatIdx=${s2.formatIdx}`);

  console.log(`\n결과: ${pass} 통과 / ${fail} 실패`);
  process.exit(fail > 0 ? 1 : 0);
}
