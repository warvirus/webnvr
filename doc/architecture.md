# CCTV 관제 시스템 - 아키텍처 및 구현 계획서

## 1. 프로젝트 개요

| 항목 | 내용 |
|------|------|
| **프로젝트명** | cctv-control |
| **목적** | ONVIF 카메라 자동 검색, 멀티뷰 실시간 스트리밍, PTZ 제어 |
| **아키텍처** | Wails v2 셸 + Go 백엔드(REST API + WS 중계 서버) + React 클라이언트 (v1.1: 프론트는 서버 순수 클라이언트) |
| **스트림 처리** | 백엔드 RTSP/RTP/RTMP 수신 → WebSocket → 프론트엔드 WebCodecs + WebGL 디코딩/렌더링 |
| **설정 관리** | JSON 파일 (`./config/`, 서버 소유) → UI는 API로 조회/수정 → 추후 SQLite 마이그레이션 |
| **대상 OS** | macOS (우선), Windows, Linux |
| **인증** | Phase 6+ 연기 (현재 인증 없음) |

---

## 2. 기술 스택 확정

| 영역 | 선택 | 버전/비고 |
|------|------|-----------|
| **GUI 프레임워크** | Wails v2 | Go + WebView2/WebKit |
| **프론트엔드** | React 18 + TypeScript + Vite | Zustand, WebCodecs, WebGL |
| **상태 관리** | Zustand | persist, devtools 미들웨어 |
| **비디오 렌더링** | WebGL (YUV→RGB 셰이더) | OffscreenCanvas + Web Workers |
| **디코딩 (FE)** | WebCodecs API | Hardware: "prefer-hardware", ffmpeg.wasm 폴백 |
| **RTSP 클라이언트 (BE)** | vdk/rtspv2 | 순수 Go, CGO 없음 |
| **카메라 검색** | github.com/use-go/onvif/ws-discovery | 기존 의존성 활용 (ONVIF만) |
| **ONVIF 클라이언트** | github.com/use-go/onvif | 프로필, 스트림 URI, PTZ (SDK 사용) |
| **설정 파일** | JSON (`./config/app.json`, `./config/cameras.json`) | fsnotify 핫 리로드 |
| **비밀번호 암호화** | AES-GCM + 환경변수 키 조합 | 카메라별 고유 키 파생 (PBKDF2) |
| **WebSocket** | gorilla/websocket | JSON 프로토콜, rtp_batch 배치 전송 |
| **HTTP REST API** | net/http (`/api/*`, ws_port 공유) | v1.1: 카메라 관리는 바인딩 대신 fetch로 수행 |
| **프론트→백엔드 통신** | fetch(REST) + WebSocket | Wails 바인딩 미사용 — 브라우저/네이티브 동일 경로 |
| **로깅** | zerolog / slog | 구조화 로그 |

---

## 3. 카메라 타입 분류

| 카메라 타입 | 발견 방식 | 스트림 URI | 프로필 | PTZ | 설정 필수 정보 |
|------------|-----------|------------|--------|-----|----------------|
| **ONVIF 지원** | WS-Discovery + 수동 | `GetStreamURI` 호출 | O (다중 선택) | O | IP, 포트, ID/PW |
| **RTSP 직접** | 수동 입력만 | 사용자 직접 입력 | X (단일 스트림) | X | RTSP URL |
| **RTP 직접** | 수동 입력만 | 사용자 직접 입력 | X | X | RTP URL |
| **RTMP 직접** | 수동 입력만 | 사용자 직접 입력 | X | X | RTMP URL |

---

## 4. 프로젝트 구조

