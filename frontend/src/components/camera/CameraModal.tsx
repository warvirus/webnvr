// 카메라 추가/수정 모달
import React from 'react';
import {CameraDTO} from '../../types/api';
import {CameraForm, CameraFormValue} from './CameraForm';
import {defaultStreamConfig, useCameraStore} from '../../store/cameraStore';
import {useUIStore} from '../../store/uiStore';

interface Props {
  mode: 'add' | 'edit';
  camera?: CameraDTO | null;
  presetXAddr?: string;
}

// CameraModal은 카메라 폼을 모달로 감싼다.
export function CameraModal({mode, camera, presetXAddr}: Props) {
  const {closeCameraModal, pushToast} = useUIStore();
  const {addCamera, updateCamera} = useCameraStore();

  async function handleSubmit(v: CameraFormValue) {
    if (mode === 'add') {
      const saved = await addCamera({
        name: v.name.trim(),
        type: v.type as CameraDTO['type'],
        xaddr: v.type === 'onvif' ? v.xaddr.trim() : '',
        username: v.type === 'onvif' ? v.username : '',
        password: v.type === 'onvif' ? v.password : '',
        profileToken: v.type === 'onvif' ? v.profileToken : '',
        streamUrl: v.type === 'onvif' ? '' : v.streamUrl.trim(),
        streamConfig: {
          transport: v.transport,
          protocol: v.type === 'rtsp' ? 'rtsp' : v.type === 'rtp' ? 'rtp' : v.type === 'rtmp' ? 'rtmp' : 'rtsp',
          buffer_size: defaultStreamConfig().buffer_size,
        },
        ptzSupported: v.type === 'onvif' ? v.ptzSupported : false,
        groupId: v.groupId.trim(),
      });
      if (saved) pushToast('ok', `카메라가 등록되었습니다 — ${saved.name}`);
    } else if (camera) {
      const req: Parameters<typeof updateCamera>[1] = {
        name: v.name.trim(),
        xaddr: v.type === 'onvif' ? v.xaddr.trim() : '',
        username: v.type === 'onvif' ? v.username : '',
        streamUrl: v.type === 'onvif' ? '' : v.streamUrl.trim(),
        streamConfig: {
          transport: v.transport,
          protocol: camera.streamConfig?.protocol ?? 'rtsp',
          buffer_size: camera.streamConfig?.buffer_size ?? 1024 * 1024,
        },
        ptzSupported: v.type === 'onvif' ? v.ptzSupported : false,
        groupId: v.groupId.trim(),
      };
      if (v.password) req.password = v.password; // 입력한 경우에만 변경
      if (v.type === 'onvif' && v.profileToken) req.profileToken = v.profileToken;
      await updateCamera(camera.id, req);
      pushToast('ok', `변경 사항이 저장되었습니다 — ${v.name.trim()}`);
    }
    closeCameraModal();
  }

  const title = mode === 'add' ? '카메라 추가' : `카메라 편집 — ${camera?.name ?? ''}`;

  return (
    <div className="overlay" onMouseDown={e => { if (e.target === e.currentTarget) closeCameraModal(); }}>
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        <div className="modal-head">
          <h2>{title}</h2>
          <button className="btn btn-ghost" onClick={closeCameraModal} aria-label="닫기">✕</button>
        </div>
        <CameraForm
          mode={mode}
          cameraId={camera?.id}
          initial={camera ?? null}
          presetXAddr={presetXAddr}
          onSubmit={handleSubmit}
          onCancel={closeCameraModal}
        />
      </div>
    </div>
  );
}
