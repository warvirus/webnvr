# CCTV 관제 시스템 - 아키텍처 및 구현 계획서

## 1. 프로젝트 개요

| 항목 | 내용 |
|------|------|
| **프로젝트명** | cctv-control |
| **목적** | ONVIF 카메라 자동 검색, 멀티뷰 실시간 스트리밍, PTZ 제어 |
| **아키텍처** | Wails v2 (Go 백엔드 + React/WebView 프론트엔드) |
| **스트림 처리** | 백엔드 RTSP/RTP/RTMP 수신 → WebSocket → 프론트엔드 WebCodecs + WebGL 디코딩/렌더링 |
| **설정 관리** | JSON 파일 (`./config/`) → 추후 SQLite 마이그레이션 |
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
| **WebSocket** | gorilla/websocket | JSON 프로토콜 |
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
│   │   ├── stream/           # 스트림 처리
│   │   │   ├── hub.go
│   │   │   ├── rtsp_client.go
│   │   │   ├── rtmp_client.go
│   │   │   ├── forwarder.go
│   │   │   └── pipeline.go
│   │   ├── api/              # Wails 바인딩
│   │   │   ├── camera.go
│   │   │   ├── stream.go
│   │   │   └── events.go
│   │   └── ws/               # WebSocket 서버
│   │       ├── server.go
│   │       ├── handler.go
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
│   │   ├── hooks/
│   │   │   ├── useCameras.ts
│   │   │   ├── useDiscovery.ts
│   │   │   ├── useStream.ts
│   │   │   ├── useWebCodecs.ts
│   │   │   └── useWebGL.ts
│   │   ├── store/
│   │   │   ├── cameraStore.ts
│   │   │   ├── streamStore.ts
│   │   │   └── uiStore.ts
│   │   ├── services/
│   │   │   ├── api.ts
│   │   │   └── ws.ts
│   │   ├── workers/
│   │   │   └── decoder.worker.ts
│   │   ├── shaders/
│   │   │   ├── yuv2rgb.vert
│   │   │   └── yuv2rgb.frag
│   │   ├── pages/
│   │   │   ├── CameraManagementPage.tsx
│   │   │   └── MonitoringPage.tsx
│   │   ├── types/index.ts
│   │   ├── App.tsx
│   │   └── main.tsx
│   └── package.json / vite.config.ts
├── wails.json
└── build/
```

---

## 5. 백엔드 API 계약 (Wails Bindings)

### 5.1 Camera API

```go
type CameraService struct {
    mgr      *camera.CameraManager
    config   *config.ConfigManager
    onvifCli *onvif.Client
}

// 카메라 목록 조회
func (s *CameraService) ListCameras() ([]CameraDTO, error)

// 카메라 단일 조회
func (s *CameraService) GetCamera(id string) (*CameraDTO, error)

// 카메라 추가 (타입별 분기)
func (s *CameraService) CreateCamera(req CreateCameraRequest) (*CameraDTO, error)

// 카메라 수정
func (s *CameraService) UpdateCamera(id string, req UpdateCameraRequest) (*CameraDTO, error)

// 카메라 삭제
func (s *CameraService) DeleteCamera(id string) error

// 카메라 순서 변경 (드래그앤드롭)
func (s *CameraService) ReorderCameras(cameraIDs []string) error

// ──────────────────────────────────────────────
// ONVIF 전용
// ──────────────────────────────────────────────
func (s *CameraService) DiscoverONVIFCameras() ([]DiscoveredCamera, error)
func (s *CameraService) TestONVIFCamera(req TestONVIFRequest) (*TestONVIFResponse, error)
func (s *CameraService) GetONVIFProfiles(req GetProfilesRequest) ([]ProfileDTO, error)
func (s *CameraService) GetONVIFStreamURI(req GetStreamURIRequest) (string, error)

// ──────────────────────────────────────────────
// 직접 스트림 전용 (RTSP/RTP/RTMP)
// ──────────────────────────────────────────────
func (s *CameraService) TestDirectStream(req TestDirectStreamRequest) (*TestDirectStreamResponse, error)
```

### 5.2 Stream API

```go
type StreamService struct {
    hub *stream.Hub
}

func (s *StreamService) StartStream(cameraID string) error
func (s *StreamService) StopStream(cameraID string) error
func (s *StreamService) StartAllStreams() error
func (s *StreamService) StopAllStreams() error
func (s *StreamService) PTZControl(cameraID string, cmd PTZCommand) error
func (s *StreamService) GetStreamStatus(cameraID string) StreamStatus
```

### 5.3 WebSocket 프로토콜 (JSON)

```typescript
// Client → Server
type ClientMsg =
  | { type: "start_stream"; cameraId: string }
  | { type: "stop_stream"; cameraId: string }
  | { type: "start_all_streams" }
  | { type: "stop_all_streams" }
  | { type: "ptz"; cameraId: string; command: PTZCommand }
  | { type: "request_keyframe"; cameraId: string }
  | { type: "subscribe"; cameraId: string }
  | { type: "unsubscribe"; cameraId: string }
  | { type: "ping" }

