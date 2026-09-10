// 카메라 하나의 녹화기 — 액세스 유닛을 소비해 IDR 경계로 세그먼트를 회전한다.
// 모드: continuous(상시 회전 세그먼트) / event(pre-roll 링버퍼 + 트리거 클립) / both(둘 다).
package recording

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"webnvr/internal/camera"
	"webnvr/internal/stream"
)

// nowMS는 벽시계(epoch ms)다. 테스트에서 교체한다.
var nowMS = func() int64 { return time.Now().UnixMilli() }

// eventTypeRe는 이벤트 유형 화이트리스트다. 파일명에 쓰이므로 경로 조작을 차단한다.
var eventTypeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ValidEventType은 이벤트 유형이 파일명으로 안전한지 검사한다.
func ValidEventType(typ string) bool { return eventTypeRe.MatchString(typ) }

const mib = 1 << 20

// RecorderConfig는 녹화기 생성 설정이다.
type RecorderConfig struct {
	CameraID        string
	Pool            *StoragePool
	SegmentSeconds  int
	SegmentMaxMB    int
	Mode            string // camera.RecordOff | RecordContinuous | RecordEvent | RecordBoth
	PreRollSeconds  int
	PostRollSeconds int
	Store           *Store
}

// Recorder는 한 카메라의 녹화를 담당한다. 메서드는 동시 호출에 안전하다.
type Recorder struct {
	cameraID string
	pool     *StoragePool
	segMS    int64 // 세그먼트 목표 길이 (ms)
	segMaxB  int64 // 강제 컷 크기 (bytes)
	mode     string
	preRollB int64 // 90kHz
	postRoll int64 // ms
	store    *Store

	lastIdx int // 마지막으로 쓴 스토리지 인덱스 (다음 Pick 선호)

	mu          sync.Mutex
	codec       stream.Codec
	sps         []byte
	pps         []byte
	vps         []byte
	cur         segmentSink // 상시 회전 세그먼트 (continuous/both)
	curSeg      *Segment
	openWall    int64
	openPTS     int64
	lastPTS     int64
	pendDiscont bool
	rejected    bool
	closed      bool

	// 이벤트 클립 (event/both)
	eventCapable bool
	ring         []ringAU
	ringBytes    int64 // 링버퍼 대략적 바이트 합 (무한 성장 방지용 상한)
	evSink       segmentSink
	evSeg        *Segment
	evOpenPTS    int64
	evTriggerTS  int64 // 트리거 벽시계 (events 행)
	evType       string
	evDeadline   int64 // 벽시계 ms
}

// ringAU는 pre-roll 링버퍼의 액세스 유닛 하나다.
type ringAU struct {
	nalus [][]byte
	pts   int64
	wall  int64
}

// NewRecorder는 설정으로 녹화기를 만든다.
func NewRecorder(cfg RecorderConfig) *Recorder {
	mode := cfg.Mode
	if mode != camera.RecordEvent && mode != camera.RecordBoth {
		mode = camera.RecordContinuous
	}
	preRoll := cfg.PreRollSeconds
	if preRoll <= 0 {
		preRoll = 10
	}
	postRoll := cfg.PostRollSeconds
	if postRoll <= 0 {
		postRoll = 15
	}
	return &Recorder{
		cameraID:     cfg.CameraID,
		pool:         cfg.Pool,
		segMS:        int64(cfg.SegmentSeconds) * 1000,
		segMaxB:      int64(cfg.SegmentMaxMB) * mib,
		mode:         mode,
		preRollB:     int64(preRoll) * 90000,
		postRoll:     int64(postRoll) * 1000,
		store:        cfg.Store,
		eventCapable: mode == camera.RecordEvent || mode == camera.RecordBoth,
	}
}

// continuous는 상시 회전 세그먼트를 기록하는 모드인지 반환한다.
func (r *Recorder) continuous() bool {
	return r.mode == camera.RecordContinuous || r.mode == camera.RecordBoth
}

