// 상단 헤더: 페이지 제목과 주요 동작 (모니터링 제어 포함 — doc 4.7)
import React, {useEffect, useState} from 'react';
import {IconPlus, IconFull} from '../common/Icons';
import {useUIStore} from '../../store/uiStore';
import {GridMode} from '../../types';
import {selectOrderedCameras, useCameraStore} from '../../store/cameraStore';
import {useStreamStore} from '../../store/streamStore';
import {AppConfig} from '../../types/api';
import {api} from '../../services/api';

function fmtGB(b: number): string {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)} GB`;
  return `${(b / (1 << 20)).toFixed(0)} MB`;
}

const GRID_OPTIONS: {value: GridMode; label: string}[] = [
  {value: 'auto', label: 'A'},
  {value: 1, label: '1'},
  {value: 4, label: '4'},
  {value: 9, label: '9'},
  {value: 16, label: '16'},
  {value: 25, label: '25'},
];

// toggleFullscreen은 모니터링 화면의 전체화면을 전환한다 (doc 5.3).
function toggleFullscreen() {
  if (document.fullscreenElement) {
    void document.exitFullscreen();
  } else {
    void document.documentElement.requestFullscreen();
  }
}

// slotsFor와 colsFor는 CameraGrid와 동일한 로직
function slotsFor(mode: GridMode, cameraCount: number): number {
  if (mode === 'auto') return Math.max(cameraCount, 1);
  return mode;
}

function colsFor(mode: GridMode, slots: number): number {
  if (mode !== 'auto') return Math.round(Math.sqrt(mode));
  const cols = Math.ceil(Math.sqrt(slots));
  return Math.max(cols, 1);
}

export function Toolbar() {
  const currentPage = useUIStore(s => s.currentPage);
  const setPage = useUIStore(s => s.setPage);
  const openCameraModal = useUIStore(s => s.openCameraModal);
  const gridMode = useUIStore(s => s.gridMode);
  const setGridMode = useUIStore(s => s.setGridMode);
  const gridPage = useUIStore(s => s.gridPage);
  const setGridPage = useUIStore(s => s.setGridPage);

  const cameras = useCameraStore(s => s.cameras);

  const streamingCount = useStreamStore(s =>
    Object.values(s.states).filter(st => st === 'streaming').length);
  const stats = useStreamStore(s => s.stats);
  const connected = useStreamStore(s => s.connected);
  const pendingUpdate = useStreamStore(s => s.pendingCameraUpdate);
  const applyCameraUpdate = useStreamStore(s => s.applyCameraUpdate);
  const recordingCount = useStreamStore(s => Object.keys(s.recording).length);
  const recUsedBytes = useStreamStore(s => s.recUsedBytes);
  const recEnabled = useStreamStore(s => Object.keys(s.recording).length > 0 || s.recUsedBytes > 0);
  const hasPendingUpdate = pendingUpdate.count > 0 || pendingUpdate.reloadAll;

  // 녹화 할당량 — 설정 저장(config_changed) 시 즉시 갱신, 그 외에는 마운트 시 1회
  const [quotaGB, setQuotaGB] = useState<number | null>(null);
  useEffect(() => {
    const load = () => api.appConfig()
      .then((c: AppConfig) => setQuotaGB(c.recording?.max_usage_gb ?? null))
      .catch(() => {});
    load();
    window.addEventListener('webnvr-config-changed', load);
    return () => window.removeEventListener('webnvr-config-changed', load);
  }, []);

  const titles: Record<string, {eyebrow: string; title: string}> = {
    monitoring: {eyebrow: 'Live Grid', title: '모니터링'},
    playback: {eyebrow: 'Recording Search', title: '영상 검색'},
    management: {eyebrow: 'Camera Registry', title: '카메라 관리'},
    settings: {eyebrow: 'Preferences', title: '설정'},
  };
  const t = titles[currentPage] ?? titles.management;
  const enabled = selectOrderedCameras(cameras).filter(c => c.enabled);

  // 페이지네이션 계산 (CameraGrid와 동일)
  const slots = slotsFor(gridMode, enabled.length);
  const totalPages = Math.max(1, Math.ceil(enabled.length / slots));
  const page = Math.min(gridPage, totalPages - 1);

  return (
    <header className="header">
      <div>
        <div className="header-eyebrow">{t.eyebrow}</div>
        <h1 className="header-title">{t.title}</h1>
      </div>

      {currentPage === 'management' && (
        <>
          <span className="header-count">
            등록 <b>{String(cameras.length).padStart(2, '0')}</b> · 사용 <b>{String(enabled.length).padStart(2, '0')}</b>
          </span>
          <div className="header-actions">
            <button
              className="btn btn-primary"
              onClick={() => openCameraModal({mode: 'add'})}
              disabled={!connected}
              title={connected ? undefined : '백엔드 서버에 연결되어 있지 않습니다'}
            >
              <IconPlus size={14}/> 카메라 추가
            </button>
          </div>
        </>
      )}

      {currentPage === 'monitoring' && cameras.length > 0 && (
        <>
          {(() => {
            // StatsPanel과 동일한 로직으로 통계 계산
            const rows = selectOrderedCameras(cameras)
              .filter(c => stats[c.id])
              .map(c => stats[c.id]);
            const totalKbps = rows.reduce((sum, s) => sum + s.kbps, 0);
            const totalDrops = rows.reduce((sum, s) => sum + s.drops, 0);
            const totalFps = rows.reduce((sum, s) => sum + s.fps, 0);

            return (
              <span className="header-count">
                스트리밍 <b>{String(streamingCount).padStart(2, '0')}</b> / {String(enabled.length).padStart(2, '0')}
                {rows.length > 0 && (
                  <span className="header-stats">
                    {' · '}전체 <b>{totalKbps.toLocaleString()}</b> kbps
                    {' · '}<b>{Math.round(totalFps)}</b> fps
                    {totalDrops > 0 && <span className="stats-drops">{' · '}드롭 {totalDrops}</span>}
                  </span>
                )}
                {/* 녹화 상태 — 1분 폴링(recUsedBytes/recording) + 설정 변경 시 즉시 갱신 */}
                {recEnabled && (
                  <span className="header-stats">
                    {' · '}녹화 <b>{fmtGB(recUsedBytes)}</b>
                    {quotaGB !== null && quotaGB > 0 && <> / {quotaGB}GB</>}
                    {recordingCount > 0 && <> · 녹화 중 <b>{recordingCount}</b>대</>}
                  </span>
                )}
              </span>
            );
          })()}

          <div className="header-actions">
            {hasPendingUpdate && (
              <button
                className="btn btn-attention"
                onClick={() => { void applyCameraUpdate(); }}
                title="다른 곳에서 변경된 카메라 설정을 화면에 반영합니다"
              >
                설정 변경{pendingUpdate.count > 0 ? ` ${pendingUpdate.count}건` : ''} — 적용
              </button>
            )}
            <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
              {GRID_OPTIONS.map(o => (
                <button
                  key={String(o.value)}
                  className={`btn ${gridMode === o.value ? 'btn-active' : ''}`}
                  title={`분할: ${o.label}`}
                  onClick={() => setGridMode(o.value)}
                  style={{minWidth: '32px'}}
                >
                  {o.label}
                </button>
              ))}
              {totalPages > 1 && (
                <div style={{display: 'flex', gap: '4px', alignItems: 'center', marginLeft: '4px', paddingLeft: '8px', borderLeft: '1px solid rgba(0,0,0,0.1)'}}>
                  <button
                    className="btn btn-ghost"
                    disabled={page === 0}
                    title="이전 페이지"
                    onClick={() => setGridPage(Math.max(0, page - 1))}
                    style={{minWidth: '24px', padding: '4px 6px'}}
                  >
                    ‹
                  </button>
                  <span style={{fontSize: '12px', minWidth: '40px', textAlign: 'center'}}>{page + 1}/{totalPages}</span>
                  <button
                    className="btn btn-ghost"
                    disabled={page === totalPages - 1}
                    title="다음 페이지"
                    onClick={() => setGridPage(Math.min(totalPages - 1, page + 1))}
                    style={{minWidth: '24px', padding: '4px 6px'}}
                  >
                    ›
                  </button>
                </div>
              )}
            </div>
            <button className="btn" title="전체화면 전환" onClick={toggleFullscreen}>
              <IconFull size={14}/>
            </button>
          </div>
        </>
      )}
    </header>
  );
}
