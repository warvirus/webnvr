// 카메라 추가/수정 폼 — 타입별 필드 동적 표시와 유효성 검증
import React, {useMemo, useState} from 'react';
import {CameraDTO, TestDirectStreamRequest, TestONVIFRequest} from '../../types/api';
import {ConnectionTest, TestState} from './ConnectionTest';
import {ProfileSelector} from './ProfileSelector';
import {useCameraStore} from '../../store/cameraStore';

export interface CameraFormValue {
  name: string;
  type: string;
  xaddr: string;
  username: string;
  password: string;
  streamUrl: string;
  profileToken: string;
  ptzSupported: boolean;
  groupId: string;
  transport: string;
}

interface Props {
  mode: 'add' | 'edit';
  cameraId?: string; // edit 모드: 등록된 카메라의 저장 자격증명으로 프로필/URI 조회
  initial?: CameraDTO | null;
  presetXAddr?: string;
  onSubmit: (value: CameraFormValue) => Promise<void>;
  onCancel: () => void;
}

const TYPE_LABELS: [string, string][] = [
  ['onvif', 'ONVIF'],
  ['rtsp', 'RTSP'],
  ['rtp', 'RTP'],
  ['rtmp', 'RTMP'],
];

// CameraForm은 카메라 타입에 따라 필요한 필드만 보여준다.
export function CameraForm({mode, cameraId, initial, presetXAddr, onSubmit, onCancel}: Props) {
  const testONVIF = useCameraStore(s => s.testONVIF);
  const testDirectStream = useCameraStore(s => s.testDirectStream);
  const getCameraStreamURI = useCameraStore(s => s.getCameraStreamURI);

  const [value, setValue] = useState<CameraFormValue>({
    name: initial?.name ?? '',
    type: initial?.type ?? 'onvif',
    xaddr: initial?.xaddr ?? presetXAddr ?? '',
    username: initial?.username ?? '',
    password: '',
    streamUrl: initial?.streamUrl ?? '',
    profileToken: initial?.profileToken ?? '',
    ptzSupported: initial?.ptzSupported ?? false,
    groupId: initial?.groupId ?? '',
    transport: initial?.streamConfig?.transport ?? 'tcp',
  });
  const [errors, setErrors] = useState<string[]>([]);
  const [test, setTest] = useState<TestState>({kind: 'idle'});
  const [submitting, setSubmitting] = useState(false);

  const set = (patch: Partial<CameraFormValue>) => {
    setValue(v => ({...v, ...patch}));
    setTest({kind: 'idle'});
  };

  const isONVIF = value.type === 'onvif';
  const canSubmit = useMemo(() => {
    if (mode === 'add') {
      if (!value.name.trim()) return false;
      if (isONVIF) return !!(value.xaddr.trim() && value.username && value.password && value.profileToken);
      return !!value.streamUrl.trim();
    }
    // 수정: 프로필은 기존 값 유지 가능
    if (!value.name.trim()) return false;
    if (isONVIF) return !!(value.xaddr.trim() && value.username);
    return !!value.streamUrl.trim();
  }, [mode, value, isONVIF]);

  function validate(): boolean {
    const errs: string[] = [];
    if (!value.name.trim()) errs.push('카메라 이름을 입력하세요.');
    if (isONVIF) {
      if (!value.xaddr.trim()) errs.push('카메라 주소(host:port)를 입력하세요.');
      if (!value.username) errs.push('사용자 이름을 입력하세요.');
      if (mode === 'add' && !value.password) errs.push('비밀번호를 입력하세요.');
      if (mode === 'add' && !value.profileToken) errs.push('프로필을 불러와 선택하세요.');
    } else {
      if (!value.streamUrl.trim()) errs.push('스트림 URL을 입력하세요.');
      const want = value.type + '://';
      if (value.streamUrl.trim() && !value.streamUrl.toLowerCase().startsWith(want)) {
        errs.push(`URL은 ${want} 형식이어야 합니다.`);
      }
    }
    setErrors(errs);
    return errs.length === 0;
  }

  async function runTest() {
    if (isONVIF) {
      setTest({kind: 'testing'});
      try {
        const req: TestONVIFRequest = {
          xaddr: value.xaddr.trim(), username: value.username, password: value.password,
        };
        const res = await testONVIF(req);
        setTest(res.ok ? {kind: 'onvif-ok', res} : {kind: 'fail', message: res.error || '원인을 알 수 없습니다.'});
      } catch (e) {
        setTest({kind: 'fail', message: String(e)});
      }
    } else {
      setTest({kind: 'testing'});
      try {
        const req: TestDirectStreamRequest = {url: value.streamUrl.trim(), timeoutMs: 3000};
        const res = await testDirectStream(req);
        setTest(res.ok ? {kind: 'direct-ok'} : {kind: 'fail', message: res.error || '원인을 알 수 없습니다.'});
      } catch (e) {
        setTest({kind: 'fail', message: String(e)});
      }
    }
  }

  async function submit() {
    if (!validate()) return;
    setSubmitting(true);
    try {
      await onSubmit(value);
    } catch (e) {
      setErrors([String(e)]);
    } finally {
      setSubmitting(false);
    }
  }

  // 편집 모드에서 스트림 URI 미리보기 (저장 자격증명 사용 — 비밀번호 재입력 불필요)
  async function previewURI() {
    if (!cameraId) return;
    try {
      const uri = await getCameraStreamURI(cameraId);
      setTest({kind: 'onvif-ok', res: {ok: true, error: '', manufacturer: '스트림 URI', model: uri, firmware: ''}});
    } catch (e) {
      setTest({kind: 'fail', message: String(e)});
    }
  }

  return (
    <form onSubmit={e => { e.preventDefault(); submit(); }}>
      <div className="type-picker" role="radiogroup" aria-label="카메라 타입">
        {TYPE_LABELS.map(([t, label]) => (
          <button
            key={t} type="button" role="radio" aria-checked={value.type === t}
            className={`type-pick ${value.type === t ? 'active' : ''}`}
            onClick={() => set({type: t, profileToken: ''})}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="field">
        <label>카메라 이름 <span className="req">*</span></label>
        <input type="text" value={value.name} placeholder="예: 정문 카메라"
          onChange={e => set({name: e.target.value})}/>
      </div>

      {isONVIF ? (
        <>
          <div className="field">
            <label>카메라 주소 (host:port) <span className="req">*</span></label>
            <input type="text" value={value.xaddr} placeholder="예: 192.168.0.217:8090"
              onChange={e => set({xaddr: e.target.value})}/>
            <div className="hint">WS-Discovery 검색 결과에서 가져온 주소를 사용할 수도 있습니다.</div>
          </div>
          <div className="field-row">
            <div className="field">
              <label>사용자 이름 <span className="req">*</span></label>
              <input type="text" value={value.username} autoComplete="off"
                onChange={e => set({username: e.target.value})}/>
            </div>
            <div className="field">
              <label>비밀번호 {mode === 'add' ? <span className="req">*</span> : <span>(변경 시 입력)</span>}</label>
              <input type="password" value={value.password} autoComplete="new-password"
                placeholder={mode === 'edit' && initial?.hasPassword ? '저장된 비밀번호 사용' : ''}
                onChange={e => set({password: e.target.value})}/>
            </div>
          </div>
          <ProfileSelector
            cameraId={mode === 'edit' ? cameraId : undefined}
            xaddr={value.xaddr.trim()}
            username={value.username}
            password={value.password}
            selected={value.profileToken}
            onSelect={token => set({profileToken: token})}
          />
          <label className="check-field">
            <input type="checkbox" checked={value.ptzSupported}
              onChange={e => set({ptzSupported: e.target.checked})}/>
            PTZ(좌우/상하 회전) 카메라입니다
          </label>
        </>
      ) : (
        <>
          <div className="field">
            <label>스트림 URL <span className="req">*</span></label>
            <input type="text" value={value.streamUrl}
              placeholder={`${value.type}://user:pass@192.168.1.101:554/stream1`}
              onChange={e => set({streamUrl: e.target.value})}/>
          </div>
        </>
      )}

      <div className="field-row">
        <div className="field">
          <label>전송 방식</label>
          <select value={value.transport} onChange={e => set({transport: e.target.value})}>
            <option value="tcp">TCP (안정적)</option>
            <option value="udp">UDP (저지연)</option>
          </select>
        </div>
        <div className="field">
          <label>그룹 (선택)</label>
          <input type="text" value={value.groupId} placeholder="예: entrance"
            onChange={e => set({groupId: e.target.value})}/>
        </div>
      </div>

      <ConnectionTest state={test} onRetry={runTest}/>

      {errors.length > 0 && (
        <div className="test-box test-fail" role="alert">
          {errors.map((err, i) => <div key={i}>· {err}</div>)}
        </div>
      )}

      <div className="modal-foot">
        <button type="button" className="btn" onClick={runTest}>연결 테스트</button>
        {isONVIF && value.profileToken && mode === 'edit' && (
          <button type="button" className="btn btn-ghost" onClick={previewURI}>스트림 URI 확인</button>
        )}
        <button type="button" className="btn" onClick={onCancel}>취소</button>
        <button type="submit" className="btn btn-primary" disabled={!canSubmit || submitting}>
          {submitting ? '저장 중…' : mode === 'add' ? '카메라 추가' : '변경 사항 저장'}
        </button>
      </div>
    </form>
  );
}
