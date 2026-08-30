// ONVIF PTZ 제어(연속 이동, 정지, 프리셋)를 제공한다.
package onvif

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/use-go/onvif/ptz"
	sdkptz "github.com/use-go/onvif/sdk/ptz"
	"github.com/use-go/onvif/xsd"
	"github.com/use-go/onvif/xsd/onvif"
)

// PTZMove는 PTZ 이동 명령이다. 각 값은 -1.0~1.0 범위이며 0은 정지 방향이다.
type PTZMove struct {
	Pan  float64
	Tilt float64
	Zoom float64 // 0~1.0
}

// ContinuousMove는 지정 시간 동안 카메라를 연속 이동시킨다.
func (c *Client) ContinuousMove(ctx context.Context, profileToken string, mv PTZMove) error {
	req := ptz.ContinuousMove{
		ProfileToken: onvif.ReferenceToken(profileToken),
		Velocity: onvif.PTZSpeed{
			PanTilt: onvif.Vector2D{X: mv.Pan, Y: mv.Tilt},
			Zoom:    onvif.Vector1D{X: mv.Zoom},
		},
	}
	if _, err := sdkptz.Call_ContinuousMove(ctx, c.dev, req); err != nil {
		return fmt.Errorf("PTZ 연속 이동 실패: %w", err)
	}
	return nil
}

// Stop은 진행 중인 PTZ 이동을 정지한다.
func (c *Client) Stop(ctx context.Context, profileToken string) error {
	req := ptz.Stop{
		ProfileToken: onvif.ReferenceToken(profileToken),
		PanTilt:      xsd.Boolean(true),
		Zoom:         xsd.Boolean(true),
	}
	if _, err := sdkptz.Call_Stop(ctx, c.dev, req); err != nil {
		return fmt.Errorf("PTZ 정지 실패: %w", err)
	}
	return nil
}

// Preset은 저장된 PTZ 프리셋이다.
type Preset struct {
	Token string
	Name  string
}

// presetsEnvelope는 SDK의 GetPresets 응답 파싱(단일 필드) 한계를 우회하기 위한 자체 파서다.
type presetsEnvelope struct {
	Body struct {
		GetPresetsResponse struct {
			Presets []struct {
				Token string `xml:"token,attr"`
				Name  string `xml:"Name"`
			} `xml:"Preset"`
		} `xml:"GetPresetsResponse"`
	} `xml:"Body"`
}

// Presets는 카메라에 저장된 PTZ 프리셋 목록을 조회한다.
func (c *Client) Presets(ctx context.Context, profileToken string) ([]Preset, error) {
	resp, err := c.dev.CallMethod(ptz.GetPresets{
		ProfileToken: onvif.ReferenceToken(profileToken),
	})
	if err != nil {
		return nil, fmt.Errorf("PTZ 프리셋 조회 요청 실패: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("PTZ 프리셋 조회 응답 상태: %s", resp.Status)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("PTZ 프리셋 응답 읽기 실패: %w", err)
	}
	var env presetsEnvelope
	if err := xml.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("PTZ 프리셋 응답 파싱 실패: %w", err)
	}

	raws := env.Body.GetPresetsResponse.Presets
	out := make([]Preset, 0, len(raws))
	for _, p := range raws {
		out = append(out, Preset{Token: p.Token, Name: p.Name})
	}
	return out, nil
}

// GotoPreset은 지정 프리셋 위치로 카메라를 이동한다.
func (c *Client) GotoPreset(ctx context.Context, profileToken, presetToken string) error {
	req := ptz.GotoPreset{
		ProfileToken: onvif.ReferenceToken(profileToken),
		PresetToken:  onvif.ReferenceToken(presetToken),
	}
	if _, err := sdkptz.Call_GotoPreset(ctx, c.dev, req); err != nil {
		return fmt.Errorf("PTZ 프리셋 이동 실패: %w", err)
	}
	return nil
}
