// 백엔드 서비스 상태를 표시하는 임시 랜딩 컴포넌트 (Phase 3에서 카메라 관리 UI로 교체)
import {ListCameras} from "../wailsjs/go/api/CameraService";
import {useState} from 'react';
import './App.css';

function App() {
    const [status, setStatus] = useState('');

    async function checkBackend() {
        try {
            const cams = await ListCameras();
            setStatus(`백엔드 연결 정상 — 등록된 카메라 ${cams.length}대`);
        } catch (e) {
            setStatus(`백엔드 호출 실패: ${e}`);
        }
    }

    return (
        <div id="App">
            <h1>webnvr</h1>
            <p>CCTV 관제 시스템 (개발 중)</p>
            <button onClick={checkBackend}>백엔드 연결 테스트</button>
            <div className="result">{status}</div>
        </div>
    );
}

export default App;
