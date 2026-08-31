// 실시간 통계 슬림 바 — 전체 전송률 중심 표시 (doc 5.1)
// 채널별 fps 그래프는 타일 제목 옆(CameraTile)으로 이동했다.
import React from 'react';
import {selectOrderedCameras, useCameraStore} from '../../store/cameraStore';
import {useStreamStore} from '../../store/streamStore';

// StatsBar은 스트리밍 카메라 전체의 통계 요약을 표시한다.
export function StatsPanel() {
  const cameras = useCameraStore(s => s.cameras);
  const stats = useStreamStore(s => s.stats);

  const rows = selectOrderedCameras(cameras)
    .filter(c => stats[c.id])
    .map(c => stats[c.id]);

  if (rows.length === 0) {
    return null; // 스트리밍 중이 아니면 공간을 차지하지 않는다
  }

  const totalKbps = rows.reduce((sum, s) => sum + s.kbps, 0);
  const totalDrops = rows.reduce((sum, s) => sum + s.drops, 0);
  const totalFps = rows.reduce((sum, s) => sum + s.fps, 0);

  return (
    <div className="stats-bar" aria-label="통계 요약">
      <span className="stats-total">
        전체 전송률 <b>{totalKbps.toLocaleString()}</b> kbps
      </span>
      <span className="stats-item">총 {Math.round(totalFps)} fps</span>
      <span className="stats-item">채널 {rows.length}</span>
      {totalDrops > 0 && <span className="stats-item stats-drops">누적 드롭 {totalDrops}</span>}
    </div>
  );
}
