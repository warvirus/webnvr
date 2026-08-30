# context-notes.md — webnvr 설계 결정 및 근거 기록

> CLAUDE.md #7에 따라 작업 중 내린 결정과 이유를 계속 추가(append)한다.
> 새 결정이 생길 때마다 아래에 시각과 함께 추가한다.

---

## 2026-08-30 — 초기 설계 결정

### D1. 백지부터 시작
- **상황**: doc/architecture.md에 Phase 1~2 "✅ 완료" 표기가 있으나 저장소에 코드가 전혀 없음 (커밋 0개).
- **결정**: 문서의 체크 표기를 무시하고 Phase 1부터 새로 작성.
- **이유**: 사용자가 "백지부터 시작"을 명시적으로 선택. 기존 코드는 다른 곳에도 존재하지 않는 것으로 확인됨.
- **영향**: doc/architecture.md의 Phase 표기는 참고용으로만 사용. 완료 판정은 실제 코드/테스트 기준.

### D2. 프로젝트명 = webnvr
- **상황**: 문서는 `cctv-control`, 디렉토리는 `webnvr`.
- **결정**: go module path와 wails 프로젝트명 모두 `webnvr` 사용.
- **이유**: 사용자 선택. 이미 webnvr라는 디렉토리가 git 저장소 루트이므로 이름을 맞추는 것이 혼란이 적음.
- **영향**: `go mod init webnvr`, `wails init -n webnvr`. 문서의 cctv-control 표기는 webnvr로 읽는다.

### D3. RTSP 라이브러리: pion 계열 (gortsplib/v4로 재확인 예정)
- **상황**: 문서 기술스택 표는 `vdk/rtspv2`, Phase 2.1 태스크는 `pion/rtsp` — 혼재.
- **결정**: 사용자가 "pion" 계열을 선택. 단, `github.com/pion/rtsp` 자체는 유지보수가 정체된 저수준 라이브러리이므로 실행 시점에 `github.com/bluenviron/gortsplib/v4` (Pion 생태계 표준, pion/rtp·pion/rtcp 기반, TCP/UDP/Digest 인증/세션 관리 완비) 사용을 제안하고 재확인.
- **이유**: pion 생태계 유지 + 프로덕션 검증(mediamtx 사용) + 기능 완비도.
- **영향**: internal/stream/rtsp_client.go의 의존성 선택.

### D4. RTMP 클라이언트 연기
- **상황**: 문서 §10 위험요소에 "RTMP 풀링 구현 복잡도 — 필요시만 구현, 우선순위 낮춤" 명시.
- **결정**: 이번 세션에서는 인터페이스 스텁만 정의, 구현은 Phase 5.
- **영향**: internal/stream/rtmp_client.go는 인터페이스 + TODO만.

### D5. 세션 범위: Phase 1 + Phase 2
- **결정**: 백엔드 전체(Camera API + 스트림 코어 + WebSocket)까지. 프론트엔드 UI(Phase 3~5)는 다음 세션.
- **이유**: 사용자 선택. 프론트엔드는 규모가 커서 별도 세션이 적합.

### D6. MEMORY.md 운영 방침
- **결정**: 모든 작업 지시마다 MEMORY.md를 갱신하며, 각 항목에 KST 타임스탬프를 기록.
- **이유**: 사용자 지시. 세션/머신이 바뀌어도 MEMORY.md만으로 재개 가능하게 하기 위함.

---

## 2026-08-30 — Phase 1.1 작업 중 결정/발견

### D7. wails CLI 재빌드 (x/tools 호환성 수정)
- **상황**: `wails build`가 x/tools v0.30.0 ↔ Go 1.27 비호환으로 실패. 자세한 내용은 MEMORY.md T1 참조.
- **결정**: wails v2.10.1 소스를 x/tools v0.47.0으로 bump하여 재빌드 후 `GOBIN`에 설치.
- **이유**: wails 빌드 플로우의 staticanalysis 단계가 x/tools의 `packages.Load`를 사용하며, 구버전 x/tools는 Go 1.27 go list 출력을 파싱하지 못함. wails 업그레이드(v3 등)는 프로젝트 아키텍처 문서와 불일치하므로 최소 수정 선택.
- **영향**: 이 머신의 wails 바이너리는 커스텀 빌드임. 다른 머신 세션에서 재발 시 MEMORY.md T1 절차 참조.

### D8. 레이아웃 결정: Wails v2 표준 구조 사용
- **상황**: doc/architecture.md §4는 `backend/cmd/main.go` + `backend/go.mod` 구조를 제시하나, Wails v2는 프로젝트 루트에 main.go/wails.json/frontend/를 요구함.
- **결정**: Wails v2 표준 레이아웃 채택 (루트 main.go, app.go, internal/{config,camera,onvif,stream,api,ws}).
- **이유**: Wails CLI 빌드/바인딩 생성은 루트 기준으로 동작. 문서 구조는 Wails 실제 요구와 충돌.
- **영향**: 문서의 `backend/` 프리픽스는 루트로 읽음. frontend/는 wails 템플릿 위치 유지.

---

## 2026-08-31 — Phase 1.4 작업 중 결정/발견

### D9. use-go/onvif SDK 응답 파싱 한계와 우회
- **상황**: SDK 생성 응답 구조체의 네임스페이스 태그(`xml:"onvif:Resolution"` 등)는 Go encoding/xml에서 실제 카메라 응답(trt:/tt: 접두어)과 매칭되지 않음. 실험으로 확인 (해상도 등 중첩 필드가 0으로 파싱됨). 또한 GetPresetsResponse가 슬라이스가 아닌 단일 필드로 정의되어 있음.
- **결정**: Profiles와 Presets는 `dev.CallMethod()` raw 호출 후 자체 파싱 구조체로 처리. StreamURI/DeviceInformation/PTZ 명령은 SDK 헬퍼 그대로 사용(해당 응답은 정상 파싱됨).
- **영향**: 실제 카메라 연동(Phase 2.6)에서도 프로필/프리셋 파싱은 안전. SDK 업그레이드 시 재검토.

### D10. WS-Discovery 구현 세부
- **상황**: use-go/onvif의 ws-discovery.SendProbe는 인터페이스별 응답 대기가 내부적으로 1초 고정이며, 응답을 raw XML 문자열로 반환.
- **결정**: Discover(interfaces)는 인터페이스 목록을 순차 프로브하고 raw XML에서 XAddrs/Scopes를 파싱해 중복 제거 후 반환. 카메라 이름은 Scopes의 `onvif://.../name/...`에서 추출.
- **영향**: 스캔 타임아웃(app.json의 scan_timeout_ms)은 API 계층에서 래퍼 수준으로 적용 예정.

---

## 작업 중 발견 사항

(작업 진행 중 발견한 이슈와 해결 방법을 여기에 추가)

---

*마지막 갱신: 2026-08-30 23:08:45 KST*
