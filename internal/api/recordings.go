// 재생 API — 타임라인/플레이리스트/세그먼트 서빙/이벤트/상태와 수동 이벤트 트리거를 제공한다.
package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"webnvr/internal/recording"
)

// recordingStore는 App의 녹화 인덱스 DAO를 반환한다.
func recordingStoreOf(app *App) *recording.Store { return app.recording.Store() }

// RegisterRecordingHTTP는 /api/recordings/* 라우트를 등록한다.
func RegisterRecordingHTTP(mux *http.ServeMux, app *App) {
	// 전 카메라 구간 요약 (다시보기 화면의 "한눈에 보기") — /api/recordings/ 보다 긴 패턴이 우선한다
	mux.HandleFunc("/api/recordings/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
			return
		}
		from, to, herr := rangeParams(r)
		if herr != nil {
			writeErr(w, herr)
			return
		}
		ovs, err := recordingStoreOf(app).Overview(from, to)
		if err != nil {
			writeErr(w, errInternal(err.Error()))
			return
		}
		out := make([]CamOverviewDTO, 0, len(ovs))
		for _, o := range ovs {
			out = append(out, CamOverviewDTO{
				CameraID: o.CameraID, Segments: o.Segments, Bytes: o.Bytes,
				TotalDurMS: o.TotalDurMS, FirstMS: o.FirstMS, LastMS: o.LastMS, Events: o.Events,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"fromMs": from, "toMs": to, "cameras": out})
	})

	// 날짜별 녹화 요약 (달력의 녹화일 표시)
	mux.HandleFunc("/api/recordings/days", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
			return
		}
		from, to, herr := rangeParams(r)
		if herr != nil {
			writeErr(w, herr)
			return
		}
		days, err := recordingStoreOf(app).DayCounts(from, to)
		if err != nil {
			writeErr(w, errInternal(err.Error()))
			return
		}
		out := make([]DayCountDTO, 0, len(days))
		for _, d := range days {
			out = append(out, DayCountDTO{Day: d.Day, Segments: d.Segments, Bytes: d.Bytes, Cameras: d.Cameras})
		}
		writeJSON(w, http.StatusOK, map[string]any{"fromMs": from, "toMs": to, "days": out})
	})

	mux.HandleFunc("/api/recordings/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
			return
		}
		writeJSON(w, http.StatusOK, app.recording.Status())
	})

	mux.HandleFunc("/api/recordings/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/recordings/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "카메라 ID가 필요"})
			return
		}
		id := parts[0]
		sub := ""
		if len(parts) == 2 {
			sub = parts[1]
		}
		from, to, herr := rangeParams(r)
		if herr != nil {
			writeErr(w, herr)
			return
		}
		switch {
		case sub == "":
			handleTimeline(w, app, id, from, to)
		case sub == "playlist.m3u8":
			handlePlaylist(w, app, id, from, to)
		case strings.HasPrefix(sub, "seg/"):
			handleSegment(w, r, app, id, strings.TrimPrefix(sub, "seg/"))
		case sub == "events":
			handleEvents(w, app, id, from, to)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "알 수 없는 경로"})
		}
	})

	// 수동 이벤트 트리거 (R.4) — /api/cameras/ 최장 패턴 우선순위에 맡기지 않고
	// 카메라 서브트리 등록 전에 여기서 끝낸다. 다른 /api/cameras/* 경로는 무시한다.
}

// rangeParams는 from/to 쿼리(epoch ms)를 파싱한다. from 기본 = 24시간 전, to = 지금.
func rangeParams(r *http.Request) (int64, int64, *httpError) {
	now := time.Now().UnixMilli()
	to := now
	if v := r.URL.Query().Get("to"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, errBadReq("to 파라미터가 잘못되었습니다")
		}
		to = n
	}
	from := to - 24*60*60*1000
	if v := r.URL.Query().Get("from"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, errBadReq("from 파라미터가 잘못되었습니다")
		}
		from = n
	}
	if from > to {
		return 0, 0, errBadReq("from이 to보다 큽니다")
	}
	return from, to, nil
}

// CamOverviewDTO는 전 카메라 요약의 항목이다.
type CamOverviewDTO struct {
	CameraID   string `json:"cameraId"`
	Segments   int64  `json:"segments"`
	Bytes      int64  `json:"bytes"`
	TotalDurMS int64  `json:"totalDurMs"` // 실제 녹화 시간(공백 제외)
	FirstMS    int64  `json:"firstMs"`
	LastMS     int64  `json:"lastMs"`
	Events     int64  `json:"events"`
}

// DayCountDTO는 달력 표시용 하루 요약이다.
type DayCountDTO struct {
	Day      string `json:"day"` // YYYY-MM-DD (서버 로컬)
	Segments int64  `json:"segments"`
	Bytes    int64  `json:"bytes"`
	Cameras  int64  `json:"cameras"`
}

// RecRange는 연속 녹화 구간 하나다.
type RecRange struct {
	FromMS int64 `json:"fromMs"`
	ToMS   int64 `json:"toMs"`
	Bytes  int64 `json:"bytes"`
	Count  int   `json:"count"`
}

// RecEventDTO는 이벤트 마커다.
type RecEventDTO struct {
	ID        int64  `json:"id"`
	TS        int64  `json:"ts"`
	Type      string `json:"type"`
	SegmentID int64  `json:"segmentId"`
}

// RecSegDTO는 스크러버 매핑용 세그먼트다.
type RecSegDTO struct {
	ID      int64  `json:"id"`
	StartTS int64  `json:"startTs"`
	DurMS   int64  `json:"durMs"`
	Bytes   int64  `json:"bytes"`
	Kind    string `json:"kind"`
	Flags   int    `json:"flags"`
}

