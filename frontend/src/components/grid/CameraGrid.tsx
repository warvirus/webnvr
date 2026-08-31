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
  const ordered = selectOrderedCameras(cameras).filter(c => c.enabled);
  const slots = slotsFor(gridMode, ordered.length);
  const cols = colsFor(gridMode, slots);
  const visible = ordered.slice(0, slots);

  return (
    <div
      className="camera-monitor-grid"
      style={{gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`}}
    >
      {visible.map((cam, i) => (
        <CameraTile
          key={cam.id}
          camera={cam}
          channel={i + 1}
          state={states[cam.id] ?? 'idle'}
          stats={stats[cam.id]}
          selected={selectedId === cam.id}
          active={i < slots}
          onSelect={() => onSelect(cam.id)}
        />
      ))}
    </div>
  );
}
