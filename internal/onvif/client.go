// use-go/onvif SDK를 래핑해 카메라 제어에 필요한 ONVIF 연산을 제공하는 패키지
package onvif

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	goonvif "github.com/use-go/onvif"
	"github.com/use-go/onvif/device"
	sdkdevice "github.com/use-go/onvif/sdk/device"
)

// Client는 단일 ONVIF 카메라와의 세션을 나타낸다.
type Client struct {
	dev      *goonvif.Device
	xaddr    string
	username string
	password string
}

// DeviceInformation은 카메라 장치 정보다.
type DeviceInformation struct {
	Manufacturer string
	Model        string
	Firmware     string
	SerialNumber string
	HardwareID   string
}

// New는 카메라에 연결해 ONVIF 클라이언트를 생성한다.
// 연결 가능 여부를 확인하기 위해 GetCapabilities를 호출하므로,
// 카메라에 접근할 수 없으면 오류를 반환한다. xaddr은 "host:port" 형식이다.
func New(xaddr, username, password string) (*Client, error) {
	xaddr = strings.TrimPrefix(strings.TrimSpace(xaddr), "http://")
	dev, err := goonvif.NewDevice(goonvif.DeviceParams{
		Xaddr:      xaddr,
		Username:   username,
		Password:   password,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("ONVIF 장치 연결 실패 (%s): %w", xaddr, err)
	}
	return &Client{dev: dev, xaddr: xaddr, username: username, password: password}, nil
}

// DeviceInformation은 장치 정보를 조회한다. 연결 테스트 용도로도 사용된다.
func (c *Client) DeviceInformation(ctx context.Context) (*DeviceInformation, error) {
	resp, err := sdkdevice.Call_GetDeviceInformation(ctx, c.dev, device.GetDeviceInformation{})
	if err != nil {
		return nil, fmt.Errorf("장치 정보 조회 실패: %w", err)
	}
	return &DeviceInformation{
		Manufacturer: string(resp.Manufacturer),
		Model:        string(resp.Model),
		Firmware:     string(resp.FirmwareVersion),
		SerialNumber: string(resp.SerialNumber),
		HardwareID:   string(resp.HardwareId),
	}, nil
}

// XAddr은 클라이언트가 연결된 주소를 반환한다.
func (c *Client) XAddr() string {
	return c.xaddr
}
