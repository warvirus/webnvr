// 시계열 통계를 SVG 스파크라인으로 그리는 경량 차트 (외부 차트 라이브러리 없이)
import React from 'react';
import {StatSample} from '../../types/stream';

interface Props {
  samples: StatSample[];
  width?: number;
  height?: number;
  color?: string;
  metric?: 'fps' | 'kbps';
}

// Sparkline은 지정 지표(fps 또는 kbps)의 추이를 폴리라인으로 그린다.
export function Sparkline({samples, width = 220, height = 36, color = 'var(--amber)', metric = 'kbps'}: Props) {
  if (samples.length < 2) {
    return <svg width={width} height={height} aria-hidden="true"/>;
  }
  const maxVal = Math.max(...samples.map(s => s[metric]), 1);
  const stepX = width / (samples.length - 1);
  const points = samples
    .map((s, i) => `${(i * stepX).toFixed(1)},${(height - (s[metric] / maxVal) * (height - 4) - 2).toFixed(1)}`)
    .join(' ');

  return (
    <svg width={width} height={height} role="img" aria-label={`${metric} 추이`}>
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