// SetMode는 녹화 모드를 동적으로 전환한다. 세션(RTSP)은 유지되며 열린 세그먼트만 정리한다.
// 모드가 실제로 바뀌면 true를 반환한다.
func (r *Recorder) SetMode(mode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.mode == mode {
		return false
	}
	r.mode = mode
	r.eventCapable = mode == camera.RecordEvent || mode == camera.RecordBoth
	if !r.continuous() {
		r.closeCurLocked()
	} else {
		r.pendDiscont = true // 재개 세그먼트는 불연속
	}
	if !r.eventCapable {
		r.closeEvLocked()
		r.ring = r.ring[:0]
		r.ringBytes = 0
	}
	return true
}

// Mode는 현재 녹화 모드를 반환한다.
func (r *Recorder) Mode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mode
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
		r.closeEvLocked()
		r.pendDiscont = true
	}
	r.codec = codec
	r.rejected = Decide(codec) != ActionRemux
	if r.rejected {
		slog.Warn("녹화 미지원 코덱 — 이 카메라는 저장하지 않음", "camera", r.cameraID, "codec", codec)
	}
}

// OnGap은 스트림이 끊겼음을 알린다 — 열린 세그먼트를 닫고 다음 세그먼트에 불연속 표시.
// 링버퍼도 비운다(끊긴 구간을 pre-roll로 쓰면 클립이 오답이 된다).
func (r *Recorder) OnGap() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCurLocked()
	r.closeEvLocked()
	r.ring = r.ring[:0]
	r.ringBytes = 0
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
	if r.continuous() && key {
		needNew := r.cur == nil ||
			now-r.openWall >= r.segMS ||
			r.cur.bytesWritten() >= r.segMaxB
		if needNew {
			r.closeCurLocked()
			r.openCurLocked(now, pts)
		}
	}
	if r.eventCapable {
		r.ringAppendLocked(nalus, pts, now)
	}
	if r.cur != nil {
		if err := r.cur.write(nalus, pts, key); err != nil {
			slog.Warn("세그먼트 write 실패 — 세그먼트 끊고 불연속 표시", "camera", r.cameraID, "err", err)
			r.closeCurLocked()
			r.pendDiscont = true
		}
	}
	if r.evSink != nil {
		if err := r.evSink.write(nalus, pts, key); err != nil {
			slog.Warn("이벤트 클립 write 실패 — 클립을 닫음", "camera", r.cameraID, "err", err)
			r.closeEvLocked()
		} else if now >= r.evDeadline {
			r.closeEvLocked()
		}
	}
	if pts > r.lastPTS {
		r.lastPTS = pts
	}
}

