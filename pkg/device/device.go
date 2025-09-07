// Package device provides types and utilities for Tailscale device representation.
package device

import (
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/types"
)

// Device represents a Tailscale device with its metadata and status information.
type Device struct {
	ID                        types.DeviceID      `json:"id"`
	NodeID                    string              `json:"nodeId"`
	Name                      types.DeviceName    `json:"name"`
	Host                      string              `json:"host"`
	Tags                      []types.TagName     `json:"tags"`
	Online                    bool                `json:"online"`
	Authorized                bool                `json:"authorized"`
	LastSeen                  time.Time           `json:"lastSeen"`
	User                      string              `json:"user"`
	MachineKey                string              `json:"machineKey"`
	NodeKey                   string              `json:"nodeKey"`
	KeyExpiryDisabled         bool                `json:"keyExpiryDisabled"`
	Expires                   time.Time           `json:"expires"`
	AdvertisedRoutes          []string            `json:"advertisedRoutes"`
	EnabledRoutes             []string            `json:"enabledRoutes"`
	IsExitNode                bool                `json:"isExitNode"`
	ExitNodeOption            bool                `json:"exitNodeOption"`
	OS                        string              `json:"os"`
	ClientVersion             string              `json:"clientVersion"`
	UpdateAvailable           bool                `json:"updateAvailable"`
	Created                   time.Time           `json:"created"`
	IsExternal                bool                `json:"isExternal"`
	BlocksIncomingConnections bool                `json:"blocksIncomingConnections"`
	TailnetLockKey            string              `json:"tailnetLockKey"`
	TailnetLockError          string              `json:"tailnetLockError"`
	IsEphemeral               bool                `json:"isEphemeral"`
	MultipleConnections       bool                `json:"multipleConnections"`
	ClientConnectivity        *ClientConnectivity `json:"clientConnectivity"`
	PostureIdentity           *PostureIdentity    `json:"postureIdentity"`
}

// ClientConnectivity represents device connectivity information.
type ClientConnectivity struct {
	Endpoints             []string               `json:"endpoints"`
	Latency               map[string]LatencyInfo `json:"latency"`
	MappingVariesByDestIP bool                   `json:"mappingVariesByDestIP"`
	ClientSupports        ClientSupports         `json:"clientSupports"`
}

// LatencyInfo represents latency information to a DERP region.
type LatencyInfo struct {
	LatencyMs float64 `json:"latencyMs"`
	Preferred bool    `json:"preferred"`
}

// ClientSupports represents client capability information.
type ClientSupports struct {
	HairPinning bool `json:"hairPinning"`
	IPv6        bool `json:"ipv6"`
	PCP         bool `json:"pcp"`
	PMP         bool `json:"pmp"`
	UDP         bool `json:"udp"`
	UPnP        bool `json:"upnp"`
}

// PostureIdentity represents device posture identity information.
type PostureIdentity struct {
	SerialNumbers []string `json:"serialNumbers"`
}

// Validate checks if the device has valid required fields.
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
