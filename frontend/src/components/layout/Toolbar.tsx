// 상단 헤더: 페이지 제목과 주요 동작
import React from 'react';
import {api} from '../../../wailsjs/go/models';
import {IconPlus} from '../common/Icons';
import {useUIStore} from '../../store/uiStore';
import {selectOrderedCameras, useCameraStore} from '../../store/cameraStore';

export function Toolbar() {
  const {currentPage} = useUIStore();
  const cameras = useCameraStore(s => s.cameras);
  const openCameraModal = useUIStore(s => s.openCameraModal);

  const titles: Record<string, {eyebrow: string; title: string}> = {
    monitoring: {eyebrow: 'Live Grid', title: '모니터링'},
    management: {eyebrow: 'Camera Registry', title: '카메라 등록부'},
  };
  const t = titles[currentPage] ?? titles.management;
  const enabled = selectOrderedCameras(cameras).filter(c => c.enabled).length;

  return (
    <header className="header">
      <div>
        <div className="header-eyebrow">{t.eyebrow}</div>
        <h1 className="header-title">{t.title}</h1>
      </div>
      {currentPage === 'management' && (
        <>
          <span className="header-count">
            등록 <b>{String(cameras.length).padStart(2, '0')}</b> · 사용 <b>{String(enabled).padStart(2, '0')}</b>
          </span>
          <div className="header-actions">
            <button className="btn btn-primary" onClick={() => openCameraModal({mode: 'add'})}>
              <IconPlus size={14}/> 카메라 추가
            </button>
          </div>
        </>
      )}
    </header>
  );
}
