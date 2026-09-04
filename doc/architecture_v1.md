# CCTV 관제 시스템 (webnvr) — 아키텍처 정리 v1

| 항목 | 내용 |
|------|------|
| 문서 버전 | v1 (전체 재정리본) |
| 작성일 | 2026-09-03 |
| 기준 커밋 | HEAD `1f3e167` (Phase 1~5 완료 상태 고정) |
| 대체 대상 | `doc/architecture.md` (내부 버전 1.1, 2026-08-31) — 본 문서가 정식본이다 |
| 언어 | 한국어 |

> 이 문서는 지금까지 진행된 모든 작업(백엔드 Phase 1~5, 서버 중심 재설계 v1.1, 파일 로깅,
> 헤드리스 실행, 스트리밍 안정화 F1~F18, 프론트엔드 UI 재작업)을 실제 소스 기준으로 정리한다.
> 프론트엔드 UI는 §4에서 상세히 다룬다.

---

## 1. 개요

### 1.1 프로젝트 정체성

| 항목 | 내용 |
|------|------|
| 이름 | `webnvr` (go module · wails 프로젝트명 동일) |
| 목적 | ONVIF 카메라 자동 검색 · 멀티뷰 실시간 스트리밍 · PTZ 제어 |
| 형태 | Wails v2 데스크톱 앱 (창은 셸일 뿐, 프론트는 백엔드의 순수 HTTP/WS 클라이언트) |
| 대상 OS | macOS 우선, Windows/Linux |
| 인증 | 없음 (Phase 6로 연기) |
| 상정 사용처 | 주차장 출입구를 감시하는 소규모 관제, 사용자는 시설 관리자 |

### 1.2 핵심 설계 원칙

1. **서버 중심 · 단일 포트** — 백엔드가 `server.bind:server.ws_port`(기본 `0.0.0.0:8080`) 하나로
   REST API(`/api/*`), 스트림 중계 WebSocket(`/ws`), 프론트엔드 UI(`/`)를 모두 서빙한다.
   HTTPS/WSS는 `WEB_CERT`/`WEB_KEY` 환경변수가 있을 때 `:8443`에서 추가로 뜬다.
2. **브라우저 패리티** — 프론트엔드는 Wails 바인딩을 쓰지 않고 `fetch` + `WebSocket`만 쓴다.
   네이티브 셸과 DevServer 브라우저가 완전히 같은 경로로 동작한다.
3. **클라이언트별 독립 재생** — 모니터링 페이지 진입 = 활성 카메라 전체 자동 시작, 이탈 = 정지.
   백엔드 Hub가 구독 참조 수(`refs`)를 세고 마지막 구독자가 떠나면 RTSP 세션을 해제한다.
   한 클라이언트의 화면 전환이 다른 클라이언트에 영향을 주지 않는다.
4. **메인 스레드 디코딩** — WebCodecs `VideoDecoder`를 Web Worker가 아닌 메인 스레드에서 돌린다
   (WebKit이 Worker 컨텍스트의 디코더 출력 콜백을 발화하지 않는 사례 때문, §7 F8).

### 1.3 기술 스택 (실제 채택 버전)

| 계층 | 채택 | 비고 |
|------|------|------|
| 데스크톱 셸 | Wails v2.15.0 | 창만 담당. `internal/`에 wails import 없음 |
| 백엔드 언어 | Go 1.27 | |
| RTSP 클라이언트 | `bluenviron/gortsplib/v4` v4.16.2 | `third_party/gortsplib`로 벤더링 + `AllowSSRCChange` 패치 (§3.9) |
| SPS 파싱 | `bluenviron/mediacommon/v2` v2.4.1 | H.264 해상도 추출 (H.265 미구현) |
| ONVIF | `use-go/onvif` v0.0.9 | 일부는 raw SOAP `CallMethod` (§3.4) |
| WebSocket | `gorilla/websocket` v1.5.3 | |
| 로그 로테이션 | `natefinch/lumberjack.v2` v2.2.1 | 크기 기반 |
| 설정 감시 | `fsnotify` v1.10.1 | 코드에 존재하나 런타임 미배선 (§3.2) |
| 프론트 빌드 | Vite 5 + `@vitejs/plugin-react` 4 | |
| 프론트 언어 | TypeScript 5.9 + React 18 | WebCodecs 타입 때문에 TS 5.9 |
| 상태 관리 | Zustand | persist/devtools 미사용 |
| 디코딩(FE) | WebCodecs `VideoDecoder` | `prefer-hardware` → software 폴백 |
| 렌더링(FE) | WebGL2 + `VideoFrame` 직접 텍스처 업로드 | 실패 시 2D `drawImage` 폴백 |
| 폰트 | IBM Plex Sans KR + JetBrains Mono | `@fontsource` 번들 (오프라인 앱, CDN 금지) |

---

## 2. 시스템 아키텍처

### 2.1 프로세스 구성도

```
   ┌───────────┐   RTSP(TCP 기본)     ┌──────────────────────────────────────────────┐
   │ IP 카메라  │ ───────────────────▶ │                 webnvr 백엔드                  │
   │ (ONVIF/    │   RTP H.264/H.265    │                                              │
   │  RTSP 직접) │                     │  internal/stream                              │
   └───────────┘                     │   rtsp_client(gortsplib) → depayload → Hub     │
        ▲                            │        · subscriberBuf = 512 (≈30프레임)        │
        │ ONVIF SOAP                 │        · 참조 카운팅: refs==0 → RTSP 해제         │
        │ (GetStreamUri/PTZ/         │        · 백프레셔 = 느린 구독자 이벤트 드롭         │
        │  Profiles/Discovery)       │                       │                        │
        │                            │   internal/ws  pump: 채널 대기 패킷 즉시 흡수      │
        │                            │        → rtp_batch (≤128패킷/묶음)              │
        │                            │                       │                        │
        │           AccessLog( CORS( http.ServeMux ) )  ─ 단일 리스너 :8080 ─           │
        │            ├── /ws       (WebSocket: 스트림 중계 + 제어 + PTZ)                │
        │            ├── /api/*    (REST: 카메라/설정/백업/보안)                        │
        │            └── /         (임베디드 SPA, 없는 경로는 index.html 폴백)          │
        │                            └──────────────────────────────────────────────┘
        │                                          │  fetch + WebSocket (동일 오리진 또는 CORS)
        │                                          ▼
        │                            ┌──────────────────────────────────────────────┐
        │                            │              프론트엔드 (React/TS)              │
        └── (등록 카메라는 백엔드가     │  services/ws.ts  ─▶ store/streamStore.ts        │
            자격증명 복호화 후 호출.     │        rtp_batch → base64 디코드 →              │
            프론트에 비밀번호 미노출)    │  services/decoder.ts  Session/DecoderHub        │
                                     │    지터 버퍼(TICK 30ms, 큐 1200) →              │
                                     │    depay(RFC 6184/7798) → WebCodecs VideoDecoder │
                                     │        │ CustomEvent('webnvr-frame')            │
                                     │        ▼                                        │
                                     │  components/grid/VideoRenderer.ts               │
                                     │    WebGL2 texImage2D(VideoFrame) + 레터박스       │
                                     │    (실패 시 2D drawImage 폴백)                    │
                                     └──────────────────────────────────────────────┘
```

### 2.2 단일 리스너 모델

- `internal/api/app.go`의 `StartWSServer(assets)`가 `http.NewServeMux`를 만들고
  `/ws` → `ws.Server.Mux()`, `/api/*` → `RegisterHTTP`, `/` → `registerUI`(assets 있을 때만)를 등록한 뒤
  `AccessLog(CORS(mux))`로 감싸 `ws.Server.StartWithHandler`로 띄운다.
- `AccessLog`는 모든 요청(API/WS/UI)을 `slog.Info("HTTP", method, path, status, duration_ms)`로 남긴다.
  WS 업그레이드가 깨지지 않도록 `statusRecorder`가 `http.Hijacker`를 구현한다 (§7 F19-a).
- `CORS`는 `Access-Control-Allow-Origin: *`, `OPTIONS` → 204. 인증이 붙는 Phase 6에서 재검토 대상이다.
- HTTPS는 `WEB_CERT`/`WEB_KEY`가 있을 때 `:8443`에서 별도 리스너로 뜬다(WSS 동일 포트). 바인딩 실패는 경고만 한다.

### 2.3 실행 형태

| 방식 | 명령 | 창 | UI 서빙 |
|------|------|----|---------|
| 데스크톱 앱 + 핫리로드 | `wails dev` | O | Vite(34115/5173) |
| 데스크톱 프로덕션 | `wails build` → `webnvr.app` | O | 임베디드 `frontend/dist` (`fs.Sub`로 서브루팅) |
| 헤드리스 백엔드 | `go run ./cmd/server` | X | `frontend/dist` 있으면 디스크 서빙, 없으면 API/WS만 |
| 데스크톱 바이너리 헤드리스 | `./webnvr -headless` | X | 임베디드 `frontend/dist` |
| 프론트 개발 병행 | `cd frontend && npm run dev` | — | Vite 5173, `backend.ts`가 `:8080` 백엔드에 자동 연결 |

`cmd/server`는 순수 Go라 CGO/WebKit 링크 없이 빌드된다(`CGO_ENABLED=0` 확인됨).

### 2.4 영상 데이터 흐름

1. `stream.DialRTSP` — gortsplib로 DESCRIBE/SETUP/PLAY, `OnPacketRTP`로 RTP 수신. SDP나 스트림에서 SPS/PPS/VPS 수집.
2. `Hub.publish` — 각 구독자 채널(버퍼 512)에 `StartedEvent`(코덱 메타) → `PacketEvent`(RTP payload) 순서로 전달.
   느린 구독자는 `select {..; default: drops++}`로 이벤트를 버린다(절대 블로킹하지 않음). 10초마다 통계 로그.
3. `ws/handler.go`의 `pump` — 채널에서 이벤트를 꺼내면서 대기 중인 패킷을 즉시 추가로 흡수해
   최대 128개씩 `rtp_batch`(축약 키 `p`/`ts`/`m`/`sq`)로 묶어 전송. `StartedEvent`는 `stream_started`로 즉시 flush.
4. 프론트 `streamStore`의 메시지 라우터 — `rtp_batch`의 각 패킷을 `PacketIn`으로 변환해 `DecoderHub.packet`으로 넣는다.
5. `decoder.ts`의 `Session` — 지터 버퍼(30ms 틱, 큐 상한 1200), RFC 6184/7798 depay, 프레임 조립,
   WebCodecs `VideoDecoder.decode`. 첫 프레임 디코드 성공 시 `onDecoded` → 타일 상태 `streaming`.
6. `onFrame` → `window.dispatchEvent(CustomEvent('webnvr-frame', {detail:{cameraId, frame}}))`.
   `CameraTile`이 자기 카메라 프레임만 구독해 `VideoRenderer.draw(frame)` 후 `frame.close()`.

### 2.5 제어 데이터 흐름

- **카메라 관리(등록/수정/삭제/순서/검색/테스트/프로필/설정/백업)** = HTTP REST (`/api/*`).
- **스트림 시작/정지 · PTZ** = WebSocket 메시지 (`/ws`). 이유는 스트림 수신과 제어가 같은 연결에서 순서 보장이 필요하고,
  `subscribe`와 `start`가 결합 동작이기 때문이다.

---

## 3. 백엔드

