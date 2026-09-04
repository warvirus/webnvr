// webnvr의 SQLite 저장소를 연다 — 스키마 마이그레이션과 기존 JSON(config/*.json)의 1회 이관을 담당한다.
package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // 순수 Go SQLite 드라이버 (cgo 불필요) — 드라이버명 "sqlite"

	"webnvr/internal/camera"
	"webnvr/internal/config"
)

// DBFile은 설정 디렉토리 안의 SQLite 파일명이다.
const DBFile = "webnvr.db"

// migrations는 순차 적용되는 스키마다. PRAGMA user_version이 적용 완료 개수를 가리킨다.
// 새 스키마(예: Phase R의 segments 테이블)는 뒤에 추가한다 — 기존 항목은 절대 수정하지 않는다.
var migrations = []string{
	camera.SchemaSQL + `
	CREATE TABLE app_config (
		id   INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	);`,
	// #2 — Phase R 녹화 인덱스. 세그먼트를 닫을 때마다 INSERT 한 줄, 모든 재생/janitor 조회가 인덱스 range.
	`CREATE TABLE segments (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		camera_id   TEXT NOT NULL,
		kind        TEXT NOT NULL DEFAULT 'continuous',   -- continuous | event
		start_ts    INTEGER NOT NULL,                     -- 벽시계 epoch ms (세그먼트 시작)
		start_pts   INTEGER NOT NULL DEFAULT 0,           -- 90kHz PTS (내부 연속성 판정용)
		dur_ms      INTEGER NOT NULL DEFAULT 0,
		storage_idx INTEGER NOT NULL DEFAULT 0,
		rel_path    TEXT NOT NULL,                        -- storage 루트 기준 상대 경로
		bytes       INTEGER NOT NULL DEFAULT 0,
		flags       INTEGER NOT NULL DEFAULT 0,           -- bit0 = 앞 구간과 불연속
		codec       TEXT NOT NULL DEFAULT 'h264'
	);
	CREATE INDEX idx_segments_cam_ts ON segments(camera_id, start_ts);
	CREATE TABLE events (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		camera_id  TEXT NOT NULL,
		ts         INTEGER NOT NULL,                      -- 벽시계 epoch ms
		type       TEXT NOT NULL,                         -- manual | schedule | onvif ...
		segment_id INTEGER,                               -- 연결된 이벤트 클립 세그먼트
		note       TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX idx_events_cam_ts ON events(camera_id, ts);`,
	// #3 — Phase R 카메라별 녹화 모드. off면 녹화기가 생성되지 않는다.
	camera.RecordColumnsSQL,
}

// DB는 열린 SQLite 연결과 설정 디렉토리를 감싼다.
type DB struct {
	sql *sql.DB
	dir string
}

// Open은 <configDir>/webnvr.db를 열고 스키마를 적용한 뒤,
// DB가 비어 있고 기존 JSON 파일이 있으면 1회 이관한다(원본은 .bak으로 보존).
func Open(configDir string) (*DB, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("설정 디렉토리 생성 실패: %w", err)
	}
	path := filepath.Join(configDir, DBFile)
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("SQLite 열기 실패: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("SQLite 연결 실패: %w", err)
	}

	d := &DB{sql: sqlDB, dir: configDir}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := d.migrateJSON(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

// SQL은 하위 *sql.DB를 반환한다. (camera.SQLCameraStore 등에 전달)
func (d *DB) SQL() *sql.DB { return d.sql }

// Close는 연결을 닫는다.
func (d *DB) Close() error { return d.sql.Close() }

// migrate는 user_version부터 마지막 마이그레이션까지 순차 적용한다.
func (d *DB) migrate() error {
	var ver int
	if err := d.sql.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return fmt.Errorf("스키마 버전 조회 실패: %w", err)
	}
	for i := ver; i < len(migrations); i++ {
		tx, err := d.sql.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("스키마 마이그레이션 %d 실패: %w", i+1, err)
		}
		// PRAGMA는 파라미터 바인딩 불가 — 정적 문자열로 조립
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		slog.Info("SQLite 스키마 적용", "version", i+1)
	}
	return nil
}

// migrateJSON은 DB가 비어 있고 기존 JSON이 있으면 이관한다.
func (d *DB) migrateJSON() error {
	// 카메라/그룹
	var n int
	if err := d.sql.QueryRow("SELECT COUNT(*) FROM cameras").Scan(&n); err != nil {
		return err
	}
	camPath := filepath.Join(d.dir, "cameras.json")
	if n == 0 {
		if b, err := os.ReadFile(camPath); err == nil {
			var f camera.CamerasFile
			if err := json.Unmarshal(b, &f); err != nil {
				return fmt.Errorf("cameras.json 파싱 실패(이관): %w", err)
			}
			if err := camera.NewSQLCameraStore(d.sql).ImportFrom(f); err != nil {
				return fmt.Errorf("cameras.json 이관 실패: %w", err)
			}
			if err := os.Rename(camPath, camPath+".bak"); err != nil {
				slog.Warn("cameras.json.bak 보존 실패", "err", err)
			}
			slog.Info("cameras.json → SQLite 이관 완료", "cameras", len(f.Cameras), "groups", len(f.Groups))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cameras.json 읽기 실패(이관): %w", err)
		}
	}

	// 앱 설정
	if err := d.sql.QueryRow("SELECT COUNT(*) FROM app_config").Scan(&n); err != nil {
		return err
	}
	appPath := filepath.Join(d.dir, "app.json")
	if n == 0 {
		if b, err := os.ReadFile(appPath); err == nil {
			cfg, err := config.Parse(b)
			if err != nil {
				return fmt.Errorf("app.json 이관 실패: %w", err)
			}
			if err := d.SaveConfig(cfg); err != nil {
				return err
			}
			if err := os.Rename(appPath, appPath+".bak"); err != nil {
				slog.Warn("app.json.bak 보존 실패", "err", err)
			}
			slog.Info("app.json → SQLite 이관 완료")
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("app.json 읽기 실패(이관): %w", err)
		}
	}
	return nil
}

// LoadConfig는 app_config 행을 읽는다. 행이 없으면 기본값을 만들어 저장 후 반환한다.
func (d *DB) LoadConfig() (*config.AppConfig, error) {
	var data string
	err := d.sql.QueryRow("SELECT data FROM app_config WHERE id=1").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		cfg := config.Default()
		if err := d.SaveConfig(cfg); err != nil {
			return nil, fmt.Errorf("기본 설정 저장 실패: %w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("설정 조회 실패: %w", err)
	}
	return config.Parse([]byte(data))
}

// SaveConfig는 앱 설정 전체를 단일 행에 저장한다(전체 교체).
func (d *DB) SaveConfig(cfg *config.AppConfig) error {
	b, err := config.Bytes(cfg)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(
		`INSERT INTO app_config(id, data) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET data=excluded.data`, string(b))
	if err != nil {
		return fmt.Errorf("설정 저장 실패: %w", err)
	}
	return nil
}
