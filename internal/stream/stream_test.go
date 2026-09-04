// RTSP 클라이언트와 허브의 단위/통합 테스트
package stream

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

// testCameraSource는 카메라 ID를 고정 URL로 매핑하는 테스트용 소스다.
type testCameraSource struct{ urls map[string]string }

func (s testCameraSource) StreamURL(cameraID string) (string, string, error) {
	u, ok := s.urls[cameraID]
	if !ok {
		return "", "", context.Canceled
	}
	return u, "tcp", nil
}

// fakeDialer는 실제 네트워크 없이 Info와 N개의 패킷을 발생시킨다.
// onNALU 탭이 있으면 패킷마다 액세스 유닛(짝수 i는 IDR)도 발생시킨다.
func fakeDialer(packets int) Dialer {
	return func(ctx context.Context, rawURL, transport string, onInfo func(Info), onPacket func(Packet), onNALU func(codec Codec, nalus [][]byte, pts int64, key bool)) error {
		onInfo(Info{Codec: CodecH264, SPS: []byte{0x67}, PPS: []byte{0x68}, ClockRate: 90000})
		for i := 0; i < packets; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			onPacket(Packet{Codec: CodecH264, Sequence: uint16(i), Timestamp: uint32(i), Payload: []byte{1, 2, 3}})
			if onNALU != nil {
				key := i%30 == 0
				nalu := []byte{0x61} // non-IDR slice
				if key {
					nalu = []byte{0x65} // IDR slice
				}
				onNALU(CodecH264, [][]byte{nalu}, int64(i)*3000, key)
			}
			time.Sleep(1 * time.Millisecond)
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

// collectEvents는 구독 채널에서 이벤트를 수집한다. stop이 true를 반환하면 즉시 중단한다.
func collectEvents(ch <-chan Event, timeout time.Duration, stop func(evts []Event) bool) []Event {
	var out []Event
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
			if stop != nil && stop(out) {
				return out
			}
		case <-deadline:
			return out
		}
	}
}

// TestHubStartSubscribeStop는 시작→구독→패킷→정지 흐름을 확인한다.
func TestHubStartSubscribeStop(t *testing.T) {
	hub := NewHub(testCameraSource{urls: map[string]string{"cam-1": "rtsp://127.0.0.1:1/stream"}}, fakeDialer(5))

	if err := hub.Start("cam-1"); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	if err := hub.Start("cam-1"); err != nil {
		t.Fatalf("재 Start() err = %v", err)
	}

	ch, cancel1, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}
	defer cancel1()

	evts := collectEvents(ch, 3*time.Second, func(evts []Event) bool {
		return len(evts) >= 6 // started + 5 packets
	})
	if len(evts) < 6 {
		t.Fatalf("이벤트 부족: %d", len(evts))
	}
	if _, ok := evts[0].(StartedEvent); !ok {
		t.Errorf("첫 이벤트가 StartedEvent가 아님: %T", evts[0])
	}
	for i := 1; i <= 5; i++ {
		pe, ok := evts[i].(PacketEvent)
		if !ok {
			t.Fatalf("%d번째 이벤트가 PacketEvent가 아님: %T", i, evts[i])
		}
		if pe.Packet.CameraID != "cam-1" {
			t.Errorf("CameraID가 부여되지 않음: %+v", pe.Packet)
		}
	}

	if info, ok := hub.Info("cam-1"); !ok || info.SPS == nil {
		t.Errorf("Info 조회 실패: %+v %v", info, ok)
	}
	if len(hub.Running()) != 1 {
		t.Errorf("Running() = %v", hub.Running())
	}

	// 구독 해제 → 마지막 구독자이므로 세션도 종료되어야 한다 (참조 카운팅)
	cancel1()
	<-time.After(100 * time.Millisecond)
	if _, ok := hub.Info("cam-1"); ok {
		t.Error("구독 해제 후에도 스트림이 실행 중으로 표시됨")
	}
}