// TimelineResponse는 GET /api/recordings/{id} 응답이다.
type TimelineResponse struct {
	CameraID string        `json:"cameraId"`
	FromMS   int64         `json:"fromMs"`
	ToMS     int64         `json:"toMs"`
	Ranges   []RecRange    `json:"ranges"`
	Segments []RecSegDTO   `json:"segments"`
	Events   []RecEventDTO `json:"events"`
	UsedB    int64         `json:"usedBytes"`
}

func handleTimeline(w http.ResponseWriter, app *App, id string, from, to int64) {
	store := recordingStoreOf(app)
	segs, err := store.Range(id, from, to)
	if err != nil {
		writeErr(w, errInternal(err.Error()))
		return
	}
	evs, err := store.EventsRange(id, from, to)
	if err != nil {
		writeErr(w, errInternal(err.Error()))
		return
	}
	resp := TimelineResponse{
		CameraID: id, FromMS: from, ToMS: to,
		Ranges:   mergeRanges(segs),
		Segments: make([]RecSegDTO, 0, len(segs)),
		Events:   make([]RecEventDTO, 0, len(evs)),
	}
	var used int64
	for _, s := range segs {
		used += s.Bytes
		resp.Segments = append(resp.Segments, RecSegDTO{
			ID: s.ID, StartTS: s.StartTS, DurMS: s.DurMS, Bytes: s.Bytes, Kind: s.Kind, Flags: s.Flags,
		})
	}
	for _, e := range evs {
		resp.Events = append(resp.Events, RecEventDTO{ID: e.ID, TS: e.TS, Type: e.Type, SegmentID: e.SegmentID})
	}
	resp.UsedB = used
	writeJSON(w, http.StatusOK, resp)
}

// mergeRanges는 세그먼트 목록을 연속 구간으로 병합한다. 인접(간격 ≤ 1.5s)은 한 구간으로.
// both 모드에서 이벤트 클립이 상시 구간 안에 겹치므로 구간은 단조 확장만 허용한다.
func mergeRanges(segs []recording.Segment) []RecRange {
	out := []RecRange{}
	for _, s := range segs {
		end := s.StartTS + s.DurMS
		if s.DurMS <= 0 {
			end = s.StartTS + 1000
		}
		if n := len(out); n > 0 && s.StartTS-out[n-1].ToMS <= 1500 {
			if end > out[n-1].ToMS {
				out[n-1].ToMS = end
			}
			out[n-1].Bytes += s.Bytes
			out[n-1].Count++
			continue
		}
		out = append(out, RecRange{FromMS: s.StartTS, ToMS: end, Bytes: s.Bytes, Count: 1})
	}
	return out
}

func handlePlaylist(w http.ResponseWriter, app *App, id string, from, to int64) {
	store := recordingStoreOf(app)
	segs, err := store.Range(id, from, to)
	if err != nil {
		writeErr(w, errInternal(err.Error()))
		return
	}
	if len(segs) == 0 {
		http.Error(w, "구간에 녹화가 없습니다", http.StatusNotFound)
		return
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].StartTS < segs[j].StartTS })

	var maxDur float64
	for _, s := range segs {
		d := float64(s.DurMS) / 1000
		if d > maxDur {
			maxDur = d
		}
	}
	target := int(maxDur + 0.999)
	if target < 1 {
		target = 1
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("#EXT-X-TARGETDURATION:" + strconv.Itoa(target) + "\n")
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	prevEnd := int64(0)
	for _, s := range segs {
		if prevEnd > 0 && s.StartTS-prevEnd > 1500 {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		b.WriteString("#EXT-X-PROGRAM-DATE-TIME:" + time.UnixMilli(s.StartTS).UTC().Format(time.RFC3339Nano) + "\n")
		dur := float64(s.DurMS) / 1000
		if dur <= 0 {
			dur = 1
		}
		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", dur))
		fmt.Fprintf(&b, "/api/recordings/%s/seg/%d\n", id, s.ID)
		prevEnd = s.StartTS + s.DurMS
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

func handleSegment(w http.ResponseWriter, req *http.Request, app *App, id, segID string) {
	n, err := strconv.ParseInt(segID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "세그먼트 ID가 잘못되었습니다"})
		return
	}
	seg, err := recordingStoreOf(app).Get(n)
	if err != nil || seg.CameraID != id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "세그먼트를 찾을 수 없습니다"})
		return
	}
	root, err := app.recording.StorageRoot(seg.StorageIdx)
	if err != nil {
		writeErr(w, errInternal(err.Error()))
		return
	}
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(seg.RelPath)))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "세그먼트 파일이 없습니다 (삭제되었을 수 있음)"})
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "video/mp2t")
	// http.ServeContent가 Range(206)와 If-* 처리를 담당한다.
	http.ServeContent(w, req, "", modTimeOrZero(f), f)
}

func modTimeOrZero(f *os.File) time.Time {
	fi, err := f.Stat()
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func handleEvents(w http.ResponseWriter, app *App, id string, from, to int64) {
	evs, err := recordingStoreOf(app).EventsRange(id, from, to)
	if err != nil {
		writeErr(w, errInternal(err.Error()))
		return
	}
	out := make([]RecEventDTO, 0, len(evs))
	for _, e := range evs {
		out = append(out, RecEventDTO{ID: e.ID, TS: e.TS, Type: e.Type, SegmentID: e.SegmentID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cameraId": id, "fromMs": from, "toMs": to, "events": out})
}
