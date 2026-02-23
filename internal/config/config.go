// Package config loads application configuration from environment variables.
//
// Configuration is read exclusively from the process environment.  When the
// binary is started, the caller (cmd/bit-rot-detector) first loads a .env
// file via [github.com/joho/godotenv] so that the variables are already
// present in the environment before [Load] is called.  Environment variables
// always take precedence over values in .env.
//
// See .env.example in the repository root for a documented list of all
// supported variables and their default values.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
)

// Config holds all application settings resolved at startup.
type Config struct {
	// TargetPaths is the deduplicated list of directory paths to monitor.
	// Populated from TARGET_DIRECTORY (comma-separated).
	TargetPaths []string

	// Email contains the SMTP connection and notification settings.
	Email mailer.Config

	// ScrubPercentage is the fraction of files to re-verify per run (0.1–100.0).
	ScrubPercentage float64

	// ScrubFrequency controls the minimum age filter applied when selecting
	// files for scrubbing: "daily" (no age filter), "weekly" (≥7 days since
	// last scrub), or "monthly" (≥30 days).
	ScrubFrequency string

	// MaxWorkers is the upper bound on the number of concurrent hashing
	// goroutines.  The IO-aware monitor may further reduce this per drive.
	MaxWorkers int

	// LogLevel controls the minimum severity for console log output.
	// Valid values: DEBUG, INFO, WARN, ERROR.
	LogLevel string

	// LogRetentionDays is the number of days to retain old log files.
	LogRetentionDays int
}

// Load reads and validates configuration from environment variables.
// It returns a populated [Config] or an error describing the first
// validation failure encountered.
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

	smtpPortStr := envOrDefault("SMTP_PORT", "587")
	smtpPort, err := strconv.Atoi(smtpPortStr)
	if err != nil || smtpPort < 1 || smtpPort > 65535 {
		return nil, fmt.Errorf("SMTP_PORT must be a valid port number (1-65535), got %q", smtpPortStr)
	}
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
