// 단일 카메라 타일 — 캔버스 렌더링 + OSD 오버레이 + 통계 표시
import React, {useEffect, useMemo, useRef, useState} from 'react';
import {api} from '../../../wailsjs/go/models';
import {StreamState, StreamStats} from '../../types';
import {VideoRenderer} from './VideoRenderer';
import {IconCamera, IconPlay} from '../common/Icons';
import {useStreamStore} from '../../store/streamStore';

interface FrameMsg {
  type: 'frame';
  cameraId: string;
  frame: VideoFrame;
}

interface Props {
  camera: api.CameraDTO;
  channel: number;
  state: StreamState;
  stats?: StreamStats;
  selected: boolean;
  active: boolean; // 레이아웃에 표시되는 타일인지
  onSelect: () => void;
  onDismissError?: () => void;
}

// subscribeWorkerFrames는 워커의 프레임 메시지 중 해당 카메라의 것만 구독한다.
function subscribeWorkerFrames(
  worker: Worker,
  cameraId: string,
  onFrame: (frame: VideoFrame) => void,
): () => void {
  const handler = (ev: MessageEvent) => {
    const msg = ev.data as FrameMsg;
    if (msg.type === 'frame' && msg.cameraId === cameraId) {
      onFrame(msg.frame);
    }
  };
  worker.addEventListener('message', handler);
  return () => worker.removeEventListener('message', handler);
}

// getWorker는 streamStore가 만든 워커 인스턴스를 얻기 위한 우회 경로다.
// (store 모듈의 싱글턴 워커를 재생성하지 않고 참조)
import {getWorkerInstance} from '../../workers/workerInstance';

export function CameraTile({camera, channel, state, stats, selected, active, onSelect, onDismissError}: Props) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const rendererRef = useRef<VideoRenderer | null>(null);
  const [glFailed, setGlFailed] = useState(false);
  const startStream = useStreamStore(s => s.startStream);

  const worker = useMemo(() => getWorkerInstance(), []);

  useEffect(() => {
    if (!canvasRef.current || !active) return;
    const renderer = new VideoRenderer(canvasRef.current);
    rendererRef.current = renderer;
    if (!renderer.ready) setGlFailed(true);

    const off = subscribeWorkerFrames(worker, camera.id, frame => {
      renderer.draw(frame);
      frame.close();
    });
    return () => {
      off();
      renderer.dispose();
      rendererRef.current = null;
    };
  }, [worker, camera.id, active]);

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
        {active && state === 'starting' && <div className="tile-idle"><span>카메라에 연결하는 중…</span></div>}
        {glFailed && <div className="tile-idle">WebGL을 사용할 수 없습니다</div>}
        {active && state === 'error' && (
          <div className="tile-idle tile-error-msg">
            <span>스트림 오류</span>
            {onDismissError && (
              <button className="btn" onClick={e => { e.stopPropagation(); onDismissError(); }}>확인</button>
            )}
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
