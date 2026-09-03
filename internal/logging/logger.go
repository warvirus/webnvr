// 파일 로테이션을 지원하는 구조화된 로깅을 설정한다.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"

	"webnvr/internal/config"
)

// Setup은 LoggingConfig에 따라 slog의 기본 로거를 구성한다.
// - 로그 레벨을 설정하고 로그 파일 경로(logs/app.log)를 생성한다.
// - lumberjack으로 파일 로테이션을 설정한다.
// - 파일과 stderr에 동시 출력한다.
// - io.Closer를 반환해 종료 시 파일 핸들을 정리할 수 있다.
func Setup(cfg config.LoggingConfig) (io.Closer, error) {
	// 로그 파일 디렉토리 생성
	logDir := filepath.Dir(cfg.File)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("로그 디렉토리 생성 실패 %q: %w", logDir, err)
	}

	// lumberjack 로거 설정 (파일 로테이션)
	ljLogger := &lumberjack.Logger{
		Filename:   cfg.File,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		LocalTime:  true,
	}

	// 파일과 stderr에 동시 출력
	multiWriter := io.MultiWriter(ljLogger, os.Stderr)

	// slog 레벨 문자열 → slog.Level 변환
	level := parseLevel(cfg.Level)

	// slog 핸들러 생성 및 기본 로거로 설정
	handler := slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))

	return ljLogger, nil
}

// parseLevel은 문자열 로그 레벨을 slog.Level으로 변환한다.
func parseLevel(levelStr string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
