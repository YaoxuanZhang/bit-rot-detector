package config_test

import (
	"os"
	"testing"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/config"
)

// setenv sets multiple env vars and returns a cleanup function that restores
// the previous values.
func setenv(t *testing.T, pairs map[string]string) {
	t.Helper()
	prev := make(map[string]string, len(pairs))
	for k, v := range pairs {
		prev[k] = os.Getenv(k)
		os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k, pv := range prev {
			if pv == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, pv)
			}
		}
	})
}

// clearAll clears every config-related env var so each test starts clean.
func clearAll(t *testing.T) {
	t.Helper()
	keys := []string{
		"TARGET_DIRECTORY", "SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME",
		"SMTP_PASSWORD", "SMTP_SENDER", "SMTP_RECIPIENT", "NOTIFY_ON_SUCCESS",
		"SCRUB_PERCENTAGE", "SCRUB_FREQUENCY", "MAX_WORKERS", "LOG_LEVEL",
		"LOG_RETENTION_DAYS",
	}
	for _, k := range keys {
		prev := os.Getenv(k)
		os.Unsetenv(k)
		kCopy := k
		prevCopy := prev
		t.Cleanup(func() {
			if prevCopy != "" {
				os.Setenv(kCopy, prevCopy)
			}
		})
	}
}

func TestLoad_MissingTargetDirectory(t *testing.T) {
	clearAll(t)
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when TARGET_DIRECTORY is not set")
	}
}

func TestLoad_NonExistentTargetDirectory(t *testing.T) {
	clearAll(t)
	setenv(t, map[string]string{"TARGET_DIRECTORY": "/nonexistent/path/xyz"})
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for nonexistent target directory")
	}
}

func TestLoad_ValidDefaults(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{"TARGET_DIRECTORY": dir})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.TargetPaths) != 1 || cfg.TargetPaths[0] != dir {
		t.Errorf("unexpected target paths: %v", cfg.TargetPaths)
	}
	if cfg.ScrubPercentage != 1.0 {
		t.Errorf("expected scrub_pct=1.0, got %v", cfg.ScrubPercentage)
	}
	if cfg.ScrubFrequency != "daily" {
		t.Errorf("expected scrub_freq=daily, got %q", cfg.ScrubFrequency)
	}
	if cfg.MaxWorkers != 4 {
		t.Errorf("expected max_workers=4, got %d", cfg.MaxWorkers)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("expected log_level=INFO, got %q", cfg.LogLevel)
	}
	if cfg.LogRetentionDays != 7 {
		t.Errorf("expected log_retention_days=7, got %d", cfg.LogRetentionDays)
	}
	if cfg.Email.Host != "mail.smtp2go.com" {
		t.Errorf("expected default SMTP host, got %q", cfg.Email.Host)
	}
	if !cfg.Email.NotifyOnSuccess {
		t.Error("expected notify_on_success=true by default")
	}
}

func TestLoad_MultipleTargetPaths(t *testing.T) {
	clearAll(t)
	dir1, dir2 := t.TempDir(), t.TempDir()
	setenv(t, map[string]string{"TARGET_DIRECTORY": dir1 + "," + dir2})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TargetPaths) != 2 {
		t.Errorf("expected 2 target paths, got %d", len(cfg.TargetPaths))
	}
}

func TestLoad_DuplicateTargetPaths(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{"TARGET_DIRECTORY": dir + "," + dir})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TargetPaths) != 1 {
		t.Errorf("expected duplicates to be deduplicated, got %d paths", len(cfg.TargetPaths))
	}
}

func TestLoad_InvalidScrubPercentage(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{
		"TARGET_DIRECTORY": dir,
		"SCRUB_PERCENTAGE": "0.0",
	})
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for scrub_percentage=0.0")
	}
}

func TestLoad_InvalidScrubFrequency(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{
		"TARGET_DIRECTORY": dir,
		"SCRUB_FREQUENCY":  "hourly",
	})
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid scrub_frequency")
	}
}

func TestLoad_InvalidMaxWorkers(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{
		"TARGET_DIRECTORY": dir,
		"MAX_WORKERS":      "0",
	})
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for max_workers=0")
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{
		"TARGET_DIRECTORY": dir,
		"LOG_LEVEL":        "VERBOSE",
	})
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid log_level")
	}
}

func TestLoad_AllFrequencies(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	for _, freq := range []string{"daily", "weekly", "monthly"} {
		setenv(t, map[string]string{
			"TARGET_DIRECTORY": dir,
			"SCRUB_FREQUENCY":  freq,
		})
		cfg, err := config.Load()
		if err != nil {
			t.Errorf("freq=%q: unexpected error: %v", freq, err)
			continue
		}
		if cfg.ScrubFrequency != freq {
			t.Errorf("freq=%q: got %q", freq, cfg.ScrubFrequency)
		}
	}
}

func TestLoad_NotifyOnSuccessFalse(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{
		"TARGET_DIRECTORY":  dir,
		"NOTIFY_ON_SUCCESS": "false",
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Email.NotifyOnSuccess {
		t.Error("expected notify_on_success=false")
	}
}

func TestLoad_CustomSMTPSettings(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()
	setenv(t, map[string]string{
		"TARGET_DIRECTORY": dir,
		"SMTP_HOST":        "smtp.example.com",
		"SMTP_PORT":        "465",
		"SMTP_USERNAME":    "user",
		"SMTP_PASSWORD":    "pass",
		"SMTP_SENDER":      "from@example.com",
		"SMTP_RECIPIENT":   "to@example.com",
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Email.Host != "smtp.example.com" {
		t.Errorf("expected smtp.example.com, got %q", cfg.Email.Host)
	}
	if cfg.Email.Port != 465 {
		t.Errorf("expected port 465, got %d", cfg.Email.Port)
	}
	if cfg.Email.Sender != "from@example.com" {
		t.Errorf("expected sender from@example.com, got %q", cfg.Email.Sender)
	}
}

func TestLoad_ScrubPercentageBoundary(t *testing.T) {
	clearAll(t)
	dir := t.TempDir()

	// 100.0 should be valid.
	setenv(t, map[string]string{
		"TARGET_DIRECTORY": dir,
		"SCRUB_PERCENTAGE": "100.0",
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("scrub_pct=100.0: unexpected error: %v", err)
	}
	if cfg.ScrubPercentage != 100.0 {
		t.Errorf("expected 100.0, got %v", cfg.ScrubPercentage)
	}

	// 101.0 should be invalid.
	setenv(t, map[string]string{"SCRUB_PERCENTAGE": "101.0"})
	_, err = config.Load()
	if err == nil {
		t.Fatal("expected error for scrub_percentage=101.0")
	}
}