### 3.1 모듈 레이아웃

```
main.go                  Wails 셸 진입점 (-headless 플래그, fs.Sub로 UI 서브루팅)
internal/
  config/    config.go(스키마+Default) loader.go(원자적 저장) watcher.go(핫리로드-미배선)
             encryption.go(AES-256-GCM+PBKDF2) validator.go
  camera/    types.go(Camera 4타입) store.go(JSONCameraStore) manager.go(Manager+PasswordOf)
  onvif/     client.go discovery.go(WS-Discovery) profiles.go stream_uri.go ptz.go
  stream/    rtsp_client.go(gortsplib) hub.go(구독/참조카운팅/백프레셔) types.go decoder.go(서버측 스텁)
  ws/        server.go(리스너+업그레이드) handler.go(connState+pump) protocol.go(메시지 계약)
  api/       app.go(조립) camera.go(CameraService) stream.go(StreamService) httpapi.go(REST 라우트+미들웨어)
  logging/   logger.go(lumberjack+slog)
cmd/
  server/       헤드리스 백엔드 진입점 (순수 Go)
  streamtest/   RTSP 수신 진단 CLI
  rtspmock/     합성 H.264 RTSP 서버 (STAP-A + FU-A IDR 패턴)
  wstest/       WS 파이프라인 검증 + 시퀀스 갭(유실률) 측정
  rtpdump/      RTP 패킷을 JSON으로 덤프
  rtspanalyze/  NALU 구조/GOP 간격 분석
third_party/gortsplib/   패치된 gortsplib v4.16.2 로컬 복사본 (go.mod replace)
```

### 3.2 internal/config

**스키마** (`config.go`, `CurrentVersion = 1`). `Default()` 값은 아래 표와 같다.

| 그룹 | 필드 (json) | 타입 | 기본값 |
|------|------------|------|--------|
| server | `ws_port` | int | `8080` |
| server | `http_port` | int | `8081` (사실상 미사용 — v1.1에서 8080 단일화) |
| server | `bind` | string | `"0.0.0.0"` |
| server | `max_clients` | int | `0` (0 = 무제한) |
| stream | `default_transport` | string | `"tcp"` |
| stream | `rtp_timeout_ms` | int | `5000` |
| stream | `jitter_buffer_ms` | int | `150` |
| stream | `max_concurrent_streams` | int | `20` |
| discovery | `scan_timeout_ms` | int | `3000` |
| discovery | `scan_interfaces` | []string | `["en0","eth0"]` |
| discovery | `auto_scan_interval_min` | int | `30` |
| decoder | `prefer_hardware` | bool | `true` |
| decoder | `max_threads` | int | `4` |
| logging | `level` | string | `"info"` |
| logging | `file` | string | `"logs/app.log"` |
| logging | `max_size_mb` | int | `100` |
| logging | `max_backups` | int | `5` |

- `Load`는 파일이 없으면 `Default()`를 만들어 저장하고, 있으면 `Default()` 위에 unmarshal 한다(없는 필드는 기본값 상속).
- `Save`는 `MarshalIndent` → 임시 파일 → `os.Rename`으로 원자적 교체.
- `Validate`가 포트 범위/`ws_port != http_port`/transport 값/레벨 값 등을 검사한다.

**암호화** (`encryption.go`).

- 마스터 키 우선순위 — ① 환경변수 `WEBNVR_MASTER_KEY` ② 세션 키(키 파일 `config/.masterkey` 또는 마이그레이션에서 설정) ③ 개발용 폴백(`SHA-256("webnvr-dev-fallback-key")`, 사용 시 1회 경고).
- 카메라별 키 = `PBKDF2(SHA-256, master, salt=SHA-256("webnvr:camera:"+cameraID), iter=100000, 32B)`.
- 저장 형식 = `"encrypted:" + base64(nonce(12B) ‖ AES-256-GCM ciphertext)`. 접두어가 없으면 평문으로 간주(레거시 통과).
- `GET /api/security`가 현재 출처(`env` / `file` / `fallback`)를 노출한다.

**핫리로드** — `watcher.go`에 `fsnotify` 기반 디바운스(500ms) 감시가 구현돼 있으나 `api.App`이 호출하지 않는다.
런타임 설정 변경은 `PUT /api/config`(전체 교체 + 검증 + 원자적 저장 + 메모리 갱신)로만 이뤄지며, `server.*` 변경은 프로세스 재시작이 필요하다.

### 3.3 internal/camera

- `Camera` 필드 — `id, name, type(onvif|rtsp|rtp|rtmp), xaddr, username, password("encrypted:..."), profile_token, stream_url, stream_config{transport,protocol,buffer_size}, ptz_supported, group_id, layout_order, enabled, added_at, updated_at`.
- `JSONCameraStore` — `sync.RWMutex` + 원자적 파일 쓰기. `List`는 `layout_order` 정렬 사본, `Add`는 ID 자동 생성(`cam-<hex4>`) + `layout_order = len`, `Delete`는 삭제 후 전 항목 `layout_order` 재번호, `Reorder`는 `len(ids) == len(cameras)` 요구.
- `Manager` — `Create/Update` 시 비밀번호가 있으면 `config.EncryptSecret`으로 카메라별 키 암호화. `PasswordOf(cam)`이 **유일한 복호화 지점**이며 스트림 연결/PTZ/진단 CLI가 모두 이걸 거친다.

### 3.4 internal/onvif

`use-go/onvif` SDK를 감싼다. SDK 생성 응답 구조체의 네임스페이스 태그가 실제 카메라 응답(`trt:`/`tt:`)과 맞지 않아 일부는 raw SOAP로 처리한다.

| 파일 | 역할 | 방식 |
|------|------|------|
| `client.go` | `New`(생성 시 `GetCapabilities`로 도달성 확인), `DeviceInformation` | SDK 헬퍼 |
| `discovery.go` | WS-Discovery — 인터페이스별 순차 프로브(내부 ~1초), XAddrs/Scopes 파싱, 포트 없는 주소 제외, 이름은 `onvif://.../name/...`에서 추출 | 라이브러리 + 수동 XML |
| `profiles.go` | `Profiles` → `{Token,Name,Width,Height}` | **raw `CallMethod` + 커스텀 파서** |
| `stream_uri.go` | `StreamURI(profileToken, "RTSP")` | SDK 헬퍼 |
| `ptz.go` | `ContinuousMove` / `Stop` / `GotoPreset` = SDK, `Presets` = **raw SOAP** | 혼합 |

### 3.5 internal/stream

- `rtsp_client.go` — `DialRTSP(ctx, url, transport, onInfo, onPacket)` 블로킹. `gortsplib.Client{Transport: TCP(기본)|UDP, ReadTimeout: 30s, AllowSSRCChange: true}`. 비디오 미디어에서 H.264 우선, 없으면 H.265. SPS/PPS는 SDP에서 시드하고 없으면 스트림 패킷에서 수집해 준비되면 `onInfo` 1회 발행. H.264는 SPS로 해상도 계산, H.265는 0,0.
- `hub.go` — `const subscriberBuf = 512`.
  - `activeStream{cancel, subs, info, refs, packetCount, dropCount, ...}`.
  - **생애** — `Start`는 슬롯을 먼저 예약(경쟁 방지)하고 dial 고루틴 실행. `Subscribe`는 구독자 채널(버퍼 512) 생성 + `refs++`, 실행 중 스트림이면 `StartedEvent`(코덱 메타)를 새 구독자에게 즉시 재전송(늦은 합류 대응, §7 F11). `cancel`은 구독자 제거 + `refs--`, `refs <= 0`이면 `closeSession`(RTSP 해제).
  - **백프레셔** — `publish`는 각 구독자 채널에 `select { case s.ch <- ev: default: s.drops++ }`. 절대 블로킹하지 않고 드롭한다. 10초마다 pps/드롭률/구독자 수 로그.
  - `StopAll`은 앱 종료 전용.
- `decoder.go` — 서버측 디코딩 인터페이스 `FrameDecoder` + `MockDecoder` + 테스트. 스냅샷/녹화 대비 스텁이며 런타임 경로에 없다(실제 디코딩은 프론트 WebCodecs).

### 3.6 internal/ws

- `server.go` — `readWait = 90s`, `writeWait = 10s`, `upgraderBuff = 4096`. `Upgrader.CheckOrigin`은 항상 true(로컬 앱). 업그레이드 시 `maxClients()`(라이브 조회, `>0`이면) 초과면 `client_limit_exceeded` 전송 후 닫는다. `StartWithHandler`가 `WEB_CERT`/`WEB_KEY` 있으면 `:8443` TLS 리스너도 띄운다.
- `handler.go` — `connState{conn, wmu(쓰기 직렬화), cancels: map[cameraID]cancelFunc}`. 하트비트 30초 ticker로 WS ping 프레임. `pump`가 `stream.Event`를 WS 프레임으로 변환하며, `PacketEvent`가 오면 `for len(batch) < 128`로 채널의 대기 이벤트를 탐욕적으로 더 꺼내 배치를 채운다.
- `protocol.go` — 메시지 계약 (§5.2 표).

### 3.7 internal/api

- `app.go`
  - `New(configDir)` — `config.Load` → `Validate` → `logging.Setup` → `camera.NewJSONCameraStore` → `NewManager` → `ensureMasterKey` → `CameraService` + `NewStreamService`.
  - `ensureMasterKey` — 위 우선순위대로 키 확보. 키 파일이 없고 env도 없으면 32바이트 생성 → `.masterkey`(0600) 저장 → **폴백 키로 암호화돼 있던 기존 비밀번호를 복호화 후 새 키로 재암호화**(마이그레이션).
  - `StartWSServer` — 위 §2.2. `registerUI`는 `assets != nil`일 때만 등록하며 없는 경로는 `index.html`로 SPA 폴백.
  - `LANAddresses(port)` — 비루프백 IPv4 접속 URL 목록(로그 안내용).
- `camera.go` — `CameraService`. 목록/단건/생성/수정/삭제/순서, ONVIF 검색/연결 테스트/프로필/스트림 URI/프리셋, 직접 스트림 테스트, 설정 조회·교체(`UpdateAppConfig` = 전체 교체), 백업 내보내기/복원, 보안 상태. 등록 카메라의 ONVIF 호출은 `onvifCall<T>` 제네릭 헬퍼가 `mgr.Get` → `PasswordOf`(복호화) → 프로필 토큰 결정 → 콜백 순으로 처리하며 자격증명이 프론트로 나가지 않는다.
- `stream.go` — `StreamService`가 스스로 `stream.CameraSource`이자 `StreamFailureNotifier`다.
  - `StreamURL` — RTSP 직접 타입은 URL 그대로, ONVIF는 `uriCache`(TTL 30s) 확인 후 미스면 ONVIF 재조회.
  - `OnStreamFailed` — dial 실패/비정상 종료 시 캐시 폐기 → 다음 시도가 현재 포트를 다시 조회(카메라 서버 재시작으로 RTSP 포트가 바뀌는 경우 대응, §7 F16).
  - `Start`/`StartAll`/`Subscribe`/`PTZ` — `ws.Controller` 구현. PTZ는 `PTZSupported` 아니면 거부, 5초 컨텍스트, `move`/`stop`/`preset` 분기.
