// 표시 로케일 상수 — 국제화(D37) 준비를 위한 단일 출처.
// 화면 문장은 UI 카탈로그로 이관 예정이며, 현재는 날짜/시간 포맷과 요일명의 로케일만 통일한다.
export const LOCALE = 'ko-KR';

// WEEKDAY_LABELS는 요일 표시 라벨이다. (일요일 시작, LOCALE 기준)
export const WEEKDAY_LABELS = ['일', '월', '화', '수', '목', '금', '토'] as const;