// Server → Client
type ServerMsg =
  | { type: "stream_started"; cameraId: string; ssrc: number; clockRate: number; payloadType: number; sps: string; pps: string; width: number; height: number }
  | { type: "rtp_packet"; cameraId: string; payload: string; timestamp: number; marker: boolean; sequence: number }
  | { type: "stream_stopped"; cameraId: string; reason: string }
  | { type: "stream_error"; cameraId: string; error: string }
  | { type: "camera_discovered"; camera: DiscoveredCamera }
  | { type: "camera_status"; cameraId: string; status: CameraStatus }
  | { type: "stats"; cameraId: string; stats: StreamStats }
  | { type: "pong" }
  | { type: "config_updated"; section: "app" | "cameras"; data: any }
```

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

## 7. 구현 단계 (6주 + Phase 6)

### Phase 1: 백엔드 기반 + Camera API (Week 1-2) ✅ 완료
| # | 태스크 |
|---|--------|
| 1.1 | `wails init -n cctv-control -t react-ts` |
| 1.2 | JSON 설정 로더 (`internal/config/`) - 로드/저장/핫리로드/암호화 |
| 1.3 | CameraManager + JSONCameraStore + 타입별 분기 |
| 1.4 | WS-Discovery (ONVIF만) |
| 1.5 | ONVIF 클라이언트 래퍼 (프로필/URI/PTZ) |
| 1.6 | 직접 스트림 테스트 (RTSP/RTP/RTMP URL 검증) |
| 1.7 | 통합 Camera API 바인딩 (List/Create/Update/Delete/Discover/Test/GetProfiles) |
| 1.8 | 설정 암호화 (AES-GCM + 환경변수 키 파생) |

### Phase 2: 스트림 코어 + WebSocket (Week 2-3) ✅ 완료
| # | 태스크 |
|---|--------|
| 2.1 | pion/rtsp 클라이언트 (TCP/UDP, Digest 인증, 세션 관리) |
| 2.2 | RTP 디페이로더 (H.264 RFC 6184, H.265 RFC 7798) |
| 2.3 | RTMP 클라이언트 (필요시) |
| 2.4 | Stream Hub (구독자 관리, 백프레셔 채널 버퍼 30프레임) |
| 2.5 | WebSocket 서버 (gorilla/websocket, JSON, 하트비트) |
| 2.6 | Stream API 바인딩 (Start/Stop/PTZ/StartAll/StopAll) |
| 2.7 | 단일 카메라 스트림 테스트 |

### Phase 3: 프론트엔드 카메라 관리 UI (Week 3) 🔄 진행 중
| # | 태스크 |
|---|--------|
| 3.1 | Zustand cameraStore (CRUD + 낙관적 업데이트 + 서버 동기화) |
| 3.2 | CameraManagementPage + CameraList + CameraModal |
| 3.3 | DiscoveryPanel (자동 검색 UI, 3초 타임아웃) |
| 3.4 | CameraForm (타입별 필드 동적 표시, 유효성 검증) |
| 3.5 | ProfileSelector (프로필 자동 조회 후 드롭다운 선택) |
| 3.6 | ConnectionTest (ONVIF/직접 스트림 통합) |
| 3.7 | 드래그앤드롭 순서 변경 (@dnd-kit) |

### Phase 4: 프론트엔드 모니터링 + 디코딩 (Week 4)
| # | 태스크 |
|---|--------|
| 4.1 | MonitoringPage + CameraGrid 레이아웃 (1×1~4×4, 커스텀) |
| 4.2 | WebSocket 서비스 (재연결 지수백오프, 메시지 큐) |
| 4.3 | Jitter Buffer (150ms, 타임스탬프 정렬, Late 드롭) |
| 4.4 | WebCodecs 디코더 워커 (OffscreenCanvas, 하드웨어 가속) |
| 4.5 | WebGL 렌더러 (YUV420P 3텍스처 + BT.709 셰이더) |
| 4.6 | PTZ 컨트롤 UI (조이스틱, 프리셋, 속도) |
| 4.7 | Toolbar: [전체 시작] [전체 정지] 레이아웃 선택 |

### Phase 5: 폴리싱 + 백엔드 디코더 준비 (Week 5-6)
| # | 태스크 |
|---|--------|
| 5.1 | 실시간 통계 대시보드 (비트레이트, FPS, 지터, 패킷로스 그래프) |
| 5.2 | 카메라 그룹/레이아웃 프리셋 (JSON 내보내기/가져오기) |
| 5.3 | 전체화면 (F11) / 팝아웃 윈도우 |
| 5.4 | 백엔드 Decoder 인터페이스 + Mock 구현 |
| 5.5 | 토스트 알림 시스템 + 에러 처리 UX (재시도 버튼) |
| 5.6 | 설정 패널 통합 (앱 설정 + 카메라 설정) |

### Phase 6: 인증 + 마이그레이션 + 배포 (추후)
| # | 태스크 |
|---|--------|
| 6.1 | JWT 인증 + bcrypt + 로그인 UI |
| 6.2 | JSON → SQLite 마이그레이션 도구 |
| 6.3 | SQLCameraStore 구현 (인터페이스 교체) |
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

## 9. WebGL 셰이더 (YUV420P → RGB)

```glsl
// shaders/yuv2rgb.frag
#version 300 es
precision highp float;
in vec2 v_texCoord;
uniform sampler2D u_yTexture;
uniform sampler2D u_uTexture;
uniform sampler2D u_vTexture;
uniform bool u_bt709;
out vec4 fragColor;
void main() {
  float y = texture(u_yTexture, v_texCoord).r;
  float u = texture(u_uTexture, v_texCoord).r - 0.5;
  float v = texture(u_vTexture, v_texCoord).r - 0.5;
  mat3 yuv2rgb = u_bt709 
    ? mat3(1.0, 1.0, 1.0, 0.0, -0.1873, 1.8556, 1.5748, -0.4681, 0.0)
    : mat3(1.0, 1.0, 1.0, 0.0, -0.3441, 1.7720, 1.4020, -0.7141, 0.0);
  vec3 rgb = yuv2rgb * vec3(y, u, v);
  fragColor = vec4(clamp(rgb, 0.0, 1.0), 1.0);
}
```

---

## 10. 위험 요소 및 대응

| 위험 | 확률 | 영향 | 대응 |
|------|------|------|------|
| WebCodecs Safari 미지원 | 높음 | 높음 | ffmpeg.wasm 폴백, 기능 감지 자동 전환 |
| 순수 Go H.264 디코더 성능 | 중간 | 중간 | 베이스라인만, HW 가속은 별도 CGO 빌드 |
| 다중 스트림 WebGL 메모리 | 중간 | 높음 | 텍스처 풀링, 비가시 타일 일시정지 |
| RTSP UDP 패킷 로스 | 높음 | 중간 | TCP 강제 옵션, NALU 누락 복구 |
| Wails WebView 버전 차이 | 낮음 | 높음 | 최소 버전 강제 (WebView2 109+, WebKit 614+) |
| RTMP 풀링 구현 복잡도 | 중간 | 중간 | 필요시만 구현, 우선순위 낮춤 |

---

## 11. 테스트 전략

| 레벨 | 도구 | 대상 | 목표 |
|------|------|------|------|
| 단위 (BE) | `testing`, `testify` | Config, CameraManager, RTSP Client, Hub | 80%+ |
| 단위 (FE) | Vitest, RTL | Store, Hooks, Utils | 70%+ |
| 통합 | ONVIF Mock 서버 | Discovery → Stream 전체 플로우 | 주요 경로 100% |
| E2E | Playwright | UI 상호작용, 레이아웃, PTZ | 크리티컬 플로우 |
| 부하 | k6 | 20카메라 × 10클라이언트 | 드롭 < 2%, 메모리 안정 |

---

## 12. 즉시 실행 태스크 (시작 순서)

```bash
# 1. 프로젝트 생성
wails init -n cctv-control -t react-ts

# 2. 백엔드 설정 시스템
# internal/config/ (loader, watcher, encryption, validator)

# 3. 백엔드 카메라 관리
# internal/camera/ (manager, store, discovery, types, validator)
# internal/onvif/ (client, profiles, ptz, stream_uri)

# 4. Camera API 바인딩
# internal/api/camera.go

# 5. 프론트엔드 카메라 관리 UI
# store/cameraStore.ts
# components/camera/ (CameraList, CameraModal, DiscoveryPanel, CameraForm, ProfileSelector, ConnectionTest)
# pages/CameraManagementPage.tsx

# 6. 스트림 코어 + WebSocket
# internal/stream/, internal/ws/, internal/api/stream.go

# 7. 프론트엔드 모니터링 + WebCodecs/WebGL
# store/streamStore.ts, services/ws.ts, workers/decoder.worker.ts
# hooks/useWebGL.ts, shaders/, components/grid/
# pages/MonitoringPage.tsx
```

---

*문서 버전: 1.0 | 작성일: 2026-08-29 | 최종 수정: 2026-08-29*
