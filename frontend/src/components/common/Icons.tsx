// 인라인 SVG 아이콘 컴포넌트 모음 (외부 아이콘 라이브러리 없이 사용)
import React from 'react';

interface IconProps {
  size?: number;
}

function svg(path: React.ReactNode, viewBox = '0 0 24 24') {
  return function Icon({size = 16}: IconProps) {
    return (
      <svg width={size} height={size} viewBox={viewBox} fill="none"
        stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {path}
      </svg>
    );
  };
}

export const IconGrid = svg(<>
  <rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/>
  <rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/>
</>);

export const IconCamera = svg(<>
  <rect x="2" y="6" width="13" height="12" rx="2"/>
  <path d="M15 10.5 21 7v10l-6-3.5"/>
</>);

export const IconPlus = svg(<><path d="M12 5v14M5 12h14"/></>);

export const IconSearch = svg(<><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></>);

export const IconEdit = svg(<>
  <path d="M17 3.5 20.5 7 8.5 19H5v-3.5L17 3.5Z"/>
</>);

export const IconTrash = svg(<>
  <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>
</>);

export const IconClose = svg(<><path d="M6 6l12 12M18 6 6 18"/></>);

export const IconCheck = svg(<><path d="m4 12.5 5 5L20 6.5"/></>);

export const IconAlert = svg(<>
  <path d="M12 3 2.5 20h19L12 3Z"/><path d="M12 10v4.5"/><path d="M12 17.6v.1"/>
</>);

export const IconRefresh = svg(<>
  <path d="M20 11a8 8 0 1 0-2.3 6.3M20 5v6h-6"/>
</>);

export const IconGrip = svg(<>
  <circle cx="9" cy="6" r="1"/><circle cx="15" cy="6" r="1"/>
  <circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/>
  <circle cx="9" cy="18" r="1"/><circle cx="15" cy="18" r="1"/>
</>);
