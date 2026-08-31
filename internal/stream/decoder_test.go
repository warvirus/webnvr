// MockDecoder 동작 검증
package stream

import (
	"errors"
	"testing"
)

func TestMockDecoder(t *testing.T) {
	var d FrameDecoder = &MockDecoder{}

	// 설정 전 디코딩은 오류
	if _, err := d.Decode(Packet{Payload: []byte{1}}); err == nil {
		t.Error("설정 전 디코딩이 성공함")
	}

	// 잘못된 코덱
	if err := d.Configure("weird", nil, nil, nil); err == nil {
		t.Error("잘못된 코덱 설정이 성공함")
	}

	// 정상 설정 후 디코딩
	if err := d.Configure(CodecH264, []byte{0x67}, []byte{0x68}, nil); err != nil {
		t.Fatalf("Configure() err = %v", err)
	}
	payload := []byte{1, 2, 3}
	frames, err := d.Decode(Packet{Payload: payload})
	if err != nil {
		t.Fatalf("Decode() err = %v", err)
	}
	if len(frames) != 1 || string(frames[0]) != string(payload) {
		t.Errorf("프레임 불일치: %v", frames)
	}

	// 주입된 오류 전파
	mock := d.(*MockDecoder)
	mock.DecodeErr = errors.New("강제 오류")
	if _, err := d.Decode(Packet{}); err == nil {
		t.Error("주입된 오류가 반환되지 않음")
	}

	if err := d.Close(); err != nil {
		t.Errorf("Close() err = %v", err)
	}
	if mock.Configured {
		t.Error("Close 후에도 설정 상태가 유지됨")
	}
}
