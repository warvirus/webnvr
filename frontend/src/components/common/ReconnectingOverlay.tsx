// 백엔드 연결이 끊긴 동안 "접속 중입니다" + 다음 재시도까지 카운트다운을 보여주는 오버레이
import {useEffect, useState} from 'react';
import {useStreamStore} from '../../store/streamStore';

// 짧은 재시작(2~3초)이나 앱 시작 직후의 순간적 미연결에 오버레이가 깜빡이지 않도록 지연한다.
const GRACE_MS = 1200;

export function ReconnectingOverlay() {
  const connected = useStreamStore(s => s.connected);
  const rejected = useStreamStore(s => s.rejected);
  const reconnectAt = useStreamStore(s => s.reconnectAt);
  const [visible, setVisible] = useState(false);
  const [remaining, setRemaining] = useState(0);

  // 끊긴 상태가 GRACE_MS 이상 지속될 때만 노출한다. (동시 접속 제한은 ClientLimitOverlay가 담당)
  useEffect(() => {
    if (connected || rejected) {
      setVisible(false);
      return;
    }
    const t = setTimeout(() => setVisible(true), GRACE_MS);
    return () => clearTimeout(t);
  }, [connected, rejected]);

  // 다음 재시도까지 남은 초를 계산한다.
  useEffect(() => {
    if (!visible || !reconnectAt) {
      setRemaining(0);
      return;
    }
    const tick = () => setRemaining(Math.max(0, Math.ceil((reconnectAt - Date.now()) / 1000)));
    tick();
    const id = setInterval(tick, 500);
    return () => clearInterval(id);
  }, [visible, reconnectAt]);

  if (!visible) return null;

  return (
    <div className="client-limit-overlay">
      <div className="client-limit-content">
        <div className="client-limit-title">접속 중입니다</div>
        <div className="client-limit-subtitle">백엔드 서버에 연결하고 있습니다</div>
        <div className="client-limit-message">연결되면 자동으로 다시 시작됩니다</div>
        <div className="client-limit-countdown">
          {remaining > 0 ? `${remaining}초 후 재시도` : '재시도 중…'}
        </div>
      </div>
    </div>
  );
}
