// 모니터링 페이지 — Phase 4에서 멀티뷰 그리드 + WebCodecs 디코딩으로 구현 예정
import React from 'react';
import {IconGrid} from '../components/common/Icons';

export function MonitoringPage() {
  return (
    <div className="empty" style={{marginTop: 24}}>
      <div style={{display: 'flex', justifyContent: 'center', color: 'var(--amber)'}}><IconGrid size={32}/></div>
      <h3>모니터링 그리드는 준비 중입니다</h3>
      <p>Phase 4에서 실시간 멀티뷰(WebSocket + WebCodecs 디코딩)로 열립니다. 먼저 카메라를 등록해 두세요.</p>
    </div>
  );
}
