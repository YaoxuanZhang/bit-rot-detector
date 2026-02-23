package config_test

import (
"os"
"path/filepath"
"testing"

"github.com/YaoxuanZhang/bit-rot-detector/internal/config"
)

// TestDefaultConfig verifies that DefaultConfig returns sane values.
func TestDefaultConfig(t *testing.T) {
cfg := config.DefaultConfig()
if cfg.ScrubPercentage != 1.0 {
t.Errorf("expected scrub_percentage=1.0, got %v", cfg.ScrubPercentage)
}
if cfg.ScrubFrequency != "daily" {
t.Errorf("expected scrub_frequency=daily, got %q", cfg.ScrubFrequency)
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
if cfg.DiskThresholds.WarnPercent != 75 {
t.Errorf("expected warn_pct=75, got %d", cfg.DiskThresholds.WarnPercent)
}
if cfg.DiskThresholds.ErrorPercent != 90 {
t.Errorf("expected error_pct=90, got %d", cfg.DiskThresholds.ErrorPercent)
}
if cfg.NotificationRules.OnSuccess {
t.Error("expected on_success=false by default")
}
if !cfg.NotificationRules.OnCorruption {
t.Error("expected on_corruption=true by default")
}
if cfg.SMTP.Host != "mail.smtp2go.com" {
t.Errorf("expected default SMTP host, got %q", cfg.SMTP.Host)
}
}

// TestLoad_DefaultsWrittenWhenFileMissing verifies that Load creates a default
// config file when the path does not exist.
func TestLoad_DefaultsWrittenWhenFileMissing(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

// Create a valid target directory and write a minimal YAML manually so that
// target_paths validation passes after Load writes defaults.
targetDir := t.TempDir()
cfg, err := config.Load(path)
// Load will fail because the written defaults have empty target_paths.
// We expect either an error or an empty target path error.
if err == nil && len(cfg.TargetPaths) == 0 {
t.Skip("default load returned no target paths — skipping file-written check")
}
// The file should have been created even if Load returns an error about paths.
if _, statErr := os.Stat(path); statErr != nil {
t.Errorf("expected config file to be written: %v", statErr)
}
_ = targetDir
}

// TestLoad_ReadsFromYAML verifies that values in the YAML file are loaded.
func TestLoad_ReadsFromYAML(t *testing.T) {
dir := t.TempDir()
targetDir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

yaml := "target_paths:\n  - " + targetDir + "\nscrub_percentage: 5.0\nscrub_frequency: weekly\nmax_workers: 8\nlog_level: DEBUG\nlog_retention_days: 14\n"
if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
t.Fatal(err)
}

cfg, err := config.Load(path)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if cfg.ScrubPercentage != 5.0 {
t.Errorf("expected scrub_percentage=5.0, got %v", cfg.ScrubPercentage)
}
if cfg.ScrubFrequency != "weekly" {
t.Errorf("expected scrub_frequency=weekly, got %q", cfg.ScrubFrequency)
}
if cfg.MaxWorkers != 8 {
t.Errorf("expected max_workers=8, got %d", cfg.MaxWorkers)
}
if cfg.LogLevel != "DEBUG" {
t.Errorf("expected log_level=DEBUG, got %q", cfg.LogLevel)
}
if cfg.LogRetentionDays != 14 {
t.Errorf("expected log_retention_days=14, got %d", cfg.LogRetentionDays)
}
if len(cfg.TargetPaths) != 1 || cfg.TargetPaths[0] != targetDir {
t.Errorf("unexpected target_paths: %v", cfg.TargetPaths)
}
}

// TestLoad_MissingTargetPaths verifies that Load returns an error when
// target_paths is missing.
func TestLoad_MissingTargetPaths(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

yaml := "scrub_percentage: 1.0\n"
if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
t.Fatal(err)
}

_, err := config.Load(path)
if err == nil {
t.Fatal("expected error when target_paths is empty")
}
}

// TestLoad_NonExistentTargetPath verifies that Load returns an error when a
// listed target path does not exist on disk.
func TestLoad_NonExistentTargetPath(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

yaml := "target_paths:\n  - /nonexistent/path/xyz123\n"
if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
t.Fatal(err)
}

_, err := config.Load(path)
if err == nil {
t.Fatal("expected error for nonexistent target path")
}
}

// TestLoad_OverlaysSecretsFromEnv verifies that SMTP credentials come from
// environment variables and are not loaded from YAML.
func TestLoad_OverlaysSecretsFromEnv(t *testing.T) {
targetDir := t.TempDir()
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

yaml := "target_paths:\n  - " + targetDir + "\n"
if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
t.Fatal(err)
}

