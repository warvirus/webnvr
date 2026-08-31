// webnvr 앱 루트 — 레일/헤더/페이지/상태바 조립
import React, {useEffect} from 'react';
import {Sidebar} from './components/layout/Sidebar';
import {Toolbar} from './components/layout/Toolbar';
import {StatusBar} from './components/layout/StatusBar';
import {CameraManagementPage} from './pages/CameraManagementPage';
import {SettingsPage} from './pages/SettingsPage';
import {MonitoringPage} from './pages/MonitoringPage';
import {useUIStore} from './store/uiStore';

export default function App() {
  const currentPage = useUIStore(s => s.currentPage);
  const toasts = useUIStore(s => s.toasts);
  const dismissToast = useUIStore(s => s.dismissToast);

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
      <Sidebar/>
      <Toolbar/>
      <main className="main">
        {currentPage === 'monitoring' && <MonitoringPage/>}
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
