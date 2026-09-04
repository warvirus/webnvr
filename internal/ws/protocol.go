// WebSocket JSON 프로토콜 메시지(doc §5.3)를 정의한다.
package ws

// ClientMsg는 클라이언트가 서버로 보내는 메시지다.
type ClientMsg struct {
	Type     string      `json:"type"` // start_stream|stop_stream|start_all_streams|stop_all_streams|ptz|request_keyframe|subscribe|unsubscribe|reload_stream|ping
	CameraID string      `json:"cameraId,omitempty"`
	Command  *PTZCommand `json:"command,omitempty"`
}

// PTZCommand는 PTZ 제어 명령이다.
type PTZCommand struct {
	// Action: move(연속 이동)|stop|preset(프리셋 이동)
	Action      string  `json:"action"`
	Pan         float64 `json:"pan,omitempty"`  // -1.0~1.0
	Tilt        float64 `json:"tilt,omitempty"` // -1.0~1.0
	Zoom        float64 `json:"zoom,omitempty"` // 0~1.0
	PresetToken string  `json:"presetToken,omitempty"`
}

// ServerMsg는 서버가 클라이언트로 보내는 메시지다.
// 목적별 필드가 하나의 구조체에 모여 있으며 omitempty로 필요한 필드만 직렬화된다.
type ServerMsg struct {
	Type     string `json:"type"` // stream_started|rtp_packet|stream_stopped|stream_error|cameras_changed|config_changed|stats|pong|client_limit_exceeded
	CameraID string `json:"cameraId,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Error    string `json:"error,omitempty"`

	// stream_started
	Codec       string `json:"codec,omitempty"`
	SSRC        uint32 `json:"ssrc,omitempty"`
	ClockRate   uint32 `json:"clockRate,omitempty"`
	PayloadType uint8  `json:"payloadType,omitempty"`
	SPS         string `json:"sps,omitempty"` // base64
	PPS         string `json:"pps,omitempty"` // base64
	VPS         string `json:"vps,omitempty"` // base64 (H.265)
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`

	// rtp_packet
	Payload   string `json:"payload,omitempty"` // base64
	Timestamp uint32 `json:"timestamp,omitempty"`
	Marker    bool   `json:"marker,omitempty"`
	Sequence  uint16 `json:"sequence,omitempty"`
}

// 클라이언트 메시지 타입 상수
const (
	MsgStartStream     = "start_stream"
	MsgStopStream      = "stop_stream"
	MsgStartAllStreams = "start_all_streams"
	MsgStopAllStreams  = "stop_all_streams"
	MsgPTZ             = "ptz"
	MsgRequestKeyframe = "request_keyframe"
	MsgSubscribe       = "subscribe"
	MsgUnsubscribe     = "unsubscribe"
	MsgReloadStream    = "reload_stream" // 실행 중인 RTSP 세션을 강제 종료 → 새 설정으로 재다이얼
	MsgPing            = "ping"
)

// 서버 메시지 타입 상수
const (
	MsgStreamStarted       = "stream_started"
	MsgRTPPacket           = "rtp_packet"
	MsgRTPBatch            = "rtp_batch" // 여러 RTP 패킷의 배치 전송 (고비트레이트 스트림용 확장)
	MsgStreamStopped       = "stream_stopped"
	MsgStreamError         = "stream_error"
	MsgCamerasChanged      = "cameras_changed" // 카메라 목록/설정 변경 — 클라이언트가 새로고침
	MsgConfigChanged       = "config_changed"  // 앱 설정 변경
	MsgStats               = "stats"
	MsgPong                = "pong"
	MsgClientLimitExceeded = "client_limit_exceeded"
)

// RTPPacketItem은 rtp_batch의 개별 패킷이다. JSON 크기 절감을 위해 축약 키를 사용한다.
type RTPPacketItem struct {
	Payload   string `json:"p"`           // base64 RTP payload
	Timestamp uint32 `json:"ts"`          // RTP 타임스탬프
	Marker    bool   `json:"m,omitempty"` // marker 비트
	Sequence  uint16 `json:"sq"`          // 시퀀스 번호
}

// RTPBatchMsg는 카메라 하나의 RTP 패킷들을 묶어 전송한다.
type RTPBatchMsg struct {
	Type     string          `json:"type"` // "rtp_batch"
	CameraID string          `json:"cameraId"`
	Codec    string          `json:"codec,omitempty"`
	Packets  []RTPPacketItem `json:"packets"`
}
