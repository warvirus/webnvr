// 모니터링 페이지 — 진입 시 자동 시작, 이탈 시 정지 (클라이언트별 독립 재생, 2026-09-02)
// PTZ 패널은 선택된 PTZ 카메라에만 표시된다.
import React, {useEffect, useMemo, useRef, useState} from 'react';
import {CameraGrid} from '../components/grid/CameraGrid';
import {PTZControl} from '../components/grid/PTZControl';
import {IconGrid} from '../components/common/Icons';
import {useCameraStore} from '../store/cameraStore';
import {useStreamStore} from '../store/streamStore';
import {useUIStore} from '../store/uiStore';

// MonitoringPage는 활성화된 카메라의 실시간 스트림을 그리드로 표시한다.
export function MonitoringPage() {
  // console.log('🎥 MonitoringPage 마운트됨');
  const cameras = useCameraStore(s => s.cameras);
  const fetchCameras = useCameraStore(s => s.fetchCameras);
  const states = useStreamStore(s => s.states);
  const stats = useStreamStore(s => s.stats);
  const desired = useStreamStore(s => s.desired);
  const retries = useStreamStore(s => s.retries);
  const connected = useStreamStore(s => s.connected);
  const lastError = useStreamStore(s => s.lastError);
  const startStream = useStreamStore(s => s.startStream);
  const stopStream = useStreamStore(s => s.stopStream);
  const clearError = () => useStreamStore.setState({lastError: null});
  const pushToast = useUIStore(s => s.pushToast);
  const setPage = useUIStore(s => s.setPage);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // 카메라 목록 로드
  useEffect(() => {
    fetchCameras().catch(err => pushToast('error', `카메라 목록 조회 실패: ${String(err)}`));
  }, [fetchCameras, pushToast]);

  // ── 클라이언트별 재생 라이프사이클 ──
  // 활성 카메라 id 집합을 추적해 추가/삭제분만 start/stop 한다.
  // (cameras 레퍼런스가 바뀔 때마다 전 채널을 끊었다 재시작하면 이름 변경 등에도 영상이 깜빡인다.)
  // 이탈(언마운트): 이 클라이언트의 모든 구독 해제 + 자동 재연결 의사 해제.
  // 다른 클라이언트의 화면 전환은 백엔드 참조 카운팅이 관리하므로 무영향이다.
  const appliedRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const wanted = new Set(cameras.filter(c => c.enabled).map(c => c.id));
    for (const id of wanted) if (!appliedRef.current.has(id)) startStream(id);
    for (const id of appliedRef.current) if (!wanted.has(id)) stopStream(id);
    appliedRef.current = wanted;
  }, [cameras, startStream, stopStream]);
  useEffect(() => () => {
    for (const id of appliedRef.current) stopStream(id);
    appliedRef.current = new Set();
  }, [stopStream]);

  // 보던 카메라가 다른 클라이언트에 의해 삭제되면 선택 상태를 정리한다.
  useEffect(() => {
    if (selectedId && !cameras.some(c => c.id === selectedId)) setSelectedId(null);
  }, [cameras, selectedId]);

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
        desired={desired}
        retries={retries}
        selectedId={selectedId}
        onSelect={id => setSelectedId(id === selectedId ? null : id)}
      />

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
