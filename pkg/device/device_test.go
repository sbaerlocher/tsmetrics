package device

import (
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/types"
)

func TestDeviceValidate(t *testing.T) {
	validDeviceID, _ := types.NewDeviceID("device123")
	validDeviceName, _ := types.NewDeviceName("test-device")
	validTag, _ := types.NewTagName("production")

	tests := []struct {
		name    string
		device  Device
		wantErr bool
	}{
		{
			name: "valid device",
			device: Device{
				ID:   validDeviceID,
				Name: validDeviceName,
				Host: "example.com",
				Tags: []types.TagName{validTag},
			},
			wantErr: false,
		},
		{
			name: "invalid device ID",
			device: Device{
				ID:   types.DeviceID(""),
				Name: validDeviceName,
				Host: "example.com",
				Tags: []types.TagName{validTag},
			},
			wantErr: true,
		},
		{
			name: "invalid device name",
			device: Device{
				ID:   validDeviceID,
				Name: types.DeviceName(""),
				Host: "example.com",
				Tags: []types.TagName{validTag},
			},
			wantErr: true,
		},
		{
			name: "invalid hostname",
			device: Device{
				ID:   validDeviceID,
				Name: validDeviceName,
				Host: "192.168.1.1",
				Tags: []types.TagName{validTag},
			},
			wantErr: true,
		},
		{
			name: "invalid tag",
			device: Device{
				ID:   validDeviceID,
				Name: validDeviceName,
				Host: "example.com",
				Tags: []types.TagName{types.TagName("")},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.device.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Device.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeviceCreation(t *testing.T) {
	deviceID, err := types.NewDeviceID("device123")
	if err != nil {
		t.Fatalf("Failed to create device ID: %v", err)
	}

	deviceName, err := types.NewDeviceName("test-device")
	if err != nil {
		t.Fatalf("Failed to create device name: %v", err)
	}

	tag, err := types.NewTagName("production")
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	device := Device{
		ID:                deviceID,
		Name:              deviceName,
		Host:              "example.com",
		Tags:              []types.TagName{tag},
		Online:            true,
		Authorized:        true,
		LastSeen:          time.Now(),
		User:              "test@example.com",
		MachineKey:        "mkey:abcd1234",
		KeyExpiryDisabled: false,
		Expires:           time.Now().Add(24 * time.Hour),
		AdvertisedRoutes:  []string{"10.0.0.0/24"},
		EnabledRoutes:     []string{"10.0.0.0/24"},
		IsExitNode:        false,
		ExitNodeOption:    true,
		OS:                "linux",
		ClientVersion:     "1.54.0",
	}

	if err := device.Validate(); err != nil {
		t.Errorf("Valid device failed validation: %v", err)
	}

	if device.ID.String() != "device123" {
		t.Errorf("Expected device ID 'device123', got %s", device.ID.String())
	}

	if device.Name.String() != "test-device" {
		t.Errorf("Expected device name 'test-device', got %s", device.Name.String())
	}
}
