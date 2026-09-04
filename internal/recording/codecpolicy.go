// 코덱별 저장 방식을 정한다 — v1은 H.264/H.265 리먹스만 지원한다.
package recording

import "webnvr/internal/stream"

// Action은 녹화기가 코덱에 대해 취할 저장 방식이다.
type Action int

const (
	// ActionRemux — 재인코딩 없이 그대로 MPEG-TS 세그먼트로 저장.
	ActionRemux Action = iota
	// ActionTranscode — 재인코딩 필요(후속 H, v1 미구현).
	ActionTranscode
	// ActionReject — 지원하지 않는 코덱.
	ActionReject
)

// Decide는 코덱에 대한 저장 방식을 반환한다.
// v1: H.264/H.265 → remux, 그 외 → reject (트랜스코드는 후속 H).
func Decide(codec stream.Codec) Action {
	switch codec {
	case stream.CodecH264, stream.CodecH265:
		return ActionRemux
	default:
		return ActionReject
	}
}
