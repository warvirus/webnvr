// 하단 상태바 — 실제 WS 연결 상태와 스트리밍 대수를 streamStore에서 구독해 표시한다
import {useStreamStore} from '../../store/streamStore';
import {backendHost} from '../../services/backend';

export function StatusBar() {
  const connected = useStreamStore(s => s.connected);
  const reconnectAt = useStreamStore(s => s.reconnectAt);
  const desired = useStreamStore(s => s.desired);
  const states = useStreamStore(s => s.states);
  const streaming = Object.values(states).filter(st => st === 'streaming').length;
  const wanted = Object.values(desired).filter(Boolean).length;
  const stateText = connected ? '백엔드 연결됨' : reconnectAt ? '재연결 대기 중' : '연결 안 됨';
  return (
    <footer className="statusbar">
      <span>WS <b>{backendHost()}</b></span>
      <span className={connected ? 'live' : 'down'}>● {stateText}</span>
      <span>스트림 <b>{streaming}</b>/{wanted}</span>
      <span style={{marginLeft: 'auto'}}>webnvr v0.1</span>
    </footer>
  );
}
