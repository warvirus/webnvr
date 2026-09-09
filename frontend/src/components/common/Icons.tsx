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

export const IconSettings = svg(<>
  <circle cx="12" cy="12" r="3"/>
  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.01a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.01a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.01a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"/>
</>);

export const IconFull = svg(<>
  <path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M16 3h3a2 2 0 0 1 2 2v3"/>
  <path d="M8 21H5a2 2 0 0 1-2-2v-3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/>
</>);

export const IconPlay = svg(<><path d="M7 4.5v15l13-7.5-13-7.5Z"/></>);
export const IconRewind = svg(<><path d="M3 5v14"/><path d="m21 5-10 7 10 7V5Z"/></>);

export const IconStop = svg(<><rect x="6" y="6" width="12" height="12" rx="1.5"/></>);

export const IconChevronLeft = svg(<><path d="M15 6l-6 6 6 6"/></>);

export const IconChevronRight = svg(<><path d="M9 6l6 6-6 6"/></>);
