// MPEG-TS 세그먼트 파일 하나에 액세스 유닛을 기록한다. 세그먼트는 IDR + 파라미터 셋으로 시작한다.
package recording

import (
	"bufio"
	"fmt"
	"os"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts"

	"webnvr/internal/stream"
)

// segmentSink는 세그먼트 파일 하나의 기록기다. Recorder가 회전 시점에 새로 만든다.
// tsSink가 실제 구현이며, 테스트는 가짜 구현을 주입한다.
type segmentSink interface {
	// write는 액세스 유닛 하나(원시 NALU 목록, 시작코드 없음)를 기록한다.
	// pts는 90kHz, key는 IDR 포함 여부. 첫 write는 반드시 key여야 한다(아니면 무시).
	write(nalus [][]byte, pts int64, key bool) error
	// bytesWritten은 지금까지 파일에 flush된 바이트 수다(버퍼 대기분은 제외 — 회전 판정용 근사).
	bytesWritten() int64
	// close는 flush 후 파일을 닫고 최종 기록 바이트 수를 반환한다.
	close() (bytes int64, err error)
}

// newSink는 segmentSink 팩토리다. 테스트에서 교체한다.
var newSink = func(absPath string, codec stream.Codec) (segmentSink, error) {
	return newTSSink(absPath, codec)
}

// tsSink는 mediacommon mpegts.Writer로 .ts 파일을 쓴다.
type tsSink struct {
	f     *os.File
	cw    *countWriter
	b     *bufio.Writer
	w     *mpegts.Writer
	track *mpegts.Track
	codec stream.Codec

	dts264 *h264.DTSExtractor
	dts265 *h265.DTSExtractor
	sps    []byte
	pps    []byte
	vps    []byte
	begun  bool
}

func newTSSink(absPath string, codec stream.Codec) (*tsSink, error) {
	f, err := os.Create(absPath)
	if err != nil {
		return nil, fmt.Errorf("세그먼트 파일 생성: %w", err)
	}
	s := &tsSink{f: f, codec: codec}
	s.cw = &countWriter{w: f}
	s.b = bufio.NewWriterSize(s.cw, 64*1024)

	if codec == stream.CodecH265 {
		s.track = &mpegts.Track{Codec: &mpegts.CodecH265{}}
	} else {
		s.track = &mpegts.Track{Codec: &mpegts.CodecH264{}}
	}
	s.w = &mpegts.Writer{W: s.b, Tracks: []*mpegts.Track{s.track}}
	if err := s.w.Initialize(); err != nil {
		f.Close()
		os.Remove(absPath)
		return nil, fmt.Errorf("mpegts writer 초기화: %w", err)
	}
	return s, nil
}

func (s *tsSink) write(nalus [][]byte, pts int64, key bool) error {
	if s.codec == stream.CodecH265 {
		return s.writeH265(nalus, pts, key)
	}
	return s.writeH264(nalus, pts, key)
}

func (s *tsSink) writeH264(au [][]byte, pts int64, key bool) error {
	var filtered [][]byte
	randomAccess := false
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch h264.NALUType(nalu[0] & 0x1f) {
		case h264.NALUTypeSPS:
			s.sps = append([]byte(nil), nalu...)
			continue
		case h264.NALUTypePPS:
			s.pps = append([]byte(nil), nalu...)
			continue
		case h264.NALUTypeAccessUnitDelimiter:
			continue
		case h264.NALUTypeIDR:
			randomAccess = true
		}
		filtered = append(filtered, nalu)
	}
	_ = key
	if len(filtered) == 0 {
		return nil
	}
	if randomAccess && s.sps != nil && s.pps != nil {
		filtered = append([][]byte{s.sps, s.pps}, filtered...)
	}
	if s.dts264 == nil {
		if !randomAccess {
			return nil // 첫 IDR 전까지 조용히 버림
		}
		s.dts264 = &h264.DTSExtractor{}
		s.dts264.Initialize()
	}
	dts, err := s.dts264.Extract(filtered, pts)
	if err != nil {
		return fmt.Errorf("h264 DTS 추출: %w", err)
	}
	s.begun = true
	return s.w.WriteH264(s.track, pts, dts, filtered)
}

func (s *tsSink) writeH265(au [][]byte, pts int64, key bool) error {
	var filtered [][]byte
	randomAccess := false
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch h265.NALUType((nalu[0] >> 1) & 0x3f) {
		case h265.NALUType_VPS_NUT:
			s.vps = append([]byte(nil), nalu...)
			continue
		case h265.NALUType_SPS_NUT:
			s.sps = append([]byte(nil), nalu...)
			continue
		case h265.NALUType_PPS_NUT:
			s.pps = append([]byte(nil), nalu...)
			continue
		case h265.NALUType_AUD_NUT:
			continue
		case h265.NALUType_IDR_W_RADL, h265.NALUType_IDR_N_LP, h265.NALUType_CRA_NUT:
			randomAccess = true
		}
		filtered = append(filtered, nalu)
	}
	_ = key
	if len(filtered) == 0 {
		return nil
	}
	if randomAccess && s.vps != nil && s.sps != nil && s.pps != nil {
		filtered = append([][]byte{s.vps, s.sps, s.pps}, filtered...)
	}
	if s.dts265 == nil {
		if !randomAccess {
			return nil
		}
		s.dts265 = &h265.DTSExtractor{}
		s.dts265.Initialize()
	}
	dts, err := s.dts265.Extract(filtered, pts)
	if err != nil {
		return fmt.Errorf("h265 DTS 추출: %w", err)
	}
	s.begun = true
	return s.w.WriteH265(s.track, pts, dts, filtered)
}

func (s *tsSink) bytesWritten() int64 { return s.cw.n }

func (s *tsSink) close() (int64, error) {
	ferr := s.b.Flush()
	cerr := s.f.Close()
	if ferr != nil {
		return s.cw.n, ferr
	}
	if cerr != nil {
		return s.cw.n, cerr
	}
	return s.cw.n, nil
}

// countWriter는 통과 바이트를 센다.
type countWriter struct {
	w interface{ Write([]byte) (int, error) }
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
