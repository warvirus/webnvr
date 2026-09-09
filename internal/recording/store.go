// segments/events 테이블 DAO — 모든 재생·janitor 조회가 (camera_id, start_ts) 인덱스 range다.
package recording

import (
	"database/sql"
	"fmt"
	"sort"
)

// Flag 비트.
const (
	// FlagDiscontinuity — 이 세그먼트가 앞 구간과 시간적으로 불연속(재연결/포트 변경 등).
	FlagDiscontinuity = 1 << 0
)

// Segment는 저장된 세그먼트 하나의 인덱스 행이다.
type Segment struct {
	ID         int64
	CameraID   string
	Kind       string // continuous | event
	StartTS    int64  // 벽시계 epoch ms
	StartPTS   int64  // 90kHz
	DurMS      int64
	StorageIdx int
	RelPath    string
	Bytes      int64
	Flags      int
	Codec      string
}

// Store는 segments 테이블을 다룬다.
type Store struct {
	db *sql.DB
}

// NewStore는 열린 *sql.DB로 DAO를 만든다. (스키마는 internal/db 마이그레이션이 보장)
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const segCols = `id, camera_id, kind, start_ts, start_pts, dur_ms, storage_idx, rel_path, bytes, flags, codec`

func scanSeg(s interface{ Scan(...any) error }) (Segment, error) {
	var g Segment
	err := s.Scan(&g.ID, &g.CameraID, &g.Kind, &g.StartTS, &g.StartPTS, &g.DurMS,
		&g.StorageIdx, &g.RelPath, &g.Bytes, &g.Flags, &g.Codec)
	return g, err
}

