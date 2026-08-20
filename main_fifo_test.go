package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupFIFORefusesToReplaceRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-fifo")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := setup_fifo(path); err == nil {
		t.Fatal("setup_fifo replaced a regular file")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "keep" {
		t.Fatalf("regular file was modified: %q", contents)
	}
}