// TriggerEvent는 이벤트 녹화를 트리거한다 — pre-roll 링버퍼를 클립으로 flush하고
// post_roll_seconds 동안 더 기록한 뒤 닫는다. 이미 진행 중이면 post-roll을 연장한다.
func (r *Recorder) TriggerEvent(typ string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, errors.New("녹화기가 정지됨")
	}
	if !r.eventCapable {
		return 0, fmt.Errorf("이벤트 녹화 모드가 아닌 카메라입니다 (mode=%s)", r.mode)
	}
	if r.rejected || r.codec == "" {
		return 0, errors.New("코덱이 확정되지 않아 트리거할 수 없음 (스트림 확인 필요)")
	}
	if typ == "" {
		typ = "manual"
	}
	// 유형은 클립 파일명에 쓰이므로 경로 조작을 차단한다
	if !eventTypeRe.MatchString(typ) {
		return 0, fmt.Errorf("이벤트 유형은 영문/숫자/_/- 32자 이하여야 합니다")
	}
	now := nowMS()
	if r.evSink != nil {
		r.evDeadline = now + r.postRoll // 진행 중 클립 연장
		return 0, nil
	}
	idx, root, err := r.pool.Pick(r.lastIdx)
	if err != nil {
		return 0, fmt.Errorf("스토리지 대상 없음: %w", err)
	}
	r.lastIdx = idx
	rel, err := uniqueRel(root, filepath.ToSlash(filepath.Join(r.cameraID, "events",
		fmt.Sprintf("%d_%s.ts", now, typ))))
	if err != nil {
		return 0, err
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, fmt.Errorf("이벤트 디렉토리 생성 실패: %w", err)
	}
	sink, err := newSink(abs, r.codec)
	if err != nil {
		return 0, fmt.Errorf("이벤트 클립 생성 실패: %w", err)
	}
	if ts, ok := sink.(*tsSink); ok {
		ts.sps = append([]byte(nil), r.sps...)
		ts.pps = append([]byte(nil), r.pps...)
		ts.vps = append([]byte(nil), r.vps...)
	}
	r.evSink = sink
	r.evSeg = &Segment{
		CameraID: r.cameraID, Kind: "event",
		StorageIdx: idx, RelPath: rel, Codec: string(r.codec),
	}
	r.evTriggerTS = now
	r.evType = typ
	r.evOpenPTS = r.lastPTS
	if len(r.ring) > 0 {
		r.evOpenPTS = r.ring[0].pts
		r.evSeg.StartTS = r.ring[0].wall
		r.evSeg.StartPTS = r.ring[0].pts
		for _, e := range r.ring {
			if werr := sink.write(e.nalus, e.pts, false); werr != nil {
				slog.Warn("pre-roll flush 실패 — 이후 AU부터 기록", "camera", r.cameraID, "err", werr)
				break
			}
		}
		r.ring = r.ring[:0] // 링버퍼는 클립으로 소비됨 — ringBytes도 함께 초기화
		r.ringBytes = 0
	}
	if r.evSeg.StartTS == 0 {
		r.evSeg.StartTS = now
	}
	r.evDeadline = now + r.postRoll
	slog.Info("이벤트 녹화 시작", "camera", r.cameraID, "type", typ,
		"pre_roll_entries", len(r.ring), "post_roll_ms", r.postRoll)
	return 0, nil
}

// Close는 진행 중 세그먼트/클립을 flush하고 녹화기를 정지한다.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCurLocked()
	r.closeEvLocked()
	r.closed = true
}

// ringAppendLocked는 AU를 링버퍼에 넣고 pre_roll 범위를 유지한다.
// PTS 정체/역행 카메라(펌웨어 버그, RTP base 리셋)에서 링이 무한 성장하지 않게
// 시간 기준 외에 개수·바이트 상한도 둔다.
func (r *Recorder) ringAppendLocked(nalus [][]byte, pts, wall int64) {
	cp := make([][]byte, len(nalus))
	total := 0
	for i, n := range nalus {
		cp[i] = append([]byte(nil), n...)
		total += len(n)
	}
	r.ring = append(r.ring, ringAU{nalus: cp, pts: pts, wall: wall})
	r.ringBytes += int64(total)
	for len(r.ring) > 1 && (pts-r.ring[0].pts > r.preRollB || r.ringBytes > ringMaxBytes || len(r.ring) > ringMaxAUs) {
		for _, n := range r.ring[0].nalus {
			r.ringBytes -= int64(len(n))
		}
		r.ring = r.ring[1:]
	}
}

// 링버퍼 상한 — 30fps 10초 pre-roll은 약 900AU 수준이므로 충분한 여유다.
const (
	ringMaxAUs   = 3600      // 개수 상한 (2분 @30fps)
	ringMaxBytes = 256 << 20 // 256MB 상한 (4K 고프레임 대비)
)

// closeCurLocked는 현재 상시 세그먼트를 닫고 segments 행을 INSERT한다.
func (r *Recorder) closeCurLocked() {
	if r.cur == nil {
		return
	}
	r.insertSegLocked(r.cur, r.curSeg, r.openPTS, "세그먼트")
	r.cur = nil
	r.curSeg = nil
}