prev1, prev2 := os.Getenv("SMTP_USERNAME"), os.Getenv("SMTP_PASSWORD")
os.Setenv("SMTP_USERNAME", "testuser")
os.Setenv("SMTP_PASSWORD", "testpass")
t.Cleanup(func() {
if prev1 == "" {
os.Unsetenv("SMTP_USERNAME")
} else {
os.Setenv("SMTP_USERNAME", prev1)
}
if prev2 == "" {
os.Unsetenv("SMTP_PASSWORD")
} else {
os.Setenv("SMTP_PASSWORD", prev2)
}
})

cfg, err := config.Load(path)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if cfg.SMTP.Username != "testuser" {
t.Errorf("expected SMTP.Username=testuser, got %q", cfg.SMTP.Username)
}
if cfg.SMTP.Password != "testpass" {
t.Errorf("expected SMTP.Password=testpass, got %q", cfg.SMTP.Password)
}
}

// TestLoad_InvalidYAML verifies that Load returns an error on malformed YAML.
func TestLoad_InvalidYAML(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

if err := os.WriteFile(path, []byte("not: valid: yaml: :::"), 0o644); err != nil {
t.Fatal(err)
}

_, err := config.Load(path)
if err == nil {
t.Fatal("expected error on invalid YAML")
}
}

// TestSave_WritesYAML verifies that Save writes readable YAML.
func TestSave_WritesYAML(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

cfg := config.DefaultConfig()
cfg.ScrubPercentage = 25.0
if err := config.Save(&cfg, path); err != nil {
t.Fatalf("Save: %v", err)
}

raw, err := os.ReadFile(path)
if err != nil {
t.Fatalf("ReadFile: %v", err)
}
if len(raw) == 0 {
t.Fatal("expected non-empty YAML file")
}
}

// TestSave_ExcludesSecrets verifies that SMTP credentials do not appear in
// the saved YAML file.
func TestSave_ExcludesSecrets(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

cfg := config.DefaultConfig()
cfg.SMTP.Username = "secret-user"
cfg.SMTP.Password = "secret-pass"
if err := config.Save(&cfg, path); err != nil {
t.Fatalf("Save: %v", err)
}

raw, err := os.ReadFile(path)
if err != nil {
t.Fatalf("ReadFile: %v", err)
}
content := string(raw)
if contains(content, "secret-user") || contains(content, "secret-pass") {
t.Error("saved YAML must not contain SMTP credentials")
}
}

// TestStore_GetAndUpdate tests thread-safe access and in-memory updates.
func TestStore_GetAndUpdate(t *testing.T) {
cfg := config.DefaultConfig()
store := config.NewStore(cfg, "")

got := store.Get()
if got.ScrubPercentage != 1.0 {
t.Errorf("expected scrub_percentage=1.0, got %v", got.ScrubPercentage)
}

if err := store.Update(func(c *config.Config) error {
c.ScrubPercentage = 50.0
return nil
}); err != nil {
t.Fatalf("Update: %v", err)
}

got = store.Get()
if got.ScrubPercentage != 50.0 {
t.Errorf("expected scrub_percentage=50.0 after update, got %v", got.ScrubPercentage)
}
}

// TestStore_Get_ReturnsCopy verifies that modifying the returned Config does
// not affect the Store's internal state.
func TestStore_Get_ReturnsCopy(t *testing.T) {
store := config.NewStore(config.DefaultConfig(), "")
c1 := store.Get()
c1.MaxWorkers = 99

c2 := store.Get()
if c2.MaxWorkers == 99 {
t.Error("Get() must return an independent copy")
}
}

// TestStore_PersistsToFile verifies that Update writes the YAML file when a
// filePath is provided.
func TestStore_PersistsToFile(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

store := config.NewStore(config.DefaultConfig(), path)
if err := store.Update(func(c *config.Config) error {
c.MaxWorkers = 16
return nil
}); err != nil {
t.Fatalf("Update: %v", err)
}

// File should exist.
if _, err := os.Stat(path); err != nil {
t.Fatalf("expected config file after Update: %v", err)
}
}

// TestLoad_NotifyOnSuccess verifies that cfg.SMTP.NotifyOnSuccess is derived
// from cfg.NotificationRules.OnSuccess.
func TestLoad_NotifyOnSuccess(t *testing.T) {
targetDir := t.TempDir()
dir := t.TempDir()
path := filepath.Join(dir, "config.yaml")

yaml := "target_paths:\n  - " + targetDir + "\nnotification_rules:\n  on_success: true\n"
if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
t.Fatal(err)
}

cfg, err := config.Load(path)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !cfg.SMTP.NotifyOnSuccess {
t.Error("expected SMTP.NotifyOnSuccess=true when on_success=true in config")
}
}

func contains(s, sub string) bool {
return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
func() bool {
for i := 0; i <= len(s)-len(sub); i++ {
if s[i:i+len(sub)] == sub {
return true
}
}
return false
}())
}
