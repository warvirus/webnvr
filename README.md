# webnvr

ONVIF 카메라 자동 검색 · 멀티뷰 실시간 스트리밍 · PTZ 제어 데스크톱 앱.

- 아키텍처 = Wails v2 셸(창) + Go 백엔드 + React/TS 프론트엔드
- 백엔드가 `:8080` 하나로 REST API(`/api/*`) + 스트림 중계 WS(`/ws`) + 프론트엔드 UI(`/`)를 모두 서빙한다
- 프론트엔드는 백엔드에 `fetch` / WebSocket 으로만 접속한다 (Wails 바인딩 미사용)
- 상세 설계는 `doc/architecture.md` 참조

## 사전 준비

- Go 1.27+
- Node 20+ / npm
- Wails CLI v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
- 첫 빌드 전 프론트엔드 의존성 설치 — `cd frontend && npm install`

## 실행 방법

모든 명령은 **저장소 루트**에서 실행한다 (`config/` 를 상대 경로로 읽는다).

### 1. 데스크톱 앱 + 핫 리로드 — `wails dev`

```bash
wails dev
```

Wails 창이 뜨고, 프론트엔드 변경이 즉시 반영된다. 브라우저로 디버깅하려면
`http://localhost:34115` 에 접속한다.

### 2. 백엔드만 실행 (데스크톱 창 없이)

API 테스트, 헤드리스 상주, 프론트엔드 별도 개발용.

```bash
go run ./cmd/server
# 또는 바이너리로
go build -o webnvr-server ./cmd/server && ./webnvr-server
```

- `frontend/dist` 가 있으면 `http://localhost:8080/` 로 빌드된 UI 도 함께 서빙한다
  (없으면 `/api/*` 와 `/ws` 만 제공 — `cd frontend && npm run build` 후 재실행)
- `Ctrl+C` 로 종료

데스크톱 빌드 바이너리를 창 없이 띄우려면 `-headless` 플래그를 쓴다 (터미널 실행 전용).

```bash
./build/bin/webnvr.app/Contents/MacOS/webnvr -headless
```

### 3. 프론트엔드만 개발 서버로

백엔드(위 2번)를 띄운 상태에서 다른 터미널에서 실행한다.

```bash
cd frontend && npm run dev
```

`http://localhost:5173` 접속 시 `services/backend.ts` 가 접속 호스트를 기준으로
`:8080` 백엔드에 자동 연결한다 (백엔드가 CORS 허용).

## 빌드

```bash
wails build          # build/bin/webnvr.app (재배포용 프로덕션 패키지)
```

HTTPS 로 띄우려면 `WEB_CERT` / `WEB_KEY` 환경변수에 인증서 경로를 지정한다
(`run-https.sh` 참고). 이때 HTTPS 는 `:8443`, WSS 도 같은 포트를 쓴다.

## 설정

`config/app.json` (최초 실행 시 기본값으로 자동 생성).

| 키 | 기본값 | 설명 |
|----|--------|------|
| `server.ws_port` | `8080` | HTTP/WS/UI 공용 포트 |
| `server.bind` | `0.0.0.0` | 바인드 주소. `127.0.0.1` = 로컬 전용, `0.0.0.0` = LAN 공개 |
| `server.max_clients` | `4` | 동시 접속 제한 |
| `logging.file` | `logs/app.log` | 파일 로그 (lumberjack 로테이션) |

> `bind` 가 `0.0.0.0` 이면 인증 없이 LAN 에 노출된다. 신뢰 네트워크에서만 사용한다.
