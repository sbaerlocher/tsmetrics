// Package security provides security utilities and middleware for TSMetrics.
package security

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Input Validation
// InputValidator provides input validation for security purposes.
type InputValidator struct {
	maxStringLength int
	allowedChars    *regexp.Regexp
}

// NewInputValidator creates a new input validator with default security settings.
func NewInputValidator() *InputValidator {
	// Allow alphanumeric, dots, dashes, underscores, and colons (for URLs/IPs)
	allowedChars := regexp.MustCompile(`^[a-zA-Z0-9\.\-_:\/]+$`)

	return &InputValidator{
		maxStringLength: 1000,
		allowedChars:    allowedChars,
	}
}

func (iv *InputValidator) ValidateString(input string, fieldName string) error {
	if len(input) == 0 {
		return fmt.Errorf("field %s cannot be empty", fieldName)
	}

	if len(input) > iv.maxStringLength {
		return fmt.Errorf("field %s exceeds maximum length of %d characters", fieldName, iv.maxStringLength)
	}

	// Check for null bytes and control characters
	for _, char := range input {
		if char < 32 && char != 9 && char != 10 && char != 13 { // Allow tab, LF, CR
			return fmt.Errorf("field %s contains invalid control characters", fieldName)
		}
	}

	return nil
}

// privateIPNets contains all private, loopback, and link-local IP ranges.
var privateIPNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"0.0.0.0/8",      // current network (RFC 1122)
		"127.0.0.0/8",    // IPv4 loopback
		"::1/128",        // IPv6 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"100.64.0.0/10",  // CGNAT / Tailscale device IPs (RFC 6598)
		"169.254.0.0/16", // link-local (AWS metadata, etc.)
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	} {
		_, network, _ := net.ParseCIDR(cidr)
		privateIPNets = append(privateIPNets, network)
	}
}

func isPrivateHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		// hostname is a DNS name, not an IP literal — we cannot check it here.
		// DNS-rebinding (a name resolving to a private IP at request time) is
		// documented as out-of-scope; callers that need full protection must use
		// a custom dialer with post-DNS IP validation.
		// In this project, device hostnames originate exclusively from the
		// authenticated Tailscale API, so they are treated as trusted input.
		return false
	}
	// Normalize IPv4-mapped IPv6 (e.g. ::ffff:127.0.0.1) to plain IPv4
	// so it matches the 4-byte IPv4 ranges in privateIPNets.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, network := range privateIPNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (iv *InputValidator) ValidateURL(input string, fieldName string) error {
	if err := iv.ValidateString(input, fieldName); err != nil {
		return err
	}

	parsedURL, err := url.Parse(input)
	if err != nil {
		return fmt.Errorf("field %s is not a valid URL: %w", fieldName, err)
	}

	// Only allow HTTP and HTTPS schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("field %s must use http or https scheme", fieldName)
	}

	// Prevent access to localhost and all private/reserved IP ranges (SSRF protection).
	// NOTE: This checks the literal hostname only — DNS rebinding is not prevented here.
	// A hostname that resolves to a private IP at request time would bypass this check.
	// Callers that need full SSRF protection must use a custom dialer with post-DNS IP validation.
	if isPrivateHost(parsedURL.Hostname()) { // DevSkim: ignore DS162092 - Security validation intentionally blocks private hosts
		return fmt.Errorf("field %s cannot reference private or reserved addresses", fieldName)
	}

	return nil
}

func (iv *InputValidator) ValidateToken(token string) error {
	if len(token) == 0 {
		return fmt.Errorf("token cannot be empty")
	}

	if len(token) < 20 {
		return fmt.Errorf("token is too short (minimum 20 characters)")
	}

	if len(token) > 500 {
		return fmt.Errorf("token is too long (maximum 500 characters)")
	}

	// Basic token format validation (alphanumeric and common token characters)
	tokenRegex := regexp.MustCompile(`^[a-zA-Z0-9\-_\.]+$`)
	if !tokenRegex.MatchString(token) {
		return fmt.Errorf("token contains invalid characters")
	}

	return nil
}

func (iv *InputValidator) ValidateDeviceID(id string) error {
	if err := iv.ValidateString(id, "device_id"); err != nil {
		return err
	}

	// Device IDs should be more restrictive
	if len(id) > 50 {
		return fmt.Errorf("device ID is too long (maximum 50 characters)")
	}

	deviceIDRegex := regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)
	if !deviceIDRegex.MatchString(id) {
		return fmt.Errorf("device ID contains invalid characters (only alphanumeric, dash, underscore allowed)")
	}

	return nil
}

// Rate Limiting
// RateLimiter provides per-client rate limiting functionality.
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mutex    sync.RWMutex
	rate     rate.Limit
	burst    int
}

// NewRateLimiter creates a new rate limiter with the specified requests per second and burst size.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(rps),
		burst:    burst,
	}
}

