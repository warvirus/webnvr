// 카메라 연결 테스트 결과를 표시하는 컴포넌트 (ONVIF / 직접 스트림 공용)
import React from 'react';
import {IconCheck, IconAlert} from '../common/Icons';

export type TestState =
  | {kind: 'idle'}
  | {kind: 'testing'}
  | {kind: 'onvif-ok'; res: import('../../types/api').TestONVIFResponse}
  | {kind: 'direct-ok'}
  | {kind: 'fail'; message: string};

interface Props {
  state: TestState;
  onRetry: () => void;
}

// ConnectionTest는 연결 테스트 상태를 사람이 읽는 결과로 보여준다.
export function ConnectionTest({state, onRetry}: Props) {
  if (state.kind === 'idle') return null;

  if (state.kind === 'testing') {
    return <div className="test-box">연결을 확인하는 중… (최대 10초)</div>;
  }

  if (state.kind === 'onvif-ok') {
    return (
      <div className="test-box test-ok">
        <div style={{display: 'flex', alignItems: 'center', gap: 6, fontWeight: 700}}>
          <IconCheck size={14}/> 연결됨 — {state.res.manufacturer} {state.res.model}
        </div>
        <div className="mono">펌웨어 {state.res.firmware || '정보 없음'}</div>
      </div>
    );
  }

  if (state.kind === 'direct-ok') {
    return (
      <div className="test-box test-ok">
        <div style={{display: 'flex', alignItems: 'center', gap: 6, fontWeight: 700}}>
          <IconCheck size={14}/> 스트림 주소에 연결됨
        </div>
      </div>
    );
  }

  // fail
  return (
    <div className="test-box test-fail">
      <div style={{display: 'flex', alignItems: 'center', gap: 6, fontWeight: 700}}>
        <IconAlert size={14}/> 연결 실패
      </div>
      <div className="mono">{state.message}</div>
      <div style={{marginTop: 6}}>
        <button type="button" className="btn" onClick={onRetry}>다시 시도</button>
      </div>
    </div>
  );
}