```
cctv-control/
├── backend/
│   ├── cmd/main.go
│   ├── internal/
│   │   ├── config/           # 설정 관리 (JSON + 핫 리로드)
│   │   │   ├── config.go
│   │   │   ├── loader.go
│   │   │   ├── watcher.go
│   │   │   ├── encryption.go
│   │   │   ├── migration.go
│   │   │   └── validator.go
│   │   ├── camera/           # 카메라 관리
│   │   │   ├── manager.go
│   │   │   ├── discovery.go
│   │   │   ├── store.go
│   │   │   ├── types.go
│   │   │   └── stream_test.go
│   │   ├── onvif/            # ONVIF 클라이언트
│   │   │   ├── client.go
│   │   │   ├── profiles.go
│   │   │   ├── ptz.go
│   │   │   ├── stream_uri.go
│   │   │   └── discovery.go
│   │   ├── stream/           # 스트림 처리 (gortsplib 기반)
│   │   │   ├── hub.go         #   구독자/백프레셔 (버퍼 512 패킷)
│   │   │   ├── rtsp_client.go #   RTSP 수신 + 코덱 메타데이터
│   │   │   └── types.go
│   │   ├── api/              # 서비스 계층 + HTTP/WS 어댑터
│   │   │   ├── app.go         #   서비스 조립
│   │   │   ├── camera.go      #   카메라 서비스 로직
│   │   │   ├── stream.go      #   스트림 서비스 로직
│   │   │   └── httpapi.go     #   /api/* REST 핸들러 (v1.1)
│   │   └── ws/               # WebSocket 서버
│   │       ├── server.go
│   │       ├── handler.go     #   배치 펌프 포함
│   │       └── protocol.go
│   ├── config/               # 런타임 생성
│   │   ├── app.json
│   │   └── cameras.json
│   └── go.mod / go.sum
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   │   ├── camera/
│   │   │   │   ├── CameraList.tsx
│   │   │   │   ├── CameraModal.tsx
│   │   │   │   ├── CameraForm.tsx
│   │   │   │   ├── DiscoveryPanel.tsx
│   │   │   │   ├── ProfileSelector.tsx
│   │   │   │   └── ConnectionTest.tsx
│   │   │   ├── grid/
│   │   │   │   ├── CameraGrid.tsx
│   │   │   │   ├── CameraTile.tsx
│   │   │   │   ├── VideoCanvas.tsx
│   │   │   │   └── PTZControl.tsx
│   │   │   ├── layout/
│   │   │   │   ├── Toolbar.tsx
│   │   │   │   ├── Sidebar.tsx
│   │   │   │   └── StatusBar.tsx
│   │   │   └── common/
│   │   ├── hooks/            # (v1.1: 대부분 store/컴포넌트로 흡수됨)
│   │   ├── store/
│   │   │   ├── cameraStore.ts   # REST api.ts 기반
│   │   │   ├── streamStore.ts   # WS + 디코더 워커 연동
│   │   │   └── uiStore.ts
│   │   ├── services/
│   │   │   ├── api.ts           # HTTP REST 클라이언트 (v1.1 — 바인딩 대체)
│   │   │   └── ws.ts            # WS 클라이언트 (재연결 지수백오프)
│   │   ├── workers/
│   │   │   ├── decoder.worker.ts  # RFC 6184/7798 + Jitter + WebCodecs
│   │   │   └── workerInstance.ts
│   │   ├── components/grid/
│   │   │   └── VideoRenderer.ts   # WebGL VideoFrame 직접 업로드
│   │   ├── pages/
│   │   │   ├── CameraManagementPage.tsx
│   │   │   └── MonitoringPage.tsx
│   │   ├── types/               # index.ts (WS/통계) + api.ts (REST DTO)
│   │   ├── app.css              # 디자인 토큰 (v1.0 OSD 야간 테마)
│   │   ├── App.tsx
│   │   └── main.tsx
│   └── package.json / vite.config.ts
├── cmd/
│   ├── streamtest/            # RTSP 수신 진단 CLI
│   ├── wstest/                # WS 파이프라인 검증 CLI (시퀀스 갭 측정)
│   ├── rtspmock/              # 모의 RTSP 서버 (STAP-A/FU-A 패턴)
│   └── rtspanalyze/           # NALU 구조 분석 CLI
├── wails.json
└── build/
```

---

## 5. 백엔드 API 계약 (HTTP REST + WebSocket)

> **v1.1 아키텍처 개정 (2026-08-31)**: 프론트엔드는 백엔드의 **순수 클라이언트**다.
> 카메라 설정/관리는 HTTP REST API(`:8080/api/*`)로, 영상은 WS(`:8080/ws`)로 모두
> 서버 경유 — Wails 바인딩은 제거되며 Wails는 창(셸) 역할만 한다.
> 이로써 네이티브 앱과 DevServer 브라우저가 동일한 경로로 동작한다(브라우저 패리티).

### 5.1 HTTP REST API (`http://127.0.0.1:{ws_port}/api/*`)

