// ONVIF WS-Discovery 기반 카메라 자동 검색을 제공한다.
package onvif

import (
	"encoding/xml"
	"regexp"
	"strings"

	wsdiscovery "github.com/use-go/onvif/ws-discovery"
)

// probeResponse는 WS-Discovery ProbeMatch 응답의 필요한 부분만 파싱한다.
type probeResponse struct {
	XAddrs   string `xml:"Body>ProbeMatches>ProbeMatch>XAddrs"`
	Scopes   string `xml:"Body>ProbeMatches>ProbeMatch>Scopes"`
	Endpoint string `xml:"Header>EndpointReference>Address"`
}

// Discovered는 검색된 카메라 후보 정보다.
type Discovered struct {
	XAddr  string // "host:port"
	Types  string
	Scopes string
}

// scopesNameRe는 Scopes 필드에서 카메라 이름을 추출한다.
var scopesNameRe = regexp.MustCompile(`(?i)onvif://[^ ]*/name/([^ ]+)`)

// Discover는 로컬 네트워크의 ONVIF 카메라를 검색한다.
// interfaces는 시도할 네트워크 인터페이스 목록이며 빈 값이면 첫 번째 성공 인터페이스를 사용한다.
// WS-Discovery 응답 대기는 라이브러리 내부에서 약 1초로 고정되어 있다.
func Discover(interfaces []string) ([]Discovered, error) {
	if len(interfaces) == 0 {
		interfaces = []string{""} // ""는 시스템 기본 라우팅 인터페이스를 의미
	}

	seen := map[string]bool{}
	var out []Discovered
	for _, ifc := range interfaces {
		replies, err := wsdiscovery.SendProbe(ifc, nil, []string{"dn:NetworkVideoTransmitter"}, map[string]string{"dn": "http://www.onvif.org/ver10/network/wsdl"})
		if err != nil {
			continue // 지정 인터페이스가 없는 등의 오류는 다음 인터페이스로 진행
		}
		for _, reply := range replies {
			var pr probeResponse
			if err := unmarshalProbe(reply, &pr); err != nil {
				continue
			}
			for _, raw := range strings.Fields(pr.XAddrs) {
				host := xaddrHost(raw)
				if host == "" || seen[host] {
					continue
				}
				seen[host] = true
				out = append(out, Discovered{
					XAddr:  host,
					Types:  "NetworkVideoTransmitter",
					Scopes: extractName(pr.Scopes),
				})
			}
		}
	}
	return out, nil
}

// unmarshalProbe는 WS-Discovery 응답 XML을 파싱한다. (테스트를 위해 분리)
func unmarshalProbe(reply string, pr *probeResponse) error {
	return xml.Unmarshal([]byte(reply), pr)
}

// xaddrHost는 "http://192.168.0.1:80/onvif/..." 형태에서 "192.168.0.1:80"을 추출한다.
func xaddrHost(raw string) string {
	s := strings.TrimPrefix(raw, "http://")
	s = strings.TrimPrefix(s, "https://")
	if i := strings.IndexAny(s, "/"); i >= 0 {
		s = s[:i]
	}
	if s == "" || !strings.Contains(s, ":") {
		return "" // 포트 없는 주소는 사용하지 않는다 (포트는 필수 요건).
	}
	return s
}

// extractName은 Scopes에서 카메라 이름을 추출한다. 없으면 빈 값을 반환한다.
func extractName(scopes string) string {
	m := scopesNameRe.FindStringSubmatch(scopes)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
