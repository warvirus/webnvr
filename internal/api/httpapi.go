// /api/* REST 핸들러 — 서비스 계층(CameraService)의 위임자다 (doc v1.1 §5.1).
// 로직 중복 금지: 모든 핸들러는 CameraService 메서드를 호출할 뿐이다.
package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// httpError는 서비스 오류를 HTTP 상태 코드로 매핑한다.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

func errNotFound(id string) *httpError {
	return &httpError{status: http.StatusNotFound, message: fmt.Sprintf("카메라를 찾을 수 없음: %s", id)}
}

func errBadReq(msg string) *httpError {
	return &httpError{status: http.StatusBadRequest, message: msg}
}

func errInternal(msg string) *httpError {
	return &httpError{status: http.StatusInternalServerError, message: msg}
}

// writeJSON은 응답을 JSON으로 직렬화한다.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr는 오류를 적절한 상태 코드로 변환해 응답한다.
func writeErr(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeJSON(w, he.status, map[string]string{"error": he.message})
		return
	}
	if isNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

// statusRecorder는 응답 상태 코드를 캡처하는 ResponseWriter 래퍼다.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.statusCode = code
	sr.ResponseWriter.WriteHeader(code)
}

// Hijack는 WebSocket 업그레이드를 지원하기 위해 http.Hijacker를 구현한다.
func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := sr.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("ResponseWriter does not support Hijacker")
	}
	return h.Hijack()
}

// AccessLog는 HTTP 요청/응답을 로깅하는 미들웨어다.
// 모든 요청(API/WS/정적 UI)의 경로, 메서드, 상태 코드, 소요 시간을 기록한다.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(sr, r)
		duration := time.Since(start).Milliseconds()
		slog.Info("HTTP", "method", r.Method, "path", r.URL.Path, "status", sr.statusCode, "duration_ms", duration)
	})
}

// CORS는 DevServer 등 크로스 오리진 접근을 허용하는 미들웨어다.
// 서버는 127.0.0.1에만 바인딩되므로 로컬 노출에 한정된다 (Phase 6에서 재검토).
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// decodeBody는 요청 본문을 JSON으로 파싱한다.
func decodeBody(r *http.Request, v any) *httpError {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errBadReq("요청 본문 파싱 실패: " + err.Error())
	}
	return nil
}

// RegisterHTTP는 /api/* 라우트를 mux에 등록한다.
func RegisterHTTP(mux *http.ServeMux, app *App) {
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": 1})
	})

	mux.HandleFunc("/api/cameras", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cams, err := app.Camera.ListCameras()
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, cams)

		case http.MethodPost:
			var req CreateCameraRequest
			if he := decodeBody(r, &req); he != nil {
				writeErr(w, he)
				return
			}
			saved, err := app.Camera.CreateCamera(req)
			if err != nil {
				writeErr(w, errBadReq(err.Error()))
				return
			}
			writeJSON(w, http.StatusCreated, saved)

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "허용하지 않는 메서드"})
		}
	})

	mux.HandleFunc("/api/cameras/reorder", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST만 허용"})
			return
		}
		var req struct {
			IDs []string `json:"ids"`
		}
		if he := decodeBody(r, &req); he != nil {
			writeErr(w, he)
			return
		}
		if err := app.Camera.ReorderCameras(req.IDs); err != nil {
			writeErr(w, errBadReq(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("/api/cameras/discover", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST만 허용"})
			return
		}
		found, err := app.Camera.DiscoverONVIFCameras()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, found)
	})

	mux.HandleFunc("/api/cameras/test-onvif", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST만 허용"})
			return
		}
		var req TestONVIFRequest
		if he := decodeBody(r, &req); he != nil {
			writeErr(w, he)
			return
		}
		res, err := app.Camera.TestONVIFCamera(req)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("/api/cameras/test-direct", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST만 허용"})
			return
		}
		var req TestDirectStreamRequest
		if he := decodeBody(r, &req); he != nil {
			writeErr(w, he)
			return
		}
		res, err := app.Camera.TestDirectStream(req)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// 미등록 카메라용 ONVIF 조회 — 본문으로 자격증명을 전달한다 (doc §5.1 자격증명 규칙)
	mux.HandleFunc("/api/onvif/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST만 허용"})
			return
		}
		var req GetProfilesRequest
		if he := decodeBody(r, &req); he != nil {
			writeErr(w, he)
			return
		}
		profiles, err := app.Camera.GetONVIFProfiles(req)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profiles)
	})

	mux.HandleFunc("/api/security", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
			return
		}
		writeJSON(w, http.StatusOK, SecurityStatusOf())
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, app.Camera.AppConfig())
			return
		}
		if r.Method != http.MethodPut {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET/PUT만 허용"})
			return
		}
		var cfg map[string]any
		if he := decodeBody(r, &cfg); he != nil {
			writeErr(w, he)
			return
		}
		saved, err := app.Camera.UpdateAppConfig(cfg)
		if err != nil {
			writeErr(w, errBadReq(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, saved)
	})

	// 백업: 카메라 목록(비밀번호 제외) + 앱 설정을 하나의 JSON으로 내려준다.
	mux.HandleFunc("/api/backup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
			return
		}
		backup, err := app.Camera.ExportBackup()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, backup)
	})

	// 복원: 백업 JSON을 받아 카메라를 대체한다. 비밀번호는 백업에 없으므로 재입력이 필요하다.
	mux.HandleFunc("/api/backup/restore", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST만 허용"})
			return
		}
		var backup BackupFile
		if he := decodeBody(r, &backup); he != nil {
			writeErr(w, he)
			return
		}
		added, err := app.Camera.RestoreBackup(backup)
		if err != nil {
			writeErr(w, errBadReq(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restored": added})
	})

	// /api/cameras/{id}[/...] 하위 라우트
	mux.HandleFunc("/api/cameras/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/cameras/")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		id := parts[0]

		switch {
		case len(parts) == 1:
			cameraByID(w, r, app, id)
		case len(parts) == 2 && parts[1] == "profiles":
			cameraProfiles(w, r, app, id)
		case len(parts) == 2 && parts[1] == "presets":
			cameraPresets(w, r, app, id)
		case len(parts) == 2 && parts[1] == "stream-uri":
			cameraStreamURI(w, r, app, id)
		case len(parts) == 3 && parts[1] == "record" && parts[2] == "event":
			cameraTriggerEvent(w, r, app, id)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "알 수 없는 경로: " + r.URL.Path})
		}
	})
}