// TestHubNotRunning는 실행되지 않은 스트림에 대한 오류를 확인한다.
func TestHubNotRunning(t *testing.T) {
	hub := NewHub(testCameraSource{urls: map[string]string{}}, nil)
	if _, _, err := hub.Subscribe("nope"); err == nil {
		t.Error("없는 스트림 구독이 성공함")
	}
	if err := hub.Start("nope"); err == nil {
		t.Error("URL 없는 카메라 시작이 성공함")
	}
}

// TestHubBackpressure는 느린 구독자의 이벤트가 드롭됨을 확인한다.
func TestHubBackpressure(t *testing.T) {
	hub := NewHub(testCameraSource{urls: map[string]string{"cam-1": "rtsp://127.0.0.1:1/stream"}}, fakeDialer(700))

	if err := hub.Start("cam-1"); err != nil {
		t.Fatal(err)
	}
	ch, cancel, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	// 구독자가 소비하지 않는 상태로 200 패킷 생산을 기다린다.
	time.Sleep(500 * time.Millisecond)

	// 채널 버퍼(30)를 초과한 패킷은 드롭되었어야 한다.
	drained := 0
drain:
	for {
		select {
		case <-ch:
			drained++
		default:
			break drain
		}
	}
	if drained > subscriberBuf+5 {
		t.Errorf("버퍼 초과 이벤트가 드롭되지 않음: drained=%d", drained)
	}
}

// TestHubStopAll는 전체 정지를 확인한다.
func TestHubStopAll(t *testing.T) {
	hub := NewHub(testCameraSource{urls: map[string]string{
		"cam-1": "rtsp://a", "cam-2": "rtsp://b",
	}}, fakeDialer(100))
	_ = hub.Start("cam-1")
	_ = hub.Start("cam-2")
	if err := hub.StopAll(); err != nil {
		t.Fatalf("StopAll() err = %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if len(hub.Running()) != 0 {
		t.Errorf("StopAll 후 실행 중: %v", hub.Running())
	}
}

// sampleSPS/PPS는 유효한 H.264 파라미터 셋 (352x288, mediacommon 테스트 벡터)이다.
var sampleSPS = []byte{
	0x67, 0x64, 0x00, 0x0c, 0xac, 0x3b, 0x50, 0xb0,
	0x4b, 0x42, 0x00, 0x00, 0x03, 0x00, 0x02, 0x00,
	0x00, 0x03, 0x00, 0x3d, 0x08,
}
var samplePPS = []byte{0x68, 0xeb, 0xec, 0xb2, 0x2c}

// TestDimensionsOf는 SPS 해상도 파싱을 확인한다.
func TestDimensionsOf(t *testing.T) {
	w, h := dimensionsOf(CodecH264, sampleSPS)
	if w != 352 || h != 288 {
		t.Errorf("dimensionsOf() = %dx%d, want 352x288", w, h)
	}
	if w, h := dimensionsOf(CodecH264, nil); w != 0 || h != 0 {
		t.Errorf("nil SPS 처리 실패: %dx%d", w, h)
	}
}

// testRTSPServer는 실제 RTSP 서버를 흉내 내는 통합 테스트 서버다.
type testRTSPServer struct {
	srv        *gortsplib.Server
	stream     *gortsplib.ServerStream
	medi       *description.Media
	h264f      *format.H264
	ssrcSwitch bool // true면 스트림 도중 SSRC 변경 (SSRC 관용 검증용)
}

func newTestRTSPServer(t *testing.T) *testRTSPServer {
	return newTestRTSPServerOpt(t, false)
}

func newTestRTSPServerOpt(t *testing.T, ssrcSwitch bool) *testRTSPServer {
	t.Helper()
	ts := &testRTSPServer{ssrcSwitch: ssrcSwitch}

	ts.h264f = &format.H264{
		PayloadTyp:        96,
		SPS:               sampleSPS,
		PPS:               samplePPS,
		PacketizationMode: 1,
	}
	ts.medi = &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{ts.h264f},
	}

	ts.srv = &gortsplib.Server{
		Handler:     ts,
		RTSPAddress: freePort(t),
	}
	if err := ts.srv.Start(); err != nil {
		t.Fatalf("RTSP 서버 시작 실패: %v", err)
	}
	// 서버 초기화 후 스트림을 생성해야 한다.
	ts.stream = gortsplib.NewServerStream(ts.srv, &description.Session{
		BaseURL: nil,
		Medias:  []*description.Media{ts.medi},
	})
	t.Cleanup(func() { ts.srv.Close() })
	return ts
}

