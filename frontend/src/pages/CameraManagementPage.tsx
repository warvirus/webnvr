// 카메라 관리 페이지 — 검색 스트립 + OSD 카드 그리드
import React, {useEffect} from 'react';
import {DiscoveryPanel} from '../components/camera/DiscoveryPanel';
import {CameraList} from '../components/camera/CameraList';
import {CameraModal} from '../components/camera/CameraModal';
import {useCameraStore} from '../store/cameraStore';
import {useUIStore} from '../store/uiStore';

export function CameraManagementPage() {
  const fetchCameras = useCameraStore(s => s.fetchCameras);
  const loaded = useCameraStore(s => s.loaded);
  const cameras = useCameraStore(s => s.cameras);
  const cameraModal = useUIStore(s => s.cameraModal);

  useEffect(() => {
    fetchCameras().catch(err => {
      useUIStore.getState().pushToast('error', `카메라 목록 조회 실패: ${String(err)}`);
    });
  }, [fetchCameras]);

  const editing = cameraModal?.cameraId
    ? cameras.find(c => c.id === cameraModal.cameraId) ?? null
    : null;

  return (
    <>
      <DiscoveryPanel/>
      {loaded ? <CameraList/> : <div className="empty">카메라 목록을 불러오는 중…</div>}
      {cameraModal && (
        <CameraModal
          mode={cameraModal.mode}
          camera={editing}
          presetXAddr={cameraModal.presetXAddr}
        />
      )}
    </>
  );
}
