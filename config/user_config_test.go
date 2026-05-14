package config

import (
	"os"
	"path/filepath"
	"testing"

	"git.stormux.org/storm/barnard/uiterm"
)

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
}
