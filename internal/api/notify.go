// 카메라/앱 설정 변경을 접속 중인 모든 클라이언트에 알리는 통지 인터페이스다. (ws.Server가 구현)
package api

// changeBroadcaster는 mutation 발생 시 전 클라이언트에 변경을 브로드캐스트한다.
// nil이어도 안전하도록 호출부에서 가드한다(헤드리스 도구/유닛 테스트).
type changeBroadcaster interface {
	// BroadcastCamerasChanged는 카메라 목록/설정 변경을 알린다.
	// reason ∈ added|updated|deleted|reordered|restored. cameraID는 단일 카메라 변경 시에만 채워진다.
	BroadcastCamerasChanged(reason, cameraID string)
	// BroadcastConfigChanged는 앱 설정 변경을 알린다.
	BroadcastConfigChanged()
}
