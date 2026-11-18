// Package config resolves CLI configuration from flags, environment and defaults.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// EnvAPIURL overrides the API base URL.
const EnvAPIURL = "DEVCRAFT_API_URL"

// DefaultAPIBaseURL is a PLACEHOLDER until the backend is deployed. It can be
// overridden at build time with -ldflags "-X .../internal/config.DefaultAPIBaseURL=https://api.example.com".
var DefaultAPIBaseURL = "http://localhost:8080"

// Config is the resolved CLI configuration.
type Config struct {
	APIBaseURL string
}

// Load resolves configuration. Precedence: flag, then environment, then default.
func Load(flagAPIURL string) (*Config, error) {
	apiURL := DefaultAPIBaseURL
	if v := strings.TrimSpace(os.Getenv(EnvAPIURL)); v != "" {
		apiURL = v
	}
	if v := strings.TrimSpace(flagAPIURL); v != "" {
		apiURL = v
	}
	if err := validateAPIURL(apiURL); err != nil {
		return nil, err
	}
	return &Config{APIBaseURL: strings.TrimRight(apiURL, "/")}, nil
}

// validateAPIURL requires https, except for loopback hosts used in development.
func validateAPIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid API URL %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("API URL %q must use https", raw)
	default:
		return fmt.Errorf("invalid API URL %q: scheme must be https", raw)
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
