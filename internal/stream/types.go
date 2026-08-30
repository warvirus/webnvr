// 스트림 코어의 공용 타입(코덱, 패킷, 메타데이터, 이벤트)을 정의한다.
package stream

// Codec은 비디오 코덱 종류이다.
type Codec string

const (
	CodecH264 Codec = "h264"
	CodecH265 Codec = "h265"
)

// Packet은 허브를 통해 구독자에게 전달되는 RTP 패킷이다.
// Payload는 RTP 페이로드이며 헤더 정보는 개별 필드로 전달된다.
type Packet struct {
	CameraID    string
	Codec       Codec
	SSRC        uint32
	ClockRate   uint32
	PayloadType uint8
	Sequence    uint16
	Timestamp   uint32
	Marker      bool
	Payload     []byte
}

// Info는 스트림 시작 시 전달되는 코덱 메타데이터다.
// H.265인 경우 VPS가 의미를 가지며, H.264에서는 nil이다.
type Info struct {
	CameraID    string
	Codec       Codec
	VPS         []byte
	SPS         []byte
	PPS         []byte
	Width       int
	Height      int
	SSRC        uint32
	ClockRate   uint32
	PayloadType uint8
}

// Event는 구독자 채널로 전달되는 이벤트다.
type Event interface{ isEvent() }

// StartedEvent는 스트림이 시작되어 코덱 정보가 확정되었음을 나타낸다.
type StartedEvent struct{ Info Info }

// PacketEvent는 RTP 패킷 도착을 나타낸다.
type PacketEvent struct{ Packet Packet }

// StoppedEvent는 스트림이 종료되었음을 나타낸다.
type StoppedEvent struct{ Reason string }

func (StartedEvent) isEvent() {}
func (PacketEvent) isEvent()  {}
func (StoppedEvent) isEvent() {}
