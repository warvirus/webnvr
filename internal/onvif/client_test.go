// ONVIF 클라이언트/검색 기능을 SOAP 목업 서버로 검증하는 테스트
package onvif

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// soapOf는 내부 응답 XML을 SOAP 엔벨로프로 감싼다.
func soapOf(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">` +
		`<s:Body>` + inner + `</s:Body></s:Envelope>`
}

// newMockCamera는 표준 ONVIF 응답을 흉내 내는 목업 카메라 서버를 시작한다.
func newMockCamera(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := string(body)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		switch {
		case strings.Contains(req, "GetCapabilities"):
			resp := `<tds:GetCapabilitiesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">` +
				`<tds:Capabilities>` +
				`<tds:Device><tds:XAddr>http://mock/onvif/device_service</tds:XAddr></tds:Device>` +
				`<tds:Media><tds:XAddr>http://mock/onvif/media_service</tds:XAddr></tds:Media>` +
				`<tds:PTZ><tds:XAddr>http://mock/onvif/ptz_service</tds:XAddr></tds:PTZ>` +
				`</tds:Capabilities></tds:GetCapabilitiesResponse>`
			_, _ = w.Write([]byte(soapOf(resp)))

		case strings.Contains(req, "GetDeviceInformation"):
			resp := `<tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">` +
				`<tds:Manufacturer>MockCam</tds:Manufacturer>` +
				`<tds:Model>M-1000</tds:Model>` +
				`<tds:FirmwareVersion>1.0</tds:FirmwareVersion>` +
				`<tds:SerialNumber>SN123</tds:SerialNumber>` +
				`<tds:HardwareId>HW1</tds:HardwareId>` +
				`</tds:GetDeviceInformationResponse>`
			_, _ = w.Write([]byte(soapOf(resp)))

		case strings.Contains(req, "GetProfiles"):
			resp := `<trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
				`<trt:Profiles token="profile_1" fixed="true">` +
				`<trt:Name>main_profile</trt:Name>` +
				`<trt:VideoEncoderConfiguration><tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution></trt:VideoEncoderConfiguration>` +
				`</trt:Profiles>` +
				`<trt:Profiles token="profile_2" fixed="true">` +
				`<trt:Name>sub_profile</trt:Name>` +
				`<trt:VideoEncoderConfiguration><tt:Resolution><tt:Width>640</tt:Width><tt:Height>480</tt:Height></tt:Resolution></trt:VideoEncoderConfiguration>` +
				`</trt:Profiles>` +
				`</trt:GetProfilesResponse>`
			_, _ = w.Write([]byte(soapOf(resp)))

		case strings.Contains(req, "GetStreamUri"):
			resp := `<trt:GetStreamUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">` +
				`<trt:MediaUri><trt:Uri>rtsp://192.168.0.217:554/stream1</trt:Uri></trt:MediaUri>` +
				`</trt:GetStreamUriResponse>`
			_, _ = w.Write([]byte(soapOf(resp)))

		case strings.Contains(req, "GetPresets"):
			resp := `<tptz:GetPresetsResponse xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
				`<tptz:Preset token="preset_1"><tt:Name>Entrance</tt:Name></tptz:Preset>` +
				`<tptz:Preset token="preset_2"><tt:Name>Gate</tt:Name></tptz:Preset>` +
				`</tptz:GetPresetsResponse>`
			_, _ = w.Write([]byte(soapOf(resp)))

		case strings.Contains(req, "ContinuousMove"), strings.Contains(req, "Stop"), strings.Contains(req, "GotoPreset"):
			_, _ = w.Write([]byte(soapOf(`<tptz:OK xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl"/>`)))

		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestClient는 목업 서버에 연결된 클라이언트를 만든다.
func newTestClient(t *testing.T) (*Client, *httptest.Server) {
	t.Helper()
	srv := newMockCamera(t)
	addr := strings.TrimPrefix(srv.URL, "http://")
	c, err := New(addr, "admin", "password")
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}
	return c, srv
}

// TestNewRejectsUnreachable는 접근 불가 장치에 대해 오류를 반환하는지 확인한다.
func TestNewRejectsUnreachable(t *testing.T) {
	srv := newMockCamera(t)
	srv.Close() // 이미 닫힌 서버 = 연결 거부
	addr := strings.TrimPrefix(srv.URL, "http://")
	if _, err := New(addr, "admin", "pw"); err == nil {
		t.Fatal("연결 불가 장치에 대해 오류 없이 생성됨")
	}
}

// TestDeviceInformation은 장치 정보 조회를 확인한다.
func TestDeviceInformation(t *testing.T) {
	c, _ := newTestClient(t)
	info, err := c.DeviceInformation(context.Background())
	if err != nil {
		t.Fatalf("DeviceInformation() err = %v", err)
	}
	if info.Manufacturer != "MockCam" || info.Model != "M-1000" {
		t.Errorf("장치 정보 불일치: %+v", info)
	}
}

// TestProfiles는 프로필 목록(토큰/이름/해상도) 파싱을 확인한다.
func TestProfiles(t *testing.T) {
	c, _ := newTestClient(t)
	profiles, err := c.Profiles(context.Background())
	if err != nil {
		t.Fatalf("Profiles() err = %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("프로필 수 = %d, want 2", len(profiles))
	}
	p1 := profiles[0]
	if p1.Token != "profile_1" || p1.Name != "main_profile" || p1.Width != 1920 || p1.Height != 1080 {
		t.Errorf("profile_1 파싱 불일치: %+v", p1)
	}
	if profiles[1].Width != 640 {
		t.Errorf("profile_2 해상도 불일치: %+v", profiles[1])
	}
}

// TestStreamURI는 스트림 URI 조회를 확인한다.
func TestStreamURI(t *testing.T) {
	c, _ := newTestClient(t)
	uri, err := c.StreamURI(context.Background(), "profile_1", "RTSP")
	if err != nil {
		t.Fatalf("StreamURI() err = %v", err)
	}
	if uri != "rtsp://192.168.0.217:554/stream1" {
		t.Errorf("URI = %q", uri)
	}
}

// TestPTZCommands는 PTZ 이동/정지/프리셋 명령을 확인한다.
func TestPTZCommands(t *testing.T) {
	c, _ := newTestClient(t)
	ctx := context.Background()

	if err := c.ContinuousMove(ctx, "profile_1", PTZMove{Pan: 0.5, Tilt: -0.2, Zoom: 0.1}); err != nil {
		t.Errorf("ContinuousMove() err = %v", err)
	}
	if err := c.Stop(ctx, "profile_1"); err != nil {
		t.Errorf("Stop() err = %v", err)
	}
	presets, err := c.Presets(ctx, "profile_1")
	if err != nil {
		t.Fatalf("Presets() err = %v", err)
	}
	if len(presets) != 2 || presets[0].Name != "Entrance" || presets[1].Token != "preset_2" {
		t.Errorf("프리셋 파싱 불일치: %+v", presets)
	}
	if err := c.GotoPreset(ctx, "profile_1", "preset_1"); err != nil {
		t.Errorf("GotoPreset() err = %v", err)
	}
}

// TestXAddrHost는 XAddrs 파싱을 확인한다.
func TestXAddrHost(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://192.168.0.217:8090/onvif/device_service", "192.168.0.217:8090"},
		{"http://192.168.0.10/onvif/device_service", ""}, // 포트 없음 → 제외
		{"https://10.0.0.5:8899/onvif/device_service", "10.0.0.5:8899"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := xaddrHost(tc.in); got != tc.want {
			t.Errorf("xaddrHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestProbeReplyParsing은 벤더별 WS-Discovery 응답 구조 파싱을 확인한다.
func TestProbeReplyParsing(t *testing.T) {
	// (1) 표준: ProbeMatches 래퍼 + d: 접두어
	spec := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
<s:Body><d:ProbeMatches><d:ProbeMatch>
<d:Scopes>onvif://www.onvif.org/Profile/Streaming onvif://www.onvif.org/name/FrontDoor</d:Scopes>
<d:XAddrs>http://192.168.0.217:8090/onvif/device_service</d:XAddrs>
</d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`
	// (2) 비표준(PythonCam): ProbeMatches 래퍼 없음, 이름에 공백
	loose := `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <soap:Body>
    <d:ProbeMatch>
      <d:Types>dn:NetworkVideoTransmitter tds:Device</d:Types>
      <d:Scopes>onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/Virtual Camera 1</d:Scopes>
      <d:XAddrs>http://192.168.0.217:8090/onvif/device_service</d:XAddrs>
    </d:ProbeMatch>
  </soap:Body>
</soap:Envelope>`

	x, s := parseReply(spec)
	if len(x) != 1 || xaddrHost(x[0]) != "192.168.0.217:8090" {
		t.Errorf("표준 XAddrs 파싱 실패: %v", x)
	}
	if got := extractName(s); got != "FrontDoor" {
		t.Errorf("표준 name = %q", got)
	}

	x, s = parseReply(loose)
	if len(x) != 1 || xaddrHost(x[0]) != "192.168.0.217:8090" {
		t.Errorf("비표준 XAddrs 파싱 실패: %v", x)
	}
	if got := extractName(s); got != "Virtual Camera 1" {
		t.Errorf("비표준(공백 포함) name = %q, want %q", got, "Virtual Camera 1")
	}

	// %20 인코딩된 이름
	if got := extractName("onvif://x/name/Front%20Door"); got != "Front Door" {
		t.Errorf("%%20 디코딩 실패: %q", got)
	}
}
