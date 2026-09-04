// 카메라 도메인 타입과 cameras.json 파일 구조를 정의한다.
package camera

import "time"

// CameraType은 카메라 연결 방식을 나타낸다.
type CameraType string

const (
	TypeONVIF CameraType = "onvif"
	TypeRTSP  CameraType = "rtsp"
	TypeRTP   CameraType = "rtp"
	TypeRTMP  CameraType = "rtmp"
)

// RecordMode는 카메라별 녹화 방식이다(Phase R). event/both는 R.4에서 배선된다.
const (
	RecordOff        = "off"
	RecordContinuous = "continuous"
	RecordEvent      = "event"
	RecordBoth       = "both"
)

// StreamConfig는 개별 카메라의 스트림 수신 옵션이다.
type StreamConfig struct {
	Transport  string `json:"transport"` // "tcp" | "udp"
	Protocol   string `json:"protocol"`  // "rtsp" | "rtp" | "rtmp"
	BufferSize int    `json:"buffer_size"`
}

// Camera는 관리 대상 카메라 하나를 나타낸다. 비밀번호는 암호화된 상태로 저장된다.
type Camera struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Type         CameraType   `json:"type"`
	XAddr        string       `json:"xaddr,omitempty"`         // ONVIF 전용: "host:port"
	Username     string       `json:"username,omitempty"`      // ONVIF 전용
	Password     string       `json:"password,omitempty"`      // "encrypted:..." 형식
	ProfileToken string       `json:"profile_token,omitempty"` // ONVIF 전용
	StreamURL    string       `json:"stream_url,omitempty"`    // RTSP/RTP/RTMP 직접 입력
	StreamConfig StreamConfig `json:"stream_config"`
	PTZSupported bool         `json:"ptz_supported"`
	GroupID      string       `json:"group_id,omitempty"`
	LayoutOrder  int          `json:"layout_order"`
	Enabled      bool         `json:"enabled"`
	RecordMode   string       `json:"record_mode,omitempty"`   // off(기본) | continuous | event | both
	PreRoll      int          `json:"pre_roll_seconds,omitempty"`
	PostRoll     int          `json:"post_roll_seconds,omitempty"`
	AddedAt      time.Time    `json:"added_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// Group은 카메라 묶음과 레이아웃 정보를 나타낸다.
type Group struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	LayoutType string `json:"layout_type"` // "grid"
	LayoutCols int    `json:"cols"`
	LayoutRows int    `json:"rows"`
}

// CamerasFile은 config/cameras.json의 전체 구조다.
type CamerasFile struct {
	Version int      `json:"version"`
	Cameras []Camera `json:"cameras"`
	Groups  []Group  `json:"groups"`
}
