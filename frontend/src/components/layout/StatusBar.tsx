// 하단 상태바: 백엔드 연결 정보 표시 (Phase 4에서 실시간 통계로 확장)
export function StatusBar() {
  return (
    <footer className="statusbar">
      <span>WS <b>127.0.0.1:8080</b></span>
      <span className="live">● 백엔드 연결됨</span>
      <span>스트림 <b>0</b>/0</span>
      <span style={{marginLeft: 'auto'}}>webnvr v0.1</span>
    </footer>
  );
}