func (ts *testRTSPServer) url() string { return "rtsp://" + ts.srv.RTSPAddress + "/stream" }

func (ts *testRTSPServer) OnConnOpen(*gortsplib.ServerHandlerOnConnOpenCtx)         {}
func (ts *testRTSPServer) OnConnClose(*gortsplib.ServerHandlerOnConnCloseCtx)       {}
func (ts *testRTSPServer) OnSessionOpen(*gortsplib.ServerHandlerOnSessionOpenCtx)   {}
func (ts *testRTSPServer) OnSessionClose(*gortsplib.ServerHandlerOnSessionCloseCtx) {}

func (ts *testRTSPServer) OnDescribe(*gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, ts.stream, nil
}

func (ts *testRTSPServer) OnSetup(*gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, ts.stream, nil
}

func (ts *testRTSPServer) OnPlay(*gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	// PLAY 후 IDR/슬라이스 NALU 10개를 전송한다. ssrcSwitch가 true면
	// 중간에 SSRC를 변경해 전송한다 (카메라 서버의 인코더 재시작 재현).
	go func() {
		enc, err := ts.h264f.CreateEncoder()
		if err != nil {
			return
		}
		for i := 0; i < 10; i++ {
			naluType := byte(5) // IDR
			if i%2 == 1 {
				naluType = 1 // non-IDR
			}
			nalu := []byte{naluType, 0x01, 0x02, byte(i)}
			pkts, err := enc.Encode([][]byte{nalu})
			if err != nil || len(pkts) == 0 {
				continue
			}
			if ts.ssrcSwitch && i == 5 {
				for _, p := range pkts {
					p.SSRC = 0xdeadbeef // 중간 SSRC 변경
				}
			}
			for _, p := range pkts {
				_ = ts.stream.WritePacketRTP(ts.medi, p)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// testRTSPServer에 ssrcSwitch 필드 추가를 위한 래퍼 필드 (아래에서 구조체에 추가)

// freePort는 사용 가능한 로컬 포트를 할당한다.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// TestDialRTSPIntegration은 로컬 RTSP 서버와의 실제 연결 흐름을 확인한다.
func TestDialRTSPIntegration(t *testing.T) {
	ts := newTestRTSPServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		mu       sync.Mutex
		gotInfo  bool
		pktCount int
	)
	err := DialRTSP(ctx, ts.url(), "tcp",
		func(info Info) {
			mu.Lock()
			defer mu.Unlock()
			gotInfo = true
			if info.Codec != CodecH264 || info.SPS == nil || info.PPS == nil {
				t.Errorf("Info 불일치: %+v", info)
			}
			if info.Width != 352 || info.Height != 288 {
				t.Errorf("해상도 불일치: %dx%d", info.Width, info.Height)
			}
		},
		func(Packet) {
			mu.Lock()
			defer mu.Unlock()
			pktCount++
		},
		nil,
	)
	mu.Lock()
	defer mu.Unlock()
	if err != nil && ctx.Err() == nil {
		t.Errorf("DialRTSP() err = %v", err)
	}
	if !gotInfo {
		t.Error("Info 콜백이 호출되지 않음")
	}
	if pktCount < 10 {
		t.Errorf("수신 패킷 수 = %d, want >= 10", pktCount)
	}
}

// TestHubLateSubscriber는 늦게 합류한 구독자에게 코덱 정보가 즉시 재전송되는지 확인한다.
// (2번째 클라이언트가 영상을 못 보던 원인 — 2026-09-01)
func TestHubLateSubscriber(t *testing.T) {
	hub := NewHub(testCameraSource{urls: map[string]string{"cam-1": "rtsp://127.0.0.1:1/stream"}}, fakeDialer(50))

	if err := hub.Start("cam-1"); err != nil {
		t.Fatal(err)
	}

	// 첫 구독자가 Started를 받을 때까지 대기 (코덱 정보 확정)
	ch1, cancel1, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel1()
	evts1 := collectEvents(ch1, 2*time.Second, func(evts []Event) bool {
		_, ok := evts[0].(StartedEvent)
		return ok
	})
	if len(evts1) == 0 {
		t.Fatal("첫 구독자 Started 미수신")
	}

	// 늦은 구독자 합류 — 코덱 정보가 이미 확정된 상태
	ch2, cancel2, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel2()

	evts2 := collectEvents(ch2, 2*time.Second, func(evts []Event) bool {
		return len(evts) >= 3 // started + 패킷 2개
	})
	if len(evts2) < 3 {
		t.Fatalf("늦은 구독자 이벤트 부족: %d", len(evts2))
	}
	if _, ok := evts2[0].(StartedEvent); !ok {
		t.Errorf("늦은 구독자의 첫 이벤트가 StartedEvent가 아님: %T", evts2[0])
	}
	info := evts2[0].(StartedEvent).Info
	if info.Codec != CodecH264 || info.SPS == nil {
		t.Errorf("재전송된 Info 불일치: %+v", info)
	}
}

// TestDialRTSPSSRCChange는 스트림 도중 SSRC가 바뀌어도 연결이 유지되고
// 모든 패킷을 수신하는지 확인한다 (AllowSSRCChange 검증 — PythonCam 서버 대응).
func TestDialRTSPSSRCChange(t *testing.T) {
	ts := newTestRTSPServerOpt(t, true) // 중간 SSRC 변경 서버

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		mu       sync.Mutex
		pktCount int
		gotErr   error
	)
	err := DialRTSP(ctx, ts.url(), "tcp",
		func(Info) {},
		func(Packet) {
			mu.Lock()
			defer mu.Unlock()
			pktCount++
		},
		nil,
	)
	mu.Lock()
	defer mu.Unlock()
	if err != nil && ctx.Err() == nil {
		t.Errorf("DialRTSP() err = %v", err)
	}
	if gotErr != nil {
		t.Errorf("수신 중 오류: %v", gotErr)
	}
	if pktCount < 10 {
		t.Errorf("수신 패킷 수 = %d, want >= 10 (SSRC 변경 후에도 수신되어야 함)", pktCount)
	}
}

// TestHubRefCount는 참조 카운팅 의미론을 확인한다:
// 두 구독자 중 하나가 해제되어도 세션은 유지되고, 둘 다 해제되면 종료된다.
// (클라이언트별 독립 재생 — 2026-09-02 요구사항)
func TestHubRefCount(t *testing.T) {
	hub := NewHub(testCameraSource{urls: map[string]string{"cam-1": "rtsp://127.0.0.1:1/stream"}}, fakeDialer(100))

	if err := hub.Start("cam-1"); err != nil {
		t.Fatal(err)
	}
	_, cancel1, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatal(err)
	}
	ch2, cancel2, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatal(err)
	}

	// 첫 구독자 해제 → 세션은 유지되어야 한다
	cancel1()
	time.Sleep(100 * time.Millisecond)
	if _, ok := hub.Info("cam-1"); !ok {
		t.Fatal("한 구독자 해제 후 세션이 종료됨 (유지되어야 함)")
	}

	// 두 번째 구독자는 계속 패킷을 수신한다
	got := collectEvents(ch2, 2*time.Second, func(evts []Event) bool {
		_, ok := evts[0].(StartedEvent)
		return ok && len(evts) >= 2
	})
	if len(got) < 2 {
		t.Fatalf("잔여 구독자 이벤트 부족: %d", len(got))
	}

	// 두 번째 구독자 해제 → 세션 종료
	cancel2()
	time.Sleep(100 * time.Millisecond)
	if _, ok := hub.Info("cam-1"); ok {
		t.Error("모든 구독자 해제 후에도 세션이 유지됨 (종료되어야 함)")
	}
}

