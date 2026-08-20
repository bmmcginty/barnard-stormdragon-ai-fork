package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSaveConfigCreatesMissingParentDirectory(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing")
	path := filepath.Join(parent, "barnard.toml")
	cfg := NewConfig(&path)
	if err := cfg.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("parent directory permissions = %o, want no group or other access", info.Mode().Perm())
	}
}

func TestSaveConfigDoesNotUsePredictableTemporaryPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "barnard.toml")
	legacyTemp := path + ".tmp"
	if err := os.WriteFile(legacyTemp, []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig(&path)
	if err := cfg.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(legacyTemp)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "sentinel" {
		t.Fatalf("predictable temporary file was modified: %q", contents)
	}
}

func TestConcurrentConfigurationUpdatesAndWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "barnard.toml")
	cfg := NewConfig(&path)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(enabled bool) {
			defer wg.Done()
			cfg.SetNoiseSuppressionEnabled(enabled)
			if err := cfg.SaveConfig(); err != nil {
				t.Errorf("SaveConfig: %v", err)
			}
		}(i%2 == 0)
	}
	wg.Wait()
}
