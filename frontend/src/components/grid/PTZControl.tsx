// PTZ 조이스틱 + 속도 + 프리셋 이동 컨트롤
import React, {useCallback, useEffect, useRef, useState} from 'react';
import {PresetDTO} from '../../types/api';
import {useCameraStore} from '../../store/cameraStore';
import {useStreamStore} from '../../store/streamStore';
import {useUIStore} from '../../store/uiStore';

interface Props {
  cameraId: string;
}

const SEND_INTERVAL_MS = 200;
const DEADZONE = 0.15;

// PTZControl은 선택된 카메라의 PTZ를 제어한다.
export function PTZControl({cameraId}: Props) {
  const ptzControl = useStreamStore(s => s.ptzControl);
  const getCameraPresets = useCameraStore(s => s.getCameraPresets);
  const pushToast = useUIStore(s => s.pushToast);
  const [speed, setSpeed] = useState(0.5);
  const [presets, setPresets] = useState<PresetDTO[] | null>(null);
  const [loadingPresets, setLoadingPresets] = useState(false);
  const padRef = useRef<HTMLDivElement | null>(null);
  const knobRef = useRef<HTMLDivElement | null>(null);
  const moveTimer = useRef<ReturnType<typeof setInterval> | null>(null);
  const moveVec = useRef({pan: 0, tilt: 0});

  // sendMove는 현재 조이스틱 벡터를 속도와 함께 전송한다.
  const sendMove = useCallback(() => {
    const {pan, tilt} = moveVec.current;
    if (pan === 0 && tilt === 0) return;
    ptzControl(cameraId, {action: 'move', pan: pan * speed, tilt: tilt * speed});
  }, [cameraId, ptzControl, speed]);

  const stopMove = useCallback(() => {
    if (moveTimer.current) {
      clearInterval(moveTimer.current);
      moveTimer.current = null;
    }
    moveVec.current = {pan: 0, tilt: 0};
    if (knobRef.current) knobRef.current.style.transform = '';
    ptzControl(cameraId, {action: 'stop'});
  }, [cameraId, ptzControl]);

  // 언마운트 시 정지 보장
  useEffect(() => stopMove, [stopMove]);

  function onPointerDown(e: React.PointerEvent) {
    e.stopPropagation();
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
    handlePoint(e);
    moveTimer.current = setInterval(sendMove, SEND_INTERVAL_MS);
  }

  function onPointerMove(e: React.PointerEvent) {
    if (moveTimer.current === null) return;
    e.stopPropagation();
    handlePoint(e);
  }

  function handlePoint(e: React.PointerEvent) {
    const pad = padRef.current;
    if (!pad) return;
    const rect = pad.getBoundingClientRect();
    let dx = (e.clientX - (rect.left + rect.width / 2)) / (rect.width / 2);
    let dy = (e.clientY - (rect.top + rect.height / 2)) / (rect.height / 2);
    dx = Math.max(-1, Math.min(1, dx));
    dy = Math.max(-1, Math.min(1, dy));

    // 데드존
    const mag = Math.hypot(dx, dy);
    if (mag < DEADZONE) {
      dx = 0;
      dy = 0;
    }
    moveVec.current = {pan: dx, tilt: -dy}; // 화면 위쪽 = 카메라 위쪽
    if (knobRef.current) {
      knobRef.current.style.transform = `translate(${dx * 32}px, ${dy * 32}px)`;
    }
    sendMove();
  }

  function loadPresets() {
    setLoadingPresets(true);
    getCameraPresets(cameraId)
      .then(list => setPresets(list))
      .catch(err => pushToast('error', `프리셋 조회 실패: ${String(err)}`))
      .finally(() => setLoadingPresets(false));
  }

  function gotoPreset(token: string) {
    ptzControl(cameraId, {action: 'preset', presetToken: token});
  }

  return (
    <div className="ptz-panel" onClick={e => e.stopPropagation()} aria-label="PTZ 제어">
      <div className="ptz-row">
        <div
          ref={padRef}
          className="ptz-pad"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={stopMove}
          onPointerCancel={stopMove}
          role="application"
          aria-label="PTZ 조이스틱"
        >
          <div className="ptz-cross" aria-hidden="true"/>
          <div ref={knobRef} className="ptz-knob"/>
        </div>
        <div className="ptz-side">
          <div className="field" style={{marginBottom: 8}}>
            <label>속도 {Math.round(speed * 100)}%</label>
            <input
              type="range" min={0.1} max={1} step={0.05} value={speed}
              onChange={e => setSpeed(Number(e.target.value))}
              style={{accentColor: 'var(--amber)'}}
            />
          </div>
          <div className="ptz-zoom">
            <button className="btn" onClick={() => ptzControl(cameraId, {action: 'move', zoom: 0.4 * speed})}
              onPointerUp={() => ptzControl(cameraId, {action: 'stop'})}>줌 +</button>
            <button className="btn" onClick={() => ptzControl(cameraId, {action: 'move', zoom: 0})}
              onPointerUp={() => ptzControl(cameraId, {action: 'stop'})}>줌 −</button>
          </div>
        </div>
      </div>
      <div className="ptz-presets">
        {presets === null ? (
          <button className="btn btn-ghost" onClick={loadPresets} disabled={loadingPresets}>
            {loadingPresets ? '조회 중…' : '프리셋 불러오기'}
          </button>
        ) : presets.length === 0 ? (
          <span className="ptz-hint">저장된 프리셋이 없습니다</span>
        ) : (
          <select onChange={e => { if (e.target.value) gotoPreset(e.target.value); }} value="" aria-label="프리셋 이동">
            <option value="" disabled>프리셋으로 이동…</option>
            {presets.map(p => (
              <option key={p.token} value={p.token}>{p.name || p.token}</option>
            ))}
          </select>
        )}
      </div>
    </div>
  );
}
