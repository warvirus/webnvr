// WS-Discovery 자동 검색 패널 — 3초 내외 스캔 후 결과 칩 목록 표시
import React from 'react';
import {IconPlus, IconSearch, IconRefresh} from '../common/Icons';
import {useCameraStore} from '../../store/cameraStore';
import {useUIStore} from '../../store/uiStore';
import {useStreamStore} from '../../store/streamStore';

// DiscoveryPanel은 로컬 네트워크의 ONVIF 카메라를 찾아 추가를 유도한다.
export function DiscoveryPanel() {
  const {discovered, isDiscovering, discover} = useCameraStore();
  const {pushToast, openCameraModal} = useUIStore();
  const connected = useStreamStore(s => s.connected);

  async function scan() {
    try {
      await discover();
      if (useCameraStore.getState().discovered.length === 0) {
        pushToast('info', '검색 결과가 없습니다. 카메라와 같은 네트워크에 연결되어 있는지 확인하세요.');
      }
    } catch (e) {
      pushToast('error', `네트워크 검색 실패: ${String(e)}`);
    }
  }

  function nameOf(scopes: string): string {
    const m = scopes.match(/onvif:\/\/[^ ]*\/name\/([^ ]+)/i);
    return m ? decodeURIComponent(m[1]) : '';
  }

  return (
    <section className="discovery" aria-label="네트워크 카메라 검색">
      <div className="discovery-head">
        <h3>네트워크 검색</h3>
        <button
          className="btn"
          onClick={scan}
          disabled={isDiscovering || !connected}
          title={connected ? undefined : '백엔드 서버에 연결되어 있지 않습니다'}
        >
          {isDiscovering ? <IconRefresh size={14}/> : <IconSearch size={14}/>}
          {isDiscovering ? '검색 중…' : '네트워크 검색'}
        </button>
        <span className="discovery-hint">같은 네트워크의 ONVIF 카메라를 약 3초간 탐색합니다</span>
      </div>
      {discovered.length > 0 && (
        <div className="discovery-results">
          {discovered.map(d => {
            const name = nameOf(d.scopes);
            return (
              <div className="discovered" key={d.xaddr}>
                <span className="discovered-ip">{d.xaddr}</span>
                {name && <span className="discovered-name">{name}</span>}
                <button className="btn" onClick={() => openCameraModal({mode: 'add', presetXAddr: d.xaddr})}>
                  <IconPlus size={12}/> 추가
                </button>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
