// 단일 카메라 타일 — 캔버스 렌더링 + OSD 오버레이 + 통계 표시
import React, {useEffect, useMemo, useRef, useState} from 'react';
import {CameraDTO} from '../../types/api';
import {StreamState, StreamStats} from '../../types';
import {VideoRenderer} from './VideoRenderer';
import {IconCamera, IconPlay} from '../common/Icons';
import {useStreamStore} from '../../store/streamStore';

interface Props {
  camera: CameraDTO;
  channel: number;
  state: StreamState;
  stats?: StreamStats;
  selected: boolean;
  active: boolean; // 레이아웃에 표시되는 타일인지
  onSelect: () => void;
  onDismissError?: () => void;
}

// subscribeFrames는 디코더(메인 스레드)의 프레임 이벤트 중 해당 카메라의 것만 구독한다.
// v1.1: Worker가 제거되어 디코더가 메인 스레드에서 CustomEvent로 프레임을 발행한다.
function subscribeFrames(
  cameraId: string,
  onFrame: (frame: VideoFrame) => void,
): () => void {
  const handler = (ev: Event) => {
    const detail = (ev as CustomEvent<{cameraId: string; frame: VideoFrame}>).detail;
    if (detail.cameraId === cameraId) {
      onFrame(detail.frame);
    }
  };
  window.addEventListener('webnvr-frame', handler);
  return () => window.removeEventListener('webnvr-frame', handler);
}

export function CameraTile({camera, channel, state, stats, selected, active, onSelect, onDismissError}: Props) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const rendererRef = useRef<VideoRenderer | null>(null);
  const [glFailed, setGlFailed] = useState(false);
  const startStream = useStreamStore(s => s.startStream);

  useEffect(() => {
    if (!canvasRef.current || !active) return;
    const renderer = new VideoRenderer(canvasRef.current);
    rendererRef.current = renderer;
    if (!renderer.ready) setGlFailed(true);

    const off = subscribeFrames(camera.id, frame => {
      renderer.draw(frame);
      frame.close();
    });
    return () => {
      off();
      renderer.dispose();
      rendererRef.current = null;
    };
  }, [camera.id, active]);

  const host = camera.type === 'onvif' ? camera.xaddr : camera.streamUrl?.replace(/^\w+:\/\//, '').split('/')[0];

  return (
    <div
      className={`tile ${selected ? 'selected' : ''} ${state === 'error' ? 'tile-error' : ''}`}
      onClick={onSelect}
      role="button"
      tabIndex={0}
      onKeyDown={e => { if (e.key === 'Enter') onSelect(); }}
    >
      <i className="osd-corner tl" aria-hidden="true"/><i className="osd-corner tr" aria-hidden="true"/>
      <i className="osd-corner bl" aria-hidden="true"/><i className="osd-corner br" aria-hidden="true"/>

      <div className="tile-head">
        <span className="plate">CH {String(channel).padStart(2, '0')}</span>
        <span className="tile-name">{camera.name}</span>
        <span className="osd-status">
          {state === 'streaming' && stats ? (
            <span className="tile-stats">{stats.fps} fps · {stats.kbps} kbps{stats.drops > 0 ? ` · 드롭 ${stats.drops}` : ''}</span>
          ) : state === 'starting' ? '연결 중…' : state === 'error' ? '오류' : '대기'}
        </span>
      </div>

      <div className="tile-canvas-wrap">
        <canvas ref={canvasRef} className="tile-canvas"/>
        {!active && <div className="tile-offline">레이아웃에서 제외됨</div>}
        {active && state === 'idle' && (
          <div className="tile-idle">
            <IconCamera size={22}/>
            <span>스트림 대기 중</span>
            <button className="btn" onClick={e => { e.stopPropagation(); startStream(camera.id); }}>
              <IconPlay size={12}/> 시작
            </button>
          </div>
        )}
        {active && state === 'starting' && (
          <div className="tile-idle"><span>카메라에 연결하는 중… (첫 키프레임 대기)</span></div>
        )}
        {glFailed && <div className="tile-idle">WebGL을 사용할 수 없습니다</div>}
        {active && state === 'error' && (
          <div className="tile-idle tile-error-msg">
            <span>스트림 오류</span>
            <div style={{display: 'flex', gap: 6}}>
              <button className="btn" onClick={e => { e.stopPropagation(); startStream(camera.id); }}>재시도</button>
              {onDismissError && (
                <button className="btn btn-ghost" onClick={e => { e.stopPropagation(); onDismissError(); }}>확인</button>
              )}
            </div>
          </div>
        )}
      </div>

      <div className="tile-foot">
        <span className="mono">{host}</span>
        <span className="tile-badges">
          {camera.ptzSupported && <span className="meta-tag ptz">PTZ</span>}
          <span className="meta-tag">{camera.type.toUpperCase()}</span>
        </span>
      </div>
    </div>
  );
}
