// 스트림 라이프사이클과 구독자 관리, 백프레셔(느린 구독자 프레임 드롭)를 담당한다.
package stream

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"
)

// subscriberBuf는 구독자 채널 버퍼 크기다.
// 문서 의도는 "30프레임" 버퍼이며 RTP 패킷 단위로는 프레임당 30~40패킷이므로
// 30패킷 버퍼는 1프레임 버스트에도 넘친다 → 프레임 기준 30프레임에 해당하는 크기 사용.
const subscriberBuf = 512

// CameraSource는 카메라 ID로 스트림 URL과 전송 방식을 제공한다.
type CameraSource interface {
	// StreamURL은 카메라의 RTSP URL을 반환한다. (예: rtsp://user:pass@host:554/stream)
	StreamURL(cameraID string) (rawURL string, transport string, err error)
}

// StreamFailureNotifier는 스트림 실패를 소스 계층에 통지하는 옵셔널 인터페이스다.
// (StreamService가 구현 — 실패 시 캐시된 URI를 폐기해 다음 시도가 재조회하게 한다)
type StreamFailureNotifier interface {
	// OnStreamFailed는 스트림이 비정상 종료되었음을 통지한다.
	OnStreamFailed(cameraID string)
}

// notifyFailed는 소스가 StreamFailureNotifier를 구현하면 통지한다.
func notifyFailed(src CameraSource, cameraID string) {
	if n, ok := src.(StreamFailureNotifier); ok {
		n.OnStreamFailed(cameraID)
	}
}

// subscriber는 스트림 구독자 한 명이다.
type subscriber struct {
	ch    chan Event
	drops uint64
}

// activeStream은 실행 중인 스트림 하나다.
// refs는 활성 구독자 수이며 0이 되면 세션이 자동 종료된다 (클라이언트별 재생 의미론).
type activeStream struct {
	cancel context.CancelFunc
	subs   map[*subscriber]struct{}
	info   Info
	refs   int
	closed bool // 구독자 정리(채널 close) 완료 — h.mu로 보호, 이중 close 방지
	// 진단 통계
	packetCount uint64
	dropCount   uint64
	lastLogTime time.Time
}

// NALUTap은 완성된 액세스 유닛 하나를 받는 녹화용 콜백이다.
type NALUTap func(codec Codec, nalus [][]byte, pts int64, key bool)

// Hub는 카메라별 스트림을 관리하고 구독자에게 이벤트를 배포한다.
type Hub struct {
	src  CameraSource
	dial Dialer

	mu      sync.Mutex
	streams map[string]*activeStream
	taps    map[string]NALUTap // cameraID → 녹화 탭 (재연결 사이에도 유지)
}

// NewHub는 카메라 소스와 연결 함수로 허브를 생성한다.
func NewHub(src CameraSource, dial Dialer) *Hub {
	if dial == nil {
		dial = DialRTSP
	}
	return &Hub{src: src, dial: dial, streams: map[string]*activeStream{}, taps: map[string]NALUTap{}}
}

// SetNALUTap은 카메라의 녹화 탭을 등록/해제한다(nil이면 해제). 재연결에도 유지된다.
func (h *Hub) SetNALUTap(cameraID string, tap NALUTap) {
	h.mu.Lock()
	if tap == nil {
		delete(h.taps, cameraID)
	} else {
		h.taps[cameraID] = tap
	}
	h.mu.Unlock()
}

