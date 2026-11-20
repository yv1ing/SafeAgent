// Package config loads the process configuration from a TOML file and applies defaults.
// A missing database URL is the only value without a default and fails at startup.
package config

import (
	"bytes"
	"errors"
	"net"
	"net/url"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Listen              string   `toml:"listen"`
	DatabaseURL         string   `toml:"database_url"`
	SecureCookies       bool     `toml:"secure_cookies"`
	PublicOrigin        string   `toml:"public_origin"`
	MaxRequestBytes     int64    `toml:"max_request_bytes"`
	MaxResponseBytes    int64    `toml:"max_response_bytes"`
	MaxStreamBytes      int64    `toml:"max_stream_bytes"`
	StreamIdleSeconds   int      `toml:"stream_idle_seconds"`
	TrustedProxies      []string `toml:"trusted_proxies"`
	MaxAttempts         int      `toml:"max_attempts"`
	ShutdownSeconds     int      `toml:"shutdown_seconds"`
	OperationLogDays    int      `toml:"operation_log_days"`
	ToolHoldBytes       int      `toml:"tool_hold_bytes"`
	AnalysisConcurrency int      `toml:"analysis_concurrency"`
}

func defaults() Config {
	return Config{
		Listen:              "127.0.0.1:8080",
		SecureCookies:       true,
		MaxRequestBytes:     8 << 20,
		MaxResponseBytes:    32 << 20,
		MaxStreamBytes:      64 << 20,
		StreamIdleSeconds:   60,
		MaxAttempts:         3,
		ShutdownSeconds:     30,
		OperationLogDays:    365,
		ToolHoldBytes:       1 << 20,
		AnalysisConcurrency: 2,
	}
}

func Load(path string) (Config, error) {
	configuration := defaults()
	payload, err := os.ReadFile(path)
	if err != nil {
		return configuration, err
	}
	if err = toml.NewDecoder(bytes.NewReader(payload)).DisallowUnknownFields().Decode(&configuration); err != nil {
		return configuration, err
	}
	if configuration.PublicOrigin != "" {
		origin, err := url.Parse(configuration.PublicOrigin)
		if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.Path != "" {
			return configuration, errors.New("public_origin must be an absolute HTTP or HTTPS URL")
		}
	}
	switch {
	case configuration.DatabaseURL == "":
		return configuration, errors.New("database_url is required")
	case configuration.MaxRequestBytes < 1024 || configuration.MaxRequestBytes > 64<<20:
		return configuration, errors.New("max_request_bytes must be between 1 KiB and 64 MiB")
	case configuration.MaxResponseBytes < 1024 || configuration.MaxResponseBytes > 128<<20:
		return configuration, errors.New("max_response_bytes must be between 1 KiB and 128 MiB")
	case configuration.MaxStreamBytes < 1024 || configuration.MaxStreamBytes > 128<<20:
		return configuration, errors.New("max_stream_bytes must be between 1 KiB and 128 MiB")
	case configuration.StreamIdleSeconds < 1 || configuration.StreamIdleSeconds > 600:
		return configuration, errors.New("stream_idle_seconds must be between 1 and 600")
	case configuration.MaxAttempts < 1 || configuration.MaxAttempts > 10:
		return configuration, errors.New("max_attempts must be between 1 and 10")
	case configuration.ShutdownSeconds < 1 || configuration.ShutdownSeconds > 600:
		return configuration, errors.New("shutdown_seconds must be between 1 and 600")
	case configuration.OperationLogDays < 1 || configuration.OperationLogDays > 3650:
		return configuration, errors.New("operation_log_days must be between 1 and 3650")
	case configuration.ToolHoldBytes < 4<<10 || configuration.ToolHoldBytes > 64<<20:
		return configuration, errors.New("tool_hold_bytes must be between 4 KiB and 64 MiB")
	case configuration.AnalysisConcurrency < 1 || configuration.AnalysisConcurrency > 16:
		return configuration, errors.New("analysis_concurrency must be between 1 and 16")
	}
	for _, proxy := range configuration.TrustedProxies {
		if net.ParseIP(proxy) == nil {
			if _, _, err := net.ParseCIDR(proxy); err != nil {
				return configuration, errors.New("trusted_proxies entries must be IP addresses or CIDR ranges")
			}
		}
	}
	return configuration, nil
}
