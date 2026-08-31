// Phase 4 WS 파이프라인 end-to-end 검증 클라이언트
// 사용법: go run ./cmd/wstest <cameraId> [seconds]
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type msg map[string]any

func main() {
	if len(os.Args) < 2 {
		log.Fatal("사용법: wstest <cameraId,...(콤마 구분)> [seconds]")
	}
	cameraIDs := strings.Split(os.Args[1], ",")
	seconds := 5
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			seconds = n
		}
	}

	ws, _, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:8080/ws", nil)
	if err != nil {
		log.Fatal("WS 연결 실패:", err)
	}
	defer ws.Close()

	send := func(m msg) {
		b, _ := json.Marshal(m)
		if err := ws.WriteMessage(websocket.TextMessage, b); err != nil {
			log.Fatal("전송 실패:", err)
		}
	}

	for _, id := range cameraIDs {
		send(msg{"type": "start_stream", "cameraId": id})
		defer send(msg{"type": "stop_stream", "cameraId": id})
	}

	ws.SetReadDeadline(time.Now().Add(time.Duration(seconds+5) * time.Second))

	startedSet := map[string]bool{}
	packets := make(map[string]int)
	bytesArr := make(map[string]int64)
	lastSeqs := make(map[string]int64)
	gapMap := make(map[string]int64)
	startTime := time.Now()
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			log.Fatal("수신 오류:", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		cid, _ := m["cameraId"].(string)
		switch m["type"] {
		case "stream_started":
			startedSet[cid] = true
			startTime = time.Now()
			fmt.Printf("stream_started [%s]: codec=%v width=%v height=%v\n",
				cid, m["codec"], m["width"], m["height"])
		case "rtp_packet":
			packets[cid]++
			if p, ok := m["payload"].(string); ok {
				bytesArr[cid] += int64(len(p))
			}
			if seq, ok := m["sequence"].(float64); ok {
				lastSeqs[cid] = trackGap(lastSeqs, gapMap, cid, int64(seq))
			}
		case "rtp_batch":
			items, _ := m["packets"].([]any)
			for _, it := range items {
				item, _ := it.(map[string]any)
				if item == nil {
					continue
				}
				packets[cid]++
				if p, ok := item["p"].(string); ok {
					bytesArr[cid] += int64(len(p))
				}
				if sq, ok := item["sq"].(float64); ok {
					lastSeqs[cid] = trackGap(lastSeqs, gapMap, cid, int64(sq))
				}
			}
		case "stream_error":
			log.Fatalf("stream_error [%s]: %v", cid, m["error"])
		case "stream_stopped":
			return
		}
	}
	elapsed := time.Since(startTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	totalPkts := 0
	totalGaps := 0
	for _, id := range cameraIDs {
		if !startedSet[id] {
			log.Fatalf("stream_started 수신 실패: %s", id)
		}
		g := gapMap[id]
		p := packets[id]
		totalPkts += p
		totalGaps += int(g)
		fmt.Printf("[%s] 패킷 %d개, %.1f kbps, 갭 %d (%.2f%%)\n",
			id, p, float64(bytesArr[id])*8/1000/elapsed, g,
			float64(g)*100/float64(p+int(g)))
	}
	fmt.Printf("합계: 패킷 %d개, 서버→클라이언트 유실 %.2f%% (%.1f초)\n",
		totalPkts, float64(totalGaps)*100/float64(totalPkts+totalGaps), elapsed)
}

// trackGap은 시퀀스 갭을 누적하고 새 lastSeq를 반환한다.
func trackGap(lastSeqs map[string]int64, gaps map[string]int64, cid string, seq int64) int64 {
	newLast := seq & 0xffff
	if last, ok := lastSeqs[cid]; ok {
		gap := newLast - (last & 0xffff)
		if gap < 0 {
			gap += 0x10000
		}
		if gap > 1 {
			gaps[cid] += gap - 1
		}
	}
	return newLast
}