바인딩용 서비스(CameraService/StreamService)의 로직을 HTTP 핸들러가 재사용한다.
응답 본문은 기존 DTO(CameraDTO 등)와 동일한 JSON 스키마. 모든 오리진 허용(CORS),
서버는 127.0.0.1에만 바인딩(Phase 6 인증 전까지 — doc §10 참조).

| Method | Path | 용도 | 비고 |
|--------|------|------|------|
| GET | `/api/health` | 서버 생존 확인 | `{ok: true, version}` |
| GET | `/api/cameras` | 카메라 목록 | layout_order 순 |
| GET | `/api/cameras/{id}` | 카메라 단일 조회 | |
| POST | `/api/cameras` | 카메라 추가 | 본문: CreateCameraRequest |
| PUT | `/api/cameras/{id}` | 카메라 수정 | 본문: UpdateCameraRequest (비밀번호 입력 시 재암호화) |
| DELETE | `/api/cameras/{id}` | 카메라 삭제 | |
| POST | `/api/cameras/reorder` | 표시 순서 변경 | 본문: `{ids: string[]}` |
| POST | `/api/cameras/discover` | ONVIF WS-Discovery | `DiscoveredCamera[]` (인터페이스별 ~1초 순차 프로브) |
| POST | `/api/cameras/test-onvif` | ONVIF 연결 테스트 | 미등록 카메라용 — 본문에 자격증명 전달 |
| POST | `/api/cameras/test-direct` | 직접 스트림 URL 검증 | rtsp/rtmp: TCP 도달성, rtp: 형식만 |
| GET | `/api/cameras/{id}/profiles` | 미디어 프로필 조회 | **저장 자격증명 사용** (프로필 미지정 시 첫 프로필로 URI 조회에 사용) |
| GET | `/api/cameras/{id}/presets` | PTZ 프리셋 목록 | **저장 자격증명 사용** — 자격증명 미노출 |
| GET | `/api/cameras/{id}/stream-uri` | RTSP URI 조회 | **저장 자격증명 사용** |

자격증명 규칙: 미등록 카메라의 연결 테스트/프로필 조회만 본문으로 자격증명을 받고,
등록된 카메라에 대한 조회는 항상 카메라 ID + 서버 측 복호화(PasswordOf)를 사용한다.

원칙: 핸들러는 서비스 계층(CameraService/StreamService)의 위임자일 것 — 로직 중복 금지.

### 5.2 스트림 제어 (WebSocket 메시지로 수행)

스트림 시작/정지/PTZ는 REST가 아니라 WS 메시지로 수행한다(§5.3).
이유: 스트림 수신과 제어가 동일 연결에서 순서 보장되며, 구독(subscribe)과
시작(start)의 결합 동작을 하나의 채널에서 관리하기 위함.

### 5.3 WebSocket 프로토콜 (JSON)

```typescript
// Client → Server
type ClientMsg =
  | { type: "start_stream"; cameraId: string }
  | { type: "stop_stream"; cameraId: string }
  | { type: "start_all_streams" }
  | { type: "stop_all_streams" }
  | { type: "ptz"; cameraId: string; command: PTZCommand }
  | { type: "request_keyframe"; cameraId: string }  // 미구현(Phase 5 검토)
  | { type: "subscribe"; cameraId: string }
  | { type: "unsubscribe"; cameraId: string }
  | { type: "ping" }

// Server → Client
type ServerMsg =
  | { type: "stream_started"; cameraId: string; codec: string; ssrc: number; clockRate: number; sps: string; pps: string; vps?: string; width: number; height: number }
  | { type: "rtp_packet"; cameraId: string; payload: string; timestamp: number; marker: boolean; sequence: number }
  | { type: "rtp_batch"; cameraId: string; codec: string; packets: { p: string; ts: number; m?: boolean; sq: number }[] }  // v1.1: 고비트레이트 배치 전송
  | { type: "stream_stopped"; cameraId: string; reason: string }
  | { type: "stream_error"; cameraId: string; error: string }
  | { type: "camera_discovered"; camera: DiscoveredCamera }
  | { type: "camera_status"; cameraId: string; status: CameraStatus }
  | { type: "stats"; cameraId: string; stats: StreamStats }
  | { type: "pong" }
  | { type: "config_updated"; section: "app" | "cameras"; data: any }
```

