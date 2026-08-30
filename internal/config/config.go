// 앱 설정(app.json)의 타입 정의와 기본값을 관리하는 패키지
package config

// AppConfig는 config/app.json의 전체 구조를 나타낸다.
type AppConfig struct {
	Version   int             `json:"version"`
	Server    ServerConfig    `json:"server"`
	Stream    StreamConfig    `json:"stream"`
	Discovery DiscoveryConfig `json:"discovery"`
	Decoder   DecoderConfig   `json:"decoder"`
	Logging   LoggingConfig   `json:"logging"`
}

// ServerConfig는 WebSocket/HTTP 서버 포트 설정을 나타낸다.
type ServerConfig struct {
	WSPort   int `json:"ws_port"`
	HTTPPort int `json:"http_port"`
}

// StreamConfig는 스트림 처리 기본 동작을 나타낸다.
type StreamConfig struct {
	DefaultTransport     string `json:"default_transport"` // "tcp" | "udp"
	RTPTimeoutMS         int    `json:"rtp_timeout_ms"`
	JitterBufferMS       int    `json:"jitter_buffer_ms"`
	MaxConcurrentStreams int    `json:"max_concurrent_streams"`
}

// DiscoveryConfig는 ONVIF WS-Discovery 동작을 나타낸다.
type DiscoveryConfig struct {
	ScanTimeoutMS       int      `json:"scan_timeout_ms"`
	ScanInterfaces      []string `json:"scan_interfaces"`
	AutoScanIntervalMin int      `json:"auto_scan_interval_min"`
}

// DecoderConfig는 디코더(프론트엔드/백엔드 공통) 기본 설정을 나타낸다.
type DecoderConfig struct {
	PreferHardware bool `json:"prefer_hardware"`
	MaxThreads     int  `json:"max_threads"`
}

// LoggingConfig는 로깅 동작을 나타낸다.
type LoggingConfig struct {
	Level      string `json:"level"`
	File       string `json:"file"`
	MaxSizeMB  int    `json:"max_size_mb"`
	MaxBackups int    `json:"max_backups"`
}

// CurrentVersion은 현재 설정 스키마 버전이다.
const CurrentVersion = 1

// Default는 문서 §8 스키마에 정의된 기본 설정을 반환한다.
func Default() *AppConfig {
	return &AppConfig{
		Version: CurrentVersion,
		Server: ServerConfig{
			WSPort:   8080,
			HTTPPort: 8081,
		},
		Stream: StreamConfig{
			DefaultTransport:     "tcp",
			RTPTimeoutMS:         5000,
			JitterBufferMS:       150,
			MaxConcurrentStreams: 20,
		},
		Discovery: DiscoveryConfig{
			ScanTimeoutMS:       3000,
			ScanInterfaces:      []string{"en0", "eth0"},
			AutoScanIntervalMin: 30,
		},
		Decoder: DecoderConfig{
			PreferHardware: true,
			MaxThreads:     4,
		},
		Logging: LoggingConfig{
			Level:      "info",
			File:       "logs/app.log",
			MaxSizeMB:  100,
			MaxBackups: 5,
		},
	}
}