- `httpapi.go` — 라우트(§5.1) + 미들웨어. 오류 매핑 — 검증 실패/잘못된 JSON = 400, 미존재(`errNotFound` 또는 메시지에 "카메라를 찾을 수 없음") = 404, 메서드 불일치 = 405, 그 외 = 500.

### 3.8 internal/logging

`Setup(cfg)` — `MkdirAll(dir)` → `lumberjack.Logger{Filename, MaxSize: MaxSizeMB, MaxBackups, LocalTime: true}`(MaxAge/Compress 없음) → `io.MultiWriter(lumberjack, os.Stderr)` → `slog.NewTextHandler(level)` → `slog.SetDefault`. 반환값(`io.Closer`)은 앱 종료 시 `OnShutdown`/`appCtx.Close`에서 닫힌다.

### 3.9 third_party/gortsplib 패치

벤더링(`go.mod`의 `replace ... => ./third_party/gortsplib`)한 v4.16.2에 `// webnvr patch` 3곳을 넣었다.

1. `client.go` — `Client`에 `AllowSSRCChange bool` 필드 추가.
2. `client_format.go` — `clientFormat.start()`가 이 플래그를 per-format `rtcpreceiver.RTCPReceiver`로 전파.
3. `pkg/rtcpreceiver/rtcpreceiver.go` `ProcessPacket2` — `pkt.SSRC != remoteSSRC`일 때 상위는 무조건 오류를 반환해 세션을 죽인다. 패치는 `AllowSSRCChange`면 오류 대신 `rr.remoteSSRC = pkt.SSRC`로 **새 SSRC를 채택**한다.

**이유** — PythonCam 등 일부 카메라 서버는 새 클라이언트 접속 시 인코더를 재시작해 SSRC를 바꾼다.
표준 gortsplib는 이를 치명 오류로 처리해 기존 세션이 전부 죽었고, 프론트 리컨실리어가 재시작하면 서버가 또 인코더를 재시작하는 핑퐁이 생겼다 (§7 F17). `rtsp_client.go`가 `AllowSSRCChange: true`를 무조건 설정한다.

---

## 4. 프론트엔드 (상세)

> 경로는 모두 `frontend/src/` 기준이다.

### 4.1 앱 셸 & 내비게이션

- **`main.tsx`** — `@fontsource`로 IBM Plex Sans KR(400/500/700) + JetBrains Mono(400/600)와 `./app.css`를 로드하고 `<App/>`를 `<React.StrictMode>` 안에서 `#root`에 마운트한다. Provider도 Router도 없다.
- **`App.tsx`** — react-router 없이 **스토어 기반 페이지 전환**이다. `useUIStore`의 `currentPage`를 보고 `MonitoringPage` / `CameraManagementPage` / `SettingsPage` 중 하나만 조건부 마운트한다. `Escape` 키는 가장 최근 토스트를 닫는다. `useEffect(() => streamStore.init(), [])`로 **WS 연결을 앱 전역에서 1회 초기화**하므로 어느 페이지에서도 `streamStore.connected`가 유효하다(관리 페이지의 `네트워크 검색`·`카메라 추가` 버튼이 이 값으로 disable 된다).
  - 레이아웃 = `.shell` CSS 그리드. `grid-template-columns: 60px 1fr`, `grid-template-areas: "rail header" / "rail main" / "rail status"`.
  - 상시 마운트 요소 — `<ClientLimitOverlay/>`·`<ReconnectingOverlay/>`(둘 다 스스로 숨김), `<Sidebar/>`(rail), `<Toolbar/>`(header), `<main>`(스크롤 영역), `<StatusBar/>`(status), `.toasts`(우하단 고정, `aria-live="polite"`).
- **`components/layout/Sidebar.tsx`** — 60px 아이콘 레일. 상단 브랜드 `WEBNVR`, 항목 3개 — `모니터링`(IconGrid) / `카메라 관리`(IconCamera) / `설정`(IconSettings). 클릭 시 `setPage`. 라벨은 `title`/`aria-label`로만 노출(아이콘 전용), 활성 항목에 `aria-current="page"`.
- **`components/layout/Toolbar.tsx`** — 항상 `.header-eyebrow` + `<h1>`를 그리고(페이지별 `titles` 맵: `Live Grid`/`모니터링`, `Camera Registry`/`카메라 등록부`, `Preferences`/`설정`), 페이지에 따라 컨트롤을 붙인다.
  - `management` — `등록 NN · 사용 NN` 카운트 + `카메라 추가` 기본 버튼(`openCameraModal({mode:'add'})`).
  - `monitoring`(카메라가 있을 때) — `스트리밍 NN / NN` + 전체 kbps/fps/드롭 요약, 분할 버튼 그룹 `[A][1][4][9][16][25]`(`GRID_OPTIONS`, 활성 항목 `.btn-active` 앰버색), 페이지가 여럿이면 `‹ page/total ›` 페이저, 전체화면 토글(`IconFull`).
- **`components/layout/StatusBar.tsx`** — **현재는 정적 플레이스홀더**다. `WS 127.0.0.1:8080`, `● 백엔드 연결됨`, `스트림 0/0`, `webnvr v0.1`을 하드코딩하며 스토어를 구독하지 않는다(실시간화는 미완).

### 4.2 컴포넌트 트리

```
App
├─ ClientLimitOverlay              streamStore.rejected/retryAt, 60초 카운트다운 전면 차단
├─ ReconnectingOverlay             streamStore.connected/reconnectAt, 미연결 1.2s 지속 시 "접속 중입니다" + 재시도 카운트다운
├─ Sidebar                         uiStore.currentPage/setPage
├─ Toolbar                         페이지별 컨트롤 (분할/페이저/전체화면/카메라 추가)
├─ main
│  ├─ MonitoringPage
│  │  ├─ (배너) WS 끊김 / lastError
│  │  ├─ CameraGrid
│  │  │  └─ CameraTile × N        canvas + VideoRenderer, OSD 오버레이, Sparkline(fps)
│  │  └─ PTZControl               showPtz일 때만 (선택된 PTZ 카메라)
│  ├─ CameraManagementPage
│  │  ├─ DiscoveryPanel           WS-Discovery 스캔 → 결과 칩 → 추가 프리필
│  │  ├─ CameraList               @dnd-kit 정렬 그리드
│  │  │  └─ CameraCard × N        OSD 카드, 사용 토글, 2단계 삭제, 드래그 핸들
│  │  └─ CameraModal              add/edit
│  │     └─ CameraForm
│  │        ├─ ProfileSelector    ONVIF 프로필 조회/선택
│  │        └─ ConnectionTest     테스트 상태 표시
│  └─ SettingsPage                앱 설정 / 보안 / 백업·복원
└─ .toasts                        uiStore.toasts (4200ms 자동 해제)
```

### 4.3 페이지

#### 4.3.1 MonitoringPage

- WS 연결/메시지 라우터는 `App.tsx`에서 전역 초기화한다(§4.1). MonitoringPage는 별도로 `init()`을 호출하지 않는다.
- `useEffect([cameras, ...])` — 활성 카메라 id 집합(`appliedRef`)을 추적해 **추가/삭제분만** `startStream`/`stopStream`. 언마운트 전용 effect가 남은 구독을 정리. (이름 변경 등에 전 채널이 깜빡이지 않도록.) 다른 클라이언트는 백엔드 참조 카운팅 덕에 무영향.
- 보던 카메라가 다른 클라이언트에 의해 삭제되면 `selectedId`를 정리하는 effect.
- `selectedId`(로컬) — 타일 클릭으로 토글. `selectedCamera.ptzSupported && state === 'streaming'`이면 하단에 `<PTZControl/>` 섹션 표시(닫기 버튼).
- 배너 — `!connected`면 `백엔드와 연결이 끊겼습니다. 자동으로 재연결 중…`, `lastError`면 메시지 + `닫기`.
- 빈 상태(`cameras.length === 0`) — `등록된 카메라가 없습니다` + `카메라 관리로 이동` 버튼.

#### 4.3.2 CameraManagementPage

`<DiscoveryPanel/>` + (`loaded` ? `<CameraList/>` : `카메라 목록을 불러오는 중…`) + (`cameraModal` ? `<CameraModal/>`). 마운트 시 `fetchCameras()`, 실패하면 토스트.

#### 4.3.3 SettingsPage

세 개의 `.discovery` 섹션.

1. **앱 설정** — `api.appConfig()`로 불러온 `cfg`를 `.settings-grid`에 편집(WS 포트/최대 접속/전송 방식/지터 버퍼/최대 스트림/검색 인터페이스/HW 디코딩). `설정 저장`(`api.updateAppConfig`)은 `설정이 저장되었습니다. 일부 항목(ws_port 등)은 앱 재시작 후 적용됩니다.` 토스트.
2. **보안** — `api.security()`의 `masterKeySource`를 `환경변수 (WEBNVR_MASTER_KEY)` / `키 파일 (config/.masterkey)` / `개발용 폴백 키`로 표시. `fallback`이면 `test-fail` 스타일 + 안내문.
3. **백업 및 복원** — `백업 내보내기 (JSON)`(`api.backup()` → `<a download>`), `백업 가져오기`(파일 선택 → `api.restoreBackup`). 안내 — 복원 시 현재 목록이 대체되고 비밀번호는 백업에 없어 재입력이 필요하다.

### 4.4 카메라 관리 컴포넌트

| 컴포넌트 | 역할 · 주요 동작 |
|----------|----------------|
| `CameraList` | `@dnd-kit`(`PointerSensor` distance 4, `KeyboardSensor`) 정렬 그리드. `onDragEnd`에서 `arrayMove` 후 `reorderCameras(ids)` → `표시 순서가 변경되었습니다.`. 빈 상태 안내. `activeId`로 드래그 중 카드 흐림. |
| `CameraCard`(내부) | OSD 카드 — 모서리 꺾쇠 4개, `CH NN` 플레이트, 타입 배지, 상태 점(`사용`/`중지`), 이름/호스트, 메타 태그(`PTZ`/전송/`인증 저장됨`/`#group`). 사용 토글은 `updateCamera({enabled})` 낙관적. 삭제는 **2단계** — `삭제` → `삭제 확인`/`취소`. 드래그 핸들 `IconGrip`. 편집 → `openCameraModal({mode:'edit'})`. |
| `CameraModal` | `.overlay` + `.modal`(`role="dialog"`). 배경 클릭 시 닫힘. `handleSubmit` — add는 `addCamera`, edit는 `updateCamera`(비밀번호는 **입력값이 있을 때만** 요청에 포함, `profileToken`은 ONVIF일 때만). 성공/실패 토스트 후 `closeCameraModal`. |
| `CameraForm` | 타입 라디오(`ONVIF`/`RTSP`/`RTP`/`RTMP`) → 필드 동적 표시. `canSubmit`(메모)와 `validate`(한국어 오류 목록)로 제출 게이트. `runTest` — ONVIF는 `testONVIF`, 직접은 `testDirectStream`. `previewURI`(edit+ONVIF) — `getCameraStreamURI`. 아무 편집이나 하면 이전 테스트 결과 무효화. |
| `DiscoveryPanel` | `네트워크 검색` 버튼 → `discover()`. 약 3초 스캔. 결과 없으면 `검색 결과가 없습니다…` 토스트. 각 결과는 IP + 이름 + `추가` 버튼(`openCameraModal({mode:'add', presetXAddr})`). |
| `ProfileSelector` | `profiles === null`이면 `프로필 불러오기` 버튼만. 로드 후 `role="listbox"`로 프로필 목록(이름/토큰/해상도), 선택 없으면 첫 항목 자동 선택. 등록 카메라는 `getCameraProfiles(id)`(저장 자격증명), 미등록은 `getProfiles({xaddr,username,password})`. |
| `ConnectionTest` | 순수 표시. `TestState` 유니온 — `idle`(null) / `testing`(`연결을 확인하는 중… (최대 10초)`) / `onvif-ok`(제조사·모델·펌웨어) / `direct-ok`(`스트림 주소에 연결됨`) / `fail`(원인 + `다시 시도`). |

