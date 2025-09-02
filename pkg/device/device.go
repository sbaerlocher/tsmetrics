package device

import (
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/types"
)

type Device struct {
	ID                types.DeviceID   `json:"id"`
	Name              types.DeviceName `json:"name"`
	Host              string           `json:"host"`
	Tags              []types.TagName  `json:"tags"`
	Online            bool             `json:"online"`
	Authorized        bool             `json:"authorized"`
	LastSeen          time.Time        `json:"lastSeen"`
	User              string           `json:"user"`
	MachineKey        string           `json:"machineKey"`
	KeyExpiryDisabled bool             `json:"keyExpiryDisabled"`
	Expires           time.Time        `json:"expires"`
	AdvertisedRoutes  []string         `json:"advertisedRoutes"`
	EnabledRoutes     []string         `json:"enabledRoutes"`
	IsExitNode        bool             `json:"isExitNode"`
	ExitNodeOption    bool             `json:"exitNodeOption"`
	OS                string           `json:"os"`
	ClientVersion     string           `json:"clientVersion"`
}

func (d Device) Validate() error {
	if !d.ID.IsValid() {
		return types.ErrInvalidDeviceID
	}
	if !d.Name.IsValid() {
		return types.ErrInvalidDeviceName
	}
	if err := types.ValidateHostname(d.Host); err != nil {
		return err
	}
	for _, tag := range d.Tags {
		if !tag.IsValid() {
			return types.ErrInvalidTagName
		}
	}
	return nil
}