// notifyingSource는 testCameraSource + StreamFailureNotifier 구현이다.
type notifyingSource struct {
	testCameraSource
	mu     sync.Mutex
	failed []string
}

func (s *notifyingSource) OnStreamFailed(cameraID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = append(s.failed, cameraID)
}

// TestHubReload는 Reload가 구독자에게 StoppedEvent를 보내고 채널을 닫으며
// 세션을 제거하고 실패 통지(URI 캐시 폐기)를 하는지 확인한다.
func TestHubReload(t *testing.T) {
	src := &notifyingSource{testCameraSource: testCameraSource{urls: map[string]string{"cam-1": "rtsp://127.0.0.1:1/stream"}}}
	hub := NewHub(src, fakeDialer(1000))

	if err := hub.Start("cam-1"); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	chA, cancelA, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatalf("Subscribe(A) err = %v", err)
	}
	defer cancelA()
	chB, cancelB, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatalf("Subscribe(B) err = %v", err)
	}
	defer cancelB()

	hub.Reload("cam-1")

	for name, ch := range map[string]<-chan Event{"A": chA, "B": chB} {
		evts := collectEvents(ch, 2*time.Second, nil) // 채널이 닫힐 때까지 수집
		if len(evts) == 0 {
			t.Fatalf("%s: 이벤트 없음 (StoppedEvent 기대)", name)
		}
		last := evts[len(evts)-1]
		if _, ok := last.(StoppedEvent); !ok {
			t.Errorf("%s: 마지막 이벤트가 StoppedEvent가 아님: %T", name, last)
		}
	}

	if _, ok := hub.Info("cam-1"); ok {
		t.Error("Reload 후에도 세션이 실행 중으로 표시됨")
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.failed) == 0 || src.failed[0] != "cam-1" {
		t.Errorf("OnStreamFailed 미호출: %v", src.failed)
	}
}

