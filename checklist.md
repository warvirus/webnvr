# checklist.md — webnvr 작업 체크리스트

> CLAUDE.md #7에 따라 구체적 태스크를 체크박스로 관리한다.
> 완료 시마다 `[x]`로 갱신한다.

## Phase 0: 준비

- [x] 0.1 MEMORY.md, context-notes.md 생성 (본 파일과 함께) — 2026-08-30 23:08
- [ ] 0.2 첫 커밋: CLAUDE.md, doc/, .skills/, MEMORY.md, checklist.md, context-notes.md

## Phase 1: 백엔드 기반 + Camera API

- [x] 1.1 wails init (react-ts) 골격 생성 → 임시 dir에서 생성 후 저장소 루트로 이동
  - [x] `wails build` 성공 확인 — 2026-08-30 23:52 (wails CLI 재빌드 후, context-notes D7 참조)
  - [ ] semantic commit: "wails 프로젝트 골격 추가"
- [ ] 1.2 internal/config/
  - [ ] config.go — 타입 정의 + 기본값 (doc §8 스키마)
  - [ ] loader.go — JSON 로드/저장
  - [ ] watcher.go — fsnotify 핫 리로드
  - [ ] encryption.go — AES-GCM + 환경변수 키 + PBKDF2 카메라별 키 파생
  - [ ] validator.go — 설정 유효성 검증
  - [ ] `go test ./internal/config/` 통과
  - [ ] semantic commit: "설정 관리 모듈 추가"
- [ ] 1.3 internal/camera/
  - [ ] types.go — Camera 타입 (onvif/rtsp/rtp/rtmp)
  - [ ] store.go — JSONCameraStore (스레드 세이프 CRUD)
  - [ ] manager.go — 타입별 분기 로직 + reorder
  - [ ] `go test ./internal/camera/` 통과
  - [ ] semantic commit: "카메라 관리 모듈 추가"
- [x] 1.4 internal/onvif/
  - [x] client.go — use-go/onvif SDK 래퍼
  - [x] discovery.go — WS-Discovery
  - [x] profiles.go, stream_uri.go, ptz.go
  - [x] SOAP 목업 서버 단위 테스트 통과
  - [x] semantic commit: "ONVIF 클라이언트 모듈 추가"
- [x] 1.5 internal/api/
  - [x] camera.go — CameraService (doc §5.1 계약 전체)
  - [x] events.go — Phase 2 (WS Hub 필요 시점)로 연기 결정
  - [x] 컴파일 + wails 바인딩 생성 확인 (frontend/wailsjs/go/api/CameraService.d.ts)
  - [x] semantic commit: "Camera API 바인딩 추가"

## Phase 2: 스트림 코어 + WebSocket

- [ ] 2.1 RTSP 클라이언트 (TCP/UDP, Digest 인증, 세션 관리)
  - [ ] 라이브러리 최종 확정 (gortsplib/v4 vs pion/rtsp — 실행 시 재확인)
  - [ ] mock RTSP 서버 테스트 통과
  - [ ] semantic commit: "RTSP 클라이언트 추가"
- [ ] 2.2 RTP 디페이로더 — H.264(RFC 6184), H.265(RFC 7798)
  - [ ] 단위 테스트 통과
  - [ ] semantic commit: "RTP 디페이로더 추가"
- [ ] 2.3 Stream Hub
  - [ ] 구독자 관리 + 30프레임 버퍼 + 느린 구독자 드롭
  - [ ] 백프레셔 테스트 통과
  - [ ] semantic commit: "스트림 허브 추가"
- [ ] 2.4 internal/ws/ — gorilla/websocket 서버
  - [ ] JSON 프로토콜 (doc §5.3) 구현
  - [ ] 하트비트 ping/pong
  - [ ] WS 통합 테스트 통과
  - [ ] semantic commit: "WebSocket 서버 추가"
- [ ] 2.5 internal/api/stream.go — StreamService 바인딩
  - [ ] 컴파일 확인
  - [ ] semantic commit: "Stream API 바인딩 추가"
- [ ] 2.6 실제 카메라(192.168.0.217) 단일 스트림 수동 테스트
  - [ ] 사용자에게 수동 테스트 절차 안내

## 제외/연기 항목 (이번 세션 대상 아님)

- [ ] RTMP 클라이언트 구현 → Phase 5 (인터페이스 스텁만)
- [ ] Phase 3~5 프론트엔드 (다음 세션)
- [ ] Phase 6 인증/SQLite/배포 (추후)
