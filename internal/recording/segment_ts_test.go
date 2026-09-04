package recording

import (
	"os"
	"path/filepath"
	"testing"

	"webnvr/internal/stream"
)

// 실제 SPS/PPS(rtspmock와 동일) + 합성 IDR/P 슬라이스로 tsSink가 유효한 .ts를 쓰는지 확인한다.
var testSPS = []byte{
	0x67, 0x64, 0x00, 0x0c, 0xac, 0x3b, 0x50, 0xb0,
	0x4b, 0x42, 0x00, 0x00, 0x03, 0x00, 0x02, 0x00,
	0x00, 0x03, 0x00, 0x3d, 0x08,
}
var testPPS = []byte{0x68, 0xeb, 0xec, 0xb2, 0x2c}

func TestTSSinkWritesValidContainer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seg.ts")
	sink, err := newTSSink(path, stream.CodecH264)
	if err != nil {
		t.Fatalf("newTSSink: %v", err)
	}

	idr := append([]byte{0x65}, make([]byte, 2000)...) // NALU 타입 5 (IDR slice)
	p := append([]byte{0x41}, make([]byte, 500)...)    // NALU 타입 1 (non-IDR slice)

	// IDR 액세스 유닛 (SPS/PPS 인밴드 포함)
	if err := sink.write([][]byte{testSPS, testPPS, idr}, 90000, true); err != nil {
		t.Fatalf("IDR write: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if err := sink.write([][]byte{p}, int64(90000+i*3000), false); err != nil {
			t.Fatalf("P write %d: %v", i, err)
		}
	}
	n, err := sink.close()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if n <= 0 {
		t.Fatalf("bytes = %d", n)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[0] != 0x47 {
		t.Fatalf("MPEG-TS 동기 바이트 아님: len=%d first=0x%02x", len(b), b[0])
	}
	if len(b)%188 != 0 {
		t.Errorf("TS 패킷 크기(188) 배수가 아님: %d", len(b))
	}
}