### 4.5 모니터링 컴포넌트

| 컴포넌트 | 역할 · 주요 동작 |
|----------|----------------|
| `CameraGrid` | `gridMode ∈ 'auto' | 1 | 4 | 9 | 16 | 25`. `slotsFor`(auto = `max(len,1)`, 그 외 = mode), `colsFor`(auto = `ceil(sqrt(slots))`, 그 외 = `round(sqrt(mode))` → 1/2/3/4/5). 페이지네이션 `totalPages = ceil(len/slots)`, `visible = ordered.slice(offset, offset+slots)`. 더블클릭 = `zoomToggle(offset+i, len)` (별도 포커스 상태 없음, §4.6 uiStore). |
| `CameraTile` | `role="button"`, 클릭 = 선택, 더블클릭 = 줌 토글, Enter = 선택. OSD 리티클(모서리 꺾쇠), `.tile-head`(`CH NN` + 이름 + fps 스파크라인 + 상태), `.tile-canvas-wrap`(`<canvas>` + `VideoRenderer` + 상태별 오버레이), `.tile-foot`(호스트 + 해상도 + 타입/PTZ 배지). 프레임은 `window` `'webnvr-frame'` CustomEvent를 자기 `cameraId`로 필터해 받는다. 상태별 오버레이 문구 — `레이아웃에서 제외됨` / `스트림 대기 중` / `카메라에 연결하는 중… (첫 키프레임 대기)` / `WebGL을 사용할 수 없습니다` / `스트림 오류 — 자동 재연결 중…` / `재연결 시도 중…`. |
| `PTZControl` | `SEND_INTERVAL_MS = 200`, `DEADZONE = 0.15`. 조이스틱 패드 — 포인터 위치를 `[-1,1]`로 정규화(데드존 내면 0), 200ms 간격으로 `ptzControl({action:'move', pan, tilt})`, 놓으면 `{action:'stop'}`. 속도 슬라이더(0.1~1). 줌 `+`/`−` 버튼(누르면 move, 떼면 stop). 프리셋 — `프리셋 불러오기` → 없으면 `저장된 프리셋이 없습니다`, 있으면 `<select>`로 `{action:'preset', presetToken}`. |
| `VideoRenderer` | 컴포넌트가 아닌 클래스. WebGL2, 단일 텍스처 RGBA 패스스루 셰이더(Y 뒤집기). `draw(frame)` — `texImage2D(TEXTURE_2D, ..., frame)` 직접 업로드, 예외 시 첫 회는 1회 로그 후 스킵하고 이후로는 2D `drawImage`로 우회 캔버스에 그린 뒤 업로드. 레터박스 — `scale = min(pw/fw, ph/fh)`로 중앙 `gl.viewport`. 실패 시 `ready === false`(타일이 `WebGL을 사용할 수 없습니다` 표시). |
| `Sparkline` | 외부 차트 라이브러리 없이 `<polyline>` 하나. `metric ∈ 'fps'|'kbps'`. 샘플 2개 미만이면 빈 SVG. `CameraTile`은 `metric="fps" width={56} height={14}`. |

### 4.6 상태 관리 (Zustand)

#### `store/cameraStore.ts` — `useCameraStore`

- 상태 — `cameras: CameraDTO[]`, `loaded`(첫 조회 후 true), `discovered: DiscoveredCamera[]`, `isDiscovering`, `isBusy`.
- `fetchCameras` / `discover` — 서버 조회.
- `addCamera` / `updateCamera` / `deleteCamera` / `reorderCameras` — **낙관적 업데이트**. `cameras`를 즉시 바꾸고, 실패하면 스냅샷으로 롤백 후 rethrow(호출자가 토스트). `updateCamera`는 `password != null`이면 `hasPassword = true`도 반영.
- `testONVIF` / `testDirectStream` / `getProfiles` / `getCameraProfiles` / `getCameraStreamURI` / `getCameraPresets` — `api.ts` 위임.
- `selectOrderedCameras(cameras)` = `[...cameras].sort((a,b) => a.layoutOrder - b.layoutOrder)`.
- `defaultStreamConfig()` = `{transport:'tcp', protocol:'rtsp', buffer_size: 1048576}`.

#### `store/uiStore.ts` — `useUIStore`

- 상태 — `currentPage`(초기 `'monitoring'`), `cameraModal`, `toasts`, `gridMode`(초기 `'auto'`), `gridPage`(0-based), `zoomReturnMode: GridMode | null`.
- `pushToast(kind, text)` — `{id, kind, text}` 추가 + 4200ms 후 자동 해제.
- `setGridMode(m)` — `{gridMode: m, gridPage: 0, zoomReturnMode: null}`. **분할 버튼을 누르면 확대가 해제된다.**
- `zoomToggle(cameraIndex, orderedLen)` — 확대가 아니면(`zoomReturnMode === null`) `{zoomReturnMode: gridMode, gridMode: 1, gridPage: cameraIndex}`로 해당 카메라 1분할 확대. 확대 중이면 `prevSlots = prev==='auto' ? max(len,1) : prev`로 `{zoomReturnMode: null, gridMode: prev, gridPage: floor(gridPage/prevSlots)}` — 원래 분할 모드와 그 카메라가 있던 페이지로 복귀. 별도 포커스 상태 없이 `gridMode`/`gridPage`만으로 확대를 표현하므로 상단 분할 버튼·페이저가 확대 상태와 항상 일치한다.

#### `store/streamStore.ts` — `useStreamStore`

- 상태 — `states`(cameraId → `idle`/`starting`/`streaming`/`error`), `stats`, `history`(카메라별 최근 `STATS_HISTORY_MAX = 60` 샘플), `resolution`, `desired`(재생 의사 — 리컨실리어의 목표), `retries`(자동 재연결 시도 횟수), `connected`, `lastError`, `rejected`, `retryAt`.
- 모듈 레벨 리컨실리어 — `RETRY_BASE_MS = 1000`, `RETRY_MAX_MS = 60000`, `ATTEMPT_TIMEOUT_MS = 20000`, `retryDelayMs(n) = min(1000 * 2^min(n-1,4), 60000)`. `ensureReconciler()`가 1초 `setInterval` 시작.
- `tickReconciler()` — `desired[id] === true`인 카메라마다 — `streaming`이면 재시도 카운터 리셋. `starting`이고 경과 < 20초면 건드리지 않음(진행 중). `error`/미정의이고 경과 < 백오프면 대기. 그 외에는 재시도 — `lastAttemptAt` 기록, `retries++`, `getHub().reset(id)`, `wsService.send({type:'start_stream', cameraId})`.
- `getHub()` — `DecoderHub` 싱글턴을 지연 생성하며 콜백을 연결한다.
  - `onFrame(id, frame)` → `dispatchEvent(CustomEvent('webnvr-frame', {detail}))` (프레임을 스토어에 보관하지 않아 GC 부담 최소).
  - `onDecoded(id)` → `states[id] = 'streaming'`, `retries[id] = 0`, `lastError = null`.
  - `onNotice(id, msg)` → `lastError = msg` (자가 치유 중, 상태는 유지).
  - `onError(id, msg)` → `states[id] = 'error'`, `lastError = msg`.
  - `onStats(id, s)` → `stats[id]` 갱신 + `history[id]`에 `{fps, kbps}` 추가(60개로 트림).
- `init()` — `wsService.on/onStatus/onRejected` 등록 + `connect()` + `ensureReconciler()`. cleanup에서 리스너 해제.
  - 메시지 라우터 `switch(msg.type)` — `stream_started`(`hub.detach` → `attach` + `config({codec, sps, pps, vps, clockRate})`, `states='starting'`, `resolution` 저장. 화면 전환은 `onDecoded`에서), `rtp_packet`/`rtp_batch`(각 패킷을 `PacketIn`으로 → `hub.packet`), `stream_stopped`(`hub.detach` + 상태 삭제), `stream_error`(`states='error'`), `cameras_changed`(`added`/`deleted` → 디바운스 `fetchCameras()`, 그 외 → `pendingCameraUpdate` 누적), `config_changed`(`webnvr-config-changed` 커스텀 이벤트 디스패치), `pong`(무시).
  - `pendingCameraUpdate {count, ids, reloadAll}` + `applyCameraUpdate()` — 툴바 배지 클릭 시 `fetchCameras()` 후 변경 카메라에 `reload_stream` 전송.
  - `onStatus(connected)` — 끊기면 전 세션 detach + `states/stats/history` 삭제(`desired`는 유지 → 재연결 시 리컨실리어가 복구). 재연결 시 백오프/`lastAttemptAt` 초기화로 즉시 복구.
  - `onRejected(retryAt)` — `rejected = true` (`ClientLimitOverlay` 표시).
- `startStream` / `stopStream` / `startAllStreams` / `stopAllStreams` / `ptzControl` — 각각 `desired` 설정/해제 + WS 송신 + Hub attach/detach.

### 4.7 서비스

#### `services/api.ts` — REST 클라이언트

- `BASE = backendBase()`를 모듈 로드 시 1회 계산.
- `request<T>(method, path, body?)` — 네트워크 실패 시 `백엔드 서버(${BASE})에 연결할 수 없습니다. 앱이 실행 중인지 확인하세요.`, non-OK면 본문의 `{error}` 또는 `HTTP {status}`를 throw, `204`면 `undefined`.
- 엔드포인트 — `health / listCameras / getCamera / createCamera / updateCamera / deleteCamera / reorderCameras / discover / testONVIF / testDirectStream / onvifProfiles / cameraProfiles / cameraPresets / cameraStreamURI / appConfig / updateAppConfig / security / backup / restoreBackup` (매핑은 §5.1).

#### `services/ws.ts` — WebSocket 싱글턴

- `HEARTBEAT_MS = 25000`, `MAX_BACKOFF_MS = 30000`, `REJECT_RETRY_MS = 60000`, 초기 `backoff = 1000`.
- `connect()` — `backendWS()` URL. `open` 시 `backoff` 리셋 + 하트비트 시작 + 큐 flush. `message`에서 `client_limit_exceeded`면 `backoff = 60000`, `retryAt = now + 60000`, reject 핸들러 통지 후 반환(포워딩 안 함). 그 외는 모든 핸들러로 전달. `close` 시 `scheduleReconnect()`.
- `send(msg)` — 소켓 OPEN이면 즉시 전송. 아니면 `ptz`/`ping`은 **버리고**(일회성) 나머지는 큐잉.
- `scheduleReconnect()` — `delay = backoff` 후 `connect`, `backoff = min(backoff*2, 30000)` (지수 백오프, 캡 30초. 거절 유발 시엔 60초로 선설정됨).

#### `services/backend.ts` — 백엔드 주소 결정

