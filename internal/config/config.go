// Package config loads application configuration from environment variables
// (optionally via a .env file loaded by the caller).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
)

// Config holds all application settings.
type Config struct {
	TargetPaths     []string
	Email           mailer.Config
	ScrubPercentage float64
	ScrubFrequency  string
	MaxWorkers      int
	LogLevel        string
	LogRetentionDays int
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	targetDirStr := os.Getenv("TARGET_DIRECTORY")
	if targetDirStr == "" {
		return nil, fmt.Errorf("TARGET_DIRECTORY not set")
	}

	rawPaths := strings.Split(targetDirStr, ",")
	seen := make(map[string]bool)
	var targetPaths []string
	for _, raw := range rawPaths {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("target directory does not exist: %s", p)
		}
		if !seen[p] {
			seen[p] = true
			targetPaths = append(targetPaths, p)
		}
	}
	if len(targetPaths) == 0 {
		return nil, fmt.Errorf("TARGET_DIRECTORY contains no valid paths")
	}

	smtpPort, _ := strconv.Atoi(envOrDefault("SMTP_PORT", "587"))
	notifyOnSuccess := strings.ToLower(envOrDefault("NOTIFY_ON_SUCCESS", "true")) == "true"

	scrubPct, err := strconv.ParseFloat(envOrDefault("SCRUB_PERCENTAGE", "1.0"), 64)
	if err != nil || scrubPct < 0.1 || scrubPct > 100.0 {
		return nil, fmt.Errorf("SCRUB_PERCENTAGE must be 0.1–100.0, got %q", os.Getenv("SCRUB_PERCENTAGE"))
	}

	scrubFreq := strings.ToLower(envOrDefault("SCRUB_FREQUENCY", "daily"))
	switch scrubFreq {
	case "daily", "weekly", "monthly":
	default:
		return nil, fmt.Errorf("SCRUB_FREQUENCY must be daily/weekly/monthly, got %q", scrubFreq)
	}

	maxWorkers, err := strconv.Atoi(envOrDefault("MAX_WORKERS", "4"))
	if err != nil || maxWorkers < 1 {
		return nil, fmt.Errorf("MAX_WORKERS must be >= 1, got %q", os.Getenv("MAX_WORKERS"))
	}

	logLevel := strings.ToUpper(envOrDefault("LOG_LEVEL", "INFO"))
	switch logLevel {
	case "DEBUG", "INFO", "WARN", "WARNING", "ERROR":
	default:
		return nil, fmt.Errorf("LOG_LEVEL must be DEBUG/INFO/WARN/ERROR, got %q", logLevel)
	}

	logRetention, _ := strconv.Atoi(envOrDefault("LOG_RETENTION_DAYS", "7"))

	return &Config{
		TargetPaths: targetPaths,
		Email: mailer.Config{
			Host:            envOrDefault("SMTP_HOST", "mail.smtp2go.com"),
			Port:            smtpPort,
			Username:        os.Getenv("SMTP_USERNAME"),
			Password:        os.Getenv("SMTP_PASSWORD"),
			Sender:          os.Getenv("SMTP_SENDER"),
			Recipient:       os.Getenv("SMTP_RECIPIENT"),
			NotifyOnSuccess: notifyOnSuccess,
		},
		ScrubPercentage:  scrubPct,
		ScrubFrequency:   scrubFreq,
		MaxWorkers:       maxWorkers,
		LogLevel:         logLevel,
		LogRetentionDays: logRetention,
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
