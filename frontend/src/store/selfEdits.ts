// 이 클라이언트가 방금 만든 변경을 표시해, 되돌아온 cameras_changed 에코로
// 자기 자신에게 "설정 변경" 배지를 띄우지 않게 한다.
const TTL_MS = 5_000;
const marks = new Map<string, number>(); // key → 만료 시각(epoch ms)

function key(reason: string, id?: string): string {
  return reason === 'updated' && id ? `updated:${id}` : reason;
}

// markSelfEdit는 mutation 요청 직전에 호출한다 (에코가 HTTP 응답보다 먼저 와도 흡수).
export function markSelfEdit(reason: string, id?: string): void {
  marks.set(key(reason, id), Date.now() + TTL_MS);
}

// consumeSelfEdit는 해당 변경이 이 클라이언트의 에코이면 true를 반환하고 표시를 소비한다.
export function consumeSelfEdit(reason: string | undefined, id?: string): boolean {
  if (!reason) return false;
  const k = key(reason, id);
  const exp = marks.get(k);
  if (exp !== undefined && exp > Date.now()) {
    marks.delete(k);
    return true;
  }
  if (exp !== undefined) marks.delete(k); // 만료된 잔재 정리
  return false;
}
