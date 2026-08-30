# MEMORY.md — webnvr 프로젝트 작업 기록

> 이 파일은 모든 작업 지시/결정/진행 상황을 기록하여 향후 세션에서 작업을 이어갈 수 있게 한다.
> 모든 항목에 정확한 날짜/시각(KST)을 포함한다.

---

## 프로젝트 개요

| 항목 | 내용 |
|------|------|
| **프로젝트명** | `webnvr` (go module / wails 프로젝트명 모두) |
| **목적** | ONVIF 카메라 자동 검색, 멀티뷰 실시간 스트리밍, PTZ 제어 |
| **아키텍처** | Wails v2 (Go 백엔드 + React/WebView 프론트엔드) |
| **스트림 처리** | 백엔드 RTSP 수신 → WebSocket → 프론트엔드 WebCodecs + WebGL |
| **작업 디렉토리** | /Volumes/DATA/work/projects/cctv/webnvr |
| **상세 아키텍처** | `doc/architecture.md` (문서 버전 1.0, 2026-08-29) |

---

## 확정된 결정 사항

| 날짜/시각 | 결정 | 근거/비고 |
|-----------|------|----------|
| 2026-08-30 23:00 | 백지부터 시작 (기존 코드 없음) | 문서에 Phase 1~2 완료 표기되어 있으나 저장소가 비어 있었음 → 불일치 해소 |
| 2026-08-30 23:00 | 프로젝트명 `webnvr` (문서의 `cctv-control` 아님) | 디렉토리명 기준, 사용자 선택 |
| 2026-08-30 23:00 | RTSP 라이브러리: pion 계열 (실행 시 gortsplib/v4 재확인 예정) | 문서에 vdk/rtspv2와 pion/rtsp 혼재 → 사용자가 pion 선택 |
| 2026-08-30 23:00 | 이번 세션 범위: Phase 1 + Phase 2 | 프론트엔드(Phase 3~5)는 다음 세션 |
| 2026-08-30 23:00 | RTMP 클라이언트: 인터페이스 스텁만, 구현은 Phase 5로 연기 | 문서상 "필요시만 구현" |
| 2026-08-30 23:08 | MEMORY.md 운영 방침: 모든 작업 지시마다 이 파일에 기록 | 사용자 지시 — 세션 재개 가능성 보장 |

## 도구 환경 (2026-08-30 확인)

- Go 1.27.0 darwin/arm64
- Wails CLI v2.10.1
- Node v23.11.0 / npm 11.14.1

## 문서-실제 불일치 3건 (2026-08-30 발견, 모두 해소됨)

1. 문서 Phase 1~2 "완료" 표기 ↔ 저장소 비어 있음 → **백지 시작으로 해소** (doc/architecture.md의 체크표기는 무시)
2. 문서 프로젝트명 `cctv-control` ↔ 디렉토리 `webnvr` → **`webnvr`로 확정**
3. RTSP 라이브러리 혼재 (vdk/rtspv2 vs pion/rtsp) → **pion 계열 선택**

---

## 진행 상황

### Phase 0: 준비 — 진행 중
| 작업 | 상태 | 완료 시각 |
|------|------|----------|
| 0.1 MEMORY.md, checklist.md, context-notes.md 생성 | ✅ 완료 | 2026-08-30 23:08 |
| 0.2 첫 커밋 (CLAUDE.md, doc/, .skills/, 3개 문서) | ⏳ 대기 | — |

### Phase 1: 백엔드 기반 + Camera API — 대기
| 작업 | 상태 | 완료 시각 |
|------|------|----------|
| 1.1 wails init 골격 → 저장소 루트 이동 → wails build 확인 | ⏳ 대기 | — |
| 1.2 internal/config/ (loader, watcher, encryption, validator) | ⏳ 대기 | — |
| 1.3 internal/camera/ (types, store, manager) | ⏳ 대기 | — |
| 1.4 internal/onvif/ (client, discovery, profiles, stream_uri, ptz) | ⏳ 대기 | — |
| 1.5 internal/api/ CameraService 바인딩 | ⏳ 대기 | — |

### Phase 2: 스트림 코어 + WebSocket — 대기
| 작업 | 상태 | 완료 시각 |
|------|------|----------|
| 2.1 RTSP 클라이언트 (TCP/UDP, Digest, 세션) | ⏳ 대기 | — |
| 2.2 RTP 디페이로더 (H.264 RFC 6184, H.265 RFC 7798) | ⏳ 대기 | — |
| 2.3 Stream Hub (구독자, 30프레임 버퍼, 백프레셔) | ⏳ 대기 | — |
| 2.4 WebSocket 서버 (JSON 프로토콜, 하트비트) | ⏳ 대기 | — |
| 2.5 StreamService 바인딩 | ⏳ 대기 | — |
| 2.6 실제 카메라(192.168.0.217) 단일 스트림 수동 테스트 | ⏳ 대기 | — |

---

## 다음 세션 재개 지점

- **재개 위치**: Phase 0.1 진행 중 (세 파일 생성 후 커밋 예정)
- **다음 작업**: Phase 0.2 첫 커밋 → Phase 1.1 wails init
- **미해결 이슈**: 없음

---

## 작업 지시 로그

| 날짜/시각 | 지시 | 결과 |
|-----------|------|------|
| 2026-08-30 22:41 | CLAUDE.md 지침 준수 + Go 전문가 역할 선언 | 프로젝트 상태 확인 (빈 저장소) |
| 2026-08-30 22:41 | 기존 계획/문서 제공 방식 선택 | doc/architecture.md 수신 |
| 2026-08-30 22:41 | doc/architecture.md 참조 후 계획 요청 | 불일치 3건 발견 + Phase 0~2 계획 제시 |
| 2026-08-30 23:00 | 계획 관련 4개 결정 (백지/webnvr/pion/Phase 1+2) | 계획 확정 |
| 2026-08-30 23:08 | 모든 내용을 MEMORY.md에 저장 + 작업 지시마다 갱신 지시 | MEMORY.md 운영 방침 수립, 본 파일 생성 시작 |
| 2026-08-30 23:08 | 계획대로 진행 승인 | Phase 0 실행 시작 |

---

*마지막 갱신: 2026-08-30 23:08:45 KST*
