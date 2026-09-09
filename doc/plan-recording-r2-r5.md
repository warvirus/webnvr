# Phase R.2~R.5 개발 계획 — 녹화 완성 (스토리지 풀/이벤트/재생/설정 UI)

> 원본 설계: `~/.claude/plans/wails-dev-velvet-moonbeam.md` (Phase R 전체 계획). 이 문서는 R.1 완료(커밋 40d6dd6) 이후 R.2~R.5 실행 계획이다.
> 작성: 2026-09-09

## 범위

| 단계 | 내용 | 게이트 |
|------|------|--------|
| R.2 | 다중 스토리지 풀 — fill-then-next, 오프라인 페일오버, janitor/재생 전 대상 동작 | 대상 분리·복구 테스트 |
| R.4 | 이벤트 모드 — pre-roll 링버퍼, 이벤트 클립, post-roll, 수동 트리거 API, per-camera 모드 | 이벤트 클립 pre/post-roll 검증 |
| R.3 | 재생 — 타임라인/플레이리스트/세그먼트 서빙 API + PlaybackPage(hls.js) + 스크러버 | 브라우저 재생 + seek |
| R.5 | 설정 UI — 녹화 섹션(enabled/할당량/storages/세그먼트), 카메라별 record_mode, 사용량 표시, 동적 반영 | 실사용 |

실행 순서: R.2 → R.4 → R.3 → R.5 (R.4가 R.5 폼 필드를, R.3가 R.5 사용량 표시를 먹여준다)

## 확정된 범위 결정

1. **schedule(시간대 모드 전환)은 이번 세션에서 제외** — 계획서 R.4에서도 "(옵션)" 표기. off/continuous/event/both + 수동 트리거 + pre/post-roll이 게이트("이벤트 클립 pre/post-roll 검증")의 본체. schedule은 후속으로 분리하고 체크리스트에 명시.
2. **동적 반영 (R.5 일부)** — record_mode 변경·녹화 enabled 토글은 즉시 반영(매니저 리컨실). 할당량/보관기간/keep_min/reclaim은 janitor에 SetCfg로 다음 스윕 반영. **스토리지 경로·세그먼트 길이 변경은 재시작 필요** — UI에 안내 표시.
3. **enabled=false여도 매니저는 항상 생성** (아이들 상태) — 동적 토글을 위해. 기존 "enabled 시에만 생성" 배선을 변경. enabled=false 동작 불변(녹화기 0개)은 회귀 테스트로 보장.
4. **이벤트 클립도 segments 테이블에 행을 남긴다** — kind='event', 별도 events 행은 클립 메타(type/시각)를 가리킴. 재생 타임라인/서빙은 segments 단일 경로로 단순화. (계획서의 events 테이블 역할 = 클립 인덱스)
5. **m3u8은 요청 구간 [from,to]의 VOD** — 세그먼트 순회 + 공백 지점 `#EXT-X-DISCONTINUITY`. `#EXT-X-PROGRAM-DATE-TIME` 포함해 벽시계 매핑 제공.
6. **seek UX (v1)** — 타임라인 클릭 = 그 시각을 포함하는 세그먼트부터 플레이리스트 재로딩. 실행 중 벽시계 표시는 현재 세그먼트 start_ts + 실행 위치. 플레이리스트 내 연속 구간 사이 seek는 video.currentTime.

## 파일별 변경 (R.2~R.5)

| 파일 | 내용 | 단계 |
|------|------|------|
| `internal/recording/storage.go` (신규) | StoragePool — roots[] {abs, minFree}, Pick(쓰기 가능+여유 확인, 실패 시 다음), Root(i), WriteTest | R.2 |
| `internal/recording/recorder.go` | 단일 root → Pool 주입, 세그먼트 열 때 Pick, storageIdx 기록, (R.4) 링버퍼+이벤트 클립, (R.4) 모드/프리롤 | R.2·R.4 |
| `internal/recording/segment.go` | sink가 storageIdx를 알고 닫을 때 INSERT에 반영, (R.4) 이벤트 클립 경로/INSERT | R.2·R.4 |
| `internal/recording/janitor.go` | 삭제 경로를 storage_idx로 해석, SetCfg(동적 반영) | R.2·R.5 |
| `internal/recording/manager.go` | Pool 생성, (R.4) 모드별 recorder 정책/TriggerEvent, Reconcile(카메라 목록 대조), (R.5) UpdateConfig | R.2·R.4·R.5 |
| `internal/recording/event.go` (신규) | 수동 EventSource — Manager.TriggerEvent 위임 | R.4 |
| `internal/camera` | (확인 후) pre/post_roll DTO 노출, record_mode 상수(both/event) — 마이그레이션 #3에서 컬럼은 이미 존재 | R.4 |
| `internal/api/camera.go` | UpdateCamera에 recordMode/preRoll/postRoll 반영 + 변경 시 녹화 매니저 notify | R.4·R.5 |
| `internal/api/recordings.go` (신규) | GET /api/recordings/{id}(타임라인) · /playlist.m3u8 · /seg/{segId}(Range) · /events · POST /api/cameras/{id}/record/event · GET /api/recordings/status | R.3·R.4·R.5 |
| `internal/api/app.go` / `main.go` | 매니저 항상 생성(아이들), 녹화 서비스 배선, mutation→Reconcile 콜백 | R.4·R.5 |
| `frontend/src/services/recordings.ts` (신규) | 재생 API 클라이언트 | R.3 |
| `frontend/src/pages/PlaybackPage.tsx` (신규) | 카메라 선택/날짜 네비/타임라인 스크러버/hls.js video | R.3 |
| `frontend/src/components/layout/Sidebar.tsx`, `uiStore.ts` | 'playback' 페이지 추가 | R.3 |
| `frontend/src/pages/SettingsPage.tsx` | 녹화 섹션 — enabled/할당량/보관/storages 편집/세그먼트/사용량·경고 | R.5 |
| `frontend/src/components/camera/CameraForm.tsx` (+카드 배지) | record_mode 선택 + pre/post-roll | R.5 |
| `frontend/package.json` | hls.js | R.3 |
| 테스트 | storage pool pick/페일오버, 이벤트 클립 pre/post-roll, janitor 다중 스토리지 삭제, API httptest | 전체 |

## 검증

- `go test -race ./...` 전체 + 신규 테스트
- `npm run build` (tsc+vite), `wails build`
- 실기 스모크: ①연속 녹화 회귀 0 ②수동 트리거 → 이벤트 클립(pre-roll 포함) ③playlist.m3u8 유효성 + ffprobe/브라우저 재생 ④Range 206 ⑤설정 UI에서 모드 변경 → 즉시 반영
- 회귀: enabled=false에서 기존 동작 불변

## 진행 기록

| 시각 | 내용 |
|------|------|
| 2026-09-09 14:00 | 계획 수립. schedule 제외/동적 반영/매니저 상시 생성 등 범위 결정 (context-notes D32~) |
