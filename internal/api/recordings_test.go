// 재생 API(타임라인/m3u8/세그먼트 Range)를 httptest로 검증한다.
package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webnvr/internal/recording"
)

// seedRecording은 테스트 DB에 세그먼트 2개(+공백 1개)와 파일을 만든다.
func seedRecording(t *testing.T, app *App, camID string, base int64) {
	t.Helper()
	store := app.recording.Store()
	root, err := app.recording.StorageRoot(0)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(startMS int64, durMS int64, bytes int) int64 {
		rel := filepath.ToSlash(filepath.Join(camID, fmt.Sprintf("%d.ts", startMS)))
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, make([]byte, bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		id, err := store.Insert(recording.Segment{
			CameraID: camID, Kind: "continuous", StartTS: startMS, DurMS: durMS,
			StorageIdx: 0, RelPath: rel, Bytes: int64(bytes), Codec: "h264",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mk(base, 10_000, 1880)          // [base, base+10s]
	mk(base+30_000, 10_000, 940)    // [base+30s, +10s) — 앞 구간과 20s 공백
	if _, err := store.InsertEvent(recording.Event{CameraID: camID, TS: base + 5_000, Type: "manual"}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordingsTimelineAndPlaylist(t *testing.T) {
	app, srv := newTestAppFull(t)
	camID := "cam-rec-1"
	base := int64(1_000_000)
	seedRecording(t, app, camID, base)

	// 타임라인
	status, tl := doJSON(t, http.MethodGet, srv.URL+"/api/recordings/"+camID+"?from=0&to=9000000000000", nil)
	if status != 200 {
		t.Fatalf("timeline status=%d", status)
	}
	tm := tl.(map[string]any)
	if got := len(tm["ranges"].([]any)); got != 2 {
		t.Errorf("ranges = %d, want 2 (공백 분리)", got)
	}
	if got := len(tm["events"].([]any)); got != 1 {
		t.Errorf("events = %d, want 1", got)
	}

	// 플레이리스트 — DISCONTINUITY + ENDLIST + 2 세그먼트
	res, err := http.Get(srv.URL + fmt.Sprintf("/api/recordings/%s/playlist.m3u8?from=0&to=9000000000000", camID))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("playlist status=%d", res.StatusCode)
	}
	body := make([]byte, 4096)
	n, _ := res.Body.Read(body)
	text := string(body[:n])
	if !strings.Contains(text, "#EXT-X-DISCONTINUITY") {
		t.Error("공백 구간에 DISCONTINUITY 없음")
	}
	if !strings.Contains(text, "#EXT-X-ENDLIST") || !strings.Contains(text, "#EXTM3U") {
		t.Error("m3u8 필수 태그 누락")
	}
	if strings.Count(text, "/seg/") != 2 {
		t.Errorf("seg 라인 = %d, want 2", strings.Count(text, "/seg/"))
	}

	// 세그먼트 Range 요청 → 206
	rows, _ := app.recording.Store().Oldest(1)
	if len(rows) == 0 {
		t.Fatal("seed 실패")
	}
	res2, err := http.Get(srv.URL + fmt.Sprintf("/api/recordings/%s/seg/%d", camID, rows[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != 200 || res2.Header.Get("Content-Type") != "video/mp2t" {
		t.Errorf("seg status=%d type=%s", res2.StatusCode, res2.Header.Get("Content-Type"))
	}

	req3, _ := http.NewRequest(http.MethodGet, srv.URL+fmt.Sprintf("/api/recordings/%s/seg/%d", camID, rows[0].ID), nil)
	req3.Header.Set("Range", "bytes=0-99")
	res3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	res3.Body.Close()
	if res3.StatusCode != 206 {
		t.Errorf("Range status = %d, want 206", res3.StatusCode)
	}
}

func TestRecordingsOverview(t *testing.T) {
	app, srv := newTestAppFull(t)
	seedRecording(t, app, "cam-rec-1", 1_000_000)

	status, raw := doJSON(t, http.MethodGet, srv.URL+"/api/recordings/overview?from=0&to=9000000000000", nil)
	if status != 200 {
		t.Fatalf("overview status=%d", status)
	}
	m := raw.(map[string]any)
	cams := m["cameras"].([]any)
	if len(cams) != 1 {
		t.Fatalf("cameras = %d, want 1", len(cams))
	}
	c := cams[0].(map[string]any)
	if c["cameraId"] != "cam-rec-1" {
		t.Errorf("cameraId = %v", c["cameraId"])
	}
	if n, _ := c["segments"].(float64); n != 2 {
		t.Errorf("segments = %v, want 2", c["segments"])
	}
	if n, _ := c["events"].(float64); n != 1 {
		t.Errorf("events = %v, want 1", c["events"])
	}
	if n, _ := c["bytes"].(float64); n != 2820 {
		t.Errorf("bytes = %v, want 2820", c["bytes"])
	}
}