- `backendHost()` — `location.hostname` 소문자. 빈 값이거나 `wails.localhost`/`*.wails.localhost`(Wails 가상 호스트, 라우팅 불가)면 `'127.0.0.1'`, 그 외엔 그대로(LAN IP / localhost / 동일 오리진).
- `backendPort()` — `location.protocol === 'https:' ? 8443 : 8080` (**포트 하드코딩** — 외부 접근 시 주의, §9.3).
- `backendBase()` / `backendWS()` — `${http|https}://host:port` / `${ws|wss}://host:port/ws`.

#### `services/decoder.ts` — 디코드 파이프라인 (메인 스레드)

- 튜너블 — `TICK_MS = 30`(큐 처리 주기), `MAX_QUEUE = 1200`(버스트 흡수), `STATS_MS = 1000`, `STALL_MS = 4000`(키프레임 후 무출력 → 포맷 전환), `STALL_GIVEUP_MS = 12000`(모든 포맷 시도 후 포기).
- 청크 포맷 — `FORMAT_ANNEXB = 0`(description 없음, 시작코드 청크 — 대상 WebKit 메인 스레드에서 동작), `FORMAT_AVCC = 1`(`description = avcC` + 길이 접두 청크 — Annex B 실패 환경 폴백). 모듈 레벨 `lastGoodFormat`이 마지막 성공 포맷을 기억해 재접속 시 처음부터 사용(스톨 재발 방지).
- `class Session` — 카메라 1개 파이프라인.
  - `push(p)` — 지터 버퍼. 중복 시퀀스/늦은 패킷 드롭(16비트 랩어라운드 비교), 도착 순 push, `queue.length > 1200`이면 앞에서 초과분 splice.
  - `flush()` — `TICK_MS`마다 `DecoderHub`가 호출. 큐 전체를 `processPacket`으로 흘리고 `watchdog()`.
  - `depacketize` — H.264 단일 NAL / STAP-A(24) / FU-A(28 재조립), H.265 단일 NAL / AP(48) / FU(49 재조립). 조립 중이면 `null`.
  - `completeFrame` — 첫 키프레임 전 델타 프레임 폐기. 디코더 미준비면 키프레임을 `heldKey`에 보관.
  - `decodeFrame` — AVCC면 길이 접두, Annex B면 키프레임에 VPS/SPS/PPS를 시작코드로 삽입. `EncodedVideoChunk` 생성 후 `decoder.decode`. 첫 프레임에서 `lastGoodFormat` 저장 + `onDecoded`.
  - `watchdog` — `frames === 0`일 때만. 키프레임 후 `> STALL_MS` → 포맷 전환 + `디코딩 출력이 없어 청크 포맷을 전환했습니다 (Annex B|AVCC)` notice. 포맷 소진 + `> STALL_GIVEUP_MS` → `키프레임 이후 12초간 프레임이 출력되지 않았습니다 …` 오류. 키프레임 없이 `> 20s` → `20초간 키프레임을 수신하지 못했습니다 — 카메라의 GOP … 확인` 오류.
- `class DecoderHub` — 세션 맵 + 타이머(`TICK_MS`마다 전 세션 `flush`, `STATS_MS`마다 `onStats`). `attach`/`config`/`packet`/`detach`/`reset`(= detach 후 attach, 모든 (재)시작 전에 호출).

### 4.8 타입

- **`types/api.ts`** — 백엔드 `/api/*` JSON과 1:1 DTO. `CameraDTO`(비밀번호 없음, `hasPassword: boolean`, 타임스탬프는 문자열), `CreateCameraRequest`, `UpdateCameraRequest`(전 필드 옵셔널), `DiscoveredCamera`, `TestONVIFRequest/Response`, `ProfileDTO`, `TestDirectStreamRequest/Response`, `PresetDTO`, `HealthResponse`, `AppConfig`, `SecurityInfo{masterKeySource}`, `BackupFile`, `RestoreResult`.
- **`types/stream.ts`** — `PacketIn{seq, ts, marker, payload: Uint8Array}`, `StreamStats{fps, kbps, packets, drops}`, `StatSample{fps, kbps}`, `StreamState = 'idle' | 'starting' | 'streaming' | 'error'`.
- **`types/index.ts`** — `ClientMsg` / `ServerMsg`(WS 프로토콜, §5.2), `PTZCommand{action:'move'|'stop'|'preset', pan?, tilt?, zoom?, presetToken?}`, `RTPPacketItem{p, ts, m?, sq}`, `GridMode = 'auto' | 1 | 4 | 9 | 16 | 25`.

### 4.9 UX 패턴

- **낙관적 업데이트 + 롤백** — 카메라 수정/삭제/순서 변경은 UI를 즉시 바꾸고 실패 시 스냅샷 복원 후 rethrow.
- **토스트** — 모든 사용자 피드백의 단일 창구. 4200ms 자동 해제, `Escape`로 최신 1개, 클릭으로 1개. 종류 `ok`/`error`/`info`.
- **인라인 확인** — 카메라 삭제는 모달 없이 `삭제 확인`/`취소` 2단계 스왑.
- **빈 상태** — MonitoringPage(등록 카메라 없음 + 이동 버튼), CameraList(안내문), DiscoveryPanel(토스트), ProfileSelector, PTZ 프리셋.
- **로딩 상태** — `카메라 목록을 불러오는 중…`, `설정을 불러오는 중…`, 버튼 라벨 스왑(`검색 중…`/`조회 중…`/`저장 중…`).
- **오류/재시도** — MonitoringPage 배너(WS 끊김·`lastError`), 타일 오버레이 문구, `ConnectionTest`의 `다시 시도`, WS 지수 백오프 재연결, 스트림 리컨실리어 자동 복구. 백엔드 미연결 시 `네트워크 검색`·`카메라 추가` 버튼은 `connected`로 disable 되고, WS가 (재)연결되면 `App.tsx`가 `fetchCameras()`를 다시 호출해 목록·모니터링·스트림이 새로고침 없이 복구된다.
- **동시 접속 제한** — `client_limit_exceeded` 수신 시 `ClientLimitOverlay`가 60초 카운트다운으로 전면 차단(`접속자가 많아` / `지금은 지원 할 수 없습니다.` / `잠시만 기다려 주십시오` / `N초 후 재시도`).
- **백엔드 미연결** — WS가 끊긴 상태가 1.2초(`GRACE_MS`) 이상 지속되면 `ReconnectingOverlay`가 `접속 중입니다` + 다음 재시도까지 `N초 후 재시도` 카운트다운을 전면 표시한다. `reconnectAt`은 `scheduleReconnect`가 `Date.now() + 백오프`로 계산해 `onStatus(false, reconnectAt)`로 전달한다. `rejected` 상태에서는 숨는다(위 오버레이가 담당). 짧은 재시작·앱 시작 시 순간 미연결에는 grace 때문에 뜨지 않는다.
- **접근성** — `role="dialog"/"radiogroup"/"listbox"/"option"/"application"`, `aria-live="polite"` 토스트, `aria-current="page"`, `:focus-visible` 링, `prefers-reduced-motion` 존중.

### 4.10 디자인 시스템 (frontend-design 스킬 적용, context-notes D13)

- **주제 고정** — 주차장 출입구를 감시하는 소규모 CCTV 관제 앱. 사용자는 시설 관리자. 화면의 일 = 카메라를 찾아 등록하고 모니터링 순서를 정한다.
- **팔레트** — `app.css` `:root`, 관제실 야간 테마이며 dark + acid-green 기본값을 의도적으로 회피한다.

  | 토큰 | 값 | 용도 |
  |------|----|------|
  | `--ink` | `#0f141b` | 관제실 야간 배경 |
  | `--panel` | `#151c26` | 패널 |
  | `--panel2` | `#1b2432` | 상승 패널 / hover |
  | `--line` | `#263140` | 헤어라인 |
  | `--text` | `#dce3ee` | 본문 |
  | `--dim` | `#8494a7` | 보조 텍스트 |
  | `--amber` | `#e8b45a` | 주 인터랙션 — 주차장 나트륨 가로등 색 |
  | `--amber-press` | `#c9983f` | 눌림 |
  | `--rec` | `#e5484d` | REC / 오프라인 |
  | `--ok` | `#3ecf8e` | 온라인 |

- **서체** — `--font-ui: "IBM Plex Sans KR", system-ui, sans-serif` (계기판 산세리프, 400/500/700), `--font-mono: "JetBrains Mono", ui-monospace, monospace` (채널 플레이트·텔레메트리, 400/600). `@fontsource`로 번들(오프라인 데스크톱 앱이라 CDN 금지).
- **시그니처** — 카메라 OSD 리티클. 카드/타일 네 모서리 꺾쇠 괄호 + `CH NN` 모노 채널 플레이트. `CH` 번호는 `layout_order`(모니터링 표시 순서)라 장식이 아니라 실제 정보를 인코딩한다. 카메라 관리 카드와 모니터링 타일이 같은 언어를 재사용한다.
- **레이아웃** — `.shell` CSS 그리드(좌측 60px 레일 + 헤더 + 본문 + 상태바). 모니터링 그리드는 `repeat(cols, minmax(0,1fr))`, gap 4px.
- **카피 원칙** — 능동형 버튼(`카메라 추가`, `연결 테스트`), 빈 상태는 행동 유도, 오류는 원인 + 재시도.

---

## 5. 인터페이스 계약

### 5.1 HTTP REST (`/api/*`)

기본 URL은 `backendBase()`(네이티브 `http://127.0.0.1:8080`, 브라우저는 접속 오리진). 전 오리진 CORS 허용, `OPTIONS` → 204.

| 메서드 | 경로 | 용도 | 반환 / 상태코드 |
|--------|------|------|-----------------|
| GET | `/api/health` | 생존 확인 | `{ok:true, version:1}` · 비GET 405 |
| GET | `/api/cameras` | 카메라 목록 (`layout_order` 순) | `200 CameraDTO[]` |
| POST | `/api/cameras` | 카메라 추가 | `201 CameraDTO` · 검증 실패 400 |
| GET | `/api/cameras/{id}` | 단건 조회 | `200 CameraDTO` · 없음 404 |
| PUT | `/api/cameras/{id}` | 수정 (비밀번호 입력 시 재암호화) | `200 CameraDTO` · 404 / 400 |
| DELETE | `/api/cameras/{id}` | 삭제 | `200 {ok:true}` · 404 |
| POST | `/api/cameras/reorder` | 순서 변경 `{ids:[]}` | `200 {ok:true}` · 400 · 비POST 405 |
| POST | `/api/cameras/discover` | ONVIF WS-Discovery | `200 DiscoveredCamera[]` · 비POST 405 |
| POST | `/api/cameras/test-onvif` | 미등록 카메라 ONVIF 연결 테스트 (본문에 자격증명) | `200 TestONVIFResponse` (실패도 200 본문) |
| POST | `/api/cameras/test-direct` | 직접 스트림 URL 검증 (rtsp/rtmp = TCP 도달성, rtp = 형식) | `200 TestDirectStreamResponse` |
| POST | `/api/onvif/profiles` | 미등록 카메라 프로필 조회 (본문에 자격증명) | `200 ProfileDTO[]` · 비POST 405 |
| GET | `/api/cameras/{id}/profiles` | 등록 카메라 프로필 (저장 자격증명) | `200 ProfileDTO[]` |
| GET | `/api/cameras/{id}/presets` | PTZ 프리셋 목록 (자격증명 미노출) | `200 PresetDTO[]` |
| GET | `/api/cameras/{id}/stream-uri` | RTSP URI 조회 | `200 {uri:"rtsp://..."}` |
| GET | `/api/config` | 앱 설정 조회 | `200 AppConfig` |
| PUT | `/api/config` | 앱 설정 전체 교체 (검증 후 저장, `ws_port`는 재시작 필요) | `200 AppConfig` · 400 · 그 외 405 |
| GET | `/api/security` | 마스터 키 출처 | `200 {masterKeySource:"env"|"file"|"fallback"}` · 비GET 405 |
| GET | `/api/backup` | 백업 내보내기 (비밀번호 제외) | `200 BackupFile` · 비GET 405 |
| POST | `/api/backup/restore` | 복원 (기존 전체 삭제 후 순서/사용여부 유지, ID 재발급) | `200 {ok:true, restored:N}` · 400 |