// Insert는 세그먼트 행을 추가하고 발급된 id를 반환한다.
func (s *Store) Insert(g Segment) (int64, error) {
	if g.Kind == "" {
		g.Kind = "continuous"
	}
	res, err := s.db.Exec(
		`INSERT INTO segments (camera_id, kind, start_ts, start_pts, dur_ms, storage_idx, rel_path, bytes, flags, codec)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		g.CameraID, g.Kind, g.StartTS, g.StartPTS, g.DurMS, g.StorageIdx, g.RelPath, g.Bytes, g.Flags, g.Codec)
	if err != nil {
		return 0, fmt.Errorf("segments INSERT: %w", err)
	}
	return res.LastInsertId()
}

// Range는 카메라의 [fromMS, toMS] 구간과 겹치는 세그먼트를 start_ts 순으로 반환한다.
func (s *Store) Range(cameraID string, fromMS, toMS int64) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT `+segCols+` FROM segments
		 WHERE camera_id=? AND start_ts + dur_ms >= ? AND start_ts <= ?
		 ORDER BY start_ts`, cameraID, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// Oldest는 전 카메라를 가로질러 가장 오래된 세그먼트 최대 limit개를 반환한다. (janitor)
func (s *Store) Oldest(limit int) ([]Segment, error) {
	rows, err := s.db.Query(`SELECT `+segCols+` FROM segments ORDER BY start_ts LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// OlderThan은 start_ts가 cutoffMS 미만인 세그먼트 최대 limit개를 반환한다. (retention)
func (s *Store) OlderThan(cutoffMS int64, limit int) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT `+segCols+` FROM segments WHERE start_ts < ? ORDER BY start_ts LIMIT ?`, cutoffMS, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// SumBytes는 전체 발자국(bytes 합)을 반환한다.
func (s *Store) SumBytes() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(bytes), 0) FROM segments`).Scan(&n)
	return n, err
}

// Delete는 세그먼트 행을 제거한다. (파일 unlink는 호출자 책임)
func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM segments WHERE id=?`, id)
	return err
}

// Get은 id로 세그먼트를 조회한다.
func (s *Store) Get(id int64) (Segment, error) {
	return scanSeg(s.db.QueryRow(`SELECT `+segCols+` FROM segments WHERE id=?`, id))
}

// Event는 이벤트 트리거 기록(클립 메타)이다.
type Event struct {
	ID        int64
	CameraID  string
	TS        int64  // 트리거 벽시계 epoch ms
	Type      string // manual | schedule | onvif ...
	SegmentID int64  // 연결된 이벤트 클립 세그먼트
	Note      string
}

// InsertEvent는 이벤트 기록을 추가한다.
func (s *Store) InsertEvent(e Event) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO events (camera_id, ts, type, segment_id, note) VALUES (?,?,?,?,?)`,
		e.CameraID, e.TS, e.Type, e.SegmentID, e.Note)
	if err != nil {
		return 0, fmt.Errorf("events INSERT: %w", err)
	}
	return res.LastInsertId()
}

// EventsRange는 카메라의 [fromMS, toMS] 구간 이벤트를 ts 순으로 반환한다.
func (s *Store) EventsRange(cameraID string, fromMS, toMS int64) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT id, camera_id, ts, type, COALESCE(segment_id,0), note FROM events
		 WHERE camera_id=? AND ts BETWEEN ? AND ? ORDER BY ts`, cameraID, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.CameraID, &e.TS, &e.Type, &e.SegmentID, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CamOverview는 한 카메라의 구간 녹화 요약이다. (전 카메라 한눈에 보기)
type CamOverview struct {
	CameraID string
	Segments int64
	Bytes    int64
	FirstMS  int64
	LastMS   int64
	Events   int64
}

// Overview는 [fromMS, toMS] 구간의 카메라별 녹화 요약을 반환한다.
// 녹화가 없는 카메라는 결과에 없다.
func (s *Store) Overview(fromMS, toMS int64) ([]CamOverview, error) {
	rows, err := s.db.Query(
		`SELECT camera_id, COUNT(*), COALESCE(SUM(bytes),0),
		        MIN(start_ts), MAX(start_ts + dur_ms)
		 FROM segments
		 WHERE start_ts + dur_ms >= ? AND start_ts <= ?
		 GROUP BY camera_id`, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*CamOverview{}
	for rows.Next() {
		var o CamOverview
		if err := rows.Scan(&o.CameraID, &o.Segments, &o.Bytes, &o.FirstMS, &o.LastMS); err != nil {
			return nil, err
		}
		out[o.CameraID] = &o
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 이벤트 건수 병합
	evRows, err := s.db.Query(
		`SELECT camera_id, COUNT(*) FROM events WHERE ts BETWEEN ? AND ? GROUP BY camera_id`, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	defer evRows.Close()
	for evRows.Next() {
		var camID string
		var n int64
		if err := evRows.Scan(&camID, &n); err != nil {
			return nil, err
		}
		if o, ok := out[camID]; ok {
			o.Events = n
		}
	}
	return collectOverview(out), evRows.Err()
}

// DayCount는 하루 단위 녹화 요약이다. (달력 표시용 — day는 서버 로컬 기준 YYYY-MM-DD)
type DayCount struct {
	Day      string
	Segments int64
	Bytes    int64
	Cameras  int64
}

// DayCounts는 [fromMS, toMS] 구간의 날짜별 녹화 요약을 반환한다. 녹화가 없는 날은 결과에 없다.
// 세그먼트는 시작 시각의 날짜로 귀속된다(자정 걸침 세그먼트는 시작일에만 계수).
func (s *Store) DayCounts(fromMS, toMS int64) ([]DayCount, error) {
	rows, err := s.db.Query(
		`SELECT date(start_ts/1000, 'unixepoch', 'localtime') AS day,
		        COUNT(*), COALESCE(SUM(bytes),0), COUNT(DISTINCT camera_id)
		 FROM segments
		 WHERE start_ts + dur_ms >= ? AND start_ts <= ?
		 GROUP BY day ORDER BY day`, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayCount
	for rows.Next() {
		var d DayCount
		if err := rows.Scan(&d.Day, &d.Segments, &d.Bytes, &d.Cameras); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func collectOverview(m map[string]*CamOverview) []CamOverview {
	out := make([]CamOverview, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CameraID < out[j].CameraID })
	return out
}

func collect(rows *sql.Rows) ([]Segment, error) {
	defer rows.Close()
	var out []Segment
	for rows.Next() {
		g, err := scanSeg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
