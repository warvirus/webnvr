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
	Recording RecordingConfig `json:"recording"`
}

// ServerConfig는 HTTP/WS 서버의 바인드 주소와 포트 설정을 나타낸다.
type ServerConfig struct {
	WSPort     int    `json:"ws_port"`
	TLSPort    int    `json:"tls_port"` // HTTPS/WSS 보조 포트 (WEB_CERT/WEB_KEY 설정 시, 0이면 기본 8443)
	HTTPPort   int    `json:"http_port"`
	Bind       string `json:"bind"`        // "127.0.0.1"(기본, 로컬 전용) 또는 "0.0.0.0"(LAN 공개)
	MaxClients int    `json:"max_clients"` // 최대 동시 접속 수 (0 = 무제한)
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

// RecordingConfig는 백엔드 녹화(Phase R) 동작을 나타낸다.
// enabled=false면 녹화기가 생성되지 않고 기존 동작이 완전히 유지된다.
type RecordingConfig struct {
	Enabled        bool            `json:"enabled"`
	MaxUsageGB     float64         `json:"max_usage_gb"`    // 0 = 무제한(디스크 한도까지)
	ReclaimPercent int             `json:"reclaim_percent"` // 한도 초과 시 삭제로 확보할 여유 비율
	RetentionDays  int             `json:"retention_days"`  // 0 = 시간 제한 없음
	KeepMinHours   int             `json:"keep_min_hours"`  // 이보다 최근 녹화는 공간 부족해도 유지
	ReconcileHours int             `json:"reconcile_hours"` // 발자국 재조정(SUM(bytes)+du 대조) 주기
	Storages       []StorageConfig `json:"storages"`        // fill-then-next
	SegmentSeconds int             `json:"segment_seconds"` // 세그먼트 목표 길이
	SegmentMaxMB   int             `json:"segment_max_mb"`  // 키프레임이 안 와도 이 크기에서 강제 컷
	Transcode      TranscodeConfig `json:"transcode"`       // 후속 H — v1은 비활성
}

// StorageConfig는 녹화 저장 대상 하나다.
type StorageConfig struct {
	Path           string `json:"path"`
	MinFreePercent int    `json:"min_free_percent"` // 공유 디스크에서 OS·타 앱 보호
}

// TranscodeConfig는 압축 비효율 코덱의 재인코딩 훅이다(후속 H, v1 미사용).
type TranscodeConfig struct {
	Enabled     bool   `json:"enabled"`
	TargetCodec string `json:"target_codec"`
	FFmpegPath  string `json:"ffmpeg_path"`
}

// CurrentVersion은 현재 설정 스키마 버전이다.
const CurrentVersion = 1

// Default는 문서 §8 스키마에 정의된 기본 설정을 반환한다.
// 이 프로그램은 외부 송출(스트리밍) 목적이므로 기본값은 0.0.0.0(모든 인터페이스)이다.
func Default() *AppConfig {
	return &AppConfig{
		Version: CurrentVersion,
		Server: ServerConfig{
			WSPort:     25480,
			TLSPort:    8443,
			HTTPPort:   8081,
			Bind:       "0.0.0.0",
			MaxClients: 0,
		},
		Stream: StreamConfig{
			DefaultTransport:     "tcp",
			RTPTimeoutMS:         5000,
			JitterBufferMS:       150,
			MaxConcurrentStreams: 20,
		},
		Discovery: DiscoveryConfig{
			ScanTimeoutMS:       3000,
			ScanInterfaces:      []string{}, // 빈 값 = 활성 인터페이스 자동 열거 (플랫폼 무관)
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
		Recording: RecordingConfig{
			Enabled:        false,
			MaxUsageGB:     0,
			ReclaimPercent: 10,
			RetentionDays:  0,
			KeepMinHours:   1,
			ReconcileHours: 6,
			Storages:       []StorageConfig{{Path: "recordings", MinFreePercent: 5}},
			SegmentSeconds: 300,
			SegmentMaxMB:   512,
			Transcode:      TranscodeConfig{Enabled: false, TargetCodec: "h264", FFmpegPath: ""},
		},
	}
}
