// Package api provides HTTP client functionality for interacting with the Tailscale API.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2/clientcredentials"

	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

// Client provides HTTP client functionality for the Tailscale API.
type Client struct {
	httpClient  *http.Client
	oauthConfig *clientcredentials.Config
	baseURL     string
}

// NewClient creates a new Tailscale API client using OAuth credentials.
func NewClient(clientID, clientSecret, tailnet string) *Client {
	var httpClient *http.Client

	if clientID != "" && clientSecret != "" {
		config := &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     "https://api.tailscale.com/api/v2/oauth/token",
		}
		httpClient = config.Client(context.Background())
	} else {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
				MaxIdleConnsPerHost: 2,
			},
		}
	}

	return &Client{
		httpClient:  httpClient,
		oauthConfig: nil,
		baseURL:     fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s", tailnet),
	}
}

// NewClientWithToken creates a new Tailscale API client using a direct OAuth token.
func NewClientWithToken(token, tailnet string) *Client {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			MaxIdleConnsPerHost: 2,
		},
	}

	transport := httpClient.Transport
	httpClient.Transport = &tokenTransport{
		token:     token,
		transport: transport,
	}

	return &Client{
		httpClient:  httpClient,
		oauthConfig: nil,
		baseURL:     fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s", tailnet),
	}
}

type tokenTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(req)
}

func (c *Client) FetchDevices() ([]device.Device, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("no tailnet configured")
	}

	resp, err := c.makeDevicesRequest()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	result, err := c.decodeDevicesResponse(resp.Body)
	if err != nil {
		return nil, err
	}

	return c.convertAPIDevices(result.Devices), nil
}

func (c *Client) makeDevicesRequest() (*http.Response, error) {
	apiURL := fmt.Sprintf("%s/devices", c.baseURL)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}

func (c *Client) handleAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
}

func (c *Client) decodeDevicesResponse(body io.Reader) (*devicesAPIResponse, error) {
	var result devicesAPIResponse
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

func (c *Client) convertAPIDevices(apiDevices []apiDevice) []device.Device {
	devices := make([]device.Device, 0, len(apiDevices))
	for _, d := range apiDevices {
		if dev, ok := c.convertSingleDevice(d); ok {
			devices = append(devices, dev)
		}
	}
	return devices
}

func (c *Client) convertSingleDevice(d apiDevice) (device.Device, bool) {
	if d.ID == "" || d.Name == "" {
		slog.Warn("skipping device with missing ID or Name", "device", d)
		return device.Device{}, false
	}

	deviceID, err := types.NewDeviceID(d.ID)
	if err != nil {
		slog.Warn("skipping device with invalid ID", "device", d, "error", err)
		return device.Device{}, false
	}

	deviceName, err := types.NewDeviceName(d.Name)
	if err != nil {
		slog.Warn("skipping device with invalid name", "device", d, "error", err)
		return device.Device{}, false
	}

	host := d.Hostname
	if host == "" && len(d.Addresses) > 0 {
		host = d.Addresses[0]
	}

	lastSeen, expires := c.parseTimes(d.LastSeen, d.Expires, d.KeyExpiryDisabled)
	tags := c.parseTags(d.Tags, d.Name)

	return device.Device{
		ID:                deviceID,
		Name:              deviceName,
		Host:              host,
		Tags:              tags,
		Online:            d.Online,
		Authorized:        d.Authorized,
		LastSeen:          lastSeen,
		User:              d.User,
		MachineKey:        d.MachineKey,
		KeyExpiryDisabled: d.KeyExpiryDisabled,
		Expires:           expires,
		AdvertisedRoutes:  d.AdvertisedRoutes,
		EnabledRoutes:     d.EnabledRoutes,
		IsExitNode:        d.IsExitNode,
		ExitNodeOption:    d.ExitNodeOption,
		OS:                d.OS,
		ClientVersion:     d.ClientVersion,
	}, true
}

func (c *Client) parseTimes(lastSeenStr, expiresStr string, keyExpiryDisabled bool) (time.Time, time.Time) {
	var lastSeen, expires time.Time

	if lastSeenStr != "" {
		if t, err := time.Parse(time.RFC3339, lastSeenStr); err == nil {
			lastSeen = t
		}
	}

	if expiresStr != "" && !keyExpiryDisabled {
		if t, err := time.Parse(time.RFC3339, expiresStr); err == nil {
			expires = t
		}
	}

	return lastSeen, expires
}

func (c *Client) parseTags(tagsStr []string, deviceName string) []types.TagName {
	tags := make([]types.TagName, 0, len(tagsStr))
	for _, tag := range tagsStr {
		// Remove "tag:" prefix from Tailscale API tags
		cleanTag := tag
		if strings.HasPrefix(tag, "tag:") {
			cleanTag = strings.TrimPrefix(tag, "tag:")
		}

		// Skip empty tags after prefix removal
		if cleanTag == "" {
			slog.Warn("skipping empty tag", "tag", tag, "device", deviceName)
			continue
		}

		if tagName, err := types.NewTagName(cleanTag); err == nil {
			tags = append(tags, tagName)
		} else {
			slog.Warn("skipping invalid tag", "tag", tag, "device", deviceName, "error", err)
		}
	}
	return tags
}

type devicesAPIResponse struct {
	Devices []apiDevice `json:"devices"`
}

type apiDevice struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Hostname          string   `json:"hostname"`
	Addresses         []string `json:"addresses"`
	Online            bool     `json:"online"`
	Tags              []string `json:"tags"`
	Authorized        bool     `json:"authorized"`
	LastSeen          string   `json:"lastSeen"`
	User              string   `json:"user"`
	MachineKey        string   `json:"machineKey"`
	KeyExpiryDisabled bool     `json:"keyExpiryDisabled"`
	Expires           string   `json:"expires"`
	AdvertisedRoutes  []string `json:"advertisedRoutes"`
	EnabledRoutes     []string `json:"enabledRoutes"`
	IsExitNode        bool     `json:"isExitNode"`
	ExitNodeOption    bool     `json:"exitNodeOption"`
	OS                string   `json:"os"`
	ClientVersion     string   `json:"clientVersion"`
}

func (c *Client) TestConnectivity(ctx context.Context) (bool, error) {
	if c.baseURL == "" {
		return false, fmt.Errorf("no tailnet configured")
	}

	apiURL := fmt.Sprintf("%s/devices", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "HEAD", apiURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("connectivity test failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	if resp.StatusCode == 401 {
		return true, nil
	}

	return false, fmt.Errorf("API returned status %d", resp.StatusCode)
}
