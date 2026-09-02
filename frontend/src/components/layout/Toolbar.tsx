// 상단 헤더: 페이지 제목과 주요 동작 (모니터링 제어 포함 — doc 4.7)
import React from 'react';
import {IconPlus, IconFull} from '../common/Icons';
import {useUIStore} from '../../store/uiStore';
import {GridMode} from '../../types';
import {selectOrderedCameras, useCameraStore} from '../../store/cameraStore';
import {useStreamStore} from '../../store/streamStore';

const GRID_OPTIONS: {value: GridMode; label: string}[] = [
  {value: 'auto', label: '자동'},
  {value: 1, label: '1×1'},
  {value: 4, label: '2×2'},
  {value: 9, label: '3×3'},
  {value: 16, label: '4×4'},
];

// toggleFullscreen은 모니터링 화면의 전체화면을 전환한다 (doc 5.3).
function toggleFullscreen() {
  if (document.fullscreenElement) {
    void document.exitFullscreen();
  } else {
    void document.documentElement.requestFullscreen();
  }
}

export function Toolbar() {
  const currentPage = useUIStore(s => s.currentPage);
  const openCameraModal = useUIStore(s => s.openCameraModal);
  const gridMode = useUIStore(s => s.gridMode);
  const setGridMode = useUIStore(s => s.setGridMode);

  const cameras = useCameraStore(s => s.cameras);

  const streamingCount = useStreamStore(s =>
    Object.values(s.states).filter(st => st === 'streaming').length);

  const titles: Record<string, {eyebrow: string; title: string}> = {
    monitoring: {eyebrow: 'Live Grid', title: '모니터링'},
    management: {eyebrow: 'Camera Registry', title: '카메라 등록부'},
    settings: {eyebrow: 'Preferences', title: '설정'},
  };
  const t = titles[currentPage] ?? titles.management;
  const enabled = selectOrderedCameras(cameras).filter(c => c.enabled);

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
            <button className="btn btn-primary" onClick={() => openCameraModal({mode: 'add'})}>
              <IconPlus size={14}/> 카메라 추가
            </button>
          </div>
        </>
      )}

      {currentPage === 'monitoring' && cameras.length > 0 && (
        <>
          <span className="header-count">
            스트리밍 <b>{String(streamingCount).padStart(2, '0')}</b> / {String(enabled.length).padStart(2, '0')}
          </span>
          <div className="header-actions">
            <div className="field" style={{margin: 0, width: 110}}>
              <select value={String(gridMode)} aria-label="레이아웃 선택" onChange={e => {
                const v = e.target.value;
                setGridMode(v === 'auto' ? 'auto' : Number(v) as GridMode);
              }}>
                {GRID_OPTIONS.map(o => (
                  <option key={String(o.value)} value={String(o.value)}>{o.label}</option>
                ))}
              </select>
            </div>
            <button className="btn" title="전체화면 전환" onClick={toggleFullscreen}>
              <IconFull size={14}/> 전체화면
            </button>
          </div>
        </>
      )}
    </header>
  );
}
