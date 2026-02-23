package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/settings"
)

func TestDefaultSettings_Values(t *testing.T) {
	d := settings.DefaultSettings()
	if d.DiskThresholds.WarnPercent != 75 {
		t.Errorf("expected WarnPercent=75, got %d", d.DiskThresholds.WarnPercent)
	}
	if d.DiskThresholds.ErrorPercent != 90 {
		t.Errorf("expected ErrorPercent=90, got %d", d.DiskThresholds.ErrorPercent)
	}
	if d.NotificationRules.OnSuccess {
		t.Error("expected OnSuccess=false by default")
	}
	if !d.NotificationRules.OnCorruption {
		t.Error("expected OnCorruption=true by default")
	}
}

func TestStore_NewInMemory_DefaultValues(t *testing.T) {
	s := settings.New("") // no file path → in-memory defaults
	got := s.Get()
	want := settings.DefaultSettings()
	if got.DiskThresholds != want.DiskThresholds {
		t.Errorf("expected default thresholds, got %+v", got.DiskThresholds)
	}
}

func TestStore_Set_Get(t *testing.T) {
	s := settings.New("")
	updated := settings.Settings{
		DiskThresholds: settings.DiskThresholds{WarnPercent: 60, ErrorPercent: 85},
		NotificationRules: settings.NotificationRules{
			OnSuccess:    true,
			OnWarning:    false,
			OnCorruption: true,
			OnError:      false,
		},
	}
	if err := s.Set(updated); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := s.Get()
	if got.DiskThresholds.WarnPercent != 60 {
		t.Errorf("expected WarnPercent=60, got %d", got.DiskThresholds.WarnPercent)
	}
	if !got.NotificationRules.OnSuccess {
		t.Error("expected OnSuccess=true after Set")
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Create store, set values, they are persisted to disk.
	s := settings.New(path)
	if err := s.Set(settings.Settings{
		DiskThresholds: settings.DiskThresholds{WarnPercent: 55, ErrorPercent: 80},
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A second store loading the same file should read back the persisted values.
	s2 := settings.New(path)
	got := s2.Get()
	if got.DiskThresholds.WarnPercent != 55 {
		t.Errorf("expected persisted WarnPercent=55, got %d", got.DiskThresholds.WarnPercent)
	}
}

func TestStore_LoadMissingFile_UsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	s := settings.New(path)
	got := s.Get()
	if got.DiskThresholds.WarnPercent != 75 {
		t.Errorf("expected default WarnPercent=75 when file missing, got %d", got.DiskThresholds.WarnPercent)
	}
}

func TestStore_LoadCorruptFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not valid json {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := settings.New(path)
	got := s.Get()
	if got.DiskThresholds.WarnPercent != 75 {
		t.Errorf("expected default WarnPercent=75 on corrupt file, got %d", got.DiskThresholds.WarnPercent)
	}
}

func TestStore_Get_ReturnsCopy(t *testing.T) {
	s := settings.New("")
	copy1 := s.Get()
	copy1.DiskThresholds.WarnPercent = 1

	copy2 := s.Get()
	if copy2.DiskThresholds.WarnPercent == 1 {
		t.Error("Get() should return an independent copy, not a shared reference")
	}
}
