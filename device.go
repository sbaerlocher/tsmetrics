package main

import (
	"time"
)

type Device struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Host              string    `json:"host"`
	Tags              []string  `json:"tags"`
	Online            bool      `json:"online"`
	Authorized        bool      `json:"authorized"`
	LastSeen          time.Time `json:"lastSeen"`
	User              string    `json:"user"`
	MachineKey        string    `json:"machineKey"`
	KeyExpiryDisabled bool      `json:"keyExpiryDisabled"`
	Expires           time.Time `json:"expires"`
	AdvertisedRoutes  []string  `json:"advertisedRoutes"`
	EnabledRoutes     []string  `json:"enabledRoutes"`
	IsExitNode        bool      `json:"isExitNode"`
	ExitNodeOption    bool      `json:"exitNodeOption"`
	OS                string    `json:"os"`
	ClientVersion     string    `json:"clientVersion"`
}
