package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression: tone transmission was started before opening its output file,
// so a creation error left audio transmission running without cleanup.
// Regression: tone test transmitted immediately, preventing the configured
// talk key from controlling it.
func TestToneTestRequiresAutoTransmit(t *testing.T) {
	if (&Barnard{ToneTest: true}).toneTestAutoTransmit() {
		t.Fatal("tone test started without auto-transmit")
	}
	if !(&Barnard{ToneTest: true, AutoTransmit: true}).toneTestAutoTransmit() {
		t.Fatal("tone test did not auto-transmit")
	}
}

func TestNewAudioFileSaverReportsUnavailableOutputPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "tone.pcm")
	if saver, err := NewAudioFileSaver(path); err == nil || saver != nil {
		t.Fatalf("got saver=%v err=%v", saver, err)
	}
}

func TestNewAudioFileSaverDoesNotOverwriteExistingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.pcm")
	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if saver, err := NewAudioFileSaver(path); err == nil || saver != nil {
		t.Fatalf("got saver=%v err=%v", saver, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "existing" {
		t.Fatalf("output was overwritten: %q", contents)
	}
}

type testDetacher struct{ detached bool }

func (d *testDetacher) Detach() { d.detached = true }

// Regression: reconnecting tone-test mode kept prior savers attached to the
// shared audio listener list, causing callbacks to write to closed files.
func TestCleanupToneTestAudioDetachesSaver(t *testing.T) {
	saver, err := NewAudioFileSaver(filepath.Join(t.TempDir(), "tone.pcm"))
	if err != nil {
		t.Fatal(err)
	}
	detacher := &testDetacher{}
	b := &Barnard{toneTestSaver: saver, toneTestSaverDetach: detacher}

	b.cleanupToneTestAudio()
	if !detacher.detached {
		t.Fatal("tone saver listener was not detached")
	}
	if b.toneTestSaver != nil || b.toneTestSaverDetach != nil {
		t.Fatal("tone saver cleanup retained connection state")
	}
	b.cleanupToneTestAudio()
}
