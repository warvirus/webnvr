// AppConfig의 필드 제약 조건을 검증한다.
package config

import (
	"fmt"
	"net"
	"strings"
)

// Validate는 설정 값의 유효성을 검사하고 위반 시 첫 번째 오류를 반환한다.
func Validate(cfg *AppConfig) error {
	if cfg.Version <= 0 {
		return fmt.Errorf("버전은 양수여야 함: %d", cfg.Version)
	}
	if err := validatePort("ws_port", cfg.Server.WSPort); err != nil {
		return err
	}
	if err := validatePort("http_port", cfg.Server.HTTPPort); err != nil {
		return err
	}
	if cfg.Server.WSPort == cfg.Server.HTTPPort {
		return fmt.Errorf("ws_port와 http_port가 동일함: %d", cfg.Server.WSPort)
	}
	if strings.TrimSpace(cfg.Server.Bind) == "" {
		return fmt.Errorf("server.bind는 비어 있을 수 없음 (127.0.0.1 또는 0.0.0.0)")
	}
	if cfg.Server.MaxClients < 0 {
		return fmt.Errorf("server.max_clients는 0 이상이어야 함: %d", cfg.Server.MaxClients)
	}

	switch strings.ToLower(cfg.Stream.DefaultTransport) {
	case "tcp", "udp":
	default:
		return fmt.Errorf("default_transport는 tcp 또는 udp여야 함: %q", cfg.Stream.DefaultTransport)
	}
	if cfg.Stream.RTPTimeoutMS <= 0 {
		return fmt.Errorf("rtp_timeout_ms는 양수여야 함: %d", cfg.Stream.RTPTimeoutMS)
	}
	if cfg.Stream.JitterBufferMS <= 0 {
		return fmt.Errorf("jitter_buffer_ms는 양수여야 함: %d", cfg.Stream.JitterBufferMS)
	}
	if cfg.Stream.MaxConcurrentStreams <= 0 || cfg.Stream.MaxConcurrentStreams > 100 {
		return fmt.Errorf("max_concurrent_streams는 1~100이어야 함: %d", cfg.Stream.MaxConcurrentStreams)
	}

	if cfg.Discovery.ScanTimeoutMS <= 0 {
		return fmt.Errorf("scan_timeout_ms는 양수여야 함: %d", cfg.Discovery.ScanTimeoutMS)
	}
	for _, ifc := range cfg.Discovery.ScanInterfaces {
		if strings.TrimSpace(ifc) == "" {
			return fmt.Errorf("scan_interfaces에 빈 값 포함")
		}
	}
	if cfg.Discovery.AutoScanIntervalMin < 0 {
		return fmt.Errorf("auto_scan_interval_min은 0 이상이어야 함: %d", cfg.Discovery.AutoScanIntervalMin)
	}

	if cfg.Decoder.MaxThreads <= 0 || cfg.Decoder.MaxThreads > 64 {
		return fmt.Errorf("max_threads는 1~64이어야 함: %d", cfg.Decoder.MaxThreads)
	}

	switch strings.ToLower(cfg.Logging.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level은 debug|info|warn|error 중 하나여야 함: %q", cfg.Logging.Level)
	}
	if strings.TrimSpace(cfg.Logging.File) == "" {
		return fmt.Errorf("logging.file이 비어 있음")
	}
	if cfg.Logging.MaxSizeMB <= 0 {
		return fmt.Errorf("logging.max_size_mb는 양수여야 함: %d", cfg.Logging.MaxSizeMB)
	}
	if cfg.Logging.MaxBackups < 0 {
		return fmt.Errorf("logging.max_backups는 0 이상이어야 함: %d", cfg.Logging.MaxBackups)
	}
	return nil
}

// validatePort는 포트 범위(1~65535)를 검사한다.
func validatePort(name string, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("%s는 1~65535 범위여야 함: %d", name, port)
	}
	// 포트가 실제로 열려 있는지는 검사하지 않는다 (실행 시점 문제).
	_ = net.JoinHostPort("localhost", fmt.Sprint(port))
	return nil
}
