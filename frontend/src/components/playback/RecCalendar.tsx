// 다시보기용 월간 달력 — 녹화된 날짜를 마커로 표시하고 클릭으로 날짜를 선택한다
import React, {useMemo} from 'react';
import {DayCount} from '../../services/recordings';
import {IconChevronLeft, IconChevronRight} from '../common/Icons';

const WEEKDAYS = ['일', '월', '화', '수', '목', '금', '토'];

function dayKey(y: number, m: number, d: number): string {
  return `${y}-${String(m + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`;
}

function fmtCompact(b: number): string {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)}G`;
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(0)}M`;
  return `${(b / 1024).toFixed(0)}K`;
}

interface Props {
  monthStart: number; // 선택 월 1일 00:00 epoch ms
  selectedDay: number; // 선택된 날짜 00:00 epoch ms
  days: Map<string, DayCount>; // 'YYYY-MM-DD' → 요약
  onPrevMonth: () => void;
  onNextMonth: () => void;
  onPickDay: (dayStartMs: number) => void;
}

// RecCalendar는 월간 그리드에 녹화일(파란 점+용량)을 표시한다.
export function RecCalendar({monthStart, selectedDay, days, onPrevMonth, onNextMonth, onPickDay}: Props) {
  const cells = useMemo(() => {
    const first = new Date(monthStart);
    const y = first.getFullYear();
    const m = first.getMonth();
    const daysInMonth = new Date(y, m + 1, 0).getDate();
    const lead = first.getDay(); // 1일의 요일 (0=일)
    const out: {key: string; day: number; dayStartMs: number}[] = [];
    for (let i = 0; i < lead; i++) out.push(null as never);
    for (let d = 1; d <= daysInMonth; d++) {
      const dt = new Date(y, m, d);
      out.push({key: dayKey(y, m, d), day: d, dayStartMs: dt.getTime()});
    }
    return out;
  }, [monthStart]);

  const selKey = useMemo(() => {
    const d = new Date(selectedDay);
    return dayKey(d.getFullYear(), d.getMonth(), d.getDate());
  }, [selectedDay]);

  const monthLabel = useMemo(() => {
    const d = new Date(monthStart);
    return `${d.getFullYear()}년 ${d.getMonth() + 1}월`;
  }, [monthStart]);

  return (
    <div className="rec-cal" aria-label="녹화 달력">
      <div className="rec-cal-head">
        <button type="button" className="btn" aria-label="이전 달" onClick={onPrevMonth}>
          <IconChevronLeft size={14}/>
        </button>
        <span className="rec-cal-month">{monthLabel}</span>
        <button type="button" className="btn" aria-label="다음 달" onClick={onNextMonth}>
          <IconChevronRight size={14}/>
        </button>
      </div>
      <div className="rec-cal-grid rec-cal-week">
        {WEEKDAYS.map((w, i) => (
          <span key={w} className={`rec-cal-wd ${i === 0 ? 'sun' : i === 6 ? 'sat' : ''}`}>{w}</span>
        ))}
      </div>
      <div className="rec-cal-grid">
        {cells.map((c, i) => {
          if (!c) return <span key={`pad-${i}`} className="rec-cal-cell pad"/>;
          const info = days.get(c.key);
          const selected = c.key === selKey;
          return (
            <button key={c.key}
              className={`rec-cal-cell ${info ? 'has-rec' : ''} ${selected ? 'selected' : ''}`}
              title={info ? `녹화 ${info.cameras}대 · ${fmtCompact(info.bytes)}` : '녹화 없음'}
              onClick={() => onPickDay(c.dayStartMs)}>
              <span className="rec-cal-day">{c.day}</span>
              {info && <span className="rec-cal-badge" data-cams={info.cameras}>{fmtCompact(info.bytes)}</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}