**자격증명 규칙** — 미등록 카메라의 연결 테스트·프로필 조회만 본문으로 자격증명을 받는다. 등록된 카메라는 항상 카메라 ID + 서버 측 `PasswordOf` 복호화를 쓰며 비밀번호가 응답에 실리지 않는다(`CameraDTO`는 `hasPassword`만).

### 5.2 WebSocket (`/ws`) — JSON

**클라이언트 → 서버** (`protocol.go`)

| 상수 | 문자열 | 동작 |
|------|--------|------|
| `MsgStartStream` | `start_stream` | 카메라 시작 + 구독 |
| `MsgStopStream` | `stop_stream` | 이 클라이언트 구독 해제 + `stream_stopped` |
| `MsgStartAllStreams` | `start_all_streams` | `ctrl.StartAll()` |
| `MsgStopAllStreams` | `stop_all_streams` | **이 연결의 구독 전체 해제** (전역 정지 아님) |
| `MsgPTZ` | `ptz` | `ctrl.PTZ` (`command` 없으면 오류) |
| `MsgRequestKeyframe` | `request_keyframe` | 미구현 → `stream_error` |
| `MsgSubscribe` | `subscribe` | `start_stream` 별칭 |
| `MsgUnsubscribe` | `unsubscribe` | 구독만 해제 |
| `MsgReloadStream` | `reload_stream` | 실행 중인 RTSP 세션 강제 종료 → 리컨실리어가 새 설정으로 재다이얼 (설정 변경 반영) |
| `MsgPing` | `ping` | → `pong` |

**서버 → 클라이언트**

| 상수 | 문자열 | 발행 | 페이로드 |
|------|--------|------|----------|
| `MsgStreamStarted` | `stream_started` | O | `cameraId, codec, ssrc, clockRate, payloadType, sps, pps, vps(base64), width, height` |
| `MsgRTPPacket` | `rtp_packet` | **미발행** (배치로 대체) | `cameraId, payload(base64), timestamp, marker, sequence` |
| `MsgRTPBatch` | `rtp_batch` | O | `type, cameraId, codec, packets: {p, ts, m?, sq}[]` (≤128) |
| `MsgStreamStopped` | `stream_stopped` | O | `cameraId, reason` |
| `MsgStreamError` | `stream_error` | O | `cameraId, error` |
| `MsgCamerasChanged` | `cameras_changed` | O (카메라 CRUD/재정렬/복원) | `reason`(added\|updated\|deleted\|reordered\|restored), `cameraId`(단일 변경 시) |
| `MsgConfigChanged` | `config_changed` | O (`PUT /api/config`) | `type` |
| `MsgStats` | `stats` | 미발행 (선언만) | — |
| `MsgPong` | `pong` | O | `type` |
| `MsgClientLimitExceeded` | `client_limit_exceeded` | O (max_clients 초과) | `type` |

- `rtp_batch`의 축약 키 — `p`(base64 payload), `ts`(uint32 timestamp), `m`(bool marker, 생략 가능), `sq`(uint16 sequence).
- **재생 의미론** — `start_stream`/`subscribe` = 시작 + 구독. 마지막 구독자가 `unsubscribe`/연결 종료하면 백엔드가 RTSP 세션을 해제(`refs == 0`). `stop_all_streams`는 이 연결에만 적용된다.
- **실시간 설정 반영** — mutation 시 `CameraService`가 `ws.Server.Broadcast`(conns 스냅샷 → conn별 goroutine)로 전 클라이언트에 통지한다. `added`/`deleted`는 프론트가 즉시 `fetchCameras()`(250ms 디바운스)로 반영하고, `updated`/`reordered`/`restored`는 툴바 "설정 변경 적용" 배지로 누적했다가 사용자가 클릭하면 `fetchCameras()` + 변경 카메라에 `reload_stream`을 보낸다. `reload_stream`을 받은 허브가 `Hub.Reload`(= `close(id, true)`)로 세션을 끊으면 그 카메라 구독자 전원이 `stream_stopped` 후 리컨실리어로 새 설정 재다이얼한다. `config_changed`는 SettingsPage가 열려 있으면 자동 재조회한다.

### 5.3 설정 파일 스키마

- **`config/app.json`** — §3.2 표. 최초 실행 시 `Default()`로 자동 생성. `.gitignore` 대상.
- **`config/cameras.json`** — 최상위 `version`, `cameras[]`, `groups[]`.
  - 카메라 — `id, name, type, xaddr, username, password("encrypted:..."), profile_token, stream_url, stream_config{transport, protocol, buffer_size}, ptz_supported, group_id, layout_order, enabled, added_at, updated_at`.
  - 그룹 — `id, name, layout_type("grid"), cols, rows`.
- **`config/.masterkey`** — 32바이트 hex, 권한 `0600`. 없으면 첫 실행 시 생성 + 폴백 키 마이그레이션.

---

## 6. 렌더링 파이프라인

### 6.1 전체 경로

```
RTSP 수신 (gortsplib OnPacketRTP)
  → depayload (gortsplib 내장 rtph264/rtph265)      ※ 백엔드는 원본 RTP payload를 그대로 전달
  → Hub 구독자 채널 (버퍼 512, 느린 구독자 드롭)
  → ws pump (대기 패킷 즉시 흡수, ≤128 → rtp_batch, 축약 키 p/ts/m/sq)
  → WebSocket (base64)
  → streamStore 메시지 라우터 → PacketIn
  → decoder.ts Session.push (지터 버퍼: TICK 30ms, 큐 1200, 시퀀스 랩어라운드, 늦은 패킷 드롭)
  → depacketize (RFC 6184 STAP-A/FU-A, RFC 7798 AP/FU)
  → completeFrame (첫 키프레임 전 델타 폐기, 디코더 미준비면 heldKey 보관)
  → decodeFrame → EncodedVideoChunk → WebCodecs VideoDecoder.decode
  → onFrame → CustomEvent('webnvr-frame')
  → CameraTile → VideoRenderer.draw (texImage2D(VideoFrame) 직접 업로드 + 레터박스, 실패 시 2D drawImage)
  → frame.close()
```

### 6.2 청크 포맷 전략

기본은 **Annex B**(시작코드, description 없음 — 대상 WebKit 메인 스레드 실측 동작). 키프레임 후 4초(`STALL_MS`) 무출력이면 **AVCC**(avcC description + 길이 접두)로 자동 전환, 모든 포맷 시도 후 12초(`STALL_GIVEUP_MS`)면 포기하고 오류. 첫 성공 시 `lastGoodFormat`을 모듈 레벨에 기억해 재접속 시 처음부터 쓴다(§7 F7·F14).

### 6.3 지터 / 페이싱

WS는 TCP라 순서가 보장되므로 재정렬은 방어적이다. `Session.push`가 16비트 랩어라운드 비교로 중복/늦은 패킷을 드롭하고, 큐가 1200을 넘으면 앞에서 잘라낸다. `DecoderHub`가 30ms마다 전 세션 `flush`.

### 6.4 워치독

`frames === 0`이고 패킷을 10개 이상 받은 상태에서 — 키프레임 후 4초 무출력이면 포맷 전환 + notice, 포맷 소진 후 12초면 오류, 키프레임 자체를 20초간 못 받으면 GOP 설정 안내 오류. (패킷 수가 아니라 **시간 기준** — 비트레이트에 따라 패킷 수의 의미가 달라지기 때문, §7 F2·F3.)

### 6.5 렌더러

WebGL2 단일 텍스처 RGBA 패스스루(YUV→RGB는 `texImage2D(VideoFrame)`에서 GPU가 수행). 직접 업로드 실패 시 2D `drawImage`로 우회 캔버스에 그린 뒤 업로드. 레터박스는 `scale = min(pw/fw, ph/fh)`로 중앙 `gl.viewport`. 문서 v1.0 §9의 YUV420P 3텍스처 BT.709 셰이더는 ffmpeg.wasm 폴백(미도입) 대비로만 남겨 둔다.

---

## 7. 설계 결정 · 안정화 이력

### 7.1 설계 결정 (context-notes D1~D19)

| # | 결정 | 근거 |
|---|------|------|
| D1 | 백지부터 시작 | 문서엔 Phase 1~2 완료 표기지만 저장소는 커밋 0개 |
| D2 | 프로젝트명 `webnvr` (문서의 `cctv-control` 아님) | 디렉토리명 기준, 사용자 선택 |
| D3 | RTSP = `bluenviron/gortsplib/v4` (pion 계열) | `pion/rtsp` 아카이브됨, mediamtx 검증된 라이브러리 |
| D4 | RTMP 클라이언트 연기 | 문서상 "필요시만" — 인터페이스 스텁만 |
| D5 | 1차 세션 범위 = Phase 1+2 (백엔드) | 프론트는 별도 세션 |
| D6 | MEMORY.md에 모든 지시/결정 기록, KST 타임스탬프 | 세션·머신이 바뀌어도 재개 가능 |
| D7 | Wails CLI 재빌드 (x/tools v0.47.0) | v2.10.1이 Go 1.27 `go list`와 비호환 — **현재는 공식 v2.15.0로 불필요** |
| D8 | Wails v2 표준 루트 레이아웃 채택 | 문서의 `backend/cmd/main.go` 구조는 Wails CLI와 충돌 |
| D9 | ONVIF Profiles·Presets는 raw SOAP `CallMethod` | SDK 응답 구조체 네임스페이스 태그 불일치 |
| D10 | WS-Discovery는 인터페이스별 순차 프로브(내부 ~1초) | `SendProbe` 고정 대기, raw XML 반환 |
| D11 | gortsplib **v4.16.2** + 내장 rtph264/rtph265 depay | v4.16.3은 불완전 스텁, 별도 depay 구현 불필요 |
| D12 | Hub 이벤트 = `StartedEvent`→`PacketEvent`→`StoppedEvent` 순서 | WS 계층에서 stream_started→rtp→stopped 순서 자연스러움 |
| D13 | 프론트 디자인 = 관제실 야간 테마 + OSD 리티클 시그니처 | frontend-design 스킬, §4.10 |
| D14 | Vite 3→5, `@dnd-kit` 핸들 방식, 낙관적 업데이트+롤백 | Vite 3이 `@fontsource` exports 해석 불가 |
| D15 | 렌더러 = `VideoFrame` 직접 텍스처 업로드 | WebCodecs 출력은 NV12/RGBA — 3텍스처는 CPU 복사 필요 |
| D16 | 지터 버퍼 = 워커/메인 세션 큐 (30ms 틱, 시퀀스 정렬 방어적) | WS는 TCP라 재정렬은 UDP 대비용 |
| D17 | PTZ 프리셋 목록 = 백엔드가 저장 자격증명으로 조회 | 프론트에 자격증명 미노출 |
| D18 | TS 5.9 업그레이드, 코덱 문자열은 SPS 파생, 첫 키프레임 전 델타 폐기 | WebCodecs 타입 + 중간 참여 대응 |
| D19 | 디코더 설정 동기화, 렌더러 2D 폴백, 워치독 진단 | 렌더링 미출력 버그 1차 대응 (아래 F로 이어짐) |