// closeEvLocked는 진행 중 이벤트 클립을 닫고 segments/events 행을 INSERT한다.
func (r *Recorder) closeEvLocked() {
	if r.evSink == nil {
		return
	}
	seg := r.evSeg
	if d := (r.lastPTS - r.evOpenPTS) * 1000 / 90000; d > 0 {
		seg.DurMS = d
	}
	id, err := r.insertSegLocked(r.evSink, seg, r.evOpenPTS, "이벤트 클립")
	if err == nil && r.store != nil {
		evID, err := r.store.InsertEvent(Event{
			CameraID: r.cameraID, TS: r.evTriggerTS, Type: r.evType, SegmentID: id,
		})
		if err != nil {
			slog.Error("events INSERT 실패", "camera", r.cameraID, "err", err)
		} else {
			slog.Info("이벤트 기록", "camera", r.cameraID, "event_id", evID,
				"type", r.evType, "segment_id", id)
		}
	}
	r.evSink = nil
	r.evSeg = nil
}

// insertSegLocked는 sink를 닫고 segments 행을 INSERT한다. 발급 id를 반환한다.
func (r *Recorder) insertSegLocked(sink segmentSink, seg *Segment, openPTS int64, label string) (int64, error) {
	bytes, err := sink.close()
	if err != nil {
		slog.Warn("세그먼트 닫기 실패", "camera", r.cameraID, "err", err)
	}
	seg.Bytes = bytes
	if d := (r.lastPTS - openPTS) * 1000 / 90000; d > 0 {
		seg.DurMS = d
	}
	var id int64
	if r.store != nil {
		id, err = r.store.Insert(*seg)
		if err != nil {
			slog.Error("segments INSERT 실패", "camera", r.cameraID, "path", seg.RelPath, "err", err)
		}
	}
	slog.Info(label+" 저장", "camera", r.cameraID, "path", seg.RelPath,
		"bytes", bytes, "dur_ms", seg.DurMS, "kind", seg.Kind)
	return id, err
}

// openCurLocked는 새 상시 세그먼트 파일을 만든다. key AU 시점에만 호출된다.
func (r *Recorder) openCurLocked(wallMS, pts int64) {
	idx, root, err := r.pool.Pick(r.lastIdx)
	if err != nil {
		slog.Error("스토리지 대상 없음 — 이 GOP는 건너뜀", "camera", r.cameraID, "err", err)
		r.pendDiscont = true
		return
	}
	r.lastIdx = idx
	t := time.UnixMilli(wallMS)
	rel, err := uniqueRel(root, filepath.ToSlash(filepath.Join(r.cameraID,
		t.Format("2006-01-02"), t.Format("15-04-05")+".ts")))
	if err != nil {
		slog.Error("세그먼트 경로 생성 실패", "camera", r.cameraID, "err", err)
		return
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
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
		StartTS: wallMS, StartPTS: pts, StorageIdx: idx,
		RelPath: rel, Flags: flags, Codec: string(r.codec),
	}
	r.openWall = wallMS
	r.openPTS = pts
	r.lastPTS = pts
}

// uniqueRel은 같은 초에 열리는 파일의 이름 충돌을 피한다(-N 접미사).
func uniqueRel(root, rel string) (string, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	for i := 2; ; i++ {
		_, err := os.Stat(abs)
		if err == nil {
			// 충돌 — 접미사를 붙여 재시도
		} else if os.IsNotExist(err) {
			return rel, nil
		} else {
			// 권한/I/O 오류를 존재로 오판하면 무한 루프가 된다 — 즉시 실패
			return "", fmt.Errorf("세그먼트 경로 확인 실패 (%s): %w", abs, err)
		}
		ext := filepath.Ext(rel)
		rel = rel[:len(rel)-len(ext)] + "-" + strconv.Itoa(i) + ext
		abs = filepath.Join(root, filepath.FromSlash(rel))
	}
}