// Start는 카메라 스트림을 시작한다. 이미 실행 중이면 아무 작업도 하지 않는다.
func (h *Hub) Start(cameraID string) error {
	h.mu.Lock()
	if _, ok := h.streams[cameraID]; ok {
		h.mu.Unlock()
		slog.Info("🔄 스트림 이미 실행 중", "camera", cameraID)
		return nil
	}
	// 다른 Start가 끼어들지 않도록 자리를 먼저 확보한다.
	e := &activeStream{subs: map[*subscriber]struct{}{}}
	h.streams[cameraID] = e
	h.mu.Unlock()

	slog.Info("▶️ 스트림 시작 시도", "camera", cameraID)
	rawURL, transport, err := h.src.StreamURL(cameraID)
	if err != nil {
		h.mu.Lock()
		if cur, ok := h.streams[cameraID]; ok && cur == e {
			delete(h.streams, cameraID)
		}
		h.mu.Unlock()
		notifyFailed(h.src, cameraID)
		slog.Error("❌ 스트림 URL 조회 실패", "camera", cameraID, "err", err)
		h.closeGen(e, cameraID, false) // 조기 구독자 채널 정리
		return fmt.Errorf("스트림 URL 조회 실패: %w", err)
	}
	if u, err := url.Parse(rawURL); err == nil {
		rawURL = u.Redacted() // 자격증명 마스킹 후 로그 기록
	}
	slog.Info("✅ 스트림 URL 조회 성공", "camera", cameraID, "url", rawURL, "transport", transport)

	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel

	go func() {
		var naluTap func(Codec, [][]byte, int64, bool)
		h.mu.Lock()
		if tap := h.taps[cameraID]; tap != nil {
			naluTap = func(c Codec, nalus [][]byte, pts int64, key bool) {
				h.mu.Lock()
				t := h.taps[cameraID]
				h.mu.Unlock()
				if t != nil {
					t(c, nalus, pts, key)
				}
			}
		}
		h.mu.Unlock()
		dialErr := h.dial(ctx, rawURL, transport,
			func(info Info) {
				info.CameraID = cameraID
				slog.Info("📹 스트림 시작", "camera", cameraID, "codec", info.Codec, "resolution", fmt.Sprintf("%dx%d", info.Width, info.Height))
				h.setInfo(cameraID, info)
				h.publish(cameraID, StartedEvent{Info: info})
			},
			func(pkt Packet) {
				pkt.CameraID = cameraID
				h.publish(cameraID, PacketEvent{Packet: pkt})
			},
			naluTap,
		)
		if dialErr != nil && ctx.Err() == nil {
			slog.Error("❌ RTSP 연결 실패", "camera", cameraID, "err", dialErr)
		}
		// 자신이 시작한 세대만 정리한다 — 재시작이 겹쳐도 새 세션을 파괴하지 않는다.
		h.closeGen(e, cameraID, ctx.Err() == nil)
	}()

	return nil
}

// StopAll은 모든 스트림을 강제 종료한다. (앱 종료 시 정리용)
// 일반 클라이언트 요청으로는 사용하지 않는다 — 구독 해제가 세션 생애를 관리한다.
func (h *Hub) StopAll() error {
	h.mu.Lock()
	ids := make([]string, 0, len(h.streams))
	for id := range h.streams {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.closeSession(id)
	}
	return nil
}

// Reload는 실행 중인 세션을 강제 종료한다. 구독자에게 StoppedEvent를 보내고 채널을 닫으며
// URI 캐시도 폐기한다(다음 재다이얼이 ONVIF를 다시 조회하도록). 구독자(WS 클라이언트)는
// desired 상태에 따라 리컨실리어가 재시작하며, 이때 변경된 카메라 설정이 반영된다.
// 비공개 closeSession이 아니라 close(_, true)를 호출한다 — closeSession은 맵에서 먼저
// 세션을 지워 dial 고루틴의 deferred close가 조기 반환하고 구독자 채널이 닫히지 않는다.
func (h *Hub) Reload(cameraID string) {
	h.close(cameraID, true)
}

// Subscribe는 구독자 채널과 구독 해제 함수를 반환한다.
// 스트림이 실행 중이어야 하며, 채널로 Started/Packet/Stopped 이벤트가 순서대로 전달된다.
// 늦게 합류한 구독자(2번째 클라이언트 등)에게는 코덱 메타데이터(StartedEvent)를
// 즉시 재전송한다 — 그렇지 않으면 디코더가 설정되지 않아 영상이 나오지 않는다.
// 구독 해제 시 활성 구독자가 0이 되면 세션도 자동 종료된다(클라이언트별 재생 의미론:
// 마지막 클라이언트가 모니터링을 떠나면 RTSP 연결도 해제한다).
func (h *Hub) Subscribe(cameraID string) (<-chan Event, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.streams[cameraID]
	if !ok {
		return nil, nil, fmt.Errorf("실행 중인 스트림이 없음: %s", cameraID)
	}
	s := &subscriber{ch: make(chan Event, subscriberBuf)}
	e.subs[s] = struct{}{}
	e.refs++
	// 늦은 구독자 재전송: 정보가 완전해진 후에만 전송한다.
	// 빈 Info는 디코더 설정 실패를 초래하므로 보내지 않는다.
	if e.info.Codec != "" {
		select {
		case s.ch <- StartedEvent{Info: e.info}:
		default:
			// 채널 포화는 불가능(방금 생성) — 방어용
		}
	}
	// cancel은 자신이 합류한 세대(e)에만 작동한다 — ID로 현재 세션을 찾으면
	// 세대 교체 후 stale cancel이 새 세션(refs=0인 Start~Subscribe 창)을 죽인다.
	cancel := func() {
		h.mu.Lock()
		if e.closed { // closeGen이 이미 채널을 닫음 — 해제할 것 없음
			h.mu.Unlock()
			return
		}
		if _, present := e.subs[s]; present {
			delete(e.subs, s)
			e.refs--
		}
		lastRef := e.refs <= 0
		isCurrent := h.streams[cameraID] == e
		h.mu.Unlock()

		// 마지막 구독자가 떠났고 이 세대가 아직 현재 세션이면 RTSP도 해제한다.
		// 교체된 세대는 이미 정리 대상 — 새 세션을 건드리지 않는다.
		if lastRef && isCurrent {
			h.closeSession(cameraID)
		}
	}
	return s.ch, cancel, nil
}