### 7.2 안정화 이력 (context-notes F1~F18, 커밋 해시)

| # | 커밋 | 증상 | 원인 | 해결 | 교훈 |
|---|------|------|------|------|------|
| F1 | — | — | — | 실기 스트림 검증 성공 (PythonCam, H.264 1080p, 8,708패킷/8.5Mbps) | SPS/PPS는 SDP fmtp에서 파싱 |
| F2a | — | 폴백 키 경고가 콘솔만 | 마스터 키 미설정 | Phase 5 설정 패널에서 출처 표시 | 키 변경 시 기존 비밀번호 복호화 불가 — 마이그레이션 필요 |
| F2b | `092a1b8` | 타일이 "키프레임 대기" 오류로 고착 | 패킷 수(60개) 기준 워치독 — 중간 GOP 참여는 첫 IDR까지 최대 2초라 항상 오판 | 시간 기준 워치독 + `decoded` 이벤트로 상태 전환 | 패킷 수는 비트레이트에 따라 의미가 다르다. 시간으로 판정하라 |
| F3 | `cb920fe` | "20초간 키프레임 미수신" | `new Uint8Array(n)`이 길이 n의 0 배열 — 재조립 NALU 헤더가 0x00, 쓰레기 프리픽스 | FU-A 재조립 헤더 수정, 큐 상한 150→1200 | `new Uint8Array(n)` vs `[n]`은 바이트 코드 최빈 버그. 재생 하네스가 추측을 없앴다 |
| F4 | `2ddd8f0` | 1CH 미출력 + 드롭 다수 | 허브 버퍼 30 = "30프레임" 의도였으나 RTP 패킷 단위로 1프레임 — 1080p 버스트에 매 프레임 오버플로 | 허브 버퍼 512 + `rtp_batch` 배치 전송 | 유실 위치는 시퀀스 번호로 양단을 계량해 확정하라. 유실률 3.72% → 0.00% |
| F5 | `ccaf1bb` | DevServer 브라우저에서 영상 미출력 | Wails 바인딩 의존이 설계 오류 | v1.1 서버 중심 전환 — `/api/*` REST 13종, 프론트 fetch, Wails 셸화, 8080 단일 | "N번째 클라이언트"와 브라우저는 첫 클라이언트와 상태가 다르다 |
| F6 | `6dfc8e4`/`4ad2688` | (Phase 5 구현) | — | 통계 대시보드, 설정 페이지, 백업/복원, `ensureMasterKey` 마이그레이션, 서버측 Decoder 스텁 | 마스터 키 신규 생성 시 폴백 키 비밀번호 자동 재암호화 |
| F7 | `6129070` | Safari에서 키프레임 감지되나 출력 0 | WebKit `VideoDecoder`는 `description` 없는 Annex B를 조용히 거부 (Chromium은 허용) | `buildAvcC` + AVCC 우선 → 무출력 시 Annex B 자동 전환 | 코덱 설정은 브라우저별 차이를 후보화해 자동 전환하라 |
| F8 | `eb1ff05` | AVCC 수정 후에도 Safari 미출력 | WebKit은 **Worker 내 `VideoDecoder` 출력 콜백을 발화하지 않음** | Worker 완전 제거 → `services/decoder.ts` `DecoderHub`로 메인 스레드 통합 | 크로스 브라우저는 메인 스레드 폴백까지 설계에 넣어라. 비동기 API는 Worker 이점이 적다 |
| F9 | `e74ea79` | 외부 IP 접속 불가 | 127.0.0.1 고정 바인딩 + 프론트 API 주소 하드코딩 | `server.bind` 설정 + 백엔드가 임베디드 UI 서빙 + `location.hostname` 기반 주소 | bind=0.0.0.0은 인증 없는 LAN 노출 |
| F10 | `cc42710` | 네이티브 셸에서 `wails.localhost:8080` 오류, 목록/설정 공백 | Wails 셸 내부 `location.hostname`이 가상 호스트 `wails.localhost` | `services/backend.ts`에서 `wails.localhost`·빈값 → 127.0.0.1 폴백 | 임베디드 WebView는 가상 호스트를 쓴다. 폴백 목록 필수 |
| F11 | `30a85a8` | 2사이트 동시 접속 시 한쪽 "undefined 디코딩" 오류 | `StartedEvent`(코덱 메타)를 최초 dial 시 1회만 발행 — 늦은 구독자는 `config.codec === undefined` | `Hub.Subscribe`가 실행 중 스트림 Info를 새 구독자에 즉시 재전송 + `onNotice`로 자가치유 분리 | N번째 클라이언트는 첫 클라이언트와 상태가 다르다. 메시지 `undefined`는 데이터 흐름 단절 지점 |
| F12 | `d2e2ecd` | (요청) 통계 UI 재배치 | — | fps 스파크라인을 타일 제목 옆으로, 통계 패널을 슬림 바로 축소 | — |
| F13 | `828227d` | (요청) 끊긴 채널 수동 재시도 없이 자동 복구 | — | Desired-State Reconciler — `desired`가 true인데 `streaming`이 아니면 지수 백오프(1s→15s)로 `start_stream` 재전송 | stop은 사용자 의사를 존중(자동 재시도 중단) |
| F14 | `67d03c5` | 1CH "포맷 전환" 노트 반복 | AVCC 우선이 사용자 WebKit에서 실패 후 전환 — F7의 "Safari Worker" 가설이 F8로 무효화됨 | 기본을 Annex B로, `lastGoodFormat` 모듈 기억 | 포맷 우선순위도 가설이 아니라 실측으로 정하라 |
| F15 | `6fe7d93` | 영상 영구 미출력 (전 채널) | 리컨실리어가 `starting`(GOP 대기 진행 중)을 1초 만에 실패로 오판해 세션을 계속 리셋 | 시도 타임아웃(20s) 기반 판정 — `starting`은 20s 내 무시, `error`만 백오프 | "상태 ≠ 목표 → 재시도"는 진행 중 작업을 파괴한다. 진행과 실패를 구분하라 |
| F16 | `bc7c738` | DESCRIBE 404 무한 반복 | 카메라 서버 재시작으로 RTSP 포트 동적 변경 + `video_sub` 경로 소멸, 백엔드가 ONVIF URI를 무기한 캐시 | URI 캐시 TTL 30s + `StreamFailureNotifier`로 실패 시 캐시 폐기, 프론트 재시도 상한 60s | RTSP 서버의 비표준 동작에 코드가 적응해야 한다 |
| F17 | `7a7c108` | "received packet with wrong SSRC" 다발, 세션 동시 사망 | gortsplib RTCPReceiver가 첫 SSRC에 고정 — 카메라가 인코더 재시작으로 SSRC 변경 시 세션 종료 → 리컨실리어·서버 핑퐁 | gortsplib 벤더링 + `AllowSSRCChange` 패치 (§3.9) | 라이브러리에 옵션 없으면 벤더링 후 최소 패치가 정석 |
| F18 | `316a94c` | (요구) 클라이언트별 독립 재생 | 전역 세션 모델이 클라이언트 간 결합 | Hub 참조 카운팅(`refs==0` → RTSP 해제), 모니터링 진입=자동 시작/이탈=정지, `[전체 시작/정지]` 제거, `stop_all_streams`=이 연결 한정 | 리소스 생애를 "명시적 시작/정지"에서 "구독 참조 카운팅"으로 옮기면 결합이 사라진다 |

### 7.3 Phase 4R 이후 (번호 없는 후속 작업, 2026-09-03)

| 커밋 | 작업 | 요지 |
|------|------|------|
| `cd5e6f3` | 파일 로깅 | `internal/logging` (lumberjack), `AccessLog` 미들웨어, `OnShutdown`에서 close |
| `dee938c` | WS 업그레이드 500 수정 (F19-a) | `AccessLog`의 `statusRecorder`에 `http.Hijacker` 미구현 → `Hijack()` 추가 |
| `3aa3d23` | 기본 bind = `0.0.0.0` | fresh install도 외부 접근 가능, 비루프백 경고 로그 |
| `3c5782f`~`1f3e167` | UI 정리 | 채널 시작 버튼 제거, 분할 버튼 그룹 `[A][1][4][9][16][25]`, 페이저 Toolbar 이동, 활성 앰버 강조, 그리드 gap 축소 |
| (미커밋) | 더블클릭 줌 재설계 | `focusedCameraId` 제거 → `zoomReturnMode` + `zoomToggle` (§4.6) |
| (미커밋) | 헤드리스 실행 | `cmd/server/main.go` + `main.go -headless`. 부수로 `fs.Sub(assets,"frontend/dist")`로 외부 브라우저 UI 서빙 버그 수정 |
| (미커밋, 2026-09-04) | 실시간 설정 반영 | `ws.Server.Broadcast` + `cameras_changed`/`config_changed`/`reload_stream` 메시지. 추가/삭제는 즉시, 수정/재정렬/복원은 툴바 배지→클릭 반영(§5.2). `Hub.Reload`(=`close(id,true)`)로 세션 재다이얼 |

### 7.4 트러블슈팅 (MEMORY.md T1~T4)

| # | 증상 | 해결 |
|---|------|------|
| T1 | `wails build` "package context without types" | Wails CLI를 x/tools v0.47.0으로 재빌드. **현재 머신은 공식 v2.15.0라 불필요** |
| T2 | macOS 링크 오류 `_OBJC_CLASS_$_UTType` | 직접 `go build -tags desktop,production` 시 `CGO_LDFLAGS="-framework UniformTypeIdentifiers"`. `wails build`는 자동 처리 |
| T3 | APFS 대소문자 비구분으로 `App.css` 삭제 사고 | 대소문자만 다른 파일명 혼용 금지 (`app.css` 사용) |
| T4 | 문서 갱신 누락으로 진행 기록 유실 | 문서 갱신도 작업 커밋에 포함. AI 문서 3종은 git 비추적 |

---

## 8. 구현 단계

| Phase | 내용 | 상태 | 주요 커밋 |
|-------|------|------|-----------|
| 0 | 문서 3종 + 첫 커밋 | 완료 | `e95c402` |
| 1 | 백엔드 기반 — config/camera/onvif + CameraService | 완료 | `ddd83da` `9104edb` `2e43425` `77d9f3f` |
| 2 | 스트림 코어 + WebSocket, 실기 단일 스트림 검증 | 완료 | `4ba94b6` `d3113ac` `601c949` |
| 3 | 카메라 관리 UI (검색/폼/프로필/테스트/DnD), 야간 관제 테마 | 완료 · 사용자 육안 확인 | `ae283a5` |
| 4 | 모니터링 그리드 + 디코딩 + WebGL + PTZ | 구현 완료 (안정화 F2~F4 포함) | `81182c9` `c4df2bb` … `2ddd8f0` |
| 4R | v1.0 → v1.1 서버 중심 재설계 — `/api/*` REST, 프론트 fetch 전환, Wails 셸화, 8080 단일 | 완료 | `39fd2a4` `ccaf1bb` |
| 5 | 통계 대시보드, 설정 페이지, 백업/복원, 마스터 키 관리 | 구현 완료 · 사용자 검증 대기 | `6dfc8e4` `4ad2688` |
| 6 | JWT 인증 + SQLite 마이그레이션 + 서명/배포/CI | **미착수** (6.1 JWT 일부 진행 후 사용자 지시로 전면 취소) | — |