**v1.1 구현 노트** (실제 구현 기준 확정 사항):
- `stream_started`는 코덱 메타데이터를 전달하며, 프론트는 **첫 프레임 디코딩 성공 시점부터** 화면으로 전환한다(중간 GOP 참여 대기는 정상 상태).
- `rtp_batch`: WS 펌프가 채널에 대기 중인 패킷을 즉시 흡수해 최대 128개씩 묶어 전송한다(축약 키 `p/ts/m/sq`). 단일 `rtp_packet`은 하위 호환용으로 유지.
- 허브 구독자 채널 버퍼는 **512 패킷**(≈30프레임) — 패킷 단위 30은 1프레임 버스트에도 넘쳐 IDR 머리 프래그먼트가 유실되는 결함이 있었다(2026-08-31 수정).
- PTZ 프리셋 이동은 WS `ptz {action:'preset', presetToken}`으로 하고, 프리셋 목록은 HTTP `GET /api/cameras/{id}/presets`로 조회한다(저장 자격증명 사용).

---

## 6. 프론트엔드 상태 관리 (Zustand)

```typescript
// store/cameraStore.ts
interface CameraStore {
  cameras: CameraDTO[];
  discoveredCameras: DiscoveredCamera[];
  isDiscovering: boolean;
  
  fetchCameras: () => Promise<void>;
  discoverCameras: () => Promise<void>;
  addCamera: (req: CreateCameraRequest) => Promise<CameraDTO>;
  updateCamera: (id: string, req: UpdateCameraRequest) => Promise<void>;
  deleteCamera: (id: string) => Promise<void>;
  reorderCameras: (cameraIds: string[]) => Promise<void>;
  testONVIFConnection: (req: TestONVIFRequest) => Promise<TestONVIFResponse>;
  testDirectStream: (req: TestDirectStreamRequest) => Promise<TestDirectStreamResponse>;
  getONVIFProfiles: (req: GetProfilesRequest) => Promise<ProfileDTO[]>;
}

// store/streamStore.ts
interface StreamStore {
  streamingIds: Set<string>;
  stats: Map<string, StreamStats>;
  
  startStream: (cameraId: string) => Promise<void>;
  stopStream: (cameraId: string) => Promise<void>;
  startAllStreams: () => Promise<void>;
  stopAllStreams: () => Promise<void>;
  ptzControl: (cameraId: string, cmd: PTZCommand) => Promise<void>;
}

// store/uiStore.ts
interface UIStore {
  currentPage: 'management' | 'monitoring';
  sidebarOpen: boolean;
  ptzPanelOpen: string | null;
  layout: LayoutConfig;
  toasts: Toast[];
}
```

---

## 7. 구현 단계 (실제 진행 기준, 2026-08-31 갱신)

### Phase 1: 백엔드 기반 + Camera API ✅ 완료
| # | 태스크 | 결과 |
|---|--------|------|
| 1.1 | Wails 골격 생성 (react-ts) | commit c4cb407 |
| 1.2 | JSON 설정 로더/핫리로드/AES-GCM+PBKDF2 암호화/검증 | ddd83da |
| 1.3 | CameraManager + JSONCameraStore + 타입별 분기 | 9104edb |
| 1.4 | WS-Discovery + ONVIF 클라이언트 래퍼 | 2e43425 |
| 1.5 | Camera API (바인딩 → v1.1에서 HTTP로 이행) | 77d9f3f |

### Phase 2: 스트림 코어 + WebSocket ✅ 완료
| # | 태스크 | 결과 |
|---|--------|------|
| 2.1 | RTSP 클라이언트 — gortsplib/v4 v4.16.2 (pion/rtsp는 모듈 소멸) | 4ba94b6 |
| 2.2 | RTP 디페이저 — gortsplib 내장 rtph264/rtph265 활용 | 동일 |
| 2.3 | Stream Hub (구독자, 버퍼 512패킷≈30프레임, Late 드롭) | 동일 |
| 2.4 | WebSocket 서버 (JSON, 하트비트) | d3113ac |
| 2.5 | Stream API (WS Controller + Wails 바인딩 → v1.1에서 WS 전용) | 601c949 |
| 2.6 | 실기 카메라 검증 | H.264 1080p, 10초/8,708패킷/8.5Mbps 수신 성공 |

