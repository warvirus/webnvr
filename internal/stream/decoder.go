// 서버 측 프레임 디코딩(스냅샷/녹화 등)이 필요해질 때 구현할 인터페이스와 Mock이다.
// 현재는 프론트엔드(WebCodecs)가 디코딩을 담당하며, 백엔드 디코딩은 준비 단계다.
package stream

import "fmt"

// FrameDecoder는 RTP 패킷을 받아 디코드된 프레임을 출력하는 디코더 인터페이스다.
// 구현체는 코덱 설정(파라미터 셋)을 먼저 받아야 한다.
type FrameDecoder interface {
	// Configure는 코덱과 파라미터 셋으로 디코더를 설정한다.
	Configure(codec Codec, sps, pps, vps []byte) error
	// Decode는 RTP 페이로드를 디코딩해 프레임 데이터를 반환한다.
	// 반환 값은 구현체별 의미를 가진다(예: YUV 플레인, NALU 목록).
	Decode(pkt Packet) ([][]byte, error)
	// Close는 자원을 해제한다.
	Close() error
}

// MockDecoder는 테스트/개발용 FrameDecoder 구현이다.
// 실제 디코딩 없이 입력 페이로드를 프레임 1개로 되돌려주며, 호출 횟수를 기록한다.
type MockDecoder struct {
	Configured bool
	Codec      Codec
	SPS, PPS   []byte
	Decoded    int
	DecodeErr  error // 설정 시 주입하면 Decode가 반환할 오류
}

// Configure는 코덱 설정을 저장한다.
func (m *MockDecoder) Configure(codec Codec, sps, pps, vps []byte) error {
	if codec != CodecH264 && codec != CodecH265 {
		return fmt.Errorf("지원하지 않는 코덱: %q", codec)
	}
	m.Codec = codec
	m.SPS = append([]byte(nil), sps...)
	m.PPS = append([]byte(nil), pps...)
	m.Configured = true
	return nil
}

// Decode는 설정 전 호출 시 오류를 반환하고, 그 외에는 입력 페이로드를 그대로 반환한다.
func (m *MockDecoder) Decode(pkt Packet) ([][]byte, error) {
	if m.DecodeErr != nil {
		return nil, m.DecodeErr
	}
	if !m.Configured {
		return nil, fmt.Errorf("디코더가 설정되지 않음")
	}
	m.Decoded++
	return [][]byte{pkt.Payload}, nil
}

// Close는 상태를 초기화한다.
func (m *MockDecoder) Close() error {
	m.Configured = false
	return nil
}