**Phase 4R (서버 중심 전환)** — DevServer 브라우저에서 영상이 안 나오던 문제와 "프론트는 서버 중계·설정 조회만" 원칙에서, Wails 바인딩 의존이 설계 오류임을 확정하고 v1.1로 재설계했다. `httpapi.go` `/api/*` 13종(위임자만), `onvifCall<T>` 제네릭으로 중복 제거, `ws.Server.StartWithHandler`/`Mux` 분리로 `/ws` + `/api/*` 동일 포트, 프론트 `types/api.ts` + `services/api.ts` fetch 전환, `wailsjs` 생성물 삭제, 8080 점유 시 `os.Exit(1)`.

---

## 9. 운영

### 9.1 실행 방법

```bash
# 데스크톱 앱 + 핫리로드
wails dev

# 백엔드만 (창 없음) — 저장소 루트에서
go run ./cmd/server
# 또는
go build -o webnvr-server ./cmd/server && ./webnvr-server

# 데스크톱 바이너리를 헤드리스로
./build/bin/webnvr.app/Contents/MacOS/webnvr -headless

# 프론트엔드만 개발 서버로 (백엔드는 위에서 띄운 상태)
cd frontend && npm run dev   # http://localhost:5173 → :8080 백엔드 자동 연결
```

### 9.2 빌드

```bash
wails build                  # build/bin/webnvr.app (재배포용)
```

HTTPS는 `WEB_CERT`/`WEB_KEY` 환경변수에 인증서 경로를 넣는다(`run-https.sh` 참고). HTTPS는 `:8443`, WSS도 같은 포트.

### 9.3 외부 접근 · 포트포워딩

- UI·API·WS가 전부 `:8080` 하나라서 **TCP 8080 한 포트만 포워딩**하면 된다. 호스트는 `server.bind: "0.0.0.0"`이어야 한다(현재 기본값).
- `services/backend.ts`가 포트를 `8080`(HTTP)/`8443`(HTTPS)으로 **하드코딩**하므로 외부 포트도 반드시 같아야 한다. `80 → 8080` 같은 번호 변경 포워딩은 프론트가 `공인IP:8080`으로 API/WS를 호출해 실패한다. 리버스 프록시를 표준 포트에 두려면 `backend.ts`를 `location.port` 기반으로 고쳐야 한다.
- **인증이 없다.** 8080을 인터넷에 열면 누구나 영상 조회·카메라 CRUD·설정 변경·PTZ·백업 다운로드가 가능하다. 포트포워딩보다 **VPN(WireGuard/Tailscale)** 을 권장하고, 불가피하면 공유기 소스 IP 제한 + 앞단 프록시 Basic 인증 + HTTPS.

### 9.4 진단 도구

| 명령 | 용도 |
|------|------|
| `go run ./cmd/streamtest -onvif host:port -user U -pass P` | 실기 RTSP 수신 확인 (코덱/해상도/kbps) |
| `go run ./cmd/wstest <cameraId,...> [초]` | WS 파이프라인 + 시퀀스 갭(서버→클라 유실률) |
| `go run ./cmd/rtspmock` | 합성 H.264 RTSP 서버 (STAP-A + FU-A IDR) |
| `go run ./cmd/rtspanalyze -camera <id>` | 실기 NALU 구조 / GOP 간격 분석 |
| `go run ./cmd/rtpdump -camera <id> -out x.json` | RTP 패킷 JSON 덤프 (재생 하네스 입력) |

### 9.5 테스트 전략

- 백엔드 단위 — `go test -race ./...` (config/camera/onvif/stream/api/ws 6패키지 통과).
- 통합/진단 — 위 CLI + Node 재생 하네스(워커/디코더 번들을 합성 패턴으로 검증).
- 부하 — `wstest` 다중 채널(실측 5채널 26.4Mbps 유실 0.00%).
- 프론트 단위(Vitest/RTL) 미도입, E2E는 수동 + 사용자 확인.

---

## 10. 현재 상태 · 미결 · 위험

### 10.1 현재 상태

- HEAD `1f3e167`. Phase 1~5는 코드 레벨에서 완료, 사용자 실기 검증 대기(Phase 4 렌더링/PTZ, Phase 5 설정/통계/전체화면).
- 확정 아키텍처 — 백엔드 = REST + WS + UI 서빙 (8080, `server.bind` 기본 `0.0.0.0`), 프론트 = fetch + WS 클라이언트(`backend.ts`), 디코딩 = 메인 스레드. 재생 = 클라이언트별 독립(진입=자동 시작, 이탈=정지, 리컨실리어 자동 복구), Hub 참조 카운팅. RTSP = gortsplib 벤더링 + `AllowSSRCChange`, ONVIF URI 캐시 TTL 30s + 실패 무효화, 청크 포맷 = Annex B 기본 → AVCC 폴백 + 기억.
- 미해결 이슈 없음.

### 10.2 위험 요소

| 위험 | 상태 | 대응 |
|------|------|------|
| WS 버스트 유실 | 해소 | `rtp_batch` + 허브 버퍼 512 (실측 0.00%) |
| WebCodecs 브라우저 차이 | 해소 | Annex B/AVCC 후보 + 자동 전환 + `lastGoodFormat` |
| RTSP 서버 비표준 동작 (포트 변경, SSRC 변경) | 해소 | URI 캐시 TTL + 실패 무효화, `AllowSSRCChange` 벤더 패치 |
| 인증 부재 + `bind: 0.0.0.0` 기본 | **미해결** | Phase 6 JWT 전까지 신뢰 네트워크·VPN 전용. 기동 시 경고 로그 |
| `server.max_clients` 기본값 불일치 | 확인 필요 | 코드 `Default()` = `0`(무제한), README = `4` — 문서/코드 정합화 필요 |
| config 핫리로드 미배선 | 인지됨 | `watcher.go`는 있으나 미호출. 변경은 `PUT /api/config` + 재시작 |
| `backend.ts` 포트 하드코딩 | 인지됨 | 표준 포트 리버스 프록시 불가 (§9.3) |
| 다중 WebGL 타일 메모리 | 중간 | `VideoFrame` 즉시 close, 레터박스 뷰포트. 비가시 타일 일시정지는 미구현 |

### 10.3 미결 / 연기

- Phase 6 — JWT 인증 + bcrypt + 로그인 UI, JSON → SQLite 마이그레이션, `SQLCameraStore`, macOS 서명/공증·Windows NSIS·Linux AppImage, CI/CD.
- RTMP 클라이언트 미구현 (타입만).
- H.265 SPS 해상도 파싱 미구현 (0,0 반환).
- 서버측 디코딩(`internal/stream/decoder.go`) — 스냅샷/녹화 대비 스텁, 미배선.
- `StatusBar` 실시간화 (현재 하드코딩).
- 팝아웃 윈도우 (Wails 다중 창 필요).

---

## 부록 A. 문서 이력

| 버전 | 일자 | 변경 |
|------|------|------|
| 1.0 | 2026-08-29 | 초안 (Wails 바인딩 중심 설계) |
| 1.1 | 2026-08-31 | 서버 중심 재설계 — 카메라 관리를 HTTP REST로 이행, 프론트를 순수 클라이언트로 정의. `architecture.md` |
| v1 (본 문서) | 2026-09-03 | 전체 재정리 — Phase 5 완료, 파일 로깅, 헤드리스 실행, 안정화 F1~F18, UI 상세, 운영/외부 접근. `architecture_v1.md` |

## 부록 B. 주요 파일 인덱스

### 백엔드

| 경로 | 역할 |
|------|------|
| `main.go` | Wails 셸 진입점 (`-headless`, `fs.Sub` UI 서브루팅) |
| `cmd/server/main.go` | 헤드리스 백엔드 진입점 (순수 Go) |
| `internal/config/config.go` | 설정 스키마 + `Default()` |
| `internal/config/encryption.go` | AES-256-GCM + PBKDF2 카메라별 키, 마스터 키 우선순위 |
| `internal/camera/manager.go` | `Manager`, `PasswordOf` (유일 복호화 지점) |
| `internal/camera/store.go` | `JSONCameraStore` (원자적 쓰기, `layout_order`) |
| `internal/onvif/*.go` | ONVIF SOAP (SDK 헬퍼 + raw `CallMethod` 혼합) |
| `internal/stream/rtsp_client.go` | `DialRTSP` (gortsplib, `AllowSSRCChange`) |
| `internal/stream/hub.go` | `Hub` — 구독/참조 카운팅/백프레셔 (`subscriberBuf = 512`) |
| `internal/ws/protocol.go` | WS 메시지 계약 |
| `internal/ws/handler.go` | `connState`, `pump` (배치 드레인 ≤128) |
| `internal/api/app.go` | `App` 조립, `StartWSServer`, `ensureMasterKey` |
| `internal/api/httpapi.go` | `/api/*` 라우트 + `AccessLog`/`CORS` 미들웨어 |
| `internal/api/stream.go` | `StreamService` (URI 캐시 TTL 30s, `OnStreamFailed`) |
| `third_party/gortsplib/` | 패치된 gortsplib (`AllowSSRCChange`) |

### 프론트엔드 (`frontend/src/`)

| 경로 | 역할 |
|------|------|
| `App.tsx` | 스토어 기반 페이지 전환 셸 |
| `pages/MonitoringPage.tsx` | 라이브 그리드, 진입=자동 시작 / 이탈=정지 |
| `pages/CameraManagementPage.tsx` | 검색 + 목록 + 모달 |
| `pages/SettingsPage.tsx` | 앱 설정 / 보안 / 백업·복원 |
| `components/grid/CameraGrid.tsx` | 분할 모드 + 페이지네이션 + 더블클릭 줌 |
| `components/grid/CameraTile.tsx` | OSD 타일 + canvas + 상태 오버레이 |
| `components/grid/PTZControl.tsx` | 조이스틱 + 속도 + 줌 + 프리셋 |
| `components/grid/VideoRenderer.ts` | WebGL2 렌더러 + 2D 폴백 + 레터박스 |
| `components/camera/CameraForm.tsx` | 타입별 동적 폼 + 검증 + 테스트 |
| `store/cameraStore.ts` | 카메라 CRUD (낙관적 + 롤백) |
| `store/uiStore.ts` | 페이지/모달/토스트/`gridMode`/`zoomToggle` |
| `store/streamStore.ts` | 스트림 상태 + WS 라우터 + 리컨실리어 |
| `services/api.ts` | REST 클라이언트 |
| `services/ws.ts` | WS 싱글턴 (지수 백오프, 하트비트 25s) |
| `services/backend.ts` | 백엔드 주소 결정 (`wails.localhost` 폴백) |
| `services/decoder.ts` | `Session`/`DecoderHub` — 지터 버퍼 + depay + WebCodecs |
| `app.css` | 디자인 토큰 (관제실 야간 테마) + 전역 스타일 |
