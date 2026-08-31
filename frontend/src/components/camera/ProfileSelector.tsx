// ONVIF 프로필 자동 조회 후 선택하는 드롭다운 컴포넌트
// 등록된 카메라(카메라 ID)는 저장 자격증명으로, 미등록 카메라는 입력 자격증명으로 조회한다.
import React, {useState} from 'react';
import {ProfileDTO} from '../../types/api';
import {useCameraStore} from '../../store/cameraStore';
import {useUIStore} from '../../store/uiStore';

interface Props {
  cameraId?: string; // 편집(등록된 카메라)일 때 지정
  xaddr: string;
  username: string;
  password: string;
  selected: string;
  onSelect: (token: string) => void;
}

// ProfileSelector는 카메라의 미디어 프로필을 조회해 목록에서 선택받는다.
export function ProfileSelector({cameraId, xaddr, username, password, selected, onSelect}: Props) {
  const getProfiles = useCameraStore(s => s.getProfiles);
  const getCameraProfiles = useCameraStore(s => s.getCameraProfiles);
  const pushToast = useUIStore(s => s.pushToast);
  const [profiles, setProfiles] = useState<ProfileDTO[] | null>(null);
  const [loading, setLoading] = useState(false);

  async function load() {
    setLoading(true);
    try {
      const list = cameraId
        ? await getCameraProfiles(cameraId)
        : await getProfiles({xaddr, username, password});
      setProfiles(list);
      if (list.length === 0) {
        pushToast('error', '카메라가 제공하는 프로필이 없습니다. 카메라 설정을 확인하세요.');
      } else if (!selected && list.length > 0) {
        onSelect(list[0].token);
      }
    } catch (e) {
      setProfiles([]);
      pushToast('error', `프로필 조회 실패: ${String(e)}`);
    } finally {
      setLoading(false);
    }
  }

  if (profiles === null) {
    return (
      <div className="field">
        <label>미디어 프로필 <span className="req">*</span></label>
        <button type="button" className="btn" onClick={load} disabled={loading}>
          {loading ? '조회 중…' : '프로필 불러오기'}
        </button>
        <div className="hint">카메라에 연결해 사용 가능한 화질 프로필을 가져옵니다.</div>
      </div>
    );
  }

  return (
    <div className="field">
      <label>미디어 프로필 <span className="req">*</span></label>
      {profiles.length === 0 ? (
        <div className="test-box test-fail">프로필을 찾지 못했습니다. 자격 증명을 확인하고 다시 시도하세요.</div>
      ) : (
        <div className="profile-list" role="listbox" aria-label="미디어 프로필">
          {profiles.map(p => (
            <button
              key={p.token}
              type="button"
              role="option"
              aria-selected={selected === p.token}
              className={`profile-item ${selected === p.token ? 'active' : ''}`}
              onClick={() => onSelect(p.token)}
            >
              <span>{p.name || '(이름 없음)'}</span>
              <span className="mono">{p.token}</span>
              {p.width > 0 && <span className="res">{p.width}×{p.height}</span>}
            </button>
          ))}
        </div>
      )}
      {profiles.length > 0 && (
        <button type="button" className="btn-ghost btn" style={{marginTop: 6}} onClick={() => setProfiles(null)}>
          다시 조회
        </button>
      )}
    </div>
  );
}
