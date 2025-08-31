package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"golang.org/x/oauth2/clientcredentials"
)

type APIClient struct {
	httpClient  *http.Client
	oauthConfig *clientcredentials.Config
	baseURL     string
}

func NewAPIClient(clientID, clientSecret, tailnet string) *APIClient {
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

	return &APIClient{
		httpClient:  httpClient,
		oauthConfig: nil,
		baseURL:     fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s", tailnet),
	}
}

func NewAPIClientWithToken(token, tailnet string) *APIClient {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			MaxIdleConnsPerHost: 2,
		},
	}

	// Wrap client to add Bearer token
	transport := httpClient.Transport
	httpClient.Transport = &tokenTransport{
		token:     token,
		transport: transport,
	}

	return &APIClient{
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

func (c *APIClient) fetchDevicesFromAPI() ([]Device, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("no tailnet configured")
	}

	apiURL := fmt.Sprintf("%s/devices", c.baseURL)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Devices []struct {
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
		} `json:"devices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	devices := make([]Device, 0, len(result.Devices))
	for _, d := range result.Devices {
		host := d.Hostname
		if host == "" && len(d.Addresses) > 0 {
			host = d.Addresses[0]
		}
		// Input validation
		if d.ID == "" || d.Name == "" {
			log.Printf("skipping device with missing ID or Name: %+v", d)
			continue
		}

		// Parse timestamps safely
		var lastSeen, expires time.Time
		if d.LastSeen != "" {
			if t, err := time.Parse(time.RFC3339, d.LastSeen); err == nil {
				lastSeen = t
			}
		}
		if d.Expires != "" && !d.KeyExpiryDisabled {
			if t, err := time.Parse(time.RFC3339, d.Expires); err == nil {
				expires = t
			}
		}

		devices = append(devices, Device{
			ID:                d.ID,
			Name:              d.Name,
			Host:              host,
			Tags:              d.Tags,
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
		})
	}

	return devices, nil
}

// testConnectivity performs a quick API connectivity test
func (c *APIClient) testConnectivity(ctx context.Context) (bool, error) {
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

	// Accept any 2xx or 401 (auth issue but API is reachable)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	if resp.StatusCode == 401 {
		return true, nil // API is reachable, just auth issue
	}

	return false, fmt.Errorf("API returned status %d", resp.StatusCode)
}
