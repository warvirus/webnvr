// 모니터링 페이지 — 카메라 그리드 + 스트림 상태 + PTZ 패널
// (전체 시작/정지, 레이아웃 선택은 Toolbar에 있음 — doc 4.7)
import React, {useEffect, useMemo, useState} from 'react';
import {CameraGrid} from '../components/grid/CameraGrid';
import {PTZControl} from '../components/grid/PTZControl';
import {StatsPanel} from '../components/stats/StatsPanel';
import {IconGrid} from '../components/common/Icons';
import {useCameraStore} from '../store/cameraStore';
import {useStreamStore} from '../store/streamStore';
import {useUIStore} from '../store/uiStore';

// MonitoringPage는 활성화된 카메라의 실시간 스트림을 그리드로 표시한다.
export function MonitoringPage() {
  const cameras = useCameraStore(s => s.cameras);
  const fetchCameras = useCameraStore(s => s.fetchCameras);
  const states = useStreamStore(s => s.states);
  const stats = useStreamStore(s => s.stats);
  const connected = useStreamStore(s => s.connected);
  const lastError = useStreamStore(s => s.lastError);
  const startStream = useStreamStore(s => s.startStream);
  const init = useStreamStore(s => s.init);
  const clearError = () => useStreamStore.setState({lastError: null});
  const pushToast = useUIStore(s => s.pushToast);
  const setPage = useUIStore(s => s.setPage);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // WS 연결 + 메시지 라우팅 초기화
  useEffect(() => init(), [init]);

  // 카메라 목록 로드
  useEffect(() => {
    fetchCameras().catch(err => pushToast('error', `카메라 목록 조회 실패: ${String(err)}`));
  }, [fetchCameras, pushToast]);

  const streamingCount = useMemo(
    () => Object.values(states).filter(st => st === 'streaming').length,
    [states],
  );

  const selectedCamera = useMemo(
    () => cameras.find(c => c.id === selectedId) ?? null,
    [cameras, selectedId],
  );
  const selectedState = selectedId ? states[selectedId] : undefined;
  const showPtz = !!(selectedCamera?.ptzSupported && selectedState === 'streaming');

  if (cameras.length === 0) {
    return (
      <div className="empty">
        <div style={{display: 'flex', justifyContent: 'center', color: 'var(--dim)'}}><IconGrid size={28}/></div>
        <h3>등록된 카메라가 없습니다</h3>
        <p style={{marginBottom: 12}}>먼저 카메라를 등록하세요.</p>
        <button className="btn" onClick={() => setPage('management')}>카메라 관리로 이동</button>
      </div>
    );
  }

  return (
    <>
      {!connected && (
        <div className="test-box test-fail">백엔드와 연결이 끊겼습니다. 자동으로 재연결 중…</div>
      )}
      {connected && lastError && (
        <div className="test-box test-fail">
          {lastError}
          <button className="btn" style={{marginLeft: 10}} onClick={clearError}>닫기</button>
        </div>
      )}

      <CameraGrid
        cameras={cameras}
        states={states}
        stats={stats}
        selectedId={selectedId}
        onSelect={id => setSelectedId(id === selectedId ? null : id)}
      />

      <StatsPanel/>

      {streamingCount === 0 && (
        <div className="empty" style={{padding: '24px'}}>
          <p>스트림이 중지되어 있습니다. 상단의 <b>전체 시작</b> 버튼으로 모니터링을 시작하세요.</p>
        </div>
      )}

      {showPtz && selectedCamera && (
        <section className="discovery" aria-label="PTZ 제어 패널">
          <div className="discovery-head">
            <h3>PTZ — {selectedCamera.name}</h3>
            <span className="discovery-hint">조이스틱을 드래그해 카메라를 움직입니다</span>
            <button className="btn btn-ghost" onClick={() => setSelectedId(null)}>닫기</button>
          </div>
          <PTZControl cameraId={selectedCamera.id}/>
        </section>
      )}
    </>
  );
}
