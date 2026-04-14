package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               string
	Host               string
	LogLevel           string
	APIKey             string
	BrowserTimeout     time.Duration
	SessionTimeout     time.Duration
	MaxConcurrentPages int
	RateLimitWindow    time.Duration
	RateLimitMax       int
	ScreenshotQuality  int
	ScreenshotWidth    int
	ScreenshotHeight   int
	CorsOrigin         string
	ChromiumPath       string
	Headless           bool
	NoSandbox          bool
	DisableAuth        bool
}

func Load() (*Config, error) {
	c := &Config{
		Port:               getEnv("PORT", "3000"),
		Host:               getEnv("HOST", "0.0.0.0"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		APIKey:             getEnv("API_KEY", "test-api-key-12345"),
		BrowserTimeout:     getDurationEnv("BROWSER_TIMEOUT", 30*time.Second),
		SessionTimeout:     getDurationEnv("SESSION_TIMEOUT", 30*time.Minute),
		MaxConcurrentPages: getIntEnv("MAX_CONCURRENT_PAGES", 10),
		RateLimitWindow:    getDurationEnv("RATE_LIMIT_WINDOW", 15*time.Minute),
		RateLimitMax:       getIntEnv("RATE_LIMIT_MAX", 100),
		ScreenshotQuality:  getIntEnv("SCREENSHOT_QUALITY", 80),
		ScreenshotWidth:    getIntEnv("SCREENSHOT_DEFAULT_WIDTH", 1280),
		ScreenshotHeight:   getIntEnv("SCREENSHOT_DEFAULT_HEIGHT", 720),
		CorsOrigin:         getEnv("CORS_ORIGIN", "*"),
		ChromiumPath:       getEnv("CHROMIUM_PATH", ""),
		Headless:           getBoolEnv("HEADLESS", true),
		NoSandbox:          getBoolEnv("NO_SANDBOX", true),
		DisableAuth:        getBoolEnv("DISABLE_AUTH", false),
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("API_KEY is required")
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("PORT must be a valid port number (1-65535)")
	}
	if c.ScreenshotQuality < 1 || c.ScreenshotQuality > 100 {
		return fmt.Errorf("SCREENSHOT_QUALITY must be between 1 and 100")
	}
	if c.ScreenshotWidth < 100 || c.ScreenshotWidth > 3840 {
		return fmt.Errorf("SCREENSHOT_DEFAULT_WIDTH must be between 100 and 3840")
	}
	if c.ScreenshotHeight < 100 || c.ScreenshotHeight > 2160 {
		return fmt.Errorf("SCREENSHOT_DEFAULT_HEIGHT must be between 100 and 2160")
	}
	if c.MaxConcurrentPages < 1 {
		return fmt.Errorf("MAX_CONCURRENT_PAGES must be at least 1")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return strings.ToLower(v) == "true" || v == "1"
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		if ms, err := strconv.Atoi(v); err == nil {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return fallback
}