### Phase 3: 프론트엔드 카메라 관리 UI ✅ 완료
| # | 태스크 | 결과 |
|---|--------|------|
| 3.1 | cameraStore(낙관적 업데이트/롤백) + uiStore | ae283a5 |
| 3.2~3.6 | 등록부 UI(OSD 테마)/검색/폼/프로필/연결 테스트 | 동일 |
| 3.7 | 드래그앤드롭 순서 변경 (@dnd-kit 핸들) | 동일 |

### Phase 4: 프론트엔드 모니터링 + 디코딩 ✅ 완료 (안정화 포함)
| # | 태스크 | 결과 |
|---|--------|------|
| 4.1 | MonitoringPage + CameraGrid(1×1~4×4/자동) + OSD 타일 | c4df2bb |
| 4.2 | WS 클라이언트 (재연결 지수백오프, 하트비트) | 동일 |
| 4.3 | Jitter/pacing 버퍼 (워커 30ms 틱, 상한 1200, Late 드롭) | 동일 |
| 4.4 | 디코더 워커 (RFC 6184/7798 → Annex B → WebCodecs) | 동일 |
| 4.5 | WebGL 렌더러 — VideoFrame 직접 업로드 (§9 대안, D15) | 동일 |
| 4.6 | PTZ 컨트롤 (조이스틱/속도/프리셋) + GetCameraPresets | 81182c9 |
| 4.7 | Toolbar [전체 시작]/[전체 정지] + 레이아웃 선택 | c4df2bb |
| 4.8 | 안정화: FU-A 헤더 버그(cb920fe), 워치독(092a1b8), 배치 전송(2ddd8f0) | 유실 3.72%→0% |

### Phase 4R: 아키텍처 재정비 — 서버 중심 구조 (v1.1) 🔄 진행 중
> 기획 재검토 지시(2026-08-31): 프론트엔드는 ONVIF/백엔드 함수를 직접 호출하지 않고
> **백엔드 서버(DevServer/네이티브 공통)를 경유**한다. 카메라 설정도 서버(config)가 소유하고
> UI는 서버 API로 조회한다. 브라우저(DevServer) 패리티 확보가 목적.
| # | 태스크 |
|---|--------|
| R.1 | HTTP REST API (`/api/*`) — §5.1 계약, httptest |
| R.2 | 프론트엔드 services/api.ts 전환 — Wails 바인딩 의존 제거 |
| R.3 | main.go Bind 제거 (Wails = 셸), 8080 점유 시 명확한 종료 |
| R.4 | DevServer 브라우저 패리티 실기 확인 |

### Phase 5: 폴리싱 + 통계 (예정)
| # | 태스크 |
|---|--------|
| 5.1 | 실시간 통계 대시보드 (비트레이트, FPS, 지터, 패킷로스 그래프) |
| 5.2 | 카메라 그룹/레이아웃 프리셋 (JSON 내보내기/가져오기) |
| 5.3 | 전체화면 (F11) / 팝아웃 윈도우 |
| 5.4 | 백엔드 Decoder 인터페이스 + Mock 구현 |
| 5.5 | 토스트 알림 시스템 + 에러 처리 UX (재시도 버튼) |
| 5.6 | 설정 패널 통합 (앱 설정 + 카메라 설정) + 마스터 키 관리(WEBNVR_MASTER_KEY) |

### Phase 6: 인증 + 마이그레이션 + 배포 (추후)
| # | 태스크 |
|---|--------|
| 6.1 | JWT 인증 + bcrypt + 로그인 UI (HTTP API에 적용, CORS 정책 재검토) |
| 6.2 | JSON → SQLite 마이그레이션 도구 |
| 6.3 | SQLCameraStore 구현 (Store 인터페이스 교체) |
| 6.4 | macOS 코드 서명/공증, Windows NSIS, Linux AppImage |
| 6.5 | CI/CD 파이프라인 (GitHub Actions) |

---

## 8. 설정 파일 스키마

### `config/app.json`
```json
{
  "version": 1,
  "server": { "ws_port": 8080, "http_port": 8081 },
  "stream": {
    "default_transport": "tcp",
    "rtp_timeout_ms": 5000,
    "jitter_buffer_ms": 150,
    "max_concurrent_streams": 20
  },
  "discovery": {
    "scan_timeout_ms": 3000,
    "scan_interfaces": ["en0", "eth0"],
    "auto_scan_interval_min": 30
  },
  "decoder": { "prefer_hardware": true, "max_threads": 4 },
  "logging": { "level": "info", "file": "logs/app.log", "max_size_mb": 100, "max_backups": 5 }
}
```