// cameraTriggerEvent는 POST /api/cameras/{id}/record/event를 처리한다. (R.4 수동 트리거)
func cameraTriggerEvent(w http.ResponseWriter, r *http.Request, app *App, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST만 허용"})
		return
	}
	var req struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body 없어도 허용 — type 기본값 manual
	if err := app.Camera.TriggerCameraEvent(id, req.Type); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// cameraByID는 /api/cameras/{id} (GET/PUT/DELETE)를 처리한다.
func cameraByID(w http.ResponseWriter, r *http.Request, app *App, id string) {
	switch r.Method {
	case http.MethodGet:
		cam, err := app.Camera.GetCamera(id)
		if err != nil {
			writeErr(w, errNotFound(id))
			return
		}
		writeJSON(w, http.StatusOK, cam)

	case http.MethodPut:
		var req UpdateCameraRequest
		if he := decodeBody(r, &req); he != nil {
			writeErr(w, he)
			return
		}
		saved, err := app.Camera.UpdateCamera(id, req)
		if err != nil {
			if isNotFound(err) {
				writeErr(w, errNotFound(id))
				return
			}
			writeErr(w, errBadReq(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, saved)

	case http.MethodDelete:
		if err := app.Camera.DeleteCamera(id); err != nil {
			if isNotFound(err) {
				writeErr(w, errNotFound(id))
				return
			}
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET/PUT/DELETE만 허용"})
	}
}

// cameraProfiles는 GET /api/cameras/{id}/profiles를 처리한다 (저장 자격증명 사용).
func cameraProfiles(w http.ResponseWriter, r *http.Request, app *App, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
		return
	}
	profiles, err := app.Camera.GetCameraProfiles(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profiles)
}

// cameraPresets는 GET /api/cameras/{id}/presets를 처리한다 (저장 자격증명 사용).
func cameraPresets(w http.ResponseWriter, r *http.Request, app *App, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
		return
	}
	presets, err := app.Camera.GetCameraPresets(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, presets)
}

// cameraStreamURI는 GET /api/cameras/{id}/stream-uri를 처리한다 (저장 자격증명 사용).
func cameraStreamURI(w http.ResponseWriter, r *http.Request, app *App, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET만 허용"})
		return
	}
	uri, err := app.Camera.GetCameraStreamURI(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"uri": uri})
}

// isNotFound는 서비스 계층의 "카메라를 찾을 수 없음" 오류를 판별한다.
func isNotFound(err error) bool {
	return strings.Contains(err.Error(), "카메라를 찾을 수 없음")
}
