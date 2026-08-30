// ONVIF 프로필별 스트림 URI 조회 기능을 제공한다.
package onvif

import (
	"context"
	"fmt"
	"strings"

	"github.com/use-go/onvif/media"
	sdkmedia "github.com/use-go/onvif/sdk/media"
	"github.com/use-go/onvif/xsd/onvif"
)

// StreamURI는 지정 프로필의 RTSP 스트림 URI를 조회한다.
// protocol은 "RTSP" 또는 "RTSP over HTTP" 등 전송 프로토콜이다.
func (c *Client) StreamURI(ctx context.Context, profileToken, protocol string) (string, error) {
	if strings.TrimSpace(protocol) == "" {
		protocol = "RTSP"
	}
	resp, err := sdkmedia.Call_GetStreamUri(ctx, c.dev, media.GetStreamUri{
		StreamSetup: onvif.StreamSetup{
			Stream:    onvif.StreamType("RTP-Unicast"),
			Transport: onvif.Transport{Protocol: onvif.TransportProtocol(strings.ToUpper(protocol))},
		},
		ProfileToken: onvif.ReferenceToken(profileToken),
	})
	if err != nil {
		return "", fmt.Errorf("스트림 URI 조회 실패: %w", err)
	}
	uri := string(resp.MediaUri.Uri)
	if uri == "" {
		return "", fmt.Errorf("스트림 URI가 비어 있음 (profile: %s)", profileToken)
	}
	return uri, nil
}
