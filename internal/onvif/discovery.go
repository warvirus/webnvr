// ONVIF WS-Discovery 기반 카메라 자동 검색을 제공한다.
// use-go/onvif의 SendProbe는 소켓을 0.0.0.0에 바인딩해 유니캐스트 ProbeMatch 응답을
// 받지 못하는 사례가 있어(특히 macOS + 같은 서브넷 다중 NIC), 인터페이스 IP에 직접
// 바인딩하는 자체 구현을 쓴다.
package onvif

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/ipv4"
)

// Discovered는 검색된 카메라 후보 정보다.
type Discovered struct {
	XAddr  string // "host:port"
	Types  string
	Scopes string
}

// WS-Discovery 응답은 벤더마다 네임스페이스 접두어와 구조가 제각각이라(예: ProbeMatches
// 래퍼 유무) 요소 이름 기준 정규식으로 뽑는다.
var (
	xaddrsRe = regexp.MustCompile(`(?s)<[\w.-]*:?XAddrs[^>]*>(.*?)</[\w.-]*:?XAddrs>`)
	scopesRe = regexp.MustCompile(`(?s)<[\w.-]*:?Scopes[^>]*>(.*?)</[\w.-]*:?Scopes>`)
	// name 스코프는 공백을 포함할 수 있어(비표준) 다음 onvif:// 토큰 또는 끝까지 잡는다.
	scopesNameRe = regexp.MustCompile(`(?is)onvif://\S*/name/(.+?)(?:\s+onvif://|$)`)
)

// mcastAddr는 WS-Discovery 멀티캐스트 목적지다.
var mcastAddr = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702}

// Discover는 로컬 네트워크의 ONVIF 카메라를 검색한다.
// interfaces가 비어 있으면 활성 멀티캐스트 IPv4 인터페이스를 모두 자동으로 탐색한다.
// timeout이 0 이하면 3초를 사용한다.
func Discover(interfaces []string, timeout time.Duration) ([]Discovered, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	ips := resolveProbeIPs(interfaces)
	if len(ips) == 0 {
		slog.Warn("WS-Discovery: 탐색할 인터페이스가 없습니다")
		return nil, nil
	}

	seen := map[string]bool{}
	var out []Discovered
	for _, ip := range ips {
		replies, err := probeOn(ip, timeout)
		if err != nil {
			slog.Warn("WS-Discovery 프로브 실패", "iface_ip", ip.String(), "err", err)
			continue
		}
		added := 0
		for _, reply := range replies {
			xaddrs, scopes := parseReply(reply)
			for _, raw := range xaddrs {
				host := xaddrHost(raw)
				if host == "" || seen[host] {
					continue
				}
				seen[host] = true
				out = append(out, Discovered{
					XAddr:  host,
					Types:  "NetworkVideoTransmitter",
					Scopes: extractName(scopes),
				})
				added++
			}
		}
		slog.Info("WS-Discovery 프로브", "iface_ip", ip.String(), "replies", len(replies), "new_cameras", added)
	}
	return out, nil
}

// resolveProbeIPs는 인터페이스 이름 목록을 IPv4 주소로 변환한다.
// 목록이 비었거나 이름을 하나도 해석하지 못하면 활성 인터페이스를 자동 열거한다.
func resolveProbeIPs(names []string) []net.IP {
	var ips []net.IP
	for _, name := range names {
		if name == "" {
			continue
		}
		iface, err := net.InterfaceByName(name)
		if err != nil {
			slog.Warn("WS-Discovery: 인터페이스 없음", "name", name)
			continue
		}
		if ip := firstUsableIPv4(iface); ip != nil {
			ips = append(ips, ip)
		}
	}
	if len(ips) > 0 {
		return ips
	}
	// 자동 열거 — UP, 비루프백, 멀티캐스트, 비 point-to-point(VPN 제외)
	ifaces, _ := net.Interfaces()
	for i := range ifaces {
		f := ifaces[i].Flags
		if f&net.FlagUp == 0 || f&net.FlagLoopback != 0 || f&net.FlagMulticast == 0 || f&net.FlagPointToPoint != 0 {
			continue
		}
		if strings.HasPrefix(ifaces[i].Name, "awdl") || strings.HasPrefix(ifaces[i].Name, "llw") {
			continue // Apple Wireless Direct Link / Low Latency WLAN
		}
		if ip := firstUsableIPv4(&ifaces[i]); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

// firstUsableIPv4는 인터페이스의 첫 사설/일반 IPv4를 반환한다. (링크로컬 169.254 제외)
func firstUsableIPv4(iface *net.Interface) net.IP {
	addrs, _ := iface.Addrs()
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipn.IP.To4()
		if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
			continue
		}
		return ip4
	}
	return nil
}

// probeOn은 지정 인터페이스 IP에 바인딩해 Probe를 보내고 timeout 동안 응답을 모은다.
func probeOn(ip net.IP, timeout time.Duration) ([]string, error) {
	c, err := net.ListenPacket("udp4", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		return nil, err
	}
	defer c.Close()

	p := ipv4.NewPacketConn(c)
	if iface := ifaceForIP(ip); iface != nil {
		_ = p.SetMulticastInterface(iface)
	}
	_ = p.SetMulticastTTL(2)

	if _, err := p.WriteTo([]byte(buildProbeSOAP()), nil, mcastAddr); err != nil {
		return nil, fmt.Errorf("probe 전송 실패: %w", err)
	}
	if err := p.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}

	var replies []string
	buf := make([]byte, 16384)
	for {
		n, _, _, err := p.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return replies, err
		}
		replies = append(replies, string(buf[:n]))
	}
	return replies, nil
}

// ifaceForIP는 주어진 IPv4를 가진 인터페이스를 찾는다.
func ifaceForIP(ip net.IP) *net.Interface {
	ifaces, _ := net.Interfaces()
	for i := range ifaces {
		addrs, _ := ifaces[i].Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(ip) {
				return &ifaces[i]
			}
		}
	}
	return nil
}

// buildProbeSOAP는 NetworkVideoTransmitter를 찾는 최소 WS-Discovery Probe 메시지를 만든다.
func buildProbeSOAP() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope"` +
		` xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing"` +
		` xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"` +
		` xmlns:dn="http://www.onvif.org/ver10/network/wsdl">` +
		`<e:Header>` +
		`<w:MessageID>uuid:` + uuid.NewString() + `</w:MessageID>` +
		`<w:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>` +
		`<w:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>` +
		`</e:Header>` +
		`<e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body>` +
		`</e:Envelope>`
}

// parseReply는 하나의 ProbeMatch 응답에서 XAddrs 목록과 Scopes 문자열을 뽑는다.
func parseReply(reply string) (xaddrs []string, scopes string) {
	if m := xaddrsRe.FindStringSubmatch(reply); len(m) == 2 {
		xaddrs = strings.Fields(m[1])
	}
	if m := scopesRe.FindStringSubmatch(reply); len(m) == 2 {
		scopes = strings.Join(strings.Fields(m[1]), " ")
	}
	return xaddrs, scopes
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
// (일부 카메라는 이름에 공백을 그대로 두거나 %20으로 인코딩한다 — 둘 다 처리)
func extractName(scopes string) string {
	m := scopesNameRe.FindStringSubmatch(scopes)
	if len(m) < 2 {
		return ""
	}
	name := strings.TrimSpace(m[1])
	if dec, err := url.QueryUnescape(name); err == nil {
		name = dec
	}
	return name
}
