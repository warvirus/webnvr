// 영상 검색 페이지 — 녹화 달력, 채널별 현황, 줌/팬 타임라인, 자동 표시, 실시간 이어보기 (Phase R.3/R.5)
// 사용자 확정 동작(1-1~1-4, 2-1~2-4): 카메라 클릭 시 자동 표시(첫 구간/선택 시각/근접 구간/없음),
// 휠 줌·드래그 팬, 붉은 재생 커서, 60초 자동 갱신+끝 도달 이어보기, 컴팩트 빈 메시지, 실제 녹화 시간.
import React, {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import Hls from 'hls.js';
import {useCameraStore} from '../store/cameraStore';
import {useUIStore} from '../store/uiStore';
import {IconPlay} from '../components/common/Icons';
import {RecCalendar} from '../components/playback/RecCalendar';
import {CamOverview, DayCount, recordings, RecordingStatus, Timeline} from '../services/recordings';
import {LOCALE} from '../services/locale';

const DAY_MS = 24 * 60 * 60 * 1000;
const MIN_MS = 60 * 1000;
const ZOOM_MIN_SPAN = 15 * MIN_MS;  // 최대 확대: 15분
const ZOOM_STEP = 1.25;             // 휠 한 칸 배율
const LIVE_REFRESH_MS = 60_000;     // 타임라인 자동 갱신 주기 (2-3)
const LIVE_CATCHUP_MS = 30_000;     // 끝 도달 판정 여유 — 끝에서 이 안이면 이어보기 트리거
const DRAG_CLICK_PX = 5;            // 드래그/클릭 판정 임계값

function fmtTime(ms: number): string {
  return new Date(ms).toLocaleTimeString(LOCALE, {hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false});
}
function fmtDay(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
function fmtBytes(b: number): string {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)} GB`;
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(1)} MB`;
  return `${(b / 1024).toFixed(0)} KB`;
}
// fmtDur — 실제 녹화 시간(밀리초 → "3시간 24분" / "42분" / "30초")
function fmtDur(ms: number): string {
  if (ms <= 0) return '0초';
  const totalSec = Math.round(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (h > 0) return `${h}시간 ${m}분`;
  if (m > 0) return `${m}분`;
  return `${s}초`;
}
// fmtSpan — 줌 윈도우 폭 ("24시간", "8시간", "45분", "15분")
function fmtSpan(ms: number): string {
  const h = ms / 3600_000;
  if (h >= 1) return `${h >= 24 ? 24 : Math.round(h * 10) / 10}시간`;
  return `${Math.round(ms / MIN_MS)}분`;
}

// PlaybackPage는 녹화 타임라인에서 임의 시각을 재생/seek하고 실시간 녹화를 이어본다.
export function PlaybackPage() {
  const pushToast = useUIStore(s => s.pushToast);
  const cameras = useCameraStore(s => s.cameras);
  const [camId, setCamId] = useState<string>('');
  const [dayStart, setDayStart] = useState<number>(() => {
    const d = new Date();
    d.setHours(0, 0, 0, 0);
    return d.getTime();
  });
  const [tl, setTl] = useState<Timeline | null>(null);
  const [status, setStatus] = useState<RecordingStatus | null>(null);
  const [overview, setOverview] = useState<Map<string, CamOverview>>(new Map());
  const [dayCounts, setDayCounts] = useState<Map<string, DayCount>>(new Map());
  const [loading, setLoading] = useState(false);

  const videoRef = useRef<HTMLVideoElement | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  // 선택 시각(벽시계) — 재생 시작점. null이면 시간바 미선택.
  const [seekTs, setSeekTs] = useState<number | null>(null);
  // 카메라 클릭에 의한 명시적 표시 요청 — 대상 카메라의 타임라인이 도착하면 자동 재생 트리거 (1-1~1-3)
  const autoPlayRef = useRef<{id: string; mode: 'first' | 'seekTs'} | null>(null);
  const pendingOffsetRef = useRef(0); // 다음 loadedmetadata에서 적용할 내부 오프셋(ms)
  const playBaseRef = useRef<number | null>(null); // 플레이리스트 0초 지점의 벽시계 (재생 위치 계산 기준)

  // ── 줌/팬 상태 (2-1) ──
  const [viewSpan, setViewSpan] = useState<number>(DAY_MS);           // 윈도우 폭
  const [viewOffset, setViewOffset] = useState<number>(0);            // dayStart 기준 오프셋
  const trackRef = useRef<HTMLDivElement | null>(null);
  const cursorRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<{active: boolean; startX: number; startOffset: number; moved: boolean} | null>(null);

  const ordered = useMemo(() =>
    [...cameras].sort((a, b) => a.layoutOrder - b.layoutOrder), [cameras]);

  // 기본 선택: 이 날짜에 녹화가 있는 첫 카메라 (없으면 첫 카메라) — 자동 선택은 재생하지 않는다
  useEffect(() => {
    setCamId(cur => {
      if (cur && (overview.size === 0 || overview.has(cur))) return cur;
      const withRec = ordered.find(c => overview.has(c.id));
      return (withRec ?? ordered[0])?.id ?? '';
    });
  }, [overview, ordered]);

  const loadTimeline = useCallback(() => {
    if (!camId) return;
    setLoading(true);
    recordings.timeline(camId, dayStart, dayStart + DAY_MS)
      .then(setTl)
      .catch(() => setTl(null))
      .finally(() => setLoading(false));
  }, [camId, dayStart]);

  useEffect(() => { loadTimeline(); }, [loadTimeline]);

  const loadOverview = useCallback(() => {
    recordings.overview(dayStart, dayStart + DAY_MS)
      .then(o => setOverview(new Map(o.cameras.map(c => [c.cameraId, c]))))
      .catch(() => setOverview(new Map()));
  }, [dayStart]);
  useEffect(() => { loadOverview(); }, [loadOverview]);

  // ── 달력 ──
  const [calOpen, setCalOpen] = useState(false);
  const monthOf = useCallback((ms: number) => {
    const d = new Date(ms);
    return new Date(d.getFullYear(), d.getMonth(), 1).getTime();
  }, []);
  const [viewMonth, setViewMonth] = useState<number>(() => monthOf(Date.now()));
  useEffect(() => {
    const d = new Date(viewMonth);
    const daysInMonth = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate();
    recordings.dayCounts(viewMonth, viewMonth + daysInMonth * DAY_MS)
      .then(r => setDayCounts(new Map(r.days.map(x => [x.day, x]))))
      .catch(() => setDayCounts(new Map()));
  }, [viewMonth]);
  const pickDay = useCallback((ms: number) => {
    setDayStart(ms);
    setViewMonth(monthOf(ms));
    setCalOpen(false);
    setSeekTs(null);
    stopPlayback();
  }, [monthOf]);

  useEffect(() => {
    recordings.status().then(setStatus).catch(() => setStatus(null));
  }, []);

  // ── HLS 재생 (정밀 시작: 세그먼트 시작 + 내부 오프셋) ──
  const startPlayback = useCallback((fromTs: number) => {
    const video = videoRef.current;
    if (!video || !camId) return;
    const url = recordings.playlistUrl(camId, fromTs, dayStart + DAY_MS);
    if (hlsRef.current) { hlsRef.current.destroy(); hlsRef.current = null; }
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = url; // Safari 네이티브 HLS
    } else if (Hls.isSupported()) {
      const hls = new Hls({enableWorker: true});
      hls.loadSource(url);
      hls.attachMedia(video);
      hls.on(Hls.Events.ERROR, (_e, data) => {
        if (!data.fatal) return;
        // fatal 오류도 유형별 자가 복구를 시도한다 — 복구 없으면 플레이어가 죽은 채 유지된다
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
          hls.startLoad();
        } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          hls.recoverMediaError();
        } else {
          pushToast('error', `재생 오류: ${data.details}`);
        }
      });
      hlsRef.current = hls;
    } else {
      pushToast('error', '이 브라우저는 HLS 재생을 지원하지 않습니다.');
      return;
    }
    // 첫 세그먼트 기준 내부 오프셋 — loadedmetadata에서 1회 적용 (1-2 정밀 시작).
    // playBase = 플레이리스트 0초 지점의 벽시계 → 현재 위치 = playBase + mediaTime.
    const seg = (tl?.segments ?? [])
      .filter(s => s.startTs <= fromTs && fromTs < s.startTs + Math.max(s.durMs, 1000))
      .sort((a, b) => b.startTs - a.startTs)[0];
    playBaseRef.current = seg ? seg.startTs : fromTs;
    pendingOffsetRef.current = seg ? Math.round((fromTs - seg.startTs) / 100) * 100 : 0;
    video.play().catch(() => { /* 일부 환경 — loadedmetadata에서 재시도 */ });
  }, [camId, dayStart, tl, pushToast]);

  // pendingOffset 적용 — 소스가 바뀔 때마다 1회 (리스너 누수 없음)
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const apply = () => {
      if (pendingOffsetRef.current > 0) {
        try { video.currentTime = pendingOffsetRef.current / 1000; } catch { /* 무시 */ }
        pendingOffsetRef.current = 0;
      }
      video.play().catch(() => { /* 사용자 제스처 필요 시 무시 */ });
    };
    video.addEventListener('loadedmetadata', apply);
    return () => video.removeEventListener('loadedmetadata', apply);
  }, []);

  const stopPlayback = useCallback(() => {
    if (hlsRef.current) { hlsRef.current.destroy(); hlsRef.current = null; }
    const video = videoRef.current;
    if (video) {
      video.pause();
      video.removeAttribute('src');
      video.load();
    }
  }, []);

  const seek = useCallback((ts: number) => {
    setSeekTs(ts);
    startPlayback(ts);
  }, [startPlayback]);

  // ── 자동 표시: 타임라인 로드 후 autoPlayRef 요청 처리 (1-1~1-4) ──
  // 카메라 연속 클릭 시 stale 타임라인으로 재생하는 race 방지 — 요청 카메라와 도착 타임라인 일치 시에만 실행.
  // nearestSeek: 목표 시각에 가장 가까운 세그먼트(절대 시간 차 최소, 동률이면 이전)를 찾는다 (1-3)
  const nearestSeek = useCallback((target: number): number | null => {
    const segs = tl?.segments ?? [];
    if (segs.length === 0) return null;
    let best = segs[0];
    let bestDiff = Math.abs(best.startTs - target);
    for (const s of segs) {
      const d = Math.abs(s.startTs - target);
      if (d < bestDiff || (d === bestDiff && s.startTs < best.startTs)) { // 동률이면 이전
        best = s; bestDiff = d;
      }
    }
    return best.startTs;
  }, [tl]);

  useEffect(() => {
    const req = autoPlayRef.current;
    if (!req || !tl || tl.cameraId !== req.id) return;
    autoPlayRef.current = null;
    const segs = tl.segments;
    if (segs.length === 0) {
      stopPlayback(); // 1-4 — 녹화 없음
      setSeekTs(null);
      return;
    }
    if (req.mode === 'first') {
      seek(segs[0].startTs); // 1-1 — 그날 첫 녹화부터
      return;
    }
    // req.mode === 'seekTs' — 같은 시각 포함 세그먼트, 없으면 가장 가까운 구간 (1-2/1-3)
    const target = seekTs ?? dayStart;
    const containing = segs.find(s => s.startTs <= target && target < s.startTs + Math.max(s.durMs, 1000));
    const dest = containing ? target : (nearestSeek(target) ?? segs[0].startTs);
    setSeekTs(dest); // 근접 전환 시 커서/메타를 실제 시작점으로 동기화
    startPlayback(dest);
  }, [tl, seekTs, dayStart, startPlayback, nearestSeek]);

  // 카메라 카드 클릭 — 명시적 표시 요청 (1-1/1-2)
  const selectCamera = useCallback((id: string) => {
    if (id === camId) return;
    setCamId(id);
    autoPlayRef.current = {id, mode: seekTs !== null ? 'seekTs' : 'first'};
    // 타임라인은 camId/dayStart effect가 다시 로드 — 도착 후 위 effect가 재생한다
  }, [camId, seekTs]);

  // ── 2-3: 60초 자동 갱신 — 타임라인/현황은 상시, 재생은 끝 도달 시에만 이어받기 ──
  const refreshLive = useCallback(() => {
    loadTimeline();
    loadOverview();
    const video = videoRef.current;
    if (!video || !seekTs || !tl || tl.segments.length === 0) return;
    // stale 타임라인 가드 — 요청 카메라와 도착 타임라인이 일치할 때만 이어받기를 판단한다
    if (tl.cameraId !== camId) return;
    // 일시정지 중에는 이어받기를 하지 않는다 — 60초마다 사용자의 일시정지가 풀리는 문제 방지
    const atEnd = !video.paused && !video.seeking &&
      (video.ended || video.duration > 0 && (video.duration - video.currentTime) * 1000 <= LIVE_CATCHUP_MS);
    if (atEnd) {
      // 현재 재생 위치(벽시계)에서 이어받기 — 새로 녹화된 세그먼트가 플레이리스트에 포함된다
      const wallTs = playBaseRef.current !== null
        ? playBaseRef.current + video.currentTime * 1000
        : seekTs;
      seek(wallTs);
    }
  }, [loadTimeline, loadOverview, seekTs, tl, camId, seek]);

  useEffect(() => {
    const t = setInterval(refreshLive, LIVE_REFRESH_MS);
    return () => clearInterval(t);
  }, [refreshLive]);

  // 날짜 변경 — 재생 정지 + 리셋 (이전 날짜 영상이 계속 재생되는 부작용 제거)
  useEffect(() => {
    stopPlayback();
    setSeekTs(null);
    setViewSpan(DAY_MS);
    setViewOffset(0);
  }, [dayStart]);

  useEffect(() => () => stopPlayback(), []);

  // ── 2-2: 붉은 재생 커서 + 재생 위치 라벨 — 실시각 기준, timeupdate마다 갱신 ──
  // 벽시계 = playBase(플레이리스트 0초 지점) + mediaTime — DISCONTINUITY 건너뛰기에도 정확.
  const [nowLabel, setNowLabel] = useState('');
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const tick = () => {
      const el = cursorRef.current;
      const base = playBaseRef.current;
      const active = seekTs !== null && base !== null && video.duration > 0;
      if (el) {
        if (!active) { el.style.display = 'none'; }
        else {
          const wall = base! + video.currentTime * 1000;
          const pos = ((wall - dayStart - viewOffset) / viewSpan) * 100;
          if (pos < 0 || pos > 100) el.style.display = 'none';
          else { el.style.display = 'block'; el.style.left = `${pos}%`; }
        }
      }
      // 라벨은 초 단위로 바뀔 때만 setState — timeupdate(약 4Hz)마다 페이지 전체 리렌더 방지
      const nextLabel = active ? fmtTime(base! + video.currentTime * 1000) : '';
      setNowLabel(prev => prev === nextLabel ? prev : nextLabel);
    };
    tick();
    video.addEventListener('timeupdate', tick);
    return () => video.removeEventListener('timeupdate', tick);
  }, [seekTs, dayStart, viewOffset, viewSpan]);

  // ── 2-1: 휠 줌(커서 위치 기준) + 드래그 팬 ──
  useEffect(() => {
    const track = trackRef.current;
    if (!track) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = track.getBoundingClientRect();
      const focusRatio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
      const focusTs = dayStart + viewOffset + focusRatio * viewSpan;
      const factor = e.deltaY < 0 ? 1 / ZOOM_STEP : ZOOM_STEP;
      const nextSpan = Math.min(DAY_MS, Math.max(ZOOM_MIN_SPAN, Math.round(viewSpan * factor)));
      if (nextSpan === viewSpan) return;
      // 포커스 시각이 화면상 동일 위치에 유지되도록 오프셋 보정
      const nextOffset = Math.min(DAY_MS - nextSpan, Math.max(0, Math.round(focusTs - dayStart - focusRatio * nextSpan)));
      setViewSpan(nextSpan);
      setViewOffset(nextOffset);
    };
    track.addEventListener('wheel', onWheel, {passive: false});
    return () => track.removeEventListener('wheel', onWheel);
  }, [dayStart, viewOffset, viewSpan]);

  const onPointerDown = (e: React.PointerEvent) => {
    dragRef.current = {active: true, startX: e.clientX, startOffset: viewOffset, moved: false};
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    const d = dragRef.current;
    if (!d?.active || !trackRef.current) return;
    const dx = e.clientX - d.startX;
    if (Math.abs(dx) > DRAG_CLICK_PX) d.moved = true;
    if (!d.moved) return;
    const rect = trackRef.current.getBoundingClientRect();
    const msPerPx = viewSpan / rect.width;
    setViewOffset(Math.min(DAY_MS - viewSpan, Math.max(0, Math.round(d.startOffset - dx * msPerPx))));
  };
  const onPointerUp = (e: React.PointerEvent) => {
    const d = dragRef.current;
    dragRef.current = null;
    (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
    if (!d || d.moved) return; // 드래그였으면 클릭(seek) 아님
    // 클릭 → 해당 시각의 세그먼트 seek (윈도우 좌표 기준)
    // 세그먼트 내부 클릭은 그 시각부터 정밀 재생, 공백 클릭은 다음 구간 시작부터 재생한다
    if (!hasRecording || !trackRef.current) return;
    const rect = trackRef.current.getBoundingClientRect();
    const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
    const ts = dayStart + viewOffset + ratio * viewSpan;
    const segs = tl?.segments ?? [];
    const containing = segs.find(s => s.startTs <= ts && ts < s.startTs + Math.max(s.durMs, 1000));
    if (containing) {
      seek(ts);
      return;
    }
    const next = segs.find(s => s.startTs >= ts);
    if (next) seek(next.startTs);
  };

  const dayEnd = dayStart + DAY_MS;
  const ranges = tl?.ranges ?? [];
  const events = tl?.events ?? [];
  const usedBytes = tl?.usedBytes ?? 0;
  const hasRecording = ranges.length > 0;
  const totalDayBytes = useMemo(
    () => [...overview.values()].reduce((a, c) => a + c.bytes, 0), [overview]);
  const dayWithRec = overview.size > 0;
  // 실제 녹화 시간 — 선택 카메라의 세그먼트 dur_ms 합 (공백 제외)
  const totalDur = useMemo(
    () => (tl?.segments ?? []).reduce((a, s) => a + s.durMs, 0), [tl]);
  // 줌 윈도우 내 실제 녹화 시간
  const winDur = useMemo(() => {
    const winFrom = dayStart + viewOffset;
    const winTo = winFrom + viewSpan;
    return (tl?.segments ?? [])
      .filter(s => s.startTs + Math.max(s.durMs, 1000) > winFrom && s.startTs < winTo)
      .reduce((a, s) => {
        const ov = Math.min(s.startTs + s.durMs, winTo) - Math.max(s.startTs, winFrom);
        return a + Math.max(0, ov);
      }, 0);
  }, [tl, dayStart, viewOffset, viewSpan]);
  const zoomed = viewSpan < DAY_MS;
  // 축 눈금 간격 — 윈도우 폭에 따라 6시간/2시간/30분/15분/5분 간격 (절대 시각 위치)
  const tickStep = useMemo(() => {
    const h = viewSpan / 3600_000;
    const hour = 3600_000;
    if (h >= 20) return 6 * hour;
    if (h >= 8) return 2 * hour;
    if (h >= 3) return hour / 2;
    if (h >= 1) return hour / 4;
    return 5 * MIN_MS;
  }, [viewSpan]);
  const ticks = useMemo(() => {
    const winFrom = dayStart + viewOffset;
    const first = Math.ceil(winFrom / tickStep) * tickStep;
    const out: number[] = [];
    for (let t = first; t <= winFrom + viewSpan; t += tickStep) out.push(t);
    return out;
  }, [dayStart, viewOffset, viewSpan, tickStep]);

  const selectedCam = ordered.find(c => c.id === camId);
  const camOverview = overview.get(camId);

  return (
    <div className="playback-layout">
      <section className="discovery playback-left" aria-label="영상 검색">
        <div className="discovery-head">
          <span className="discovery-hint" style={{marginLeft: 0}}>
            {/* fmtDay(dayStart) */} 녹화 {dayWithRec ? `${overview.size}대 · ${fmtBytes(totalDayBytes)}` : '없음'}
            {status && <>{' · '}전체 사용량 {fmtBytes(status.usedBytes)} · 녹화 중 {status.recording.length}대</>}
          </span>
        </div>

        {/* 날짜 네비 — 달력은 날짜 버튼 아래 오버레이(레이아웃 밀림 없음) */}
        <div className="date-row">
          <div className="date-anchor">
            <div style={{display: 'flex', gap: 4, alignItems: 'center'}}>
              <button type="button" className="btn" aria-label="이전 날"
                onClick={() => setDayStart(d => d - DAY_MS)}>◀</button>
              <button type="button" className="btn btn-date" aria-label="달력에서 선택"
                aria-expanded={calOpen}
                onClick={() => { setCalOpen(o => !o); setViewMonth(monthOf(dayStart)); }}>
                {fmtDay(dayStart)} ▾
              </button>
              <button type="button" className="btn" aria-label="다음 날"
                onClick={() => setDayStart(d => d + DAY_MS)}>▶</button>
            </div>
            {calOpen && (
              <div className="cal-pop">
                <RecCalendar
                  monthStart={viewMonth}
                  selectedDay={dayStart}
                  days={dayCounts}
                  onPrevMonth={() => setViewMonth(m => {
                    const d = new Date(m);
                    return new Date(d.getFullYear(), d.getMonth() - 1, 1).getTime();
                  })}
                  onNextMonth={() => setViewMonth(m => {
                    const d = new Date(m);
                    return new Date(d.getFullYear(), d.getMonth() + 1, 1).getTime();
                  })}
                  onPickDay={pickDay}
                />
              </div>
            )}
          </div>
          <button className="btn" disabled={!camId || loading} onClick={() => { loadTimeline(); loadOverview(); }}>
            {loading ? '불러오는 중…' : '새로고침'}
          </button>
        </div>

        {/* 채널별 녹화 현황 — 세로 목록, 클릭 시 자동 표시 (1-1~1-4) */}
        <div className="cam-picker" role="listbox" aria-label="채널별 녹화 현황">
          {ordered.length === 0 && <div className="empty">등록된 카메라가 없습니다.</div>}
          {ordered.map((c, i) => {
            const ov = overview.get(c.id);
            const active = c.id === camId;
            return (
              <button key={c.id} role="option" aria-selected={active}
                className={`cam-pick ${active ? 'active' : ''} ${ov ? 'has-rec' : ''}`}
                onClick={() => selectCamera(c.id)}>
                <span className="cam-pick-ch">CH {String(i + 1).padStart(2, '0')}</span>
                <span className="cam-pick-name">{c.name}</span>
                <span className="cam-pick-meta">
                  {ov
                    ? <>{fmtTime(ov.firstMs).slice(0, 5)}~{fmtTime(ov.lastMs).slice(0, 5)} · {fmtBytes(ov.bytes)} · 실제 녹화 {fmtDur(ov.totalDurMs)}{ov.events > 0 ? ` · 이벤트 ${ov.events}` : ''}</>
                    : <span className="cam-pick-none">녹화 없음</span>}
                </span>
                {ov && <span className="cam-pick-dot" title="녹화 있음"/>}
              </button>
            );
          })}
        </div>

      </section>

      <section className="discovery playback-right" aria-label="재생">
        {/* 줌 컨트롤 + 타임라인 (2-1 휠 줌/드래그 팬, 2-2 붉은 커서) */}
        <div className="tl-toolbar">
          <span className="tl-zoom-label">{zoomed ? `윈도우 ${fmtSpan(viewSpan)} · 실제 녹화 ${fmtDur(winDur)}` : '전체 24시간'}</span>
          {zoomed && (
            <button type="button" className="btn" onClick={() => { setViewSpan(DAY_MS); setViewOffset(0); }}>
              전체(24시간)
            </button>
          )}
          <span className="discovery-hint">휠: 확대/축소 · 드래그: 이동 · 클릭: 재생</span>
        </div>
        <div className="tl-wrap" role="slider" aria-label="녹화 타임라인" tabIndex={0}
          aria-valuemin={0} aria-valuemax={100}
          ref={trackRef}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}>
          <div className="tl-track">
            {ranges.map((r, i) => (
              <div key={i} className="tl-range" title={`${fmtTime(r.fromMs)} ~ ${fmtTime(r.toMs)} (${fmtBytes(r.bytes)})`}
                style={{
                  left: `${((r.fromMs - dayStart - viewOffset) / viewSpan) * 100}%`,
                  width: `${Math.max(0.2, ((r.toMs - r.fromMs) / viewSpan) * 100)}%`,
                }}/>
            ))}
            {events.map(ev => {
              const left = ((ev.ts - dayStart - viewOffset) / viewSpan) * 100;
              if (left < 0 || left > 100) return null;
              return <div key={ev.id} className="tl-event" title={`이벤트 ${fmtTime(ev.ts)} (${ev.type})`}
                style={{left: `${left}%`}}/>;
            })}
            {/* 2-2 — 붉은 재생 위치 커서 (timeupdate에서 ref 직접 갱신) */}
            <div ref={cursorRef} className="tl-cursor" style={{display: 'none'}}>
              <span className="tl-cursor-head"/>
            </div>
          </div>
          <div className="tl-axis">
            {ticks.map(t => (
              <span key={t} className="tl-tick"
                style={{left: `${((t - dayStart - viewOffset) / viewSpan) * 100}%`}}>
                {fmtTime(t).slice(0, 5)}
              </span>
            ))}
          </div>
        </div>
        {!hasRecording && (
          <div className="tl-empty">
            {loading ? '타임라인을 불러오는 중…'
              : !selectedCam ? '카메라를 선택하세요.'
              : !dayWithRec ? '이 날짜에 녹화된 카메라가 없습니다. 다른 날짜를 선택하세요.'
              : `${selectedCam.name}의 ${fmtDay(dayStart)} 녹화된 영상이 없습니다.`}
          </div>
        )}
        {hasRecording && (
          <div className="mono tl-meta">
            {ranges.length}개 구간 · 실제 녹화 시간 {fmtDur(totalDur)} · {fmtBytes(usedBytes)} · 이벤트 {events.length}건
            {seekTs !== null && nowLabel && <> · 재생 위치 {nowLabel}</>}
          </div>
        )}


        <div className="discovery-head">
          <h3>재생{selectedCam ? ` — ${selectedCam.name}` : ''}</h3>
          {hasRecording && (
            <button className="btn btn-primary" disabled={!camId}
              onClick={() => seek(ranges[0].fromMs)}>
              <IconPlay size={14}/> 첫 구간 재생
            </button>
          )}
        </div>
        <div className="play-video-box">
          <video ref={videoRef} controls playsInline className="play-video"/>
          {/* 1-4 — 선택 카메라의 그날 녹화가 없을 때 */}
          {!loading && camId && !camOverview && (
            <div className="play-video-empty">
              <div className="play-video-empty-title">녹화된 영상이 없습니다</div>
              <div className="play-video-empty-sub">{selectedCam?.name ?? ''} · {fmtDay(dayStart)}</div>
            </div>
          )}
        </div>
        <div className="hint" style={{marginTop: 6}}>
          채널 카드를 클릭하면 녹화 시작 위치가 자동으로 재생됩니다. 구간 사이 공백은 자동으로 건너뜁니다(DISCONTINUITY).
          실시간 녹화는 재생이 구간 끝에 도달하면 자동으로 이어서 재생됩니다.
        </div>
      </section>
    </div>
  );
}