// closeSession은 카메라의 RTSP 세션을 종료하고 스트림 목록에서 제거한다.
func (h *Hub) closeSession(cameraID string) {
	h.mu.Lock()
	e, ok := h.streams[cameraID]
	if ok {
		delete(h.streams, cameraID)
	}
	h.mu.Unlock()
	if ok && e.cancel != nil {
		e.cancel()
	}
}

// Info는 실행 중인 스트림의 코덱 메타데이터를 반환한다.
func (h *Hub) Info(cameraID string) (Info, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.streams[cameraID]
	if !ok {
		return Info{}, false
	}
	return e.info, true
}

// Running은 실행 중인 스트림 ID 목록을 반환한다.
func (h *Hub) Running() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.streams))
	for id := range h.streams {
		out = append(out, id)
	}
	return out
}

// setInfo는 스트림 메타데이터를 저장한다.
func (h *Hub) setInfo(cameraID string, info Info) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e, ok := h.streams[cameraID]; ok {
		e.info = info
	}
}

// publish는 모든 구독자에게 이벤트를 비동기로 전달한다.
// 채널이 가득 찬 느린 구독자의 이벤트는 드롭한다(백프레셔 정책).
func (h *Hub) publish(cameraID string, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.streams[cameraID]
	if !ok {
		return
	}

	// RTP 패킷인 경우만 통계 수집
	if _, isPacket := ev.(PacketEvent); isPacket {
		e.packetCount++

		// 10초마다 통계 출력
		now := time.Now()
		if now.Sub(e.lastLogTime) > 10*time.Second {
			if e.lastLogTime.IsZero() {
				e.lastLogTime = now
			} else {
				elapsed := now.Sub(e.lastLogTime).Seconds()
				pps := float64(e.packetCount) / elapsed
				dps := float64(e.dropCount) / elapsed
				dropRate := 0.0
				if e.packetCount > 0 {
					dropRate = float64(e.dropCount) / float64(e.packetCount) * 100
				}
				slog.Debug("📊 RTP 패킷 통계",
					"camera", cameraID,
					"pps", fmt.Sprintf("%.0f/sec", pps),
					"dps", fmt.Sprintf("%.1f/sec", dps),
					"total_packets", e.packetCount,
					"total_drops", e.dropCount,
					"drop_rate", fmt.Sprintf("%.2f%%", dropRate),
					"subs", len(e.subs))
				e.packetCount = 0
				e.dropCount = 0
				e.lastLogTime = now
			}
		}
	}

	for s := range e.subs {
		select {
		case s.ch <- ev:
		default:
			s.drops++
			e.dropCount++
		}
	}
}

// close는 카메라의 현재 세션을 정리한다. (Reload 등 명시적 종료 경로)
func (h *Hub) close(cameraID string, abnormal bool) {
	h.mu.Lock()
	e, ok := h.streams[cameraID]
	h.mu.Unlock()
	if !ok {
		return
	}
	h.closeGen(e, cameraID, abnormal)
}

// closeGen은 자신이 시작한 세대의 세션만 정리한다 (dial 고루틴 종료 경로).
// 자신의 세대가 이미 맵에서 교체됐으면(closeSession에 의한 재시작) 새 세션을 건드리지
// 않고 자기 세대의 구독자 채널만 닫는다 — ID 기반 정리가 새 세션을 파괴하는
// 종료/재시작 경합을 막는다. 구독자 채널 close는 세션당 정확히 1회.
func (h *Hub) closeGen(e *activeStream, cameraID string, abnormal bool) {
	h.mu.Lock()
	current := false
	if cur, ok := h.streams[cameraID]; ok && cur == e {
		delete(h.streams, cameraID)
		current = true
	}
	if e.closed {
		h.mu.Unlock()
		return
	}
	e.closed = true
	// 구독자를 스냅샷으로 떼어낸다 — 이후 e.subs 변형은 없다(cancel이 closed 보고 조기 반환,
	// Subscribe는 맵에서 제거된 세대에 도달 불가).
	detach := make([]*subscriber, 0, len(e.subs))
	for s := range e.subs {
		detach = append(detach, s)
	}
	e.subs = map[*subscriber]struct{}{}
	h.mu.Unlock()

	if e.cancel != nil {
		e.cancel() // 멱등 — 교체 경로에서 이미 취소됐을 수 있다
	}
	if current && abnormal {
		notifyFailed(h.src, cameraID)
	}
	reason := "정상 종료"
	if abnormal {
		reason = "연결 끊김"
	}
	for _, s := range detach {
		select {
		case s.ch <- StoppedEvent{Reason: reason}:
		default:
		}
		close(s.ch)
	}
}
