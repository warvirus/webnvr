// 카메라 하나의 녹화기 — 액세스 유닛을 소비해 IDR 경계로 세그먼트를 회전한다.
package recording

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"webnvr/internal/stream"
)

// nowMS는 벽시계(epoch ms)다. 테스트에서 교체한다.
var nowMS = func() int64 { return time.Now().UnixMilli() }

const mib = 1 << 20

// Recorder는 한 카메라의 상시 녹화를 담당한다. 메서드는 동시 호출에 안전하다.
type Recorder struct {
	cameraID    string
	storageRoot string // 절대 경로
	storageIdx  int
	segMS       int64 // 세그먼트 목표 길이 (ms)
	segMaxB     int64 // 강제 컷 크기 (bytes)
	store       *Store

	mu       sync.Mutex
	codec    stream.Codec
	sps      []byte
	pps      []byte
	vps      []byte
	cur      segmentSink
	curSeg   *Segment
	openWall int64
	openPTS  int64
	lastPTS  int64
	pendDiscont bool
	rejected bool
	closed   bool
}

// NewRecorder는 절대 storageRoot와 세그먼트 정책으로 녹화기를 만든다.
func NewRecorder(cameraID, storageRoot string, storageIdx, segmentSeconds, segmentMaxMB int, store *Store) *Recorder {
	return &Recorder{
		cameraID:    cameraID,
		storageRoot: storageRoot,
		storageIdx:  storageIdx,
		segMS:       int64(segmentSeconds) * 1000,
		segMaxB:     int64(segmentMaxMB) * mib,
		store:       store,
	}
}

// OnInfo는 스트림 (재)시작 시 코덱과 파라미터 셋을 알린다.
func (r *Recorder) OnInfo(codec stream.Codec, sps, pps, vps []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sps = append([]byte(nil), sps...)
	r.pps = append([]byte(nil), pps...)
	r.vps = append([]byte(nil), vps...)
	if r.codec != "" && r.codec != codec {
		r.closeCurLocked()
		r.pendDiscont = true
	}
	r.codec = codec
	r.rejected = Decide(codec) != ActionRemux
	if r.rejected {
		slog.Warn("녹화 미지원 코덱 — 이 카메라는 저장하지 않음", "camera", r.cameraID, "codec", codec)
	}
}

// OnGap은 스트림이 끊겼음을 알린다 — 현재 세그먼트를 닫고 다음 세그먼트에 불연속 표시.
func (r *Recorder) OnGap() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCurLocked()
	r.pendDiscont = true
}

// OnNALU는 액세스 유닛 하나를 소비한다. pts는 90kHz, key는 IDR 포함 여부.
// 절대 오래 블로킹하지 않는다(쓰기는 버퍼링, 오류 시 세그먼트를 끊고 불연속 표시).
func (r *Recorder) OnNALU(codec stream.Codec, nalus [][]byte, pts int64, key bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.codec == "" {
		r.codec = codec
		r.rejected = Decide(codec) != ActionRemux
	}
	if r.rejected || codec != r.codec {
		return
	}

	now := nowMS()
	if key {
		needNew := r.cur == nil ||
			now-r.openWall >= r.segMS ||
			r.cur.bytesWritten() >= r.segMaxB
		if needNew {
			r.closeCurLocked()
			r.openCurLocked(now, pts)
		}
	}
	if r.cur == nil {
		return // 첫 IDR 대기
	}
	if err := r.cur.write(nalus, pts, key); err != nil {
		slog.Warn("세그먼트 write 실패 — 세그먼트 끊고 불연속 표시", "camera", r.cameraID, "err", err)
		r.closeCurLocked()
		r.pendDiscont = true
		return
	}
	if pts > r.lastPTS {
		r.lastPTS = pts
	}
}

// Close는 진행 중인 세그먼트를 flush하고 녹화기를 정지한다.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCurLocked()
	r.closed = true
}

// closeCurLocked는 현재 세그먼트를 닫고 segments 행을 INSERT한다.
func (r *Recorder) closeCurLocked() {
	if r.cur == nil {
		return
	}
	bytes, err := r.cur.close()
	if err != nil {
		slog.Warn("세그먼트 닫기 실패", "camera", r.cameraID, "err", err)
	}
	seg := *r.curSeg
	seg.Bytes = bytes
	if d := (r.lastPTS - r.openPTS) * 1000 / 90000; d > 0 {
		seg.DurMS = d
	}
	if _, err := r.store.Insert(seg); err != nil {
		slog.Error("segments INSERT 실패", "camera", r.cameraID, "path", seg.RelPath, "err", err)
	} else {
		slog.Info("세그먼트 저장", "camera", r.cameraID, "path", seg.RelPath, "bytes", bytes, "dur_ms", seg.DurMS)
	}
	r.cur = nil
	r.curSeg = nil
}

// openCurLocked는 새 세그먼트 파일을 만든다. key AU 시점에만 호출된다.
func (r *Recorder) openCurLocked(wallMS, pts int64) {
	t := time.UnixMilli(wallMS)
	rel := filepath.ToSlash(filepath.Join(r.cameraID, t.Format("2006-01-02"), t.Format("15-04-05")+".ts"))
	abs := filepath.Join(r.storageRoot, filepath.FromSlash(rel))
	// 같은 초에 두 세그먼트가 열리는 드문 경우 파일명 충돌 방지
	for i := 2; ; i++ {
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			break
		}
		rel = filepath.ToSlash(filepath.Join(r.cameraID, t.Format("2006-01-02"),
			t.Format("15-04-05")+"-"+strconv.Itoa(i)+".ts"))
		abs = filepath.Join(r.storageRoot, filepath.FromSlash(rel))
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		slog.Error("녹화 디렉토리 생성 실패", "path", abs, "err", err)
		return
	}
	sink, err := newSink(abs, r.codec)
	if err != nil {
		slog.Error("세그먼트 생성 실패", "path", abs, "err", err)
		return
	}
	if ts, ok := sink.(*tsSink); ok {
		ts.sps = append([]byte(nil), r.sps...)
		ts.pps = append([]byte(nil), r.pps...)
		ts.vps = append([]byte(nil), r.vps...)
	}
	flags := 0
	if r.pendDiscont {
		flags = FlagDiscontinuity
		r.pendDiscont = false
	}
	r.cur = sink
	r.curSeg = &Segment{
		CameraID: r.cameraID, Kind: "continuous",
		StartTS: wallMS, StartPTS: pts, StorageIdx: r.storageIdx,
		RelPath: rel, Flags: flags, Codec: string(r.codec),
	}
	r.openWall = wallMS
	r.openPTS = pts
	r.lastPTS = pts
}