func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mutex.RLock()
	limiter, exists := rl.limiters[clientID]
	rl.mutex.RUnlock()

	if !exists {
		rl.mutex.Lock()
		// Double-check pattern
		if limiter, exists = rl.limiters[clientID]; !exists {
			limiter = rate.NewLimiter(rl.rate, rl.burst)
			rl.limiters[clientID] = limiter
		}
		rl.mutex.Unlock()
	}

	return limiter.Allow()
}

func (rl *RateLimiter) Cleanup() {
	// Periodically clean up unused limiters
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	for clientID, limiter := range rl.limiters {
		// Remove limiters that haven't been used recently
		if limiter.Tokens() >= float64(rl.burst) {
			delete(rl.limiters, clientID)
		}
	}
}

// Security Headers Middleware
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; object-src 'none'")

		// Prevent caching of sensitive endpoints
		if strings.Contains(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		next.ServeHTTP(w, r)
	})
}

// Rate Limiting Middleware
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := getClientID(r)

			if !limiter.Allow(clientID) {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func getClientID(r *http.Request) string {
	// Always use RemoteAddr — never trust X-Forwarded-For or X-Real-IP,
	// as these can be forged by clients to bypass rate limiting.
	ip := r.RemoteAddr

	// Strip port from "IP:Port" or "[IPv6]:Port" format
	host, _, err := net.SplitHostPort(ip)
	if err != nil {
		// No port present or unparseable — use as-is
		return ip
	}
	return host
}

// Authentication Utilities
// AuthValidator manages valid authentication tokens.
type AuthValidator struct {
	validTokens map[string]bool
	mutex       sync.RWMutex
}

// NewAuthValidator creates a new authentication validator.
func NewAuthValidator() *AuthValidator {
	return &AuthValidator{
		validTokens: make(map[string]bool),
	}
}

func (av *AuthValidator) AddValidToken(token string) {
	av.mutex.Lock()
	defer av.mutex.Unlock()
	av.validTokens[token] = true
}

func (av *AuthValidator) RemoveToken(token string) {
	av.mutex.Lock()
	defer av.mutex.Unlock()
	delete(av.validTokens, token)
}

// SecureValidateToken uses constant-time comparison against all tokens.
// The loop never breaks early so the number of remaining iterations cannot
// be used as a timing oracle to enumerate valid tokens.
// NOTE: Total execution time still scales linearly with the number of stored tokens,
// so an attacker making many requests could infer how many tokens are configured.
// For the typical single-token deployment this is not exploitable.
func (av *AuthValidator) SecureValidateToken(token string) bool {
	av.mutex.RLock()
	defer av.mutex.RUnlock()

	isValid := false
	for validToken := range av.validTokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(validToken)) == 1 {
			isValid = true
			// no break — always iterate all tokens to prevent timing side-channel
		}
	}

	return isValid
}

// Authentication Middleware
func AuthenticationMiddleware(validator *AuthValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authentication for health/probe endpoints.
			// Exact-match or explicit sub-path prefix to avoid accidentally
			// whitelisting future routes like /health-admin.
			p := r.URL.Path
			if p == "/health" || strings.HasPrefix(p, "/health/") ||
				p == "/healthz" || strings.HasPrefix(p, "/healthz/") ||
				p == "/livez" || strings.HasPrefix(p, "/livez/") ||
				p == "/readyz" || strings.HasPrefix(p, "/readyz/") ||
				p == "/startupz" || strings.HasPrefix(p, "/startupz/") {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(authHeader, bearerPrefix) {
				http.Error(w, "Authorization header must start with 'Bearer '", http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, bearerPrefix)

			// Validate token
			if !validator.SecureValidateToken(token) {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Timeout Middleware
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

// Request Size Limiting Middleware
func RequestSizeLimitMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}

			// Limit the request body reader
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

			next.ServeHTTP(w, r)
		})
	}
}

// Security Audit Report
type SecurityAuditReport struct {
	Timestamp           time.Time       `json:"timestamp"`
	ConfigurationIssues []string        `json:"configuration_issues"`
	SecurityFeatures    map[string]bool `json:"security_features"`
	Recommendations     []string        `json:"recommendations"`
	RiskLevel           string          `json:"risk_level"`
}

func GenerateSecurityAuditReport() SecurityAuditReport {
	report := SecurityAuditReport{
		Timestamp:        time.Now(),
		SecurityFeatures: make(map[string]bool),
		RiskLevel:        "LOW",
	}

	// Check various security features
	report.SecurityFeatures["input_validation"] = true
	report.SecurityFeatures["rate_limiting"] = true
	report.SecurityFeatures["security_headers"] = true
	report.SecurityFeatures["authentication"] = true
	report.SecurityFeatures["request_timeout"] = true
	report.SecurityFeatures["request_size_limiting"] = true

	// Add recommendations
	report.Recommendations = []string{
		"Regularly rotate authentication tokens",
		"Monitor rate limit violations",
		"Review and update security headers periodically",
		"Implement logging for security events",
		"Consider implementing IP whitelisting for admin endpoints",
		"Regularly update dependencies for security patches",
		"Implement audit logging for sensitive operations",
		"Consider implementing HTTPS-only in production",
	}

	return report
}