### `config/cameras.json`
```json
{
  "version": 1,
  "cameras": [
    {
      "id": "cam-001",
      "name": "정문 카메라",
      "type": "onvif",
      "xaddr": "192.168.0.217:8090",
      "username": "admin",
      "password": "encrypted:xyz123...",
      "profile_token": "profile_1",
      "stream_config": { "transport": "tcp", "protocol": "rtsp", "buffer_size": 1024000 },
      "ptz_supported": true,
      "group_id": "entrance",
      "layout_order": 0,
      "enabled": true,
      "added_at": "2026-08-29T10:00:00Z",
      "updated_at": "2026-08-29T10:00:00Z"
    },
    {
      "id": "cam-002",
      "name": "주차장 IP 카메라",
      "type": "rtsp",
      "stream_url": "rtsp://user:pass@192.168.1.101:554/stream1",
      "stream_config": { "transport": "tcp", "protocol": "rtsp", "buffer_size": 1024000 },
      "ptz_supported": false,
      "group_id": "parking",
      "layout_order": 1,
      "enabled": true,
      "added_at": "2026-08-29T10:00:00Z",
      "updated_at": "2026-08-29T10:00:00Z"
    },
    {
      "id": "cam-003",
      "name": "후문 주차장 I",
      "type": "onvif",
      "xaddr": "192.168.0.217:8100",
      "username": "admin",
      "password": "encrypted:xyz123...",
      "profile_token": "profile_1",
      "stream_config": { "transport": "tcp", "protocol": "rtsp", "buffer_size": 1024000 },
      "ptz_supported": true,
      "group_id": "back_parking",
      "layout_order": 2,
      "enabled": true,
      "added_at": "2026-08-29T10:00:00Z",
      "updated_at": "2026-08-29T10:00:00Z"
    },
    {
      "id": "cam-004",
      "name": "후문 주차장 II",
      "type": "onvif",
      "xaddr": "192.168.0.217:8110",
      "username": "admin",
      "password": "encrypted:xyz123...",
      "profile_token": "profile_1",
      "stream_config": { "transport": "tcp", "protocol": "rtsp", "buffer_size": 1024000 },
      "ptz_supported": true,
      "group_id": "back_parking",
      "layout_order": 3,
      "enabled": true,
      "added_at": "2026-08-29T10:00:00Z",
      "updated_at": "2026-08-29T10:00:00Z"
    }
  ],
  "groups": [
    { "id": "entrance", "name": "정문", "layout_type": "grid", "layout_config": {"cols": 2, "rows": 2} },
    { "id": "parking", "name": "주차장", "layout_type": "grid", "layout_config": {"cols": 2, "rows": 2} },
    { "id": "back_parking", "name": "후문 주차장", "layout_type": "grid", "layout_config": {"cols": 2, "rows": 2} }
  ]
}
```

---

## 9. 렌더링 파이프라인 (v1.1 구현 기준)

### 9.1 WebCodecs 경로 (주 경로)
VideoDecoder 출력(VideoFrame, NV12/RGBA)을 **WebGL 텍스처로 직접 업로드**한다.
CPU에서 YUV 플레인으로 복사하는 3텍스처 경로보다 비용이 훨씬 낮다.

```glsl
// components/grid/VideoRenderer.ts — 실제 셰이더
// VS: 전체 화면 사각형, UV 반전 / FS: 단일 텍스처 샘플링
fragColor = texture(u_tex, v_uv);
```
- 레터박스: 캔버스 크기와 프레임 종횡비 비교 → gl.viewport로 중앙 배치
- 폴백: VideoFrame 직접 업로드가 실패하는 WebView에서는 2D 캔버스
  (drawImage) 우회 후 캔버스를 텍스처로 업로드

### 9.2 YUV420P 3텍스처 셰이더 (BT.709) — ffmpeg.wasm 폴백용 (보류)
기존 v1.0 §9의 Y/U/V 3텍스처 셰이더는 VideoDecoder 미지원 환경의
ffmpeg.wasm 폴백(Phase 5 검토) 도입 시 재사용한다. 현재 미구현.

---

## 10. 위험 요소 및 대응

