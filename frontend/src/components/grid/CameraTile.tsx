// 단일 카메라 타일 — 캔버스 렌더링 + OSD 오버레이 + 통계/스파크라인 표시
import React, {useEffect, useRef, useState} from 'react';
import {CameraDTO} from '../../types/api';
import {StreamState, StreamStats} from '../../types';
import {VideoRenderer} from './VideoRenderer';
import {IconCamera} from '../common/Icons';
import {useStreamStore} from '../../store/streamStore';
import {Sparkline} from '../stats/Sparkline';

interface Props {
  camera: CameraDTO;
  channel: number;
  state: StreamState;
  stats?: StreamStats;
  retryCount?: number; // 자동 재연결 시도 횟수 (0이면 미표시)
  selected: boolean;
  active: boolean; // 레이아웃에 표시되는 타일인지
  onSelect: () => void;
  onDoubleClick?: () => void; // 더블클릭 시 확대/축소
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

export function CameraTile({camera, channel, state, stats, retryCount = 0, selected, active, onSelect, onDoubleClick}: Props) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const rendererRef = useRef<VideoRenderer | null>(null);
  const [glFailed, setGlFailed] = useState(false);
  const history = useStreamStore(s => s.history[camera.id]);
  const resolution = useStreamStore(s => s.resolution[camera.id]);

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
      onDoubleClick={onDoubleClick}
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
          {history && history.length > 1 && (
            <span className="tile-spark">
              <Sparkline samples={history} metric="fps" width={56} height={14}/>
            </span>
          )}
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
          </div>
        )}
        {active && state === 'starting' && (
          <div className="tile-idle"><span>카메라에 연결하는 중… (첫 키프레임 대기)</span></div>
        )}
        {glFailed && <div className="tile-idle">WebGL을 사용할 수 없습니다</div>}
        {active && state === 'error' && (
          <div className="tile-idle tile-error-msg">
            <span>스트림 오류 — 자동 재연결 중{retryCount > 0 ? ` (${retryCount}회)` : ''}…</span>
          </div>
        )}
        {active && state === 'starting' && retryCount > 0 && (
          <div className="tile-idle"><span>재연결 시도 중{retryCount > 1 ? ` (${retryCount}회)` : ''}…</span></div>
        )}
      </div>

      <div className="tile-foot">
        <span className="mono">{host}{resolution ? ` · ${resolution.width}x${resolution.height}` : ''}</span>
        <span className="tile-badges">
          {camera.ptzSupported && <span className="meta-tag ptz">PTZ</span>}
          <span className="meta-tag">{camera.type.toUpperCase()}</span>
        </span>
      </div>
    </div>
  );
}
