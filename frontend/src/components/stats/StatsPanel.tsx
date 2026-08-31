// 실시간 통계 대시보드 — 스트리밍 중인 카메라의 fps/비트레이트/드롭 추이 표시 (doc 5.1)
import React from 'react';
import {Sparkline} from './Sparkline';
import {selectOrderedCameras, useCameraStore} from '../../store/cameraStore';
import {useStreamStore} from '../../store/streamStore';

// StatsPanel은 스트리밍 카메라별 실시간 통계를 표시한다.
export function StatsPanel() {
  const cameras = useCameraStore(s => s.cameras);
  const stats = useStreamStore(s => s.stats);
  const history = useStreamStore(s => s.history);

  const rows = selectOrderedCameras(cameras)
    .filter(c => stats[c.id])
    .map(c => ({camera: c, stat: stats[c.id], hist: history[c.id] ?? []}));

  if (rows.length === 0) {
    return (
      <section className="discovery" aria-label="통계 대시보드">
        <div className="discovery-head">
          <h3>통계</h3>
          <span className="discovery-hint">스트리밍 중인 카메라가 없습니다</span>
        </div>
      </section>
    );
  }

  const totalKbps = rows.reduce((sum, r) => sum + r.stat.kbps, 0);
  const totalDrops = rows.reduce((sum, r) => sum + r.stat.drops, 0);

  return (
    <section className="discovery" aria-label="통계 대시보드">
      <div className="discovery-head">
        <h3>통계</h3>
        <span className="discovery-hint">
          합계 {totalKbps.toLocaleString()} kbps · 누적 드롭 {totalDrops} · 최근 60초
        </span>
      </div>
      <div className="stats-grid">
        {rows.map(({camera, stat, hist}) => (
          <div className="stats-card" key={camera.id}>
            <div className="stats-head">
              <span className="plate">{camera.name}</span>
              <span className="stats-nums">
                <b>{stat.fps}</b> fps · <b>{stat.kbps.toLocaleString()}</b> kbps
                {stat.drops > 0 && <span className="stats-drops"> · 드롭 {stat.drops}</span>}
              </span>
            </div>
            <Sparkline samples={hist}/>
          </div>
        ))}
      </div>
    </section>
  );
}
