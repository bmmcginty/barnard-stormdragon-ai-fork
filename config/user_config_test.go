package config

import (
	"os"
	"path/filepath"
	"testing"

	"git.stormux.org/storm/barnard/uiterm"
)

// Regression: an explicit -config path silently fell back to in-memory
// defaults, then overwrote the intended file on exit.
func TestRequireConfigFileRejectsMissingExplicitPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	if err := RequireConfigFile(missing); err == nil {
		t.Fatal("missing explicit config was accepted")
	}
}

func TestRequireConfigFileRejectsNonRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.fifo")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := RequireConfigFile(path); err == nil {
		t.Fatal("directory was accepted as an explicit config file")
	}
}

func TestConfigBackfillsRecordingDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "barnard.toml")
	if err := os.WriteFile(configPath, []byte("[hotkeys]\ntalk = \"f1\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig(&configPath)

	if got := cfg.GetRecordingFormat(); got != "flac" {
		t.Fatalf("expected default recording format flac, got %q", got)
	}
	if got := cfg.GetRecordingDirectory(); got != filepath.Join(os.Getenv("HOME"), "Audio") {
		t.Fatalf("expected default recording directory ~/Audio, got %q", got)
	}
	if cfg.GetHotkeys().RecordToggle == nil {
		t.Fatal("expected record toggle hotkey to be backfilled")
	}
	if got := *cfg.GetHotkeys().RecordToggle; got != uiterm.KeyCtrlR {
		t.Fatalf("expected record toggle ctrl_r, got %s", got)
	}
	if cfg.GetHotkeys().AdminMenu == nil {
		t.Fatal("expected admin menu hotkey to be backfilled")
	}
	if got := *cfg.GetHotkeys().AdminMenu; got != uiterm.KeyF11 {
		t.Fatalf("expected admin menu f11, got %s", got)
	}
	for name, got := range map[string]*uiterm.Key{
		"clear output":     cfg.GetHotkeys().ClearOutput,
		"scroll to top":    cfg.GetHotkeys().ScrollToTop,
		"scroll to bottom": cfg.GetHotkeys().ScrollToBottom,
	} {
		if got == nil {
			t.Fatalf("expected %s hotkey to be backfilled", name)
		}
	}
	if got := *cfg.GetHotkeys().ClearOutput; got != uiterm.KeyCtrlL {
		t.Fatalf("expected clear output ctrl_l, got %s", got)
	}
	if got := *cfg.GetHotkeys().ScrollToTop; got != uiterm.KeyHome {
		t.Fatalf("expected scroll to top home, got %s", got)
	}
	if got := *cfg.GetHotkeys().ScrollToBottom; got != uiterm.KeyEnd {
		t.Fatalf("expected scroll to bottom end, got %s", got)
	}
}

func TestAGCDefaultsOnAndPersists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "barnard.toml")
	if err := os.WriteFile(configPath, []byte("[hotkeys]\ntalk = \"f1\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig(&configPath)
	if !cfg.GetAGCEnabled() {
		t.Fatal("expected AGC to default to enabled")
	}
	if cfg.GetHotkeys().AGCToggle == nil {
		t.Fatal("expected AGC toggle hotkey to be backfilled")
	}
	if got := *cfg.GetHotkeys().AGCToggle; got != uiterm.KeyF12 {
		t.Fatalf("expected AGC toggle f12, got %s", got)
	}

	if err := cfg.SetAGCEnabled(false); err != nil {
		t.Fatal(err)
	}
	reloaded := NewConfig(&configPath)
	if reloaded.GetAGCEnabled() {
		t.Fatal("expected disabled AGC setting to persist")
	}
	if got := *reloaded.GetHotkeys().AGCToggle; got != uiterm.KeyF12 {
		t.Fatalf("expected saved AGC toggle to reload as f12, got %s", got)
	}
}

// Regression: malformed and IPv6 addresses were split at every colon and
// could panic while merely reading a saved user preference.
func TestMakeHostPortHandlesIPv6AndMalformedAddress(t *testing.T) {
	host, port := makeHostPort("[2001:db8::1]:64739")
	if host != "2001:db8::1" || port != 64739 {
		t.Fatalf("got %q:%d", host, port)
	}
	host, port = makeHostPort("not-a-host-port")
	if host != "not-a-host-port" || port != 64738 {
		t.Fatalf("got %q:%d", host, port)
	}
}
func TestConfigUsesHomeEnvironmentForDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	configPath := "~/.barnard.toml"

	cfg := NewConfig(&configPath)
	cfg.SaveConfig()

	if _, err := os.Stat(filepath.Join(dir, ".barnard.toml")); err != nil {
		t.Fatalf("expected config to be written under HOME: %v", err)
	}
}
