// 모니터링 카메라 그리드 — 레이아웃 모드에 따라 스트리밍 카메라를 배치한다
import React from 'react';
import {CameraDTO} from '../../types/api';
import {GridMode, StreamState, StreamStats} from '../../types';
import {CameraTile} from './CameraTile';
import {selectOrderedCameras} from '../../store/cameraStore';
import {useUIStore} from '../../store/uiStore';

interface Props {
  cameras: CameraDTO[];
  states: Record<string, StreamState>;
  stats: Record<string, StreamStats>;
  desired: Record<string, boolean>;
  retries: Record<string, number>;
  selectedId: string | null;
  onSelect: (cameraId: string) => void;
}

// slotsFor는 그리드 모드에 따라 표시할 슬롯 수를 반환한다.
function slotsFor(mode: GridMode, cameraCount: number): number {
  if (mode === 'auto') return Math.max(cameraCount, 1);
  return mode;
}

// colsFor는 슬롯 수에 따라 열 수를 정한다.
function colsFor(mode: GridMode, slots: number): number {
  if (mode !== 'auto') return Math.round(Math.sqrt(mode));
  const cols = Math.ceil(Math.sqrt(slots));
  return Math.max(cols, 1);
}

// CameraGrid는 활성화된 카메라를 OSD 타일로 배치한다.
export function CameraGrid({cameras, states, stats, desired, retries, selectedId, onSelect}: Props) {
  const gridMode = useUIStore(s => s.gridMode);
  const gridPage = useUIStore(s => s.gridPage);
  const focusedCameraId = useUIStore(s => s.focusedCameraId);
  const setFocusedCamera = useUIStore(s => s.setFocusedCamera);

  const ordered = selectOrderedCameras(cameras).filter(c => c.enabled);

  // 포커스 모드: 선택한 카메라 1개만 표시
  if (focusedCameraId) {
    const focusedIdx = ordered.findIndex(c => c.id === focusedCameraId);
    if (focusedIdx === -1) {
      // 포커스 대상을 찾지 못하면 포커스 해제
      setFocusedCamera(null);
      return <div className="camera-monitor-grid" />;
    }

    const focusedCam = ordered[focusedIdx];
    const prevIdx = focusedIdx === 0 ? ordered.length - 1 : focusedIdx - 1;
    const nextIdx = focusedIdx === ordered.length - 1 ? 0 : focusedIdx + 1;

    return (
      <>
        {ordered.length > 1 && (
          <div className="grid-pager">
            <button className="btn btn-ghost" title="이전 카메라" onClick={() => setFocusedCamera(ordered[prevIdx].id)}>
              ‹
            </button>
            <span>{focusedIdx + 1} / {ordered.length}</span>
            <button className="btn btn-ghost" title="다음 카메라" onClick={() => setFocusedCamera(ordered[nextIdx].id)}>
              ›
            </button>
          </div>
        )}
        <div className="camera-monitor-grid" style={{gridTemplateColumns: '1fr'}}>
          <CameraTile
            key={focusedCam.id}
            camera={focusedCam}
            channel={focusedIdx + 1}
            state={states[focusedCam.id] ?? 'idle'}
            stats={stats[focusedCam.id]}
            selected={selectedId === focusedCam.id}
            active={true}
            onSelect={() => onSelect(focusedCam.id)}
            onDoubleClick={() => setFocusedCamera(null)}
          />
        </div>
      </>
    );
  }

  // 일반 모드: 그리드 페이지네이션
  const slots = slotsFor(gridMode, ordered.length);
  const cols = colsFor(gridMode, slots);
  const totalPages = Math.max(1, Math.ceil(ordered.length / slots));
  const page = Math.min(gridPage, totalPages - 1);
  const offset = page * slots;
  const visible = ordered.slice(offset, offset + slots);

  return (
    <div
      className="camera-monitor-grid"
      style={{gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`}}
    >
      {visible.map((cam, i) => (
        <CameraTile
          key={cam.id}
          camera={cam}
          channel={offset + i + 1}
          state={states[cam.id] ?? 'idle'}
          stats={stats[cam.id]}
          selected={selectedId === cam.id}
          active={i < slots}
          onSelect={() => onSelect(cam.id)}
          onDoubleClick={() => setFocusedCamera(cam.id)}
        />
      ))}
    </div>
  );
}
