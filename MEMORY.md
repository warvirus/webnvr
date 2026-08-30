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

### Phase 1: 백엔드 기반 + Camera API — 진행 중
| 작업 | 상태 | 완료 시각 |
|------|------|----------|
| 1.1 wails init 골격 → 저장소 루트 이동 → wails build 확인 | ✅ 완료 | 2026-08-30 23:52 (wails CLI 재빌드 필요했음 — 아래 트러블슈팅 참조) |
| 1.2 internal/config/ (loader, watcher, encryption, validator) | ✅ 완료 | 2026-08-30 23:58 (commit ddd83da, 테스트+race 통과) |
| 1.3 internal/camera/ (types, store, manager) | ✅ 완료 | 2026-08-31 00:05 (commit 9104edb, 테스트+race 통과) |
| 1.4 internal/onvif/ (client, discovery, profiles, stream_uri, ptz) | ✅ 완료 | 2026-08-31 00:50 (commit 2e43425, SOAP 목업 테스트 통과) |
| 1.5 internal/api/ CameraService 바인딩 | ✅ 완료 | 2026-08-31 00:57 (commit 77d9f3f, wails 바인딩 생성 확인) |

### **Phase 1 완료** (2026-08-31 00:57)

### Phase 2: 스트림 코어 + WebSocket — 대기
| 작업 | 상태 | 완료 시각 |
|------|------|----------|
| 2.1 RTSP 클라이언트 (TCP/UDP, Digest, 세션) | ✅ 완료 | 2026-08-31 01:20 (commit 4ba94b6, gortsplib/v4 v4.16.2 확정 — v4.16.3은 불완전 릴리스) |
| 2.2 RTP 디페이로더 (H.264 RFC 6184, H.265 RFC 7798) | ✅ 완료 | 2026-08-31 01:20 (gortsplib 내장 rtph264/rtph265 디코더 사용 — 별도 구현 불필요) |
| 2.3 Stream Hub (구독자, 30프레임 버퍼, 백프레셔) | ✅ 완료 | 2026-08-31 01:20 (commit 4ba94b6, 백프레셔/통합 테스트 통과) |
| 2.4 WebSocket 서버 (JSON 프로토콜, 하트비트) | ⏳ 대기 | — |
| 2.5 StreamService 바인딩 | ⏳ 대기 | — |
| 2.6 실제 카메라(192.168.0.217) 단일 스트림 수동 테스트 | ⏳ 대기 | — |

---

## 트러블슈팅 기록 (다른 머신/세션에서 재발 시 참조)

### T1. `wails build` 실패: "internal error: package \"context\" without types was imported from \"webnvr\"" (2026-08-30 23:52 해결)
- **증상**: `go build`는 성공하지만 `wails build`가 컴파일 전 단계에서 위 오류로 실패
- **원인**: wails CLI v2.10.1 바이너리(2025-06 빌드, Go 1.24.3)에 포함된 `golang.org/x/tools v0.30.0`이 Go 1.27의 `go list -json=...` 출력과 비호환. wails 빌드 플로우의 `CreateEmbedDirectories → staticanalysis.GetEmbedDetails → packages.Load` 단계에서 fatal 발생 (pkg/commands/build/build.go:165)
- **해결**: wails CLI 소스를 모듈 캐시에서 복사 후 x/tools를 v0.47.0으로 bump하여 Go 1.27로 재빌드:
  ```bash
  cp -r $GOMODCACHE/github.com/wailsapp/wails/v2@v2.10.1 /tmp/wails-src && chmod -R u+w /tmp/wails-src
  cd /tmp/wails-src && go mod edit -require golang.org/x/tools@v0.47.0 && go mod tidy
  go build -o "$(go env GOBIN)/wails" ./cmd/wails
  ```
- **현재 상태**: /Volumes/DATA/work/env/go/bin/wails (go1.27.0 + x/tools v0.47.0) — 다른 머신에서 Go 1.25+ 사용 시 동일 문제 재발 가능

### T2. macOS 링크 오류: "Undefined symbols: _OBJC_CLASS_$_UTType" (2026-08-30 확인)
- **증상**: `go build -tags desktop,production` 직접 실행 시 링크 실패
- **원인**: 최신 macOS SDK에서 Wails가 UTType 참조 — `UniformTypeIdentifiers` 프레임워크 링크 필요
- **해결**: `wails build`는 내부적으로 macOS 11+에서 자동으로 `-framework UniformTypeIdentifiers`를 CGO_LDFLAGS에 추가하므로 문제 없음. **직접 `go build -tags desktop,production` 실행 시에만 발생**:
  ```bash
  CGO_LDFLAGS="-framework UniformTypeIdentifiers" go build -tags desktop,production .
  ```

---

## 다음 세션 재개 지점

- **재개 위치**: Phase 1.1 완료 (wails build 성공). 골격 커밋 후 Phase 1.2 (internal/config/) 시작
- **다음 작업**: 골격 semantic commit → internal/config/ 구현 (config.go, loader.go, watcher.go, encryption.go, validator.go) → go test
- **미해결 이슈**: 없음 (wails CLI 재빌드로 해소 — T1 참조)

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
| 2026-08-30 23:08~23:15 | (자동) Phase 0 완료: 3개 문서 생성 + 첫 커밋 e95c402 | checklist.md 체크 갱신 |
| 2026-08-30 23:16~23:52 | (자동) Phase 1.1: wails init → 루트 이동. wails build 실패 → 원인 추적 → **wails CLI 재빌드로 해소 (T1)** → 빌드 성공 | build/bin/webnvr.app 생성 확인 |

---

*마지막 갱신: 2026-08-30 23:08:45 KST*
