// Phase 4 WS 파이프라인 end-to-end 검증 클라이언트
// 사용법: go run ./cmd/wstest <cameraId> [seconds]
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

type msg map[string]any

func main() {
	if len(os.Args) < 2 {
		log.Fatal("사용법: wstest <cameraId> [seconds]")
	}
	cameraID := os.Args[1]
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

	send(msg{"type": "start_stream", "cameraId": cameraID})
	defer send(msg{"type": "stop_stream", "cameraId": cameraID})

	ws.SetReadDeadline(time.Now().Add(time.Duration(seconds+5) * time.Second))

	var (
		started   bool
		packets   int
		bytes     int64
		startTime time.Time
	)
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
		switch m["type"] {
		case "stream_started":
			started = true
			startTime = time.Now()
			fmt.Printf("stream_started: codec=%v width=%v height=%v clockRate=%v sps_len=%v pps_len=%v\n",
				m["codec"], m["width"], m["height"], m["clockRate"],
				len(fmt.Sprint(m["sps"])), len(fmt.Sprint(m["pps"])))
		case "rtp_packet":
			packets++
			if p, ok := m["payload"].(string); ok {
				bytes += int64(len(p))
			}
		case "stream_error":
			log.Fatalf("stream_error: %v", m["error"])
		case "stream_stopped":
			return
		}
	}
	elapsed := time.Since(startTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	if !started {
		log.Fatal("stream_started 수신 실패")
	}
	fmt.Printf("WS 수신 결과: 패킷 %d개, %.1f kbps, %.1f초\n",
		packets, float64(bytes)*8/1000/elapsed, elapsed)
}
