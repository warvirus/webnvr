// webnvr 앱 루트 — 레일/헤더/페이지/상태바 조립
import React, {useEffect} from 'react';
import {Sidebar} from './components/layout/Sidebar';
import {Toolbar} from './components/layout/Toolbar';
import {StatusBar} from './components/layout/StatusBar';
import {ClientLimitOverlay} from './components/common/ClientLimitOverlay';
import {ReconnectingOverlay} from './components/common/ReconnectingOverlay';
import {CameraManagementPage} from './pages/CameraManagementPage';
import {SettingsPage} from './pages/SettingsPage';
import {MonitoringPage} from './pages/MonitoringPage';
import {PlaybackPage} from './pages/PlaybackPage';
import {useUIStore} from './store/uiStore';
import {useStreamStore} from './store/streamStore';
import {useCameraStore} from './store/cameraStore';

export default function App() {
  // console.log('📱 App 컴포넌트 렌더링됨');
  const currentPage = useUIStore(s => s.currentPage);
  const toasts = useUIStore(s => s.toasts);
  const dismissToast = useUIStore(s => s.dismissToast);
  const init = useStreamStore(s => s.init);
  const connected = useStreamStore(s => s.connected);
  const fetchCameras = useCameraStore(s => s.fetchCameras);

  // WS 연결 + 메시지 라우팅 초기화 — 앱 전역(어느 페이지에서도 연결 상태를 알 수 있어야 한다)
  useEffect(() => init(), [init]);

  // 백엔드에 (재)연결되면 카메라 목록을 다시 불러온다 — 시작 시 백엔드가 죽어 있었어도
  // 살아나는 순간 목록/모니터링/스트림이 '처음 접속처럼' 복구된다.
  useEffect(() => {
    if (!connected) return;
    fetchCameras().catch(() => {
      // 자동 재시도라 조용히 실패 — 연결 배너/비활성 버튼이 이미 상태를 알린다
    });
  }, [connected, fetchCameras]);

  // ESC로 토스트 닫기 등 전역 키 처리
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && toasts.length > 0) {
        dismissToast(toasts[toasts.length - 1].id);
      }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [toasts, dismissToast]);

  return (
    <div className="shell">
      <ClientLimitOverlay/>
      <ReconnectingOverlay/>
      <Sidebar/>
      <Toolbar/>
      <main className="main">
        {currentPage === 'monitoring' && <MonitoringPage/>}
        {currentPage === 'playback' && <PlaybackPage/>}
        {currentPage === 'management' && <CameraManagementPage/>}
        {currentPage === 'settings' && <SettingsPage/>}
      </main>
      <StatusBar/>
      <div className="toasts" aria-live="polite">
        {toasts.map(t => (
          <div key={t.id} className={`toast ${t.kind}`} onClick={() => dismissToast(t.id)}>
            {t.text}
          </div>
        ))}
      </div>
    </div>
  );
}
