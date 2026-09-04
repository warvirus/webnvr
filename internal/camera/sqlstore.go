// Store 인터페이스의 SQLite 구현이다. (Phase 6.2/6.3 — JSONCameraStore는 폴백/테스트용으로 유지)
package camera

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// camColumns는 cameras 테이블의 컬럼 순서다. SELECT/scan/INSERT가 이 순서를 공유한다.
const camColumns = `id, name, type, xaddr, username, password, profile_token, stream_url,
	transport, protocol, buffer_size, ptz_supported, group_id, layout_order, enabled,
	record_mode, pre_roll_seconds, post_roll_seconds, added_at, updated_at`

// SchemaSQL은 카메라 도메인의 SQLite 스키마다. internal/db의 마이그레이션이 이를 조립해 적용한다.
const SchemaSQL = `
CREATE TABLE cameras (
	id            TEXT PRIMARY KEY,
	name          TEXT NOT NULL,
	type          TEXT NOT NULL,
	xaddr         TEXT NOT NULL DEFAULT '',
	username      TEXT NOT NULL DEFAULT '',
	password      TEXT NOT NULL DEFAULT '',
	profile_token TEXT NOT NULL DEFAULT '',
	stream_url    TEXT NOT NULL DEFAULT '',
	transport     TEXT NOT NULL DEFAULT 'tcp',
	protocol      TEXT NOT NULL DEFAULT 'rtsp',
	buffer_size   INTEGER NOT NULL DEFAULT 1048576,
	ptz_supported INTEGER NOT NULL DEFAULT 0,
	group_id      TEXT NOT NULL DEFAULT '',
	layout_order  INTEGER NOT NULL DEFAULT 0,
	enabled       INTEGER NOT NULL DEFAULT 1,
	added_at      TEXT NOT NULL,
	updated_at    TEXT NOT NULL
);
CREATE INDEX idx_cameras_layout ON cameras(layout_order);
CREATE TABLE groups (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	layout_type TEXT NOT NULL DEFAULT 'grid',
	cols        INTEGER NOT NULL DEFAULT 2,
	rows        INTEGER NOT NULL DEFAULT 2
);`

// RecordColumnsSQL은 Phase R 카메라별 녹화 컬럼을 추가한다.
// internal/db 마이그레이션 #3이 이를 적용하며, cameras 테이블 단독 테스트도 재사용한다.
const RecordColumnsSQL = `
ALTER TABLE cameras ADD COLUMN record_mode TEXT NOT NULL DEFAULT 'off';
ALTER TABLE cameras ADD COLUMN pre_roll_seconds INTEGER NOT NULL DEFAULT 10;
ALTER TABLE cameras ADD COLUMN post_roll_seconds INTEGER NOT NULL DEFAULT 15;`

// SQLCameraStore는 Store를 SQLite로 구현한다. 상태를 캐시하지 않고 매 호출을 DB에 위임한다.
type SQLCameraStore struct {
	db *sql.DB
}

// NewSQLCameraStore는 열린 *sql.DB로 저장소를 만든다. (스키마는 internal/db가 보장)
func NewSQLCameraStore(db *sql.DB) *SQLCameraStore {
	return &SQLCameraStore{db: db}
}

