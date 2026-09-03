// 동시 접속 제한 안내 오버레이 (60초 카운트다운)
import {useEffect, useState} from 'react';
import {useStreamStore} from '../../store/streamStore';

export function ClientLimitOverlay() {
  const rejected = useStreamStore(s => s.rejected);
  const retryAt = useStreamStore(s => s.retryAt);
  const [remaining, setRemaining] = useState(0);

  useEffect(() => {
    if (!rejected || !retryAt) return;

    const updateRemaining = () => {
      const now = Date.now();
      const diff = retryAt - now;
      setRemaining(Math.max(0, Math.ceil(diff / 1000)));
    };

    updateRemaining();
    const interval = setInterval(updateRemaining, 1000);
    return () => clearInterval(interval);
  }, [rejected, retryAt]);

  if (!rejected) return null;

  return (
    <div className="client-limit-overlay">
      <div className="client-limit-content">
        <div className="client-limit-title">접속자가 많아</div>
        <div className="client-limit-subtitle">지금은 지원 할 수 없습니다.</div>
        <div className="client-limit-message">잠시만 기다려 주십시오</div>
        <div className="client-limit-countdown">{remaining}초 후 재시도</div>
      </div>
    </div>
  );
}