| 위험 | 확률 | 영향 | 대응 |
|------|------|------|------|
| WebCodecs 미지원 WebView | 해소 | 높음 | macOS 15/WebKit 18 검증 완료. 구버전 대비: ffmpeg.wasm 폴백(Phase 5) |
| 다중 스트림 WebGL 메모리 | 중간 | 중간 | VideoFrame 즉시 close, 레터박스 뷰포트. 비가시 타일 일시정지(Phase 5) |
| RTSP UDP 패킷 로스 | 해소 | 중간 | TCP 기본(gortsplib 인터리브). UDP 옵션 유지 |
| Wails WebView 버전 차이 | 낮음 | 높음 | 최소 버전 강제 (WebView2 109+, WebKit 614+) |
| RTMP 풀링 구현 복잡도 | 중간 | 중간 | 미구현(타입만 지원), 필요시 Phase 5+ |
| WS 버스트로 인한 유실 | **해소** | 높음 | rtp_batch 배치 전송 + 허브 버퍼 512패킷 — 실측 0.00% (v1.1) |
| 8080 포트 충돌 | 중간 | 중간 | v1.1: 점유 시 명확한 오류와 함께 종료 (고정 포트 정책) |
| CORS (DevServer→8080) | 중간 | 낮음 | 모든 오리진 허용 + 127.0.0.1 바인딩 한정, Phase 6 인증에서 재검토 |
| 마스터 키 미설정 | 확인됨 | 중간 | 폴백 키 동작(경고 로그). Phase 5/6에서 키 관리 방안 확정 |

---

## 11. 테스트 전략

| 레벨 | 도구 | 대상 | 목표 |
|------|------|------|------|
| 단위 (BE) | `testing` 표준 | config/camera/onvif/stream/ws — SOAP 목업, 로컬 RTSP 서버, WS 통합 테스트 | ✅ 운영 중 |
| 진단 (통합) | cmd/wstest, rtspanalyze, rtspmock, streamtest | 실기 카메라/WS 파이프라인/시퀀스 갭 측정 | ✅ 운영 중 |
| 단위 (FE) | Vitest, RTL (미도입) | Store, 디페이저(재생 하네스로 대체) | Phase 5 검토 |
| E2E | 수동 + 사용자 확인 | 브라우저/네이티브 패리티, 렌더링 | 진행 중 |
| 부하 | wstest 다중 채널 | 5채널 26.4Mbps 실측 0.00% | 20채널 확장 Phase 5 |

---

## 12. 개발·진단 명령어 (현재 기준)

```bash
# 개발
wails dev                    # 네이티브 앱 + DevServer(34115, 브라우저 접속 가능)
wails build                  # 프로덕션 빌드 → build/bin/webnvr.app

# 백엔드 검증
go test -race ./...          # 전 패키지 (config/camera/onvif/stream/ws)
go run ./cmd/streamtest -onvif 192.168.0.217:8090 -user admin -pass "PW"   # RTSP 수신 진단
go run ./cmd/wstest <cameraId,...> [초]    # WS 파이프라인 + 시퀀스 갭 측정
go run ./cmd/rtspanalyze -camera <id>      # 실기 NALU 구조 분석
go run ./cmd/rtspmock        # 모의 RTSP 서버 (STAP-A + FU-A 분할 IDR)

# HTTP API 확인 (v1.1)
curl http://127.0.0.1:8080/api/health
curl http://127.0.0.1:8080/api/cameras
```

---

*문서 버전: 1.1 | 작성일: 2026-08-29 | 최종 수정: 2026-08-31*

### 부록 A. v1.1 아키텍처 개정 이력

| 버전 | 일자 | 변경 |
|------|------|------|
| 1.0 | 2026-08-29 | 초안 (Wails 바인딩 중심 설계) |
| 1.1 | 2026-08-31 | **서버 중심 재설계** — 카메라 관리를 Wails 바인딩에서 HTTP REST(`/api/*`)로 이행, 프론트엔드를 백엔드 순수 클라이언트로 정의(브라우저 패리티). 실구현 기준으로 전 섹션 갱신: gortsplib 확정, rtp_batch 배치 프로토콜, 허브 버퍼 512패킷, WebCodecs/WebGL 직접 업로드 렌더링, 진단 도구 체계(cmd/*), 실측 성능(5채널 26.4Mbps 유실 0%)
