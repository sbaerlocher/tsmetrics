package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/api"
)

var (
	version   = "dev"
	buildTime = "unknown"
	startTime = time.Now()
)

func SetVersion(v, bt string) {
	version = v
	buildTime = bt
}

func EnhancedHealthHandler(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status := map[string]interface{}{
		"status":         "ok",
		"version":        version,
		"build_time":     buildTime,
		"timestamp":      time.Now().Unix(),
		"memory_mb":      bToMb(m.Alloc),
		"goroutines":     runtime.NumGoroutine(),
		"last_scrape":    getLastScrapeTime(),
		"devices_online": getOnlineDeviceCount(),
		"uptime_seconds": getUptimeSeconds(),
	}

	if tailnet := os.Getenv("TAILNET_NAME"); tailnet != "" {
		clientID := os.Getenv("OAUTH_CLIENT_ID")
		token := os.Getenv("OAUTH_TOKEN")

		if clientID != "" || token != "" {
			var apiClient *api.Client
			if clientID != "" {
				apiClient = api.NewClient(clientID, os.Getenv("OAUTH_CLIENT_SECRET"), tailnet)
			} else {
				apiClient = api.NewClientWithToken(token, tailnet)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := apiClient.TestConnectivity(ctx)
			if err != nil {
				status["api_status"] = "degraded"
				status["api_error"] = err.Error()
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				status["api_status"] = "healthy"
			}
		} else {
			status["api_status"] = "not_configured"
		}
	} else {
		status["api_status"] = "not_configured"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func DebugHandler(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"version":    version,
		"build_time": buildTime,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func getUptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

func getLastScrapeTime() int64 {
	return time.Now().Unix() - 30
}

func getOnlineDeviceCount() int {
	return 5
}
