// 카메라 OSD 카드 목록 — 드래그앤드롭으로 모니터링 표시 순서를 변경한다
import React, {useState} from 'react';
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragEndEvent,
  DragStartEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  rectSortingStrategy,
  useSortable,
  sortableKeyboardCoordinates,
} from '@dnd-kit/sortable';
import {CSS} from '@dnd-kit/utilities';
import {CameraDTO} from '../../types/api';
import {selectOrderedCameras, useCameraStore} from '../../store/cameraStore';
import {useUIStore} from '../../store/uiStore';
import {IconCamera, IconEdit, IconGrip, IconTrash} from '../common/Icons';

// CameraList는 OSD 리티클 카드 그리드다.
export function CameraList() {
  const cameras = useCameraStore(s => s.cameras);
  const reorderCameras = useCameraStore(s => s.reorderCameras);
  const pushToast = useUIStore(s => s.pushToast);
  const [activeId, setActiveId] = useState<string | null>(null);

  const ordered = selectOrderedCameras(cameras);
  const sensors = useSensors(
    useSensor(PointerSensor, {activationConstraint: {distance: 4}}),
    useSensor(KeyboardSensor, {coordinateGetter: sortableKeyboardCoordinates}),
  );

  function onDragStart(e: DragStartEvent) {
    setActiveId(String(e.active.id));
  }

  async function onDragEnd(e: DragEndEvent) {
    setActiveId(null);
    const {active, over} = e;
    if (!over || active.id === over.id) return;
    const oldIndex = ordered.findIndex(c => c.id === active.id);
    const newIndex = ordered.findIndex(c => c.id === over.id);
    const next = arrayMove(ordered, oldIndex, newIndex);
    try {
      await reorderCameras(next.map(c => c.id));
      pushToast('ok', '표시 순서가 변경되었습니다.');
    } catch (err) {
      pushToast('error', `순서 변경 실패: ${String(err)}`);
    }
  }

  if (ordered.length === 0) {
    return (
      <div className="empty">
        <div style={{display: 'flex', justifyContent: 'center', color: 'var(--dim)'}}><IconCamera size={28}/></div>
        <h3>등록된 카메라가 없습니다</h3>
        <p>네트워크를 검색하거나 오른쪽 위의 [카메라 추가]로 직접 등록하세요.</p>
      </div>
    );
  }

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={onDragStart} onDragEnd={onDragEnd}>
      <SortableContext items={ordered.map(c => c.id)} strategy={rectSortingStrategy}>
        <div className="camera-grid">
          {ordered.map((cam, i) => (
            <CameraCard key={cam.id} camera={cam} channel={i + 1} dimmed={activeId === cam.id}/>
          ))}
        </div>
      </SortableContext>
    </DndContext>
  );
}

// CameraCard는 단일 채널 카드다. 채널 번호는 layoutOrder에서 온다.
function CameraCard({camera, channel, dimmed}: {camera: CameraDTO; channel: number; dimmed: boolean}) {
  const {updateCamera, deleteCamera} = useCameraStore();
  const {pushToast, openCameraModal} = useUIStore();
  const [confirming, setConfirming] = useState(false);
  const {attributes, listeners, setNodeRef, transform, transition, isDragging} = useSortable({id: camera.id});

  async function toggleEnabled() {
    try {
      await updateCamera(camera.id, {enabled: !camera.enabled});
    } catch (e) {
      pushToast('error', `상태 변경 실패: ${String(e)}`);
    }
  }

  async function remove() {
    try {
      await deleteCamera(camera.id);
      pushToast('ok', `카메라가 삭제되었습니다 — ${camera.name}`);
    } catch (e) {
      pushToast('error', `삭제 실패: ${String(e)}`);
    }
  }

  const host = camera.type === 'onvif' ? camera.xaddr : hostOf(camera.streamUrl);

  return (
    <article
      ref={setNodeRef}
      className={`osd-card ${dimmed || isDragging ? 'dragging' : ''}`}
      style={{transform: CSS.Transform.toString(transform), transition}}
    >
      <i className="osd-corner tl" aria-hidden="true"/><i className="osd-corner tr" aria-hidden="true"/>
      <i className="osd-corner bl" aria-hidden="true"/><i className="osd-corner br" aria-hidden="true"/>

      <div className="osd-top">
        <span className="plate">CH {String(channel).padStart(2, '0')}</span>
        <span className="type-badge">{camera.type.toUpperCase()}</span>
        <span className="osd-status">
          <span className={`status-dot ${camera.enabled ? 'on' : ''}`}/>
          {camera.enabled ? '사용' : '중지'}
        </span>
      </div>

      <div className="osd-name">{camera.name}</div>
      <div className="osd-host">{host || '—'}</div>

      <div className="osd-meta">
        {camera.ptzSupported && <span className="meta-tag ptz">PTZ</span>}
        <span className="meta-tag">{camera.streamConfig?.transport?.toUpperCase() ?? 'TCP'}</span>
        {camera.hasPassword && <span className="meta-tag">인증 저장됨</span>}
        {camera.groupId && <span className="meta-tag">#{camera.groupId}</span>}
      </div>

      <div className="osd-actions">
        <button
          className={`switch ${camera.enabled ? 'on' : ''}`}
          onClick={toggleEnabled}
          aria-pressed={camera.enabled}
          aria-label={camera.enabled ? '카메라 중지' : '카메라 사용'}
          title={camera.enabled ? '사용 중지' : '사용 시작'}
        />
        <span style={{flex: 1}}/>
        {confirming ? (
          <>
            <button className="btn btn-ghost btn-danger" onClick={remove}>삭제 확인</button>
            <button className="btn btn-ghost" onClick={() => setConfirming(false)}>취소</button>
          </>
        ) : (
          <>
            <button className="btn btn-ghost" onClick={() => openCameraModal({mode: 'edit', cameraId: camera.id})}
              aria-label={`${camera.name} 편집`}><IconEdit size={14}/></button>
            <button className="btn btn-ghost btn-danger" onClick={() => setConfirming(true)}
              aria-label={`${camera.name} 삭제`}><IconTrash size={14}/></button>
            <button className="osd-grip" {...attributes} {...listeners} aria-label="순서 변경 핸들">
              <IconGrip size={16}/>
            </button>
          </>
        )}
      </div>
    </article>
  );
}

// hostOf는 스트림 URL에서 호스트 부분을 잘라낸다.
function hostOf(url: string): string {
  if (!url) return '';
  return url.replace(/^\w+:\/\//, '').split('/').shift() ?? url;
}