// rowScanner는 *sql.Row와 *sql.Rows를 모두 받는다.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanCamera는 camColumns 순서의 한 행을 Camera로 읽는다.
func scanCamera(s rowScanner) (Camera, error) {
	var c Camera
	var ptz, enabled int
	var addedAt, updatedAt string
	err := s.Scan(
		&c.ID, &c.Name, &c.Type, &c.XAddr, &c.Username, &c.Password, &c.ProfileToken, &c.StreamURL,
		&c.StreamConfig.Transport, &c.StreamConfig.Protocol, &c.StreamConfig.BufferSize,
		&ptz, &c.GroupID, &c.LayoutOrder, &enabled,
		&c.RecordMode, &c.PreRoll, &c.PostRoll, &addedAt, &updatedAt,
	)
	if err != nil {
		return Camera{}, err
	}
	c.PTZSupported = ptz != 0
	c.Enabled = enabled != 0
	c.AddedAt, _ = time.Parse(time.RFC3339Nano, addedAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return c, nil
}

// execer는 *sql.DB와 *sql.Tx를 모두 받는다.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// insertCamera는 Camera를 모든 필드 그대로 INSERT한다. (Add/ImportFrom 공용)
func insertCamera(e execer, c Camera) error {
	_, err := e.Exec(
		`INSERT INTO cameras (`+camColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, string(c.Type), c.XAddr, c.Username, c.Password, c.ProfileToken, c.StreamURL,
		c.StreamConfig.Transport, c.StreamConfig.Protocol, c.StreamConfig.BufferSize,
		b2i(c.PTZSupported), c.GroupID, c.LayoutOrder, b2i(c.Enabled),
		recModeOr(c.RecordMode), prerollOr(c.PreRoll), postrollOr(c.PostRoll),
		c.AddedAt.UTC().Format(time.RFC3339Nano), c.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// record_mode/pre/post-roll의 빈 값을 스키마 기본값으로 보정한다.
func recModeOr(m string) string {
	if m == "" {
		return RecordOff
	}
	return m
}
func prerollOr(n int) int {
	if n <= 0 {
		return 10
	}
	return n
}
func postrollOr(n int) int {
	if n <= 0 {
		return 15
	}
	return n
}

// List는 모든 카메라를 layout_order 순으로 반환한다.
func (s *SQLCameraStore) List() ([]Camera, error) {
	rows, err := s.db.Query(`SELECT ` + camColumns + ` FROM cameras ORDER BY layout_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Camera{}
	for rows.Next() {
		c, err := scanCamera(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get은 ID로 카메라를 조회한다. 없으면 오류를 반환한다.
func (s *SQLCameraStore) Get(id string) (*Camera, error) {
	row := s.db.QueryRow(`SELECT `+camColumns+` FROM cameras WHERE id=?`, id)
	c, err := scanCamera(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("카메라를 찾을 수 없음: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Add는 새 카메라를 저장하고 ID와 타임스탬프가 채워진 결과를 반환한다.
func (s *SQLCameraStore) Add(cam Camera) (*Camera, error) {
	if cam.ID == "" {
		id, err := NewID()
		if err != nil {
			return nil, err
		}
		cam.ID = id
	}
	if _, err := s.Get(cam.ID); err == nil {
		return nil, fmt.Errorf("중복된 카메라 ID: %s", cam.ID)
	}
	now := time.Now().UTC()
	cam.AddedAt = now
	cam.UpdatedAt = now
	if cam.LayoutOrder == 0 {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM cameras`).Scan(&n); err != nil {
			return nil, err
		}
		cam.LayoutOrder = n
	}
	if err := insertCamera(s.db, cam); err != nil {
		return nil, err
	}
	c := cam
	return &c, nil
}

// Update는 기존 카메라를 교체하고 UpdatedAt을 갱신한다. AddedAt은 보존한다.
func (s *SQLCameraStore) Update(cam Camera) (*Camera, error) {
	var addedAt string
	err := s.db.QueryRow(`SELECT added_at FROM cameras WHERE id=?`, cam.ID).Scan(&addedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("카메라를 찾을 수 없음: %s", cam.ID)
	}
	if err != nil {
		return nil, err
	}
	cam.AddedAt, _ = time.Parse(time.RFC3339Nano, addedAt)
	cam.UpdatedAt = time.Now().UTC()
	_, err = s.db.Exec(
		`UPDATE cameras SET name=?, type=?, xaddr=?, username=?, password=?, profile_token=?,
		 stream_url=?, transport=?, protocol=?, buffer_size=?, ptz_supported=?, group_id=?,
		 layout_order=?, enabled=?, record_mode=?, pre_roll_seconds=?, post_roll_seconds=?,
		 updated_at=? WHERE id=?`,
		cam.Name, string(cam.Type), cam.XAddr, cam.Username, cam.Password, cam.ProfileToken,
		cam.StreamURL, cam.StreamConfig.Transport, cam.StreamConfig.Protocol, cam.StreamConfig.BufferSize,
		b2i(cam.PTZSupported), cam.GroupID, cam.LayoutOrder, b2i(cam.Enabled),
		recModeOr(cam.RecordMode), prerollOr(cam.PreRoll), postrollOr(cam.PostRoll),
		cam.UpdatedAt.Format(time.RFC3339Nano), cam.ID,
	)
	if err != nil {
		return nil, err
	}
	c := cam
	return &c, nil
}

// Delete는 카메라를 제거하고 남은 카메라의 layout_order를 0..n-1로 재정렬한다.
func (s *SQLCameraStore) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM cameras WHERE id=?`, id)
	if err != nil {
		return err
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		return fmt.Errorf("카메라를 찾을 수 없음: %s", id)
	}
	if err := renumber(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Reorder는 주어진 ID 순서대로 layout_order를 다시 배정한다.
func (s *SQLCameraStore) Reorder(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM cameras`).Scan(&n); err != nil {
		return err
	}
	if len(ids) != n {
		return fmt.Errorf("순서 목록(%d)이 카메라 수(%d)와 일치하지 않음", len(ids), n)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for order, id := range ids {
		res, err := tx.Exec(`UPDATE cameras SET layout_order=?, updated_at=? WHERE id=?`, order, now, id)
		if err != nil {
			return err
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			return fmt.Errorf("존재하지 않는 카메라 ID: %s", id)
		}
	}
	return tx.Commit()
}

// renumber는 layout_order를 현재 정렬 순서대로 0..n-1로 다시 매긴다.
func renumber(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id FROM cameras ORDER BY layout_order`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE cameras SET layout_order=? WHERE id=?`, i, id); err != nil {
			return err
		}
	}
	return nil
}

