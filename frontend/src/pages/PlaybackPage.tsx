// 다시보기 페이지 — 날짜별 전 카메라 녹화 현황(한눈에 보기), 타임라인 스크러버, HLS 재생 (Phase R.3/R.5)
import React, {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import Hls from 'hls.js';
import {useCameraStore} from '../store/cameraStore';
import {useUIStore} from '../store/uiStore';
import {IconChevronLeft, IconChevronRight, IconPlay} from '../components/common/Icons';
import {CamOverview, recordings, RecordingStatus, Timeline} from '../services/recordings';

const DAY_MS = 24 * 60 * 60 * 1000;

function fmtTime(ms: number): string {
  return new Date(ms).toLocaleTimeString('ko-KR', {hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false});
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

// PlaybackPage는 녹화 타임라인에서 임의 시각을 재생/seek한다.
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
  const [loading, setLoading] = useState(false);

  const videoRef = useRef<HTMLVideoElement | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  const // 선택 시각(벽시계) — 이 세그먼트부터 플레이리스트를 만든다
    [seekTs, setSeekTs] = useState<number | null>(null);

  const ordered = useMemo(() =>
    [...cameras].sort((a, b) => a.layoutOrder - b.layoutOrder), [cameras]);

  useEffect(() => {
    if (!camId && ordered.length > 0) setCamId(ordered[0].id);
  }, [camId, ordered]);

  const loadTimeline = useCallback(() => {
    if (!camId) return;
    setLoading(true);
    recordings.timeline(camId, dayStart, dayStart + DAY_MS)
      .then(setTl)
      .catch(e => { setTl(null); pushToast('error', `타임라인 조회 실패: ${String(e)}`); })
      .finally(() => setLoading(false));
  }, [camId, dayStart, pushToast]);

  useEffect(() => { loadTimeline(); }, [loadTimeline]);

  // 날짜별 전 카메라 녹화 현황 (한눈에 보기) — 날짜가 바뀌면 다시 조회한다
  const loadOverview = useCallback(() => {
    recordings.overview(dayStart, dayStart + DAY_MS)
      .then(o => setOverview(new Map(o.cameras.map(c => [c.cameraId, c]))))
      .catch(() => setOverview(new Map()));
  }, [dayStart]);
  useEffect(() => { loadOverview(); }, [loadOverview]);

  useEffect(() => {
    recordings.status().then(setStatus).catch(() => setStatus(null));
  }, []);

  // 기본 선택: 이 날짜에 녹화가 있는 첫 카메라 (없으면 첫 카메라)
  useEffect(() => {
    setCamId(cur => {
      if (cur && (overview.size === 0 || overview.has(cur))) return cur;
      const withRec = ordered.find(c => overview.has(c.id));
      return (withRec ?? ordered[0])?.id ?? '';
    });
  }, [overview, ordered]);

  // HLS 재생 시작 — seekTs부터 (없으면 첫 구간)
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
        if (data.fatal) pushToast('error', `재생 오류: ${data.details}`);
      });
      hlsRef.current = hls;
    } else {
      pushToast('error', '이 브라우저는 HLS 재생을 지원하지 않습니다.');
      return;
    }
    video.play().catch(() => { /* 사용자 제스처 필요 시 무시 */ });
  }, [camId, dayStart, pushToast]);

  const seek = useCallback((ts: number) => {
    setSeekTs(ts);
    startPlayback(ts);
  }, [startPlayback]);

  // 현재 벽시계 — 재생 중 세그먼트 start_ts + 실행 위치 (플레이리스트 단순 매핑)
  const [nowLabel, setNowLabel] = useState<string>('');
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !seekTs) { setNowLabel(''); return; }
    const tick = () => setNowLabel(fmtTime(seekTs + video.currentTime * 1000));
    tick();
    video.addEventListener('timeupdate', tick);
    return () => video.removeEventListener('timeupdate', tick);
  }, [seekTs]);

  useEffect(() => () => {
    hlsRef.current?.destroy();
    hlsRef.current = null;
  }, []);

  const dayEnd = dayStart + DAY_MS;
  const ranges = tl?.ranges ?? [];
  const events = tl?.events ?? [];
  const usedBytes = tl?.usedBytes ?? 0;
  const hasRecording = ranges.length > 0;
  const totalDayBytes = useMemo(
    () => [...overview.values()].reduce((a, c) => a + c.bytes, 0), [overview]);
  const dayWithRec = overview.size > 0;

  return (
    <>
      <section className="discovery" aria-label="다시보기">
        <div className="discovery-head">
          <h3>다시보기</h3>
          <span className="discovery-hint">
            {fmtDay(dayStart)} 녹화 {dayWithRec ? `${overview.size}대 · ${fmtBytes(totalDayBytes)}` : '없음'}
            {status && <> · 전체 사용량 {fmtBytes(status.usedBytes)} · 녹화 중 {status.recording.length}대</>}
          </span>
        </div>

        <div className="field-row" style={{alignItems: 'end'}}>
          <div className="field">
            <label>날짜</label>
            <div style={{display: 'flex', gap: 4, alignItems: 'center'}}>
              <button type="button" className="btn" aria-label="이전 날"
                onClick={() => setDayStart(d => d - DAY_MS)}><IconChevronLeft size={14}/></button>
              <span className="mono" style={{minWidth: 90, textAlign: 'center'}}>{fmtDay(dayStart)}</span>
              <button type="button" className="btn" aria-label="다음 날"
                onClick={() => setDayStart(d => d + DAY_MS)}><IconChevronRight size={14}/></button>
            </div>
          </div>
          <div className="field" style={{flex: 1}}>
            <label>&nbsp;</label>
            <button className="btn" disabled={!camId || loading} onClick={() => { loadTimeline(); loadOverview(); }}>
              {loading ? '불러오는 중…' : '새로고침'}
            </button>
          </div>
        </div>

        {/* 카메라별 녹화 현황 — 이 날짜의 녹화를 한눈에 본다 */}
        <div className="cam-picker" role="listbox" aria-label="카메라별 녹화 현황">
          {ordered.length === 0 && <div className="empty" style={{flex: 1}}>등록된 카메라가 없습니다.</div>}
          {ordered.map((c, i) => {
            const ov = overview.get(c.id);
            const active = c.id === camId;
            return (
              <button key={c.id} role="option" aria-selected={active}
                className={`cam-pick ${active ? 'active' : ''} ${ov ? 'has-rec' : ''}`}
                onClick={() => { setCamId(c.id); setSeekTs(null); }}>
                <span className="cam-pick-ch">CH {String(i + 1).padStart(2, '0')}</span>
                <span className="cam-pick-name">{c.name}</span>
                <span className="cam-pick-meta">
                  {ov
                    ? <>{fmtTime(ov.firstMs).slice(0, 5)}~{fmtTime(ov.lastMs).slice(0, 5)} · {fmtBytes(ov.bytes)}{ov.events > 0 ? ` · 이벤트 ${ov.events}` : ''}</>
                    : <span className="cam-pick-none">녹화 없음</span>}
                </span>
                {ov && <span className="cam-pick-dot" title="녹화 있음"/>}
              </button>
            );
          })}
        </div>

        {/* 타임라인 스크러버 */}
        <div className="tl-wrap" role="slider" aria-label="녹화 타임라인" tabIndex={0}
          aria-valuemin={0} aria-valuemax={100}
          onClick={e => {
            if (!hasRecording) return;
            const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
            const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
            const ts = dayStart + ratio * DAY_MS;
            // 클릭 시각을 포함하는 세그먼트 (없으면 그 다음 세그먼트)
            const segs = (tl?.segments ?? []).filter(s => s.startTs + s.durMs >= ts);
            const target = segs.length > 0 ? Math.max(segs[0].startTs, ts - 0) : null;
            if (target !== null) seek(Math.min(target, ts === segs[0]?.startTs ? ts : segs[0].startTs));
          }}>
          <div className="tl-track">
            {ranges.map((r, i) => (
              <div key={i} className="tl-range" title={`${fmtTime(r.fromMs)} ~ ${fmtTime(r.toMs)} (${fmtBytes(r.bytes)})`}
                style={{
                  left: `${((r.fromMs - dayStart) / DAY_MS) * 100}%`,
                  width: `${Math.max(0.2, ((r.toMs - r.fromMs) / DAY_MS) * 100)}%`,
                }}/>
            ))}
            {events.map(ev => (
              <div key={ev.id} className="tl-event" title={`이벤트 ${fmtTime(ev.ts)} (${ev.type})`}
                style={{left: `${((ev.ts - dayStart) / DAY_MS) * 100}%`}}/>
            ))}
          </div>
          <div className="tl-axis">
            {[0, 6, 12, 18, 24].map(h => (
              <span key={h} className="tl-tick"
                style={{left: `${(h / 24) * 100}%`}}>{String(h).padStart(2, '0')}시</span>
            ))}
          </div>
        </div>
        {!hasRecording && (
          <div className="empty" style={{marginTop: 8}}>
            {loading ? '타임라인을 불러오는 중…'
              : !dayWithRec ? '이 날짜에 녹화된 카메라가 없습니다. 다른 날짜를 선택하세요.'
              : `${ordered.find(c => c.id === camId)?.name ?? '선택된 카메라'}의 이 날짜 녹화가 없습니다.`}
          </div>
        )}
        {hasRecording && (
          <div className="mono tl-meta">
            {ranges.length}개 구간 · {fmtBytes(usedBytes)} · 이벤트 {events.length}건
            {nowLabel && <> · 재생 위치 {nowLabel}</>}
          </div>
        )}
      </section>

      <section className="discovery" aria-label="재생">
        <div className="discovery-head">
          <h3>재생{camId ? ` — ${ordered.find(c => c.id === camId)?.name ?? ''}` : ''}</h3>
          {hasRecording && (
            <button className="btn btn-primary" disabled={!camId}
              onClick={() => seek(ranges[0].fromMs)}>
              <IconPlay size={14}/> 첫 구간 재생
            </button>
          )}
        </div>
        <div className="play-video-box">
          <video ref={videoRef} controls playsInline className="play-video"/>
        </div>
        <div className="hint" style={{marginTop: 6}}>
          타임라인을 클릭하면 해당 시각부터 재생합니다. 구간 사이 공백은 자동으로 건너뜁니다(DISCONTINUITY).
        </div>
      </section>
    </>
  );
}
