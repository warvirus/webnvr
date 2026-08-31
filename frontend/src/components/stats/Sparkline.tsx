// kbps 시계열을 SVG 스파크라인으로 그리는 경량 차트 (외부 차트 라이브러리 없이)
import React from 'react';
import {StatSample} from '../../store/streamStore';

interface Props {
  samples: StatSample[];
  width?: number;
  height?: number;
  color?: string;
}

// Sparkline은 kbps 추이를 폴리라인으로 그린다.
export function Sparkline({samples, width = 220, height = 36, color = 'var(--amber)'}: Props) {
  if (samples.length < 2) {
    return <svg width={width} height={height} aria-hidden="true"/>;
  }
  const maxKbps = Math.max(...samples.map(s => s.kbps), 1);
  const stepX = width / (samples.length - 1);
  const points = samples
    .map((s, i) => `${(i * stepX).toFixed(1)},${(height - (s.kbps / maxKbps) * (height - 4) - 2).toFixed(1)}`)
    .join(' ');

  return (
    <svg width={width} height={height} role="img" aria-label="비트레이트 추이">
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
