// ONVIF 미디어 프로필 조회 기능을 제공한다.
package onvif

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/use-go/onvif/media"
)

// Profile은 스트림 선택에 사용할 미디어 프로필이다.
type Profile struct {
	Token  string
	Name   string
	Width  int
	Height int
}

// SDK 생성 응답 구조체의 네임스페이스 태그(xml:"onvif:...")가 실제 응답과 매칭되지 않아
// 프로필은 raw SOAP 응답을 직접 파싱한다.
type profilesEnvelope struct {
	Body struct {
		GetProfilesResponse struct {
			Profiles []struct {
				Token                     string `xml:"token,attr"`
				Name                      string `xml:"Name"`
				VideoEncoderConfiguration struct {
					Resolution struct {
						Width  int `xml:"Width"`
						Height int `xml:"Height"`
					} `xml:"Resolution"`
				} `xml:"VideoEncoderConfiguration"`
			} `xml:"Profiles"`
		} `xml:"GetProfilesResponse"`
	} `xml:"Body"`
}

// Profiles는 카메라가 제공하는 모든 미디어 프로필을 조회한다.
func (c *Client) Profiles(ctx context.Context) ([]Profile, error) {
	resp, err := c.dev.CallMethod(media.GetProfiles{})
	if err != nil {
		return nil, fmt.Errorf("프로필 조회 요청 실패: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("프로필 조회 응답 상태: %s", resp.Status)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("프로필 응답 읽기 실패: %w", err)
	}
	var env profilesEnvelope
	if err := xml.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("프로필 응답 파싱 실패: %w", err)
	}

	raws := env.Body.GetProfilesResponse.Profiles
	out := make([]Profile, 0, len(raws))
	for _, p := range raws {
		out = append(out, Profile{
			Token:  p.Token,
			Name:   p.Name,
			Width:  p.VideoEncoderConfiguration.Resolution.Width,
			Height: p.VideoEncoderConfiguration.Resolution.Height,
		})
	}
	return out, nil
}