// TestHubNALUTap는 SetNALUTap으로 등록한 콜백이 dial에서 액세스 유닛을 받는지 확인한다.
func TestHubNALUTap(t *testing.T) {
	hub := NewHub(testCameraSource{urls: map[string]string{"cam-1": "rtsp://127.0.0.1:1/s"}}, fakeDialer(100))

	var mu sync.Mutex
	var aus, keys int
	hub.SetNALUTap("cam-1", func(codec Codec, nalus [][]byte, pts int64, key bool) {
		mu.Lock()
		aus++
		if key {
			keys++
		}
		mu.Unlock()
	})

	if err := hub.Start("cam-1"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, cancel, err := hub.Subscribe("cam-1")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// 채널을 비워 세션 유지
	go func() {
		for range ch {
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := aus
		mu.Unlock()
		if n >= 30 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if aus < 30 {
		t.Fatalf("탭이 받은 AU = %d, want >= 30", aus)
	}
	if keys < 1 {
		t.Errorf("IDR AU를 하나도 못 받음 (keys=%d)", keys)
	}

	// 탭 해제 후 새 세션에는 전달 안 됨
	hub.SetNALUTap("cam-1", nil)
	if _, ok := hub.taps["cam-1"]; ok {
		t.Error("SetNALUTap(nil) 후에도 탭이 남음")
	}
}