// ListGroups는 모든 그룹을 반환한다. (Store 인터페이스 외 — JSONCameraStore와 시그니처 동일)
func (s *SQLCameraStore) ListGroups() []Group {
	rows, err := s.db.Query(`SELECT id, name, layout_type, cols, rows FROM groups`)
	if err != nil {
		return []Group{}
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.LayoutType, &g.LayoutCols, &g.LayoutRows); err != nil {
			return out
		}
		out = append(out, g)
	}
	return out
}

// ImportFrom은 CamerasFile의 카메라/그룹을 모든 필드 그대로 넣는다. (JSON → SQLite 1회 이관용)
func (s *SQLCameraStore) ImportFrom(f CamerasFile) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, c := range f.Cameras {
		if c.AddedAt.IsZero() {
			c.AddedAt = time.Now().UTC()
		}
		if c.UpdatedAt.IsZero() {
			c.UpdatedAt = c.AddedAt
		}
		if c.StreamConfig.Transport == "" {
			c.StreamConfig.Transport = "tcp"
		}
		if c.StreamConfig.Protocol == "" {
			c.StreamConfig.Protocol = "rtsp"
		}
		if c.StreamConfig.BufferSize == 0 {
			c.StreamConfig.BufferSize = 1048576
		}
		if err := insertCamera(tx, c); err != nil {
			return fmt.Errorf("카메라 %s 이관 실패: %w", c.ID, err)
		}
	}
	for _, g := range f.Groups {
		if g.LayoutType == "" {
			g.LayoutType = "grid"
		}
		if _, err := tx.Exec(
			`INSERT INTO groups (id, name, layout_type, cols, rows) VALUES (?,?,?,?,?)`,
			g.ID, g.Name, g.LayoutType, g.LayoutCols, g.LayoutRows,
		); err != nil {
			return fmt.Errorf("그룹 %s 이관 실패: %w", g.ID, err)
		}
	}
	return tx.Commit()
}
